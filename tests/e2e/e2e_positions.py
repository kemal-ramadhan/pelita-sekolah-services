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


sfx = uuid.uuid4().hex[:6]
st = mk_school("POS-" + sfx, "SMP", f"pos-{sfx}@e2e.test")
st2 = mk_school("POB-" + sfx, "SD", f"pob-{sfx}@e2e.test")

# ---------- positions ----------
c, j = req("GET", S + "/employee-positions", token=st)
pos = j.get("data") or []
check("positions seeded on first GET (20)", c == 200 and len(pos) == 20, (c, j))
cats = []
for p in pos:
    if p["category"] not in cats: cats.append(p["category"])
check("ordered by category Pimpinan, Pendidik, Tenaga Kependidikan, Koordinator", cats == ["Pimpinan", "Pendidik", "Tenaga Kependidikan", "Koordinator"], cats)
check("assignment_count present", all(p["assignment_count"] == 0 for p in pos), pos[:2])
c, j = req("GET", S + "/employee-positions", token=st)
check("second GET idempotent (still 20)", c == 200 and len(j["data"]) == 20, len(j.get("data") or []))
# race-safe: parallel first GETs on school B
import threading
outs = []
ths = [threading.Thread(target=lambda: outs.append(req("GET", S + "/employee-positions", token=st2))) for _ in range(5)]
[t.start() for t in ths]; [t.join() for t in ths]
c, j = req("GET", S + "/employee-positions", token=st2)
check("parallel first GETs seed once (20)", all(o[0] == 200 for o in outs) and len(j["data"]) == 20, [o[0] for o in outs] + [len(j["data"])])
WALAS = next(p["id"] for p in pos if p["position"] == "Wali Kelas")
KS = next(p["id"] for p in pos if p["position"] == "Kepala Sekolah")
WALAS_B = next(p["id"] for p in j["data"] if p["position"] == "Wali Kelas")

c, j = req("POST", S + "/employee-positions", {"position": "Koordinator Adiwiyata", "category": "koordinator"}, st)
check("create with auto code", c == 201 and j["data"]["position_code"] == "KA" and j["data"]["category"] == "Koordinator", (c, j))
ADI = j.get("data", {}).get("id")
c, j = req("POST", S + "/employee-positions", {"position": "Kepala Asrama", "category": "Lainnya"}, st)
check("auto code dedup KA -> KA-2", c == 201 and j["data"]["position_code"] == "KA-2", (c, j))
c, j = req("POST", S + "/employee-positions", {"position": "wali kelas", "category": "Pendidik"}, st)
check("duplicate name -> 409 DUPLICATE_NAME", c == 409 and j.get("code") == "DUPLICATE_NAME", (c, j))
c, j = req("POST", S + "/employee-positions", {"position": "Baru", "category": "Pendidik", "position_code": "ks"}, st)
check("duplicate code -> 409 DUPLICATE_CODE", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))
c, j = req("POST", S + "/employee-positions", {"position": "  ", "category": "Pendidik"}, st)
check("blank name -> 400", c == 400 and j.get("code") == "VALIDATION_ERROR", (c, j))
c, j = req("PUT", S + f"/employee-positions/{ADI}", {"position": "Koordinator Sekolah Adiwiyata", "category": "Koordinator"}, st)
check("update keeps code", c == 200 and j["data"]["position_code"] == "KA" and j["data"]["position"] == "Koordinator Sekolah Adiwiyata", (c, j))
c, j = req("PUT", S + f"/employee-positions/{ADI}", {"position": "Kepala Sekolah", "category": "Koordinator"}, st)
check("update to used name -> 409", c == 409 and j.get("code") == "DUPLICATE_NAME", (c, j))
c, j = req("PUT", S + f"/employee-positions/{WALAS_B}", {"position": "X", "category": "Pendidik"}, st)
check("update foreign position -> 404", c == 404, (c, j))

# ---------- academic setup ----------
yr, grades = setup_school(st, "SMP", "2025/2026")
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": yr["id"], "grade_level_id": grades[7], "names": ["7A", "7B"]}, st)
c, j = req("GET", S + "/classes?all=true", token=st)
cls = {x["name"]: x["id"] for x in j.get("data", [])}
c7a, c7b = cls["7A"], cls["7B"]
c, j = req("POST", S + "/subjects/preset", {"preset": "SMP"}, st)
codes = {x["subject_code"]: x["id"] for x in j.get("data", [])}
MAT, IPA = codes["MAT"], codes["IPA"]
c, j = req("POST", S + "/schedule-periods/preset", {"preset": "40_MIN"}, st)
pr = j.get("data", [])
P = {p["name"]: p for p in pr}
J1, J2, J3 = P["Jam ke-1"]["id"], P["Jam ke-2"]["id"], P["Jam ke-3"]["id"]
BRK = next(p["id"] for p in pr if p["is_break"])
check("period preset", c == 200 and len(pr) > 0, (c, j))
c, j = req("POST", S + "/schedule-periods", {"name": "Jumat Khusus", "period_order": 60, "start_time": "15:00", "end_time": "15:40", "day_of_week": 5}, st)
FRI = j.get("data", {}).get("id")

# ---------- teacher create ----------
body = {"full_name": "Guru Form", "employee_code": "GF-" + sfx, "send_invite": False,
        "subjects": [{"subject_id": MAT, "academic_id": yr["id"], "is_primary": True}, {"subject_id": IPA, "academic_id": "", "is_primary": False}],
        "assignments": [{"employee_position_id": WALAS, "start_date": "2025-07-14", "end_date": "", "is_active": True}],
        "schedules": [{"subject_id": MAT, "class_id": c7a, "day_of_week": 1, "period_id": J1},
                      {"subject_id": IPA, "class_id": c7b, "day_of_week": 2, "start_time": "09:00", "end_time": "09:40"}]}
c, j = req("POST", S + "/teachers", body, st)
check("teacher create with subjects/assignments/period schedules", c == 201, (c, j))
TF = j.get("data", {}).get("id")
c, j = req("GET", S + f"/teachers/{TF}", token=st)
d = j.get("data", {})
s1 = next((x for x in d.get("schedules", []) if x["day_of_week"] == 1), {})
check("GET: schedule has period_id + times from period", s1.get("period_id") == J1 and s1.get("start_time") == P["Jam ke-1"]["start_time"] and s1.get("end_time") == P["Jam ke-1"]["end_time"], s1)
s2 = next((x for x in d.get("schedules", []) if x["day_of_week"] == 2), {})
check("GET: manual schedule period_id null", "period_id" in s2 and s2["period_id"] is None and s2["start_time"] == "09:00", s2)
check("GET: assignments include position name", d["assignments"][0]["position"]["position"] == "Wali Kelas" and d["assignments"][0]["employee_position_id"] == WALAS, d.get("assignments"))
check("GET: subjects with subject preload", len(d["subjects"]) == 2 and all(x.get("subject") for x in d["subjects"]), d.get("subjects"))
c, j = req("GET", S + "/employee-positions", token=st)
check("assignment_count = 1 for Wali Kelas", next(p for p in j["data"] if p["id"] == WALAS)["assignment_count"] == 1, None)
c, j = req("DELETE", S + f"/employee-positions/{WALAS}", token=st)
check("delete in-use -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("DELETE", S + f"/employee-positions/{ADI}", token=st)
check("delete unused -> 204", c == 204, (c, j))

# ---------- validation ----------
base = {"full_name": "Guru X", "employee_code": "GX-" + sfx, "send_invite": False}
c, j = req("POST", S + "/teachers", {**base, "schedules": [{"subject_id": MAT, "class_id": c7b, "day_of_week": 3, "period_id": BRK}]}, st)
check("break period -> 400", c == 400 and "schedules[0].period_id" in (j.get("details") or {}), (c, j))
c, j = req("POST", S + "/teachers", {**base, "schedules": [{"subject_id": MAT, "class_id": c7b, "day_of_week": 1, "period_id": FRI}]}, st)
check("friday-only period on Monday -> 400", c == 400 and "Jumat" in j.get("error", ""), (c, j))
c, j = req("POST", S + "/teachers", {**base, "assignments": [{"employee_position_id": WALAS_B, "start_date": "2025-07-14", "is_active": True}]}, st)
check("foreign position -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
c, j = req("POST", S + "/teachers", {**base, "schedules": [{"subject_id": MAT, "class_id": c7b, "day_of_week": 1, "period_id": str(uuid.uuid4())}]}, st)
check("unknown period -> 400 INVALID_REFERENCE", c == 400 and j.get("code") == "INVALID_REFERENCE", (c, j))
# class conflict: 7A Monday Jam ke-1 is taken by Guru Form
c, j = req("POST", S + "/teachers", {**base, "schedules": [{"subject_id": IPA, "class_id": c7b, "day_of_week": 4, "period_id": J1}, {"subject_id": IPA, "class_id": c7a, "day_of_week": 1, "period_id": J1}]}, st)
check("class conflict -> 409 SCHEDULE_CONFLICT with index 1", c == 409 and j.get("code") == "SCHEDULE_CONFLICT" and j["details"]["type"] == "class" and j["details"]["index"] == "1", (c, j))
c, j = req("GET", S + "/teachers?search=GX-" + sfx, token=st)
check("conflicting create rolled back", c == 200 and j["meta"]["total"] == 0, j.get("meta"))

# ---------- teacher update ----------
upd = {"full_name": "Guru Form", "employee_code": "GF-" + sfx,
       "subjects": [{"subject_id": MAT, "academic_id": yr["id"], "is_primary": True}],
       "assignments": [{"employee_position_id": WALAS, "start_date": "2025-07-14", "end_date": "2026-06-30", "is_active": True},
                       {"employee_position_id": KS, "start_date": "2025-07-14", "end_date": "", "is_active": False}],
       "schedules": [{"subject_id": MAT, "class_id": c7a, "day_of_week": 1, "period_id": J1},
                     {"subject_id": IPA, "class_id": c7b, "day_of_week": 2, "period_id": J2},
                     {"subject_id": MAT, "class_id": c7b, "day_of_week": 1, "period_id": J1}]}
c, j = req("PUT", S + f"/teachers/{TF}", upd, st)
check("update: self double booking (row 2) -> 409 teacher, index 2", c == 409 and j["details"]["type"] == "teacher" and j["details"]["index"] == "2", (c, j))
upd["schedules"][2]["period_id"] = J3
c, j = req("PUT", S + f"/teachers/{TF}", upd, st)
check("update with periods ok", c == 200, (c, j))
c, j = req("GET", S + f"/teachers/{TF}", token=st)
d = j.get("data", {})
check("GET after update: 3 schedules with period ids", sorted(x["period_id"] for x in d["schedules"]) == sorted([J1, J2, J3]), d.get("schedules"))
check("GET after update: unchanged row kept (same id)", next(x for x in d["schedules"] if x["day_of_week"] == 1 and x["class_id"] == c7a)["id"] == s1.get("id"), None)
check("GET after update: 2 assignments with names", sorted(a["position"]["position"] for a in d["assignments"]) == ["Kepala Sekolah", "Wali Kelas"] and any(a["end_date"] == "2026-06-30" for a in d["assignments"]), d.get("assignments"))
# backward compatible: start/end-only resend keeps rows
legacy = {**upd, "schedules": [{"subject_id": x["subject_id"], "class_id": x["class_id"], "day_of_week": x["day_of_week"], "start_time": x["start_time"], "end_time": x["end_time"]} for x in d["schedules"]]}
ids_before = sorted(x["id"] for x in d["schedules"])
c, j = req("PUT", S + f"/teachers/{TF}", legacy, st)
c2, j2 = req("GET", S + f"/teachers/{TF}", token=st)
check("start/end-only resend keeps rows & periods", c == 200 and sorted(x["id"] for x in j2["data"]["schedules"]) == ids_before and all(x["period_id"] for x in j2["data"]["schedules"]), (c, j, j2.get("data", {}).get("schedules")))
c, j = req("PUT", S + f"/teachers/{TF}", {"full_name": "Guru Form", "employee_code": "GF-" + sfx, "schedules": [{"subject_id": MAT, "class_id": c7a, "day_of_week": 3, "start_time": "10:00", "end_time": "09:00"}]}, st)
check("manual end before start -> 400", c == 400, (c, j))

# ---------- teacher delete frees position ----------
c, j = req("DELETE", S + f"/teachers/{TF}", token=st)
check("delete teacher", c in (200, 204), (c, j))
c, j = req("DELETE", S + f"/employee-positions/{KS}", token=st)
check("position free after teacher delete -> 204", c == 204, (c, j))

passed = sum(1 for _, ok in results if ok)
print(f"\n{passed}/{len(results)} passed")
