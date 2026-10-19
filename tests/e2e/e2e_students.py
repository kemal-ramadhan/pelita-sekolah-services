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

# ---------------- setup & earlier-flow smoke ----------------
c, j = req("POST", "/auth/login", {"email": "admin@edupilot.local", "password": ADMIN_PW})
check("admin login", c == 200, (c, j)); admin = j.get("access_token")


def mk_school(code, level, email):
    c, j = req("POST", "/instantion/admin/schools", {"code": code, "name": "Sekolah " + code, "school_level": level,
               "account": {"email": email, "password": "SchoolPass123", "full_name": "Operator " + code}}, admin)
    check(f"admin creates school {code}", c == 201, (c, j))
    c, j = req("POST", "/auth/login", {"email": email, "password": "SchoolPass123"})
    return j.get("data", {}).get("id") if False else j.get("access_token"), j.get("refresh_token")


st, srt = mk_school("E2E-" + sfx, "SMP", f"smp-{sfx}@e2e.test")
c, j = req("POST", "/auth/refresh", {"refresh_token": srt})
check("smoke: refresh rotation", c == 200 and j.get("refresh_token") != srt, (c, j))
c2, _ = req("POST", "/auth/refresh", {"refresh_token": srt})
check("smoke: old refresh rejected", c2 == 401, c2)
st = j.get("access_token", st)
body, ct = multipart({}, [("logo", "logo.png", png(), "image/png")])
c, j = req("PUT", S + "/profile/logo", raw=body, ctype=ct, token=st)
check("smoke: logo upload", c == 200 and j["data"]["logo"].startswith("data:image/png"), (c, j))
c, j = req("POST", S + "/teachers", {"full_name": "Guru Wali", "employee_code": "GW-" + sfx, "email": f"guru-{sfx}@e2e.test", "send_invite": False}, st)
check("smoke: teacher create without invite", c == 201 and j["data"]["account_status"] == "none", (c, j))
teacher_id = j.get("data", {}).get("id")

# ---------------- references ----------------
c, j = req("GET", S + "/references", token=st)
keys = set(j.get("data", {}).keys())
need = {"religions", "genders", "blood_types", "citizenships", "special_needs", "residence_types", "transportations", "education_levels",
        "occupations", "incomes", "registration_types", "student_statuses", "history_statuses", "education_stages", "curricula", "guardian_types", "document_types"}
check("references has all §8.1 keys", c == 200 and need <= keys, need - keys)
check("references document_types seeded (9)", len(j["data"].get("document_types", [])) == 9, j["data"].get("document_types"))
c, j = req("GET", S + "/document-types", token=st)
check("document-types idempotent (still 9)", c == 200 and len(j["data"]) == 9, j)
doc_type = j["data"][0]["id"]

# ---------------- academic years ----------------
c, j = req("POST", S + "/academic-years", {"name": "2025/2026", "start_date": "2025-07-14", "end_date": "2026-06-30"}, st)
y1 = j.get("data", {})
check("year1 create: active + 2 semesters (Ganjil active)", c == 201 and y1.get("is_active") and [s["name"] for s in y1["semesters"]] == ["Ganjil", "Genap"]
      and y1["semesters"][0]["is_active"] and not y1["semesters"][1]["is_active"], (c, j))
y1id = y1.get("id")
c, j = req("POST", S + "/academic-years", {"name": "2026/2027", "start_date": "2026-07-13", "end_date": "2027-06-30"}, st)
y2 = j.get("data", {}); y2id = y2.get("id")
check("year2 create: inactive", c == 201 and y2.get("is_active") is False, (c, j))
c, j = req("POST", S + "/academic-years", {"name": "2025/2026"}, st)
check("duplicate year name -> 409 YEAR_EXISTS", c == 409 and j.get("code") == "YEAR_EXISTS", (c, j))
c, j = req("POST", S + "/academic-years", {"name": "2030/2031", "start_date": "2031-01-01", "end_date": "2030-01-01"}, st)
check("year end before start -> 400 details.end_date", c == 400 and "end_date" in j.get("details", {}), (c, j))
c, j = req("POST", S + f"/academic-years/{y2id}/activate", token=st)
c, j = req("GET", S + "/academic-years", token=st)
act = [y["name"] for y in j["data"] if y["is_active"]]
check("activate year2 -> exactly one active", act == ["2026/2027"], act)
sem_active = psql(f"select count(*) from semesters where is_active and academic_year_id in (select id from academic_years where school_id=(select school_id from academic_years where id='{y1id}'))")
check("one active semester per school after activation", sem_active == "1", sem_active)
genap1 = y1["semesters"][1]["id"]
c, j = req("POST", S + f"/semesters/{genap1}/activate", token=st)
c, j = req("GET", S + "/academic-years", token=st)
yy = {y["id"]: y for y in j["data"]}
check("activate semester Genap of year1 -> year1 active, Genap only active semester",
      yy[y1id]["is_active"] and not yy[y2id]["is_active"] and [s["is_active"] for s in yy[y1id]["semesters"]] == [False, True]
      and not any(s["is_active"] for s in yy[y2id]["semesters"]), j)
c, j = req("POST", S + f"/semesters/{y1['semesters'][0]['id']}/activate", token=st)
check("re-activate Ganjil", c == 200, (c, j))
c, j = req("GET", S + "/academic-years", token=st)
check("academic years sorted desc by name", [y["name"] for y in j["data"]] == ["2026/2027", "2025/2026"], j)

# ---------------- grade levels ----------------
c, j = req("POST", S + "/grade-levels/preset", {"preset": "SMP", "numbering": "roman"}, st)
check("preset SMP roman", c == 200 and [g["short_name"] for g in j["data"]] == ["VII", "VIII", "IX"] and j["data"][2]["is_final"], (c, j))
c, j = req("POST", S + "/grade-levels/preset", {"preset": "SMP", "numbering": "roman"}, st)
check("preset again without replace -> 409", c == 409, (c, j))
c, j = req("POST", S + "/grade-levels/preset", {"preset": "MTs", "numbering": "arabic", "replace": True}, st)
check("preset MTs replace (case-sensitive name)", c == 200 and [g["short_name"] for g in j["data"]] == ["7", "8", "9"] and j["data"][0]["education_stage"] == "MTs", (c, j))
grades = {g["number"]: g["id"] for g in j.get("data", [])}
c, j = req("POST", S + "/grade-levels", {"number": 9, "name": "Dup"}, st)
check("grade level duplicate number -> 409", c == 409 and j.get("code") == "NUMBER_EXISTS", (c, j))
c, j = req("POST", S + "/grade-levels/preset", {"preset": "XYZ"}, st)
check("unknown preset -> 400", c == 400, (c, j))

# ---------------- majors ----------------
c, j = req("POST", S + "/majors", {"code": "ipa", "name": "IPA"}, st)
check("major create (code upper)", c == 201 and j["data"]["code"] == "IPA", (c, j)); major_id = j.get("data", {}).get("id")
c, j = req("POST", S + "/majors", {"code": "IPA", "name": "IPA 2"}, st)
check("major duplicate code -> 409", c == 409, (c, j))

# ---------------- classes ----------------
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": y1id, "grade_level_id": grades[7], "names": ["7A", "7B"], "aliases": ["Ibnu Sina"], "capacity": 2}, st)
check("bulk classes 7A/7B", c == 201 and j["data"]["created"] == 2, (c, j))
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": y1id, "grade_level_id": grades[7], "names": ["7A", "7b"]}, st)
check("bulk again -> skipped 2", c == 201 and j["data"]["created"] == 0 and len(j["data"]["skipped"]) == 2, (c, j))
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": str(uuid.uuid4()), "grade_level_id": grades[7], "names": ["7Z"]}, st)
check("bulk with foreign year -> 400", c == 400 and "academic_year_id" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/classes", {"academic_year_id": y1id, "grade_level_id": grades[7], "name": "7C", "homeroom_employee_id": teacher_id, "major_id": "", "capacity": 30, "curriculum": "merdeka"}, st)
check("class 7C with homeroom", c == 201 and j["data"]["homeroom"]["employee_id"] == teacher_id and j["data"].get("major") is None if c == 201 else False, (c, j))
c7c = j.get("data", {}).get("id")
c, j = req("POST", S + "/classes", {"academic_year_id": y1id, "grade_level_id": grades[7], "name": "7D", "homeroom_employee_id": str(uuid.uuid4())}, st)
check("class with unknown homeroom -> 400", c == 400 and "homeroom_employee_id" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/classes", {"academic_year_id": y1id, "grade_level_id": grades[7], "name": "7c"}, st)
check("class duplicate name (case-insensitive) -> 409", c == 409, (c, j))
c, j = req("POST", S + "/classes", {"academic_year_id": y1id, "grade_level_id": grades[9], "name": "9A"}, st)
c9a = j.get("data", {}).get("id")
c, j = req("GET", S + "/classes?all=true", token=st)
cls = {x["name"]: x for x in j.get("data", [])}
check("classes default to active year; display_name alias", c == 200 and set(cls) == {"7A", "7B", "7C", "9A"} and cls["7A"]["display_name"] == "7A - Ibnu Sina", (c, j))
c7a, c7b = cls["7A"]["id"], cls["7B"]["id"]
c, j = req("GET", S + "/classes?academic_year_id=not-a-uuid", token=st)
check("classes invalid uuid filter -> 400", c == 400, (c, j))

# ---------------- students ----------------
full = {
    "identity": {"full_name": "Ahmad Fauzi", "nickname": "Ahmad", "gender": "L", "nisn": "0081234567", "nis": "2025001", "nik": "3201011234567890",
                 "no_kk": "3201019876543210", "birth_place": "Depok", "birth_date": "2012-05-15", "religion": "islam", "child_order": 1, "siblings_count": 2,
                 "photo": "data:image/png;base64,AAAA"},
    "contact": {"mobile_phone": "0812-3456-7890", "email": "ahmad@e2e.test"},
    "address": {"street": "Jl. Merdeka 1", "rt": "003", "rw": "005", "village": "Sukamaju", "district": "Cilodong", "city": "Depok", "province": "Jawa Barat",
                "postal_code": "16415", "residence_type": "orang_tua", "transportation": "sepeda_motor", "distance_km": 2.5},
    "physical": {"height_cm": 150, "weight_kg": 40},
    "registration": {"registration_type": "siswa_baru", "entry_date": "2025-07-14", "entry_grade_level_id": grades[7]},
    "guardians": [{"type": "father", "name": "Budi", "nik": "3201011111110001", "birth_year": 1980, "education": "s1", "occupation": "wiraswasta", "income": "5m_20m", "is_alive": True},
                  {"type": "mother", "name": "Siti", "birth_year": 1983, "is_alive": True}],
    "achievements": [{"name": "Olimpiade Matematika", "type": "akademik", "level": "kabupaten", "rank": "1", "year": 2024}],
    "scholarships": [{"name": "PIP", "type": "anak_miskin", "provider": "Kemendikbud", "start_year": 2024, "end_year": 2025, "amount": 450000}],
    "class_id": c7a,
}
c, j = req("POST", S + "/students", full, st)
d = j.get("data", {})
check("student create full + class 7A", c == 201 and d["current_class"]["display_name"] == "7A - Ibnu Sina" and len(d["guardians"]) == 2 and d["guardians"][0]["id"]
      and d["achievements"][0]["id"] and d["scholarships"][0]["amount"] == 450000 and d["contact"]["mobile_phone"] == "081234567890"
      and d["identity"]["photo"] is None and d["completeness"] == 100, (c, j))
sA = d.get("id")
base = lambda name, **kw: {"identity": dict({"full_name": name, "gender": "P", "birth_place": "Bogor", "birth_date": "2012-01-01", "religion": "islam"}, **kw)}
c, j = req("POST", S + "/students", base("Bad", nisn="123", nik="12"), st)
check("bad NISN/NIK -> 400 VALIDATION_ERROR with details", c == 400 and j.get("code") == "VALIDATION_ERROR" and {"identity.nisn", "identity.nik"} <= set(j.get("details", {})), (c, j))
c, j = req("POST", S + "/students", base("Bad", religion="jedi", blood_type="Z"), st)
check("bad enum -> 400 details", c == 400 and {"identity.religion", "identity.blood_type"} <= set(j.get("details", {})), (c, j))
c, j = req("POST", S + "/students", base("Bad", birth_date="2999-01-01"), st)
check("future birth date -> 400", c == 400 and "identity.birth_date" in j.get("details", {}), (c, j))
c, j = req("POST", S + "/students", {"identity": {"full_name": "Only name"}}, st)
check("missing required -> 400 lists required", c == 400 and {"identity.gender", "identity.birth_place", "identity.birth_date", "identity.religion"} <= set(j.get("details", {})), (c, j))
c, j = req("POST", S + "/students", base("Dup", nisn="0081234567"), st)
check("duplicate NISN -> 409 NISN_EXISTS", c == 409 and j.get("code") == "NISN_EXISTS", (c, j))
c, j = req("POST", S + "/students", dict(base("X"), class_id=str(uuid.uuid4())), st)
check("create with unknown class_id -> 400 (no orphan student)", c == 400, (c, j))
c, j = req("GET", S + "/students?search=X&limit=100", token=st)
check("  no student X left behind", j["meta"]["total"] == 0, j.get("meta"))

ids = {}
for nm, g in [("Budi Santoso", "L"), ("Citra Lestari", "P"), ("Dewi Anggraini", "P"), ("Fajar Nugroho", "L")]:
    b = base(nm); b["identity"]["gender"] = g
    c, j = req("POST", S + "/students", b, st)
    ids[nm.split()[0]] = j.get("data", {}).get("id")
check("create 4 more students", all(ids.values()), ids)
sB, sC, sD, sF = ids["Budi"], ids["Citra"], ids["Dewi"], ids["Fajar"]

# photo
body, ct = multipart({}, [("photo", "p.png", png(), "image/jpeg")])
c, j = req("PUT", S + f"/students/{sA}/photo", raw=body, ctype=ct, token=st)
check("photo upload -> sniffed png data URL", c == 200 and j["data"]["photo"].startswith("data:image/png;base64,"), (c, j))
body, ct = multipart({}, [("photo", "p.png", b"MZ\x90\x00 not an image", "image/png")])
c, j = req("PUT", S + f"/students/{sA}/photo", raw=body, ctype=ct, token=st)
check("photo non-image -> 415", c == 415, (c, j))
body, ct = multipart({}, [("photo", "big.png", png() + b"\0" * (2 << 20), "image/png")])
c, j = req("PUT", S + f"/students/{sA}/photo", raw=body, ctype=ct, token=st)
check("photo > 2MB -> 413", c == 413, (c, j))
c, j = req("PUT", S + f"/students/{uuid.uuid4()}/photo", raw=multipart({}, [("photo", "p.png", png(), "image/png")])[0], ctype=multipart({}, [])[1], token=st)
check("photo for unknown student -> 4xx", c in (400, 404), (c, j))
c, j = req("GET", S + f"/students/{sA}", token=st)
check("detail has photo", c == 200 and (j["data"]["identity"]["photo"] or "").startswith("data:image/png"), (c, j))
# PUT echoing photo (frontend sends full identity incl. photo) keeps working and keeps photo
upd = json.loads(json.dumps(full)); upd.pop("class_id"); upd["identity"]["photo"] = j["data"]["identity"]["photo"] + "A" * 1500000
upd["identity"]["nickname"] = "Fauzi"; upd.pop("achievements")
c, j = req("PUT", S + f"/students/{sA}", upd, st)
check("PUT student (1.5MB echoed photo, achievements omitted) -> 200, photo & achievements kept", c == 200 and j["data"]["identity"]["nickname"] == "Fauzi"
      and j["data"]["identity"]["photo"].startswith("data:image/png") and len(j["data"]["identity"]["photo"]) < 1000 and len(j["data"]["achievements"]) == 1, (c, str(j)[:300]))
c, j = req("GET", S + "/students?limit=100", token=st)
check("list excludes photo field", c == 200 and all("photo" not in x for x in j["data"]), j["data"][:1])

# documents
pdf = b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n"
body, ct = multipart({"document_type_id": doc_type, "notes": "Scan KK"}, [("file", r'../kk \"asli\".pdf', pdf, "application/octet-stream")])
c, j = req("POST", S + f"/students/{sA}/documents", raw=body, ctype=ct, token=st)
check("document upload pdf", c == 201 and j["data"]["mime_type"] == "application/pdf" and j["data"]["size_bytes"] == len(pdf) and j["data"]["file_name"] == "kk asli.pdf", (c, j))
doc_id = j.get("data", {}).get("id")
body, ct = multipart({"document_type_id": doc_type}, [("file", "x.exe", b"MZ\x90\x00\x03" + b"\0" * 100, "application/pdf")])
c, j = req("POST", S + f"/students/{sA}/documents", raw=body, ctype=ct, token=st)
check("document exe -> 415", c == 415, (c, j))
body, ct = multipart({"document_type_id": str(uuid.uuid4())}, [("file", "a.pdf", pdf, "application/pdf")])
c, j = req("POST", S + f"/students/{sA}/documents", raw=body, ctype=ct, token=st)
check("document unknown type -> 400", c == 400, (c, j))
c, j = req("GET", S + f"/students/{sA}/documents", token=st)
check("document list w/o data", c == 200 and len(j["data"]) == 1 and "data" not in j["data"][0] and j["data"][0]["document_type_name"], (c, j))
c, blob, h = req("GET", S + f"/students/{sA}/documents/{doc_id}/download", token=st, want_raw=True)
check("document download bytes + headers", c == 200 and blob == pdf and h.get("Content-Type") == "application/pdf" and "attachment" in h.get("Content-Disposition", ""), (c, h))
c, j = req("GET", S + f"/students/{sB}/documents/{doc_id}/download", token=st)
check("download doc via other student -> 404", c == 404, (c, j))
c, j = req("DELETE", S + f"/students/{sA}/documents/{doc_id}", token=st)
check("document delete", c == 204, c)
c, j = req("DELETE", S + f"/students/{sA}/documents/{doc_id}", token=st)
check("document delete again -> 404", c == 404, c)

# placement
c, j = req("POST", S + f"/students/{sB}/placement", {"class_id": c7a}, st)
check("place B in 7A (now full 2/2)", c == 200, (c, j))
c, j = req("POST", S + f"/students/{sC}/placement", {"class_id": c7a}, st)
check("place C in full 7A -> 409 CLASS_FULL", c == 409 and j.get("code") == "CLASS_FULL", (c, j))
c, j = req("POST", S + f"/students/{sB}/placement", {"class_id": c7a}, st)
check("place B in same class -> 409 ALREADY_IN_CLASS", c == 409 and j.get("code") == "ALREADY_IN_CLASS", (c, j))
c, j = req("POST", S + f"/students/{uuid.uuid4()}/placement", {"class_id": c7a}, st)
check("place unknown student -> 404", c == 404, (c, j))
c, j = req("POST", S + f"/students/{sC}/placement", {"class_id": c7b}, st)
check("place C in 7B", c == 200, (c, j))
c, j = req("POST", S + f"/students/{sA}/placement", {"class_id": c7c, "notes": "pindah"}, st)
check("move A 7A -> 7C", c == 200, (c, j))
c, j = req("GET", S + f"/students/{sA}/history", token=st)
hs = [(x["class"]["display_name"], x["status"]) for x in j.get("data", [])]
check("A history: 7C active, 7A moved", hs == [("7C", "active"), ("7A - Ibnu Sina", "moved")] and j["data"][1]["end_date"], hs)
c, j = req("POST", S + f"/classes/{c7b}/members", {"student_ids": [sD, sB, str(uuid.uuid4()), sD]}, st)
sk = {x["student_id"]: x["reason"] for x in j.get("data", {}).get("skipped", [])}
check("bulk members 7B: D placed, B CLASS_FULL skip, unknown skip", c == 200 and j["data"]["placed"] == 1 and sB in sk and len(sk) == 2, (c, j))
c, j = req("DELETE", S + f"/classes/{c7b}/members", raw=json.dumps({"student_ids": [sD]}).encode(), ctype="application/json", token=st)
check("remove D from 7B", c == 200 and j["data"]["removed"] == 1, (c, j))
c, j = req("GET", S + f"/students/{sD}/history", token=st)
check("D history withdrawn, no current class", j["data"][0]["status"] == "withdrawn", j)
c, j = req("GET", S + f"/students?unassigned=true&limit=100", token=st)
check("unassigned filter includes D,F", c == 200 and {sD, sF} <= {x["id"] for x in j["data"]} and sA not in {x["id"] for x in j["data"]}, (c, j))
c, j = req("POST", S + f"/classes/{c7b}/members", {"student_ids": [sD]}, st)
check("re-add D to 7B", c == 200 and j["data"]["placed"] == 1, (c, j))
c, j = req("POST", S + f"/students/{sF}/placement", {"class_id": c9a}, st)
check("place F in 9A", c == 200, (c, j))

# roster & counts
c, j = req("GET", S + f"/classes/{c7b}/students?limit=1", token=st)
check("roster 7B paginated (total 2, limit 1)", c == 200 and j["meta"]["total"] == 2 and len(j["data"]) == 1 and j["data"][0]["current_class"]["display_name"] == "7B", (c, j))
c, j = req("GET", S + f"/classes/{uuid.uuid4()}/students", token=st)
check("roster unknown class -> 404", c == 404, c)
c, j = req("GET", S + f"/classes/{c7b}", token=st)
check("class 7B counts 2 (P,P)", j["data"]["student_count"] == 2 and j["data"]["female_count"] == 2 and j["data"]["male_count"] == 0, j)
c, j = req("GET", S + "/students?class_id=" + c7a, token=st)
check("filter class_id=7A -> B only", [x["id"] for x in j["data"]] == [sB], j)
c, j = req("GET", S + "/students?grade_level_id=" + grades[7] + "&gender=P&limit=100", token=st)
check("filter grade 7 + gender P -> C,D", {x["id"] for x in j["data"]} == {sC, sD}, j)
c, j = req("GET", S + "/students?academic_year_id=" + y1id + "&limit=100", token=st)
check("filter academic_year_id -> 5 placed", j["meta"]["total"] == 5, j.get("meta"))
c, j = req("GET", S + "/students?sort=-name&limit=100", token=st)
names = [x["full_name"] for x in j["data"]]
check("sort=-name", names == sorted(names, reverse=True), names)
c, j = req("GET", S + "/students?search=0081234&limit=100", token=st)
check("search by NISN", [x["id"] for x in j["data"]] == [sA], j)
c, j = req("GET", S + "/students?search=%25", token=st)
check("search '%' escaped (0 hits)", j["meta"]["total"] == 0, j.get("meta"))
c, j = req("GET", S + "/students?status=bogus", token=st)
check("invalid status filter -> 400", c == 400, c)
c, j = req("GET", S + "/students?class_id=xyz", token=st)
check("invalid class_id filter -> 400", c == 400, c)
c, j = req("GET", S + "/students?limit=1000", token=st)
check("limit capped to 100", j["meta"]["limit"] == 100, j.get("meta"))
c, j = req("GET", S + "/students/stats", token=st)
sd = j.get("data", {})
g7 = [g for g in sd.get("by_grade_level", []) if g["grade_level_id"] == grades[7]]
check("stats", c == 200 and sd["total"] == 5 and sd["active"] == 5 and sd["unassigned"] == 0 and g7 and g7[0]["total"] == 4 and g7[0]["female"] == 2, sd)

# ---------------- import ----------------
c, blob, h = req("GET", S + "/students/import/template", token=st, want_raw=True)
zf = zipfile.ZipFile(io.BytesIO(blob)); wb = zf.read("xl/workbook.xml").decode()
check("import template xlsx with Siswa + Referensi", c == 200 and 'name="Siswa"' in wb and 'name="Referensi"' in wb, (c, wb[:200]))
open(TMP + "/template.xlsx", "wb").write(blob)
rows = [
    {"Nama Lengkap": "Eka Putri", "Jenis Kelamin": "Perempuan", "NISN": "0099887766", "Tempat Lahir": "Bandung", "Tanggal Lahir": "15/05/2012",
     "Agama": "Islam", "Alat Transportasi": "Jalan kaki", "Nama Ibu": "Rina", "Pendidikan Ibu": "SMA sederajat", "Kelas": "7C"},
    {"Nama Lengkap": "Rusak", "Jenis Kelamin": "L", "NISN": "123", "Tempat Lahir": "X", "Tanggal Lahir": "2012-01-01", "Agama": "islam"},
    {"Nama Lengkap": "Ahmad Fauzi", "Nama Panggilan": "Oji", "Jenis Kelamin": "L", "NISN": "0081234567", "Tempat Lahir": "Depok", "Tanggal Lahir": "2012-05-15", "Agama": "islam"},
    {"Nama Lengkap": "Kelas Salah", "Jenis Kelamin": "L", "Tempat Lahir": "X", "Tanggal Lahir": "2012-01-01", "Agama": "islam", "Kelas": "Z9"},
]
json.dump(rows, open(TMP + "/rows.json", "w"))
r = subprocess.run([TMP + "/xlsxgen/xlsxgen", TMP + "/template.xlsx", TMP + "/import.xlsx", TMP + "/rows.json"], capture_output=True, text=True)
check("generate filled xlsx", r.returncode == 0, r.stderr)
xbytes = open(TMP + "/import.xlsx", "rb").read()


def do_import(q):
    body, ct = multipart({}, [("file", "siswa.xlsx", xbytes, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")])
    return req("POST", S + "/students/import" + q, raw=body, ctype=ct, token=st)


c, j = do_import("?dry_run=true&mode=create")
d = j.get("data", {})
cols = {(e["row"], e.get("column")) for e in d.get("errors", [])}
check("import dry_run create: 1 created, 3 failed (NISN bad, dup NISN, unknown class)", c == 200 and d["dry_run"] and d["created"] == 1 and d["failed"] == 3
      and (3, "NISN") in cols and (4, "NISN") in cols and (5, "Kelas") in cols, (c, j))
c, j = do_import("?dry_run=true&mode=upsert")
d = j.get("data", {})
check("import dry_run upsert: created 1, updated 1, failed 2", c == 200 and d["created"] == 1 and d["updated"] == 1 and d["failed"] == 2, (c, j))
c, j = req("GET", S + "/students?search=Eka", token=st)
check("dry run wrote nothing", j["meta"]["total"] == 0, j.get("meta"))
c, j = do_import("?dry_run=false&mode=upsert")
d = j.get("data", {})
check("import commit upsert", c == 200 and not d["dry_run"] and d["created"] == 1 and d["updated"] == 1, (c, j))
c, j = req("GET", S + "/students?search=Eka", token=st)
eka = j["data"][0] if j.get("data") else {}
check("imported Eka placed in 7C", eka.get("current_class", {}) and eka["current_class"]["display_name"] == "7C" and eka["gender"] == "P", eka)
sE = eka.get("id")
c, j = req("GET", S + f"/students/{sE}", token=st)
check("imported Eka: dates/enums canonical, mother guardian", j["data"]["identity"]["birth_date"] == "2012-05-15" and j["data"]["address"]["transportation"] == "jalan_kaki"
      and j["data"]["guardians"][0]["type"] == "mother" and j["data"]["guardians"][0]["education"] == "sma", j)
c, j = req("GET", S + f"/students/{sA}", token=st)
check("upsert updated A nickname, kept photo/achievements/guardians/class", j["data"]["identity"]["nickname"] == "Oji" and j["data"]["identity"]["photo"]
      and len(j["data"]["achievements"]) == 1 and len(j["data"]["guardians"]) == 2 and j["data"]["current_class"]["display_name"] == "7C", j["data"]["identity"])
body, ct = multipart({}, [("file", "siswa.csv", b"a,b", "text/csv")])
c, j = req("POST", S + "/students/import?dry_run=true", raw=body, ctype=ct, token=st)
check("import non-xlsx -> 400 INVALID_FILE_TYPE", c == 400 and j.get("code") == "INVALID_FILE_TYPE", (c, j))
body, ct = multipart({}, [("file", "siswa.xlsx", b"PK\x03\x04garbage", "application/octet-stream")])
c, j = req("POST", S + "/students/import?dry_run=true", raw=body, ctype=ct, token=st)
check("import corrupt xlsx -> 400 (no raw error)", c == 400 and "zip" not in json.dumps(j).lower(), (c, j))
c, j = req("POST", S + "/students/import?mode=bogus", raw=multipart({}, [("file", "a.xlsx", xbytes, "x")])[0], ctype=multipart({}, [])[1], token=st)
check("import bad mode -> 400", c == 400, (c, j))

# export
c, blob, h = req("GET", S + "/students/export?class_id=" + c7c, token=st, want_raw=True)
ok = c == 200 and blob[:2] == b"PK"
sheet = zipfile.ZipFile(io.BytesIO(blob)).read("xl/worksheets/sheet1.xml").decode() if ok else ""
ss = zipfile.ZipFile(io.BytesIO(blob)).read("xl/sharedStrings.xml").decode() if ok and "xl/sharedStrings.xml" in zipfile.ZipFile(io.BytesIO(blob)).namelist() else ""
check("export filtered by class 7C -> 2 data rows + Status/Kelas Saat Ini", ok and sheet.count("<row") == 3 and ("Kelas Saat Ini" in sheet or "Kelas Saat Ini" in ss), (c, sheet[:300]))
c, blob, h = req("GET", S + "/students/export", token=st, want_raw=True)
check("export all", c == 200 and blob[:2] == b"PK" and zipfile.ZipFile(io.BytesIO(blob)).read("xl/worksheets/sheet1.xml").decode().count("<row") == 7, c)

# ---------------- copy structure & promotion ----------------
c, j = req("POST", S + "/classes/copy", {"from_academic_year_id": y1id, "to_academic_year_id": y2id, "include_homeroom": True}, st)
check("copy classes y1 -> y2", c == 200 and j["data"] == {"created": 4, "skipped": 0}, (c, j))
c, j = req("POST", S + "/classes/copy", {"from_academic_year_id": y1id, "to_academic_year_id": y2id}, st)
check("copy again -> all skipped", c == 200 and j["data"] == {"created": 0, "skipped": 4}, (c, j))
c, j = req("POST", S + "/classes/copy", {"from_academic_year_id": y1id, "to_academic_year_id": str(uuid.uuid4())}, st)
check("copy to foreign year -> 404", c == 404, (c, j))
c, j = req("GET", S + f"/classes?all=true&academic_year_id={y2id}", token=st)
y2cls = {x["name"]: x for x in j["data"]}
check("copied 7A keeps alias, 7C keeps homeroom", y2cls["7A"]["alias"] == "Ibnu Sina" and y2cls["7C"]["homeroom"] and y2cls["7A"]["student_count"] == 0, y2cls)
for nm in ["8A", "8B", "VIII-C"]:
    req("POST", S + "/classes", {"academic_year_id": y2id, "grade_level_id": grades[8], "name": nm, "alias": "Ibnu Sina" if nm == "8A" else ""}, st)
c, j = req("GET", S + f"/classes?all=true&academic_year_id={y2id}", token=st)
y2cls = {x["name"]: x["id"] for x in j["data"]}
c, j = req("GET", S + f"/promotions/preview?from_class_id={c7b}&to_academic_year_id={y2id}", token=st)
rows_ = j.get("data", {}).get("rows", [])
check("preview 7B -> promote, suggest 8B", c == 200 and len(rows_) == 2 and all(r["suggested_action"] == "promote" and r["suggested_to_class_id"] == y2cls["8B"] for r in rows_)
      and j["data"]["from_class"]["student_count"] == 2, (c, j))
c, j = req("GET", S + f"/promotions/preview?from_class_id={c9a}&to_academic_year_id={y2id}", token=st)
check("preview 9A (final) -> graduate", c == 200 and j["data"]["rows"][0]["suggested_action"] == "graduate" and j["data"]["rows"][0]["suggested_to_class_id"] is None, (c, j))
c, j = req("GET", S + f"/promotions/preview?from_class_id={c7b}&to_academic_year_id={uuid.uuid4()}", token=st)
check("preview foreign year -> 404", c == 404, c)
items = [
    {"student_id": sC, "action": "promote", "to_class_id": y2cls["8B"]},
    {"student_id": sD, "action": "retain", "to_class_id": y2cls["7B"]},
    {"student_id": sE, "action": "promote", "to_class_id": y2cls["7A"]},  # same grade -> error
    {"student_id": sB, "action": "graduate"},  # grade 7, no force -> error
    {"student_id": sF, "action": "graduate"},  # 9A final -> ok
    {"student_id": sA, "action": "promote", "to_class_id": c7c},  # class not in target year -> error
]
c, j = req("POST", S + "/promotions", {"to_academic_year_id": y2id, "items": items}, st)
d = j.get("data", {}); errs = {e["student_id"] for e in d.get("errors", [])}
check("promotions: 1 promoted, 1 retained, 1 graduated, 3 errors", c == 200 and (d["promoted"], d["retained"], d["graduated"]) == (1, 1, 1) and errs == {sE, sB, sA}, (c, j))
c, j = req("POST", S + "/promotions", {"to_academic_year_id": y2id, "items": [{"student_id": sB, "action": "graduate", "force": True}]}, st)
check("graduate non-final with force", c == 200 and j["data"]["graduated"] == 1, (c, j))
c, j = req("POST", S + "/promotions", {"to_academic_year_id": y2id, "items": [{"student_id": sC, "action": "promote", "to_class_id": y2cls["VIII-C"]}]}, st)
check("promote again into target year -> error", c == 200 and j["data"]["promoted"] == 0 and len(j["data"]["errors"]) == 1, (c, j))
c, j = req("GET", S + f"/students/{sC}/history", token=st)
hs = [(x["class"]["display_name"], x["status"], x["academic_year_name"]) for x in j["data"]]
check("C history chain: 8B active (y2) <- 7B promoted (y1)", hs == [("8B", "active", "2026/2027"), ("7B", "promoted", "2025/2026")], hs)
c, j = req("GET", S + f"/students/{sB}", token=st)
check("B graduated: status + exit_date, no class", j["data"]["status"] == "graduated" and j["data"]["exit"]["exit_date"] and j["data"]["current_class"] is None, j["data"]["exit"])
c, j = req("GET", S + f"/students/{sD}/history", token=st)
check("D retained chain", [x["status"] for x in j["data"]][:2] == ["active", "retained"], j)

# status change
c, j = req("PATCH", S + f"/students/{sE}/status", {"status": "transferred_out", "exit_date": "2026-01-10", "exit_destination": "SMP Lain"}, st)
check("status transferred_out", c == 200, (c, j))
c, j = req("GET", S + f"/students/{sE}/history", token=st)
check("history closed transferred_out", j["data"][0]["status"] == "transferred_out" and j["data"][0]["end_date"] == "2026-01-10", j)
c, j = req("PATCH", S + f"/students/{sE}/status", {"status": "zombie"}, st)
check("invalid status -> 400", c == 400, (c, j))
c, j = req("PATCH", S + f"/students/{sE}/status", {"status": "active"}, st)
c, j2 = req("GET", S + f"/students/{sE}", token=st)
check("back to active clears exit", c == 200 and j2["data"]["status"] == "active" and j2["data"]["exit"]["exit_date"] == "", j2["data"]["exit"])
c, j = req("POST", S + f"/students/{sB}/placement", {"class_id": c7a}, st)
check("place graduated student -> 409 STUDENT_NOT_ACTIVE", c == 409 and j.get("code") == "STUDENT_NOT_ACTIVE", (c, j))

# coordinator follow-ups
c, j = req("GET", S + f"/students/{sF}", token=st)
fx = j["data"]; fx["identity"]["nisn"] = "0011223344"
c, j = req("PUT", S + f"/students/{sF}", {"identity": fx["identity"]}, st)
check("update sets NISN", c == 200 and j["data"]["identity"]["nisn"] == "0011223344", (c, j))
fx["identity"]["nisn"] = None
c, j = req("PUT", S + f"/students/{sF}", {"identity": fx["identity"]}, st)
check("update clears optional NISN (null)", c == 200 and j["data"]["identity"]["nisn"] == "", (c, j))
fx["identity"]["birth_place"] = ""
c, j = req("PUT", S + f"/students/{sF}", {"identity": fx["identity"]}, st)
check("update with empty required birth_place keeps value", c == 200 and j["data"]["identity"]["birth_place"] == "Bogor", (c, j))
c, j = req("POST", S + f"/classes/{c7c}/members", {"student_ids": [sB]}, st)
check("bulk add graduated student -> skipped", c == 200 and j["data"]["placed"] == 0 and len(j["data"]["skipped"]) == 1, (c, j))
c, blob, h = req("GET", S + "/students/export?unassigned=true&status=active", token=st, want_raw=True)
exp_rows = zipfile.ZipFile(io.BytesIO(blob)).read("xl/worksheets/sheet1.xml").decode().count("<row") if c == 200 else -1
c2, j2 = req("GET", S + "/students?unassigned=true&status=active&limit=100", token=st)
check("export honours unassigned filter (= list count)", exp_rows == j2["meta"]["total"] + 1, (exp_rows, j2.get("meta")))
c, j = req("GET", S + "/academic-years", token=st2 if False else st)
check("empty arrays not null (semesters/guardians)", all(isinstance(y["semesters"], list) for y in j["data"]), j)
c, j = req("GET", S + f"/students/{sE}", token=st)
check("achievements/scholarships [] not null", j["data"]["achievements"] == [] and j["data"]["scholarships"] == [], j["data"]["achievements"])

# deletes
c, j = req("DELETE", S + f"/classes/{y2cls['8B']}", token=st)
check("delete class with members -> 409 HAS_STUDENTS", c == 409 and j.get("code") == "HAS_STUDENTS", (c, j))
c, j = req("DELETE", S + f"/classes/{y2cls['VIII-C']}", token=st)
check("delete empty class -> 204", c == 204, (c, j))
c, j = req("DELETE", S + f"/academic-years/{y1id}", token=st)
check("delete year in use -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("DELETE", S + f"/grade-levels/{grades[7]}", token=st)
check("delete grade in use -> 409 IN_USE", c == 409, (c, j))
c, j = req("DELETE", S + f"/majors/{major_id}", token=st)
check("delete unused major -> 204", c == 204, c)
c, j = req("DELETE", S + f"/students/{sD}", token=st)
check("delete student -> 204", c == 204, c)
c, j = req("GET", S + f"/students/{sD}", token=st)
check("deleted student -> 404", c == 404, c)
check("deleted student's active history closed", psql(f"select count(*) from student_class_histories where student_id='{sD}' and status='active'") == "0")

# invariants (DB)
check("index: one active year per school", psql("select count(*) from pg_indexes where indexname='idx_academic_years_school_active_unique'") == "1")
check("index: one active history per student", psql("select count(*) from pg_indexes where indexname='idx_student_class_history_active_unique'") == "1")
check("DB: no school with 2 active years", psql("select count(*) from (select school_id from academic_years where is_active group by 1 having count(*)>1) t") == "0")

# ---------------- tenant isolation ----------------
st2, _ = mk_school("E2E2-" + sfx, "SD", f"sd-{sfx}@e2e.test")
c, j = req("GET", S + f"/students/{sA}", token=st2)
check("other tenant: student -> 404", c == 404, c)
c, j = req("GET", S + f"/students/{sA}/documents", token=st2)
check("other tenant: documents -> 404", c == 404, c)
c, j = req("GET", S + f"/classes/{c7a}/students", token=st2)
check("other tenant: roster -> 404", c == 404, c)
c, j = req("POST", S + f"/students/{sA}/placement", {"class_id": c7a}, st2)
check("other tenant: placement -> 404", c == 404, c)
c, j = req("POST", S + "/classes", {"academic_year_id": y1id, "grade_level_id": grades[7], "name": "HACK"}, st2)
check("other tenant: class on foreign year/grade -> 400", c == 400, (c, j))
c, j = req("POST", S + "/promotions", {"to_academic_year_id": y2id, "items": [{"student_id": sC, "action": "graduate", "force": True}]}, st2)
check("other tenant: promotions foreign year -> 404", c == 404, (c, j))
c, j = req("POST", S + f"/semesters/{genap1}/activate", token=st2)
check("other tenant: activate foreign semester -> 404", c == 404, c)
c, j = req("GET", S + "/students", token=st2)
check("other tenant: empty list", c == 200 and j["meta"]["total"] == 0, j.get("meta"))

# ---------------- teacher role ----------------
temail = f"tteach-{sfx}@e2e.test"
c, j = req("POST", "/auth/admin/users", {"email": temail, "password": "TeacherPass123", "full_name": "Guru Token", "role": "institution"}, admin)
check("admin creates user for teacher token", c == 201, (c, j)); tuid = j.get("data", {}).get("id")
psql(f"update users set role='teacher', email_verified_at=now() where id={tuid}", db="edupilot_auth_db")
psql(f"update employees set user_id={tuid} where id='{teacher_id}'")
c, j = req("POST", "/auth/login", {"email": temail, "password": "TeacherPass123"})
tt = j.get("access_token")
check("teacher login role teacher", c == 200 and j["user"]["role"] == "teacher", (c, j))
for m, p in [("GET", "/students"), ("GET", f"/students/{sA}"), ("POST", "/students"), ("GET", "/classes"), ("GET", f"/classes/{c7a}/students"),
             ("GET", "/academic-years"), ("GET", "/references"), ("GET", "/grade-levels"), ("POST", "/promotions"), ("GET", "/students/export"),
             ("GET", "/document-types"), ("POST", f"/classes/{c7a}/members")]:
    c, j = req(m, S + p, {} if m == "POST" else None, tt)
    check(f"teacher {m} {p} -> 403 FORBIDDEN_ROLE", c == 403 and j.get("code") == "FORBIDDEN_ROLE", (c, j))
c, j = req("GET", S + "/profile", token=tt)
check("teacher GET /profile 200", c == 200, c)

# ---------------- SCHOOL_INACTIVE ----------------
c, j = req("GET", "/instantion/admin/schools?search=E2E-" + sfx, token=admin)
sid = j["data"][0]["id"]
req("PATCH", f"/instantion/admin/schools/{sid}/status", {"is_active": False}, admin)
c, j = req("GET", S + "/students", token=st)
check("inactive school -> 403 SCHOOL_INACTIVE", c == 403 and j.get("code") == "SCHOOL_INACTIVE", (c, j))
req("PATCH", f"/instantion/admin/schools/{sid}/status", {"is_active": True}, admin)

print("\n%d/%d passed" % (sum(1 for _, ok in results if ok), len(results)))
fails = [n for n, ok in results if not ok]
if fails:
    print("FAILED:", fails)
