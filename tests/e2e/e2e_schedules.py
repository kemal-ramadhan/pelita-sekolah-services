import json, urllib.request, urllib.error, subprocess, os, struct, zlib, uuid, time, re, zipfile, io, sys

BASE = "http://127.0.0.1:18888"
SVC = "/Users/admin/PROJECT/EduPilot/services"
TMP = "tests/e2e"
COMPOSE = ["docker", "compose", "-p", "edupilot-e2e", "-f", SVC + "/docker-compose.yml", "-f", SVC + "/tests/e2e/override.e2e.yml"]
env = open(SVC + "/edupilot-auth-service/.env").read()
ADMIN_PW = re.search(r'^SEED_ADMIN_PASSWORD=(.*)$', env, re.M).group(1).strip().strip('"')
results = []
S = "/instantion/school"


def req(method, path, body=None, token=None, raw=None, ctype=None, want_raw=False):
    data = None; headers = {}
    if body is not None:
        data = json.dumps(body).encode(); headers["Content-Type"] = "application/json"
    if raw is not None:
        data = raw; headers["Content-Type"] = ctype
    if token: headers["Authorization"] = "Bearer " + token
    for attempt in range(40):
        r = urllib.request.Request(BASE + path, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(r, timeout=60) as resp:
                blob = resp.read(); code = resp.status; hdrs = dict(resp.headers)
        except urllib.error.HTTPError as e:
            blob = e.read(); code = e.code; hdrs = dict(e.headers)
        if code == 429 and b"RATE" in blob.upper() or (code == 429 and "X-Ratelimit-Reset" in hdrs and "invite" not in path):
            wait = int(hdrs.get("X-Ratelimit-Reset", "10") or 10) + 1
            print("   (rate limited, sleeping %ds)" % wait); sys.stdout.flush()
            time.sleep(wait)
            continue
        break
    if want_raw:
        return code, blob, hdrs
    try: js = json.loads(blob.decode() or "{}")
    except Exception: js = {"_raw": blob[:200]}
    return code, js


def check(name, cond, info=""):
    results.append((name, bool(cond)))
    print(("PASS" if cond else "FAIL"), name, "" if cond else str(info)[:600])
    sys.stdout.flush()


def multipart(fields, files):
    b = "----e2e" + uuid.uuid4().hex
    out = b""
    for k, v in fields.items():
        out += f"--{b}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n".encode()
    for (field, filename, content, ctype) in files:
        out += (f"--{b}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{filename}\"\r\nContent-Type: {ctype}\r\n\r\n").encode() + content + b"\r\n"
    out += f"--{b}--\r\n".encode()
    return out, "multipart/form-data; boundary=" + b


def png():
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(b"\x00\xff\x00\x00")) + chunk(b"IEND", b"")


def psql(sql, db="edupilot_instantion_db"):
    r = subprocess.run(COMPOSE + ["exec", "-T", "db_auth", "psql", "-U", "postgres", "-d", db, "-tAc", sql], capture_output=True, text=True)
    return r.stdout.strip()



sfx = uuid.uuid4().hex[:6]
c, j = req("POST", "/auth/login", {"email": "admin@edupilot.local", "password": ADMIN_PW})
check("admin login", c == 200, (c, j)); admin = j.get("access_token")


def mk_school(code, level, email):
    c, j = req("POST", "/instantion/admin/schools", {"code": code, "name": "Sekolah " + code, "school_level": level,
               "account": {"email": email, "password": "SchoolPass123", "full_name": "Operator " + code}}, admin)
    check(f"admin creates school {code}", c == 201, (c, j))
    c, j = req("POST", "/auth/login", {"email": email, "password": "SchoolPass123"})
    return j.get("access_token")


def mk_teacher(tok, name, code):
    c, j = req("POST", S + "/teachers", {"full_name": name, "employee_code": code, "send_invite": False}, tok)
    check(f"teacher create {name} (no invite)", c == 201 and j["data"]["account_status"] == "none", (c, j))
    return j.get("data", {}).get("id")


def setup_school(tok, preset, year):
    c, j = req("POST", S + "/academic-years", {"name": year, "start_date": "2025-07-14", "end_date": "2026-06-30"}, tok)
    check(f"year {year} active with Ganjil/Genap", c == 201 and j["data"]["is_active"], (c, j))
    yr = j.get("data", {})
    c, j = req("POST", S + "/grade-levels/preset", {"preset": preset, "numbering": "arabic"}, tok)
    check(f"grade preset {preset}", c == 200, (c, j))
    grades = {g["number"]: g["id"] for g in j.get("data", [])}
    return yr, grades


st = mk_school("SCH-" + sfx, "SMP", f"sch-{sfx}@e2e.test")
st2 = mk_school("SCB-" + sfx, "SD", f"scb-{sfx}@e2e.test")

# ---------- smoke earlier flows ----------
body, ct = multipart({}, [("logo", "logo.png", png(), "image/png")])
c, j = req("PUT", S + "/profile/logo", raw=body, ctype=ct, token=st)
check("smoke: logo upload", c == 200 and j["data"]["logo"].startswith("data:image/png"), (c, j))
T1 = mk_teacher(st, "Guru Satu", "T1-" + sfx)
T2 = mk_teacher(st, "Guru Dua", "T2-" + sfx)
T3 = mk_teacher(st, "Guru Tiga", "T3-" + sfx)
yr, grades = setup_school(st, "SMP", "2025/2026")
y1id = yr["id"]; ganjil, genap = yr["semesters"][0]["id"], yr["semesters"][1]["id"]
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": y1id, "grade_level_id": grades[7], "names": ["7A", "7B", "7Z"]}, st)
check("classes 7A/7B/7Z", c == 201 and j["data"]["created"] == 3, (c, j))
c, j = req("GET", S + "/classes?all=true", token=st)
cls = {x["name"]: x["id"] for x in j.get("data", [])}
c7a, c7b, c7z = cls["7A"], cls["7B"], cls["7Z"]
base = {"identity": {"full_name": "Siswa Satu", "gender": "L", "birth_place": "Bogor", "birth_date": "2012-01-01", "religion": "islam"}}
c, j = req("POST", S + "/students", base, st)
check("smoke: student create", c == 201, (c, j)); sid = j.get("data", {}).get("id")
c, j = req("POST", S + f"/students/{sid}/placement", {"class_id": c7a}, st)
check("smoke: student placement 7A", c == 200, (c, j))

yrB, gradesB = setup_school(st2, "SD", "2025/2026")
c, j = req("POST", S + "/classes", {"academic_year_id": yrB["id"], "grade_level_id": gradesB[1], "name": "1A"}, st2)
cB = j.get("data", {}).get("id")
TB = mk_teacher(st2, "Guru B", "TB-" + sfx)

# ---------- subjects ----------
c, j = req("POST", S + "/subjects/preset", {"preset": "SMP"}, st)
codes = {x["subject_code"]: x["id"] for x in j.get("data", [])}
check("subject preset SMP (PAI, IPA, IPS, INF, MULOK)", c == 200 and {"PAI", "IPA", "IPS", "INF", "MULOK", "MAT"} <= set(codes), (c, j))
c, j = req("POST", S + "/subjects/preset", {"preset": "SMP"}, st)
check("subject preset again -> idempotent []", c == 200 and j["data"] == [], (c, j))
c, j = req("POST", S + "/subjects/preset", {"preset": "XYZ"}, st)
check("subject preset unknown -> 400", c == 400 and j.get("code") == "VALIDATION_ERROR", (c, j))
c, j = req("POST", S + "/subjects/preset", {"preset": "SD"}, st2)
check("school B preset SD has IPAS, no IPA", c == 200 and "IPAS" in [x["subject_code"] for x in j["data"]] and "IPA" not in [x["subject_code"] for x in j["data"]], (c, j))
subB = j["data"][0]["id"]
MAT, IPA, BIN, PAI = codes["MAT"], codes["IPA"], codes["BIN"], codes["PAI"]
c, j = req("GET", S + "/subjects?limit=500", token=st)
check("subjects list: limit capped 100, total", c == 200 and j["meta"]["limit"] == 100 and j["meta"]["total"] == len(codes), (c, j.get("meta")))
c, j = req("GET", S + "/subjects?all=true&search=mat", token=st)
check("subjects all=true + search", c == 200 and j["meta"] is None and any(x["subject_code"] == "MAT" for x in j["data"]), (c, j))
c, j = req("GET", S + "/subjects?search=%25", token=st)
check("subjects search '%' is literal", c == 200 and j["meta"]["total"] == 0, (c, j.get("meta")))
c, j = req("POST", S + "/subjects", {"subject_code": "tik", "name": "TIK Lanjut", "subject_type": "peminatan", "grade_level_id": grades[8]}, st)
check("subject create with grade level", c == 201 and j["data"]["grade_level"]["id"] == grades[8], (c, j)); TIK = j.get("data", {}).get("id")
c, j = req("POST", S + "/subjects", {"subject_code": "TIK", "name": "Other"}, st)
check("subject duplicate code (case-insensitive) -> 409 DUPLICATE_CODE", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))
c, j = req("POST", S + "/subjects", {"subject_code": "MAT2", "name": "matematika"}, st)
check("subject duplicate name -> 409 DUPLICATE_NAME", c == 409 and j.get("code") == "DUPLICATE_NAME", (c, j))
c, j = req("POST", S + "/subjects", {"subject_code": "X1", "name": "X", "subject_type": "agama"}, st)
check("subject bad type -> 400", c == 400 and "subject_type" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/subjects", {"subject_code": "X1", "name": "X", "grade_level_id": gradesB[1]}, st)
check("subject foreign grade level -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("PUT", S + f"/subjects/{TIK}", {"subject_code": "TIK", "name": "TIK Dasar", "short_name": "TIK"}, st)
check("subject update", c == 200 and j["data"]["name"] == "TIK Dasar" and j["data"].get("grade_level_id") is None if c == 200 else False, (c, j))
c, j = req("PUT", S + f"/subjects/{TIK}", {"subject_code": "MAT", "name": "TIK Dasar"}, st)
check("subject update to used code -> 409", c == 409, (c, j))
c, j = req("PUT", S + f"/subjects/{uuid.uuid4()}", {"subject_code": "Z", "name": "Z"}, st)
check("subject update unknown -> 404", c == 404, (c, j))

# ---------- teacher <-> subject ----------
c, j = req("POST", S + f"/subjects/{MAT}/teachers", {"teacher_ids": [T1, T2, T2]}, st)
check("assign teachers to MAT", c == 200, (c, j))
c, j = req("GET", S + f"/subjects/{MAT}/teachers", token=st)
check("MAT teachers = T1,T2", c == 200 and sorted(x["teacher_id"] for x in j["data"]) == sorted([T1, T2]), (c, j))
c, j = req("POST", S + f"/teachers/{T1}/subjects", {"subject_ids": [MAT, IPA]}, st)
check("assign subjects to T1", c == 200, (c, j))
c, j = req("GET", S + f"/teachers/{T1}/subjects", token=st)
check("T1 subjects = MAT, IPA", c == 200 and sorted(x["id"] for x in j["data"]) == sorted([MAT, IPA]), (c, j))
c, j = req("POST", S + f"/subjects/{MAT}/teachers", {"teacher_ids": [T2]}, st)
c, j = req("GET", S + f"/teachers/{T1}/subjects", token=st)
check("replace MAT teachers -> T1 keeps only IPA", [x["id"] for x in j["data"]] == [IPA], (c, j))
c, j = req("POST", S + f"/subjects/{MAT}/teachers", {"teacher_ids": [TB]}, st)
check("assign foreign teacher -> 400 INVALID_REFERENCE (nothing changed)", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("GET", S + f"/subjects/{MAT}/teachers", token=st)
check("  MAT teachers still T2", [x["teacher_id"] for x in j["data"]] == [T2], j)
c, j = req("POST", S + f"/teachers/{T2}/subjects", {"subject_ids": [MAT, subB]}, st)
check("assign foreign subject to teacher -> 400", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("GET", S + f"/subjects/{MAT}", token=st)
check("subject detail teachers/classes arrays", c == 200 and j["data"]["teacher_count"] == 1 and j["data"]["classes"] == [], (c, j))

# ---------- class subjects ----------
c, j = req("POST", S + f"/classes/{c7a}/subjects", {"subject_id": MAT, "primary_teacher_id": T2, "hours_per_week": 2}, st)
check("class 7A + MAT (2 JP, T2)", c == 201 and j["data"]["primary_teacher"]["employee_id"] == T2 and j["data"]["academic_year_id"] == y1id, (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects", {"subject_id": MAT}, st)
check("class subject duplicate -> 409 DUPLICATE", c == 409 and j.get("code") == "DUPLICATE", (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects", {"subject_id": IPA, "primary_teacher_id": TB}, st)
check("class subject foreign teacher -> 400", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects", {"subject_id": IPA, "hours_per_week": 100}, st)
check("class subject hours 100 -> 400", c == 400, (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects/bulk", {"subject_ids": [MAT, IPA, BIN, IPA], "hours_per_week": 4}, st)
check("bulk assign (MAT exists) -> assigned 2", c == 200 and j["data"]["assigned_count"] == 2, (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects/bulk", {"subject_ids": [MAT, IPA, BIN]}, st)
check("bulk again idempotent -> 0", c == 200 and j["data"]["assigned_count"] == 0, (c, j))
c, j = req("POST", S + f"/classes/{c7a}/subjects/bulk", {"subject_ids": [subB]}, st)
check("bulk with foreign subject -> 400", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("GET", S + f"/classes/{c7a}/subjects", token=st)
check("7A subjects = 3", c == 200 and len(j["data"]) == 3, (c, j))
c, j = req("DELETE", S + f"/classes/{c7a}/subjects/{BIN}", token=st)
check("remove class subject by subject_id", c == 200, (c, j))
c, j = req("DELETE", S + f"/classes/{c7a}/subjects/{BIN}", token=st)
check("remove again -> 404", c == 404, (c, j))
c, j = req("POST", S + f"/classes/{c7b}/subjects", {"subject_id": BIN}, st)
rowB = j.get("data", {}).get("id")
c, j = req("DELETE", S + f"/classes/{c7a}/subjects/{rowB}", token=st)
check("remove 7B's class-subject row via 7A -> 404", c == 404, (c, j))
c, j = req("DELETE", S + f"/classes/{c7b}/subjects/{rowB}", token=st2)
check("remove class-subject row from other tenant -> 404", c == 404, (c, j))
c, j = req("DELETE", S + f"/classes/{c7b}/subjects/{rowB}", token=st)
check("remove class subject by row id", c == 200, (c, j))
c, j = req("POST", S + f"/subjects/{IPA}/teachers", {"teacher_ids": [T1], "is_primary": False}, st)
c2, j2 = req("GET", S + f"/subjects/{IPA}/teachers", token=st)
check("assign with is_primary=false stored as \"false\"", c == 200 and j2["data"][0]["is_primary"] == "false", (c, j, j2))
c, j = req("POST", S + f"/teachers/{T1}/subjects", {"subject_ids": [IPA], "is_primary": "true"}, st)
c2, j2 = req("GET", S + f"/subjects/{IPA}/teachers", token=st)
check("assign with is_primary=\"true\" (string) updates kept row", c == 200 and j2["data"][0]["is_primary"] == "true", (c, j, j2))
c, j = req("GET", S + f"/subjects/{MAT}", token=st)
check("MAT detail classes include 7A with year name", c == 200 and j["data"]["classes"][0]["display_name"] == "7A" and j["data"]["classes"][0]["academic_year_name"] == "2025/2026", (c, j))

# ---------- periods ----------
c, j = req("POST", S + "/schedule-periods/preset", {"preset": "40_MIN"}, st)
pr = j.get("data", [])
check("period preset 40_MIN -> 12 slots, 9 JP, contiguous", c == 200 and len(pr) == 12 and sum(1 for p in pr if not p["is_break"]) == 9
      and all(pr[i]["end_time"] == pr[i + 1]["start_time"] for i in range(len(pr) - 1)), (c, j))
P = {p["name"]: p["id"] for p in pr}
J1, J2, J3, J4 = P["Jam ke-1"], P["Jam ke-2"], P["Jam ke-3"], P["Jam ke-4"]
BRK = [p["id"] for p in pr if p["is_break"]][1]
c, j = req("POST", S + "/schedule-periods/preset", {"preset": "90_MIN"}, st)
check("period preset unknown -> 400", c == 400, (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Overlap", "period_order": 50, "start_time": "07:20", "end_time": "07:50"}, st)
check("period overlap -> 409 PERIOD_OVERLAP", c == 409 and j.get("code") == "PERIOD_OVERLAP", (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Dup order", "period_order": 1, "start_time": "16:00", "end_time": "16:40"}, st)
check("period duplicate order -> 409 DUPLICATE_ORDER", c == 409 and j.get("code") == "DUPLICATE_ORDER", (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Bad", "period_order": 50, "start_time": "16:40", "end_time": "16:00"}, st)
check("period end<start -> 400", c == 400 and "end_time" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Bad", "period_order": 50, "start_time": "4pm", "end_time": "17:00"}, st)
check("period bad time -> 400", c == 400 and "start_time" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Jam ke-10", "period_order": 0, "start_time": "14:00", "end_time": "14:40"}, st)
check("period create with order 0 accepted", c == 201 and j["data"]["period_order"] == 0, (c, j)); P10 = j.get("data", {}).get("id")
c, j = req("PUT", S + f"/schedule-periods/{P10}", {"name": "Jam ke-10", "period_order": 0, "start_time": "13:30", "end_time": "14:10"}, st)
check("period update into overlap -> 409", c == 409 and j.get("code") == "PERIOD_OVERLAP", (c, j))
c, j = req("PUT", S + f"/schedule-periods/{P10}", {"name": "Jam ke-10", "period_order": 0, "start_time": "14:00", "end_time": "14:45"}, st)
check("period update ok", c == 200 and j["data"]["end_time"] == "14:45", (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Jumat 1", "period_order": 1, "day_of_week": 5, "start_time": "07:00", "end_time": "07:35"}, st)
check("friday-specific period may overlap every-day periods", c == 201, (c, j)); PF = j.get("data", {}).get("id")
c, j = req("DELETE", S + f"/schedule-periods/{PF}", token=st)
check("period delete", c == 200, (c, j))
c, j = req("DELETE", S + f"/schedule-periods/{PF}", token=st)
check("period delete again -> 404", c == 404, (c, j))

# ---------- schedules ----------
def sched(cls_id, sub, t, day, period=None, start=None, end=None, room=None, extra=None):
    b = {"class_id": cls_id, "subject_id": sub, "teacher_id": t, "day_of_week": day}
    if period: b["schedule_period_id"] = period
    if start: b["start_time"] = start
    if end: b["end_time"] = end
    if room: b["room"] = room
    if extra: b.update(extra)
    return b

c, j = req("POST", S + "/schedules", sched(c7a, MAT, T2, 1, J1), st)
d = j.get("data", {})
check("S1 create 7A MAT T2 Mon J1 (times from period, active semester, class_subject linked)",
      c == 201 and d["start_time"] == "07:15" and d["end_time"] == "07:55" and d["semester_id"] == ganjil and d["academic_year_id"] == y1id
      and d["class_subject_id"] and d["period"]["id"] == J1 and j.get("warnings") == [], (c, j))
S1 = d.get("id")
c, j = req("POST", S + "/schedules", sched(c7b, MAT, T2, 1, J1), st)
check("teacher double-booked -> 409 SCHEDULE_CONFLICT type teacher", c == 409 and j.get("code") == "SCHEDULE_CONFLICT" and j["details"]["type"] == "teacher", (c, j))
c, j = req("POST", S + "/schedules", sched(c7a, IPA, T1, 1, J1), st)
check("class double-booked -> 409 type class", c == 409 and j["details"]["type"] == "class", (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T2, 1, None, "07:30", "08:10"), st)
check("partial time overlap (explicit times) -> 409 teacher", c == 409 and j["details"]["type"] == "teacher", (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 1, J1), st)
check("S4 7B IPA T1 Mon J1 -> 201 with warning (IPA not assigned to 7B)", c == 201 and len(j.get("warnings", [])) == 1 and "belum ditetapkan" in j["warnings"][0], (c, j))
S4 = j.get("data", {}).get("id")
c, j = req("POST", S + "/schedules", sched(c7a, MAT, T2, 2, J1, room="Lab"), st)
check("S5 7A Tue J1 room Lab", c == 201 and j["data"]["room"] == "Lab", (c, j)); S5 = j.get("data", {}).get("id")
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 2, J1, room=" lab "), st)
check("room double-booked -> 409 type room", c == 409 and j["details"]["type"] == "room", (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 1, BRK), st)
check("break period -> 400", c == 400 and "schedule_period_id" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 8, None, "07:00", "07:40"), st)
check("day_of_week 8 -> 400", c == 400 and "day_of_week" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 3, None, "08:00", "07:40"), st)
check("end before start -> 400", c == 400, (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 3), st)
check("no times and no period -> 400", c == 400, (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, TB, 3, J1), st)
check("foreign teacher -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + "/schedules", sched(cB, IPA, T1, 3, J1), st)
check("foreign class -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, subB, T1, 3, J1), st)
check("foreign subject -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 3, J1, extra={"academic_year_id": yrB["id"]}), st)
check("foreign/mismatched academic year -> 400", c == 400, (c, j))
c, j = req("POST", S + "/schedules", sched(c7b, IPA, T1, 3, J1, extra={"semester_id": yrB["semesters"][0]["id"]}), st)
check("foreign semester -> 400", c == 400, (c, j))

c, j = req("POST", S + "/schedules/check-conflict", sched(c7z, MAT, T2, 1, J1), st)
check("check-conflict: teacher conflict reported", c == 200 and j["data"]["has_conflict"] and [x["type"] for x in j["data"]["conflicts"]] == ["teacher"], (c, j))
c, j = req("POST", S + f"/schedules/check-conflict?exclude_id={S1}", sched(c7z, MAT, T2, 1, J1), st)
check("check-conflict with exclude_id -> none + warnings array", c == 200 and not j["data"]["has_conflict"] and isinstance(j["data"]["warnings"], list), (c, j))
c, j = req("POST", S + "/schedules/check-conflict", sched(c7a, IPA, T2, 1, J1), st)
check("check-conflict: teacher+class both", c == 200 and sorted(x["type"] for x in j["data"]["conflicts"]) == ["class", "teacher"], (c, j))
c, j = req("POST", S + "/schedules/check-conflict?exclude_id=nope", sched(c7a, IPA, T2, 1, J1), st)
check("check-conflict bad exclude_id -> 400", c == 400, (c, j))

c, j = req("PUT", S + f"/schedules/{S1}", sched(c7a, MAT, T2, 1, J1, extra={"status": "scheduled"}), st)
check("update S1 unchanged (excludes self) -> 200", c == 200 and j["data"]["semester_id"] == ganjil, (c, j))
c, j = req("PUT", S + f"/schedules/{S1}", sched(c7a, MAT, T2, 1, J2), st)
check("update S1 -> Mon J2", c == 200 and j["data"]["start_time"] == "07:55", (c, j))
c, j = req("PUT", S + f"/schedules/{S4}", sched(c7b, IPA, T2, 1, J2), st)
check("update S4 into T2's slot -> 409 teacher", c == 409 and j["details"]["type"] == "teacher", (c, j))
c, j = req("PUT", S + f"/schedules/{uuid.uuid4()}", sched(c7b, IPA, T2, 1, J2), st)
check("update unknown -> 404", c == 404, (c, j))

c, j = req("POST", S + "/schedules", sched(c7a, PAI, T3, 3, J1), st)
check("unmapped teacher + subject not in class -> 201 with 2 warnings", c == 201 and len(j["warnings"]) == 2, (c, j))
S7 = j.get("data", {}).get("id")
c, j = req("POST", S + "/schedules", sched(c7a, MAT, T2, 4, J1, end="09:15"), st)
check("MAT 7A Thu J1-J3 (3 JP) -> JP over warning (4 > 2)", c == 201 and any("melebihi" in w for w in j["warnings"]), (c, j))
S8 = j.get("data", {}).get("id")

c, j = req("GET", S + f"/schedules/grid?class_id={c7a}", token=st)
g = j.get("data", {})
hs = {h["subject_id"]: h for h in g.get("hours_summary", [])}
check("grid class 7A: 6 days, arrays, names, periods, hours_summary",
      c == 200 and len(g["days"]) == 6 and all(isinstance(d["schedules"], list) for d in g["days"]) and g["class_name"] == "7A"
      and g["academic_year_name"] == "2025/2026" and g["semester_name"] == "Ganjil" and len(g["periods"]) == 13
      and hs[MAT]["scheduled_jp"] == 5 and hs[MAT]["status"] == "over" and hs[IPA]["status"] == "under", (c, j))
check("grid Mon has S1, Tue S5", [x["id"] for x in g["days"][0]["schedules"]] == [S1] and [x["id"] for x in g["days"][1]["schedules"]] == [S5], g.get("days"))
c, j = req("GET", S + f"/schedules/grid?teacher_id={T2}", token=st)
check("grid teacher T2", c == 200 and j["data"]["teacher_name"] == "Guru Dua" and sum(len(d["schedules"]) for d in j["data"]["days"]) == 3, (c, j))
c, j = req("GET", S + f"/schedules/grid?class_id={cB}", token=st)
check("grid foreign class -> 404", c == 404, (c, j))
c, j = req("GET", S + "/schedules?teacher_id=bad", token=st)
check("schedules bad teacher_id -> 400", c == 400, (c, j))
c, j = req("GET", S + f"/schedules?class_id={c7a}&day_of_week=1", token=st)
check("schedules list filter class+day", c == 200 and [x["id"] for x in j["data"]] == [S1], (c, j))

# semester scoping
c, j = req("POST", S + f"/semesters/{genap}/activate", token=st)
check("activate Genap", c == 200, (c, j))
c, j = req("GET", S + "/schedules", token=st)
check("list in Genap excludes Ganjil schedules", c == 200 and j["data"] == [], (c, j))
c, j = req("POST", S + "/schedules", sched(c7a, MAT, T2, 1, J2), st)
check("same slot in Genap -> 201 (other semester)", c == 201 and j["data"]["semester_id"] == genap, (c, j))
SG = j.get("data", {}).get("id")
c, j = req("GET", S + f"/schedules?semester_id={ganjil}", token=st)
check("list explicit semester Ganjil", c == 200 and len(j["data"]) == 5, (c, [x["id"] for x in j.get("data", [])]))
req("DELETE", S + f"/schedules/{SG}", token=st)
c, j = req("POST", S + f"/semesters/{ganjil}/activate", token=st)

# in-use guards
c, j = req("DELETE", S + f"/subjects/{MAT}", token=st)
check("delete subject in use -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("DELETE", S + f"/schedule-periods/{J1}", token=st)
check("delete period in use -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("POST", S + "/schedule-periods/preset", {"preset": "45_MIN"}, st)
check("period preset while used -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("PUT", S + f"/schedule-periods/{J2}", {"name": "Jam ke-2", "period_order": 3, "start_time": "07:55", "end_time": "08:35", "is_break": True}, st)
check("used period -> break -> 409 IN_USE", c == 409, (c, j))
c, j = req("DELETE", S + f"/classes/{c7a}/subjects/{MAT}", token=st)
check("remove class subject with schedules -> 409 HAS_SCHEDULES", c == 409 and j.get("code") == "HAS_SCHEDULES", (c, j))
c, j = req("DELETE", S + f"/subjects/{TIK}", token=st)
check("delete unused subject", c == 200, (c, j))
c, j = req("DELETE", S + f"/subjects/{TIK}", token=st)
check("delete subject again -> 404", c == 404, (c, j))

# teacher form keeps timetable data
c, j = req("GET", S + f"/teachers/{T2}", token=st)
trows = j["data"]["schedules"]; tsubs = j["data"]["subjects"]
payload = {"full_name": "Guru Dua", "employee_code": "T2-" + sfx,
           "subjects": [{"subject_id": s["subject_id"], "academic_id": s.get("academic_id") or "", "is_primary": True} for s in tsubs],
           "schedules": [{"subject_id": s["subject_id"], "class_id": s["class_id"], "day_of_week": s["day_of_week"], "start_time": s["start_time"], "end_time": s["end_time"]} for s in trows]}
c, j = req("PUT", S + f"/teachers/{T2}", payload, st)
check("teacher form PUT echoing schedules + subjects without academic_id -> 200", c == 200, (c, j))
c, j = req("GET", S + f"/schedules?teacher_id={T2}", token=st)
r5 = [x for x in j["data"] if x["id"] == S5]
check("  timetable rows kept (same ids, room/period intact)", len(j["data"]) == 3 and r5 and r5[0]["room"] == "Lab" and r5[0]["schedule_period_id"] == J1, (c, j))
payload["schedules"].append({"subject_id": IPA, "class_id": c7b, "day_of_week": 1, "start_time": "07:15", "end_time": "07:55"})
c, j = req("PUT", S + f"/teachers/{T2}", payload, st)
check("teacher form adding class-conflicting schedule -> 409", c == 409 and j.get("code") == "SCHEDULE_CONFLICT", (c, j))
payload["schedules"][-1]["day_of_week"] = 5
c, j = req("PUT", S + f"/teachers/{T2}", payload, st)
check("teacher form adding free slot -> 200", c == 200, (c, j))
c, j = req("GET", S + f"/schedules?teacher_id={T2}&day_of_week=5", token=st)
check("  new form row gets year+semester", c == 200 and len(j["data"]) == 1 and j["data"][0]["semester_id"] == ganjil and j["data"][0]["academic_year_id"] == y1id, (c, j))

# delete schedule
c, j = req("DELETE", S + f"/schedules/{S7}", token=st)
check("delete schedule", c == 200, (c, j))
c, j = req("DELETE", S + f"/schedules/{S7}", token=st)
check("delete schedule again -> 404", c == 404, (c, j))

# class delete cascades
req("POST", S + f"/classes/{c7z}/subjects", {"subject_id": IPA}, st)
c, j = req("POST", S + "/schedules", sched(c7z, IPA, T1, 6, J1), st)
check("7Z schedule", c == 201, (c, j))
c, j = req("DELETE", S + f"/classes/{c7z}", token=st)
check("delete class 7Z", c in (200, 204), (c, j))
check("  7Z schedules + class_subjects removed", psql(f"select count(*) from teacher_schedules where class_id='{c7z}'") == "0"
      and psql(f"select count(*) from class_subjects where class_id='{c7z}' and deleted_at is null") == "0", "")

# ---------- tenant isolation (school B) ----------
for m, p, b in [("GET", f"/subjects/{MAT}", None), ("GET", f"/subjects/{MAT}/teachers", None), ("GET", f"/teachers/{T1}/subjects", None),
                ("GET", f"/classes/{c7a}/subjects", None), ("POST", f"/classes/{c7a}/subjects", {"subject_id": subB}),
                ("POST", f"/classes/{c7a}/subjects/bulk", {"subject_ids": [subB]}), ("DELETE", f"/classes/{c7a}/subjects/{IPA}", None),
                ("PUT", f"/subjects/{MAT}", {"subject_code": "H", "name": "H"}), ("DELETE", f"/subjects/{MAT}", None),
                ("POST", f"/subjects/{MAT}/teachers", {"teacher_ids": [TB]}), ("POST", f"/teachers/{T1}/subjects", {"subject_ids": [subB]}),
                ("PUT", f"/schedules/{S1}", sched(cB, subB, TB, 1, None, "07:00", "07:35")), ("DELETE", f"/schedules/{S1}", None),
                ("PUT", f"/schedule-periods/{J1}", {"name": "x", "period_order": 1, "start_time": "07:00", "end_time": "07:30"}),
                ("DELETE", f"/schedule-periods/{J1}", None), ("GET", f"/schedules/grid?class_id={c7a}", None),
                ("GET", f"/schedules/grid?teacher_id={T2}", None), ("GET", f"/schedules?academic_year_id={y1id}", None)]:
    c, j = req(m, S + p, b, st2)
    check(f"tenant B {m} {p.split('?')[0].replace(MAT,'<A-subj>').replace(c7a,'<A-class>').replace(T1,'<A-T1>').replace(T2,'<A-T2>')} -> 404", c == 404, (c, j))
c, j = req("POST", S + "/schedules", sched(c7a, subB, TB, 1, None, "07:00", "07:35"), st2)
check("tenant B schedule on A class -> 400", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + "/schedules/check-conflict", sched(c7a, MAT, T2, 1, J1), st2)
check("tenant B check-conflict with A ids -> 400 (no leak)", c == 400, (c, j))
c, j = req("GET", S + "/schedules", token=st2)
check("tenant B schedules empty", c == 200 and j["data"] == [], (c, j))
c, j = req("POST", S + "/schedule-periods/preset", {"preset": "SD"}, st2)
check("tenant B period preset SD -> 35-min JP", c == 200 and [p for p in j["data"] if not p["is_break"]][0]["end_time"] == "07:50", (c, j))

# ---------- teacher role ----------
temail = f"tt-{sfx}@e2e.test"
c, j = req("POST", "/auth/admin/users", {"email": temail, "password": "TeacherPass123", "full_name": "Guru Dua", "role": "institution"}, admin)
tuid = j.get("data", {}).get("id")
psql(f"update users set role='teacher', email_verified_at=now() where id={tuid}", db="edupilot_auth_db")
psql(f"update employees set user_id={tuid} where id='{T2}'")
c, j = req("POST", "/auth/login", {"email": temail, "password": "TeacherPass123"})
tt = j.get("access_token")
check("teacher (T2) login", c == 200 and j["user"]["role"] == "teacher", (c, j))
c, j = req("GET", S + f"/teachers/{T2}/subjects", token=tt)
check("teacher: own subjects 200", c == 200 and [x["id"] for x in j["data"]] == [MAT], (c, j))
c, j = req("GET", S + f"/teachers/{T1}/subjects", token=tt)
check("teacher: other teacher's subjects 403", c == 403 and j.get("code") == "FORBIDDEN_ROLE", (c, j))
c, j = req("GET", S + "/me/schedule", token=tt)
own = [x["teacher_id"] for d in j.get("data", {}).get("days", []) for x in d["schedules"]]
check("teacher: GET /me/schedule own grid", c == 200 and j["data"]["teacher_id"] == T2 and len(own) == 4 and set(own) == {T2}, (c, j))
c, j = req("GET", S + "/schedules", token=tt)
check("teacher: GET /schedules forced to own", c == 200 and len(j["data"]) == 4 and {x["teacher_id"] for x in j["data"]} == {T2}, (c, j))
c, j = req("GET", S + f"/schedules?teacher_id={T1}", token=tt)
check("teacher: GET /schedules?teacher_id=other -> 403", c == 403, (c, j))
c, j = req("GET", S + f"/schedules/grid?class_id={c7b}", token=tt)
check("teacher: class grid shows only own rows", c == 200 and all(x["teacher_id"] == T2 for d in j["data"]["days"] for x in d["schedules"]), (c, j))
c, j = req("GET", S + "/subjects", token=tt)
check("teacher: GET /subjects 200", c == 200, c)
c, j = req("GET", S + "/schedule-periods", token=tt)
check("teacher: GET /schedule-periods 200", c == 200, c)
for m, p in [("POST", "/schedules"), ("PUT", f"/schedules/{S1}"), ("DELETE", f"/schedules/{S1}"), ("POST", "/schedules/check-conflict"),
             ("POST", "/subjects"), ("PUT", f"/subjects/{MAT}"), ("DELETE", f"/subjects/{MAT}"), ("POST", "/subjects/preset"),
             ("POST", f"/teachers/{T2}/subjects"), ("POST", f"/subjects/{MAT}/teachers"), ("GET", f"/subjects/{MAT}/teachers"),
             ("POST", "/schedule-periods"), ("POST", "/schedule-periods/preset"), ("DELETE", f"/schedule-periods/{J1}"),
             ("GET", f"/classes/{c7a}/subjects"), ("POST", f"/classes/{c7a}/subjects/bulk"), ("GET", f"/teachers/{T2}")]:
    c, j = req(m, S + p, {} if m in ("POST", "PUT") else None, tt)
    check(f"teacher {m} {p.replace(S1,'<id>').replace(MAT,'<id>').replace(T2,'<self>').replace(c7a,'<id>').replace(J1,'<id>')} -> 403", c == 403 and j.get("code") == "FORBIDDEN_ROLE", (c, j))
c, j = req("GET", S + "/me/schedule", token=st)
check("institution GET /me/schedule -> 404", c == 404, (c, j))
c, j = req("GET", S + "/me", token=tt)
check("teacher GET /me still has schedules", c == 200 and len(j["data"]["schedules"]) == 4, (c, j))

# ---------- restart (migrations idempotent) ----------
r = subprocess.run(COMPOSE + ["restart", "instantion-service"], capture_output=True, text=True)
ok = False
for i in range(60):
    try:
        c, j = req("GET", S + "/subjects?all=true", token=st)
        if c == 200:
            ok = True; break
    except Exception:
        pass
    time.sleep(1)
logs = subprocess.run(["docker", "logs", "--since", "90s", "edupilot-e2e-instantion-service-1"], capture_output=True, text=True)
lg = logs.stdout + logs.stderr
check("restart instantion-service: healthy, migrations clean", ok and "Migration failed" not in lg and "Post-migration failed" not in lg and "Pre-migration failed" not in lg, lg[-1500:])
check("  no skipped-index warnings", "duplicate groups violate" not in lg, [l for l in lg.splitlines() if "violate" in l])
idx = psql("select string_agg(indexname, ',' order by indexname) from pg_indexes where indexname in ('idx_subjects_school_code_unique','idx_subjects_school_name_level_unique','idx_class_subjects_unique','idx_teacher_schedules_school_day')")
check("  guarded/lookup indexes exist", idx.count("idx_") == 4, idx)
c, j = req("GET", S + f"/schedules/grid?class_id={c7a}", token=st)
check("  grid still works after restart", c == 200, c)

print("\n%d/%d passed" % (sum(1 for _, ok in results if ok), len(results)))
fails = [n for n, ok in results if not ok]
if fails:
    print("FAILED:", fails)
