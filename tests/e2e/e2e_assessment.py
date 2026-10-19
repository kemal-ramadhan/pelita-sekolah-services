# E2E: Asesmen Psikologi (API_CONTRACT §13) — admin catalog, billing, sessions, student flow, scoring, welfare alerts.
# Run from services/:  python3 tests/e2e/e2e_assessment.py
exec(open("tests/e2e/e2e_att_head.py").read())
import base64, datetime

AS = "/assessment"
AD, SCH, STU = AS + "/admin", AS + "/school", AS + "/student"
statuses = []
TMB_KEYS = {i["id"]: i["key"] for i in json.load(open(SVC + "/edupilot-assesment/internal/data/tmb.json"))["part2_items"]}
FORBIDDEN = ["bodoh", "tidak cocok sama sekali", "pasti", "harus", "diagnosis", "gangguan"]
DISCLAIMER_START = "Hasil ini adalah gambaran minat"


def r(method, path, body=None, token=None):
    c, j = req(method, path, body, token)
    statuses.append((c, method, path))
    return c, j


def iso(dt):
    return dt.astimezone(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def claims(tok):
    p = tok.split(".")[1]; p += "=" * (-len(p) % 4)
    return json.loads(base64.urlsafe_b64decode(p))


def teacher_account(emp_id, email, name):
    c, j = req("POST", "/auth/admin/users", {"email": email, "password": "TeacherPass123", "full_name": name, "role": "institution"}, admin)
    uid = j.get("data", {}).get("id")
    psql(f"update users set role='teacher', email_verified_at=now() where id={uid}", db="edupilot_auth_db")
    psql(f"update employees set user_id={uid} where id='{emp_id}'")
    c, j = req("POST", "/auth/login", {"email": email, "password": "TeacherPass123"})
    check(f"teacher login {name}", c == 200 and j.get("access_token"), (c, j))
    return j.get("access_token"), uid


def student_account(student_id, email, name):
    c, j = req("POST", "/auth/register", {"email": email, "password": "StudentPass123", "full_name": name, "app": "students"})
    psql(f"update users set email_otp=123456, email_otp_expires_at=now()+interval '10 minutes', email_otp_attempt=0 where email='{email}'", db="edupilot_auth_db")
    c, j = req("POST", "/auth/verify", {"email": email, "otp": "123456"})
    if c != 200:
        c, j = req("POST", "/auth/verify", {"email": email, "otp": 123456})
    tok = j.get("access_token")
    uid = claims(tok)["user_id"]
    psql(f"update students set user_id={uid}, activated_at=now() where id='{student_id}'")
    check(f"student account linked {name}", c == 200 and tok, (c, j))
    return tok


def mk_student(tok, name, nisn, cls_id):
    b = {"identity": {"full_name": name, "gender": "L", "birth_place": "Bogor", "birth_date": "2010-03-04", "religion": "islam", "nisn": nisn,
                      "nik": "3201" + nisn[-12:].rjust(12, "0")}, "class_id": cls_id}
    c, j = req("POST", S + "/students", b, tok)
    check(f"student create {name}", c == 201, (c, j))
    return j.get("data", {}).get("id")


def no_forbidden(obj):
    s = json.dumps(obj, ensure_ascii=False).lower()
    for blob in [x for x in [obj.get("disclaimer", "")] if x]:
        s = s.replace(json.dumps(blob, ensure_ascii=False)[1:-1].lower(), "")
    return [w for w in FORBIDDEN if w in s]


def wait_done(tok, att):
    for _ in range(60):
        c, j = r("GET", STU + f"/attempts/{att}/status", token=tok)
        st = j.get("data", {}).get("status")
        if st in ("completed", "failed"):
            return st, j.get("data", {}).get("result_id")
        time.sleep(0.5)
    return st, None


now = datetime.datetime.now(datetime.timezone.utc)

# ---------------------------------------------------------------- setup
st = mk_school("ASA-" + sfx, "SMP", f"asa-{sfx}@e2e.test")
stB = mk_school("ASB-" + sfx, "SMP", f"asb-{sfx}@e2e.test")
schoolA = req("GET", S + "/profile", token=st)[1]["data"]["id"]
schoolB = req("GET", S + "/profile", token=stB)[1]["data"]["id"]
yr, grades = setup_school(st, "SMP", "2025/2026")
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": yr["id"], "grade_level_id": grades[9], "names": ["9A", "9B"]}, st)
cls = {x["name"]: x["id"] for x in req("GET", S + "/classes?all=true", token=st)[1].get("data", [])}
c9a, c9b = cls["9A"], cls["9B"]
yrB, gradesB = setup_school(stB, "SMP", "2025/2026")
req("POST", S + "/classes/bulk", {"academic_year_id": yrB["id"], "grade_level_id": gradesB[9], "names": ["9X"]}, stB)
c9x = {x["name"]: x["id"] for x in req("GET", S + "/classes?all=true", token=stB)[1].get("data", [])}["9X"]
X = mk_student(st, "Andi Asesmen", "0098765401", c9a)
Y = mk_student(st, "Bunga Asesmen", "0098765402", c9a)
Z = mk_student(st, "Citra Asesmen", "0098765403", c9b)
W = mk_student(stB, "Dodi Sekolah B", "0098765404", c9x)
BK = mk_teacher(st, "Bu BK Asesmen", "ABK-" + sfx)
TR = mk_teacher(st, "Pak Guru Biasa", "ATR-" + sfx)
c, j = req("POST", S + f"/counseling/teachers/{BK}/toggle", {"is_counselor": True}, st)
check("manager flags Guru BK", c == 200, (c, j))
tBK, uidBK = teacher_account(BK, f"abk-{sfx}@e2e.test", "Bu BK Asesmen")
tTR, _ = teacher_account(TR, f"atr-{sfx}@e2e.test", "Pak Guru Biasa")
sX = student_account(X, f"asx-{sfx}@e2e.test", "Andi Asesmen")
sY = student_account(Y, f"asy-{sfx}@e2e.test", "Bunga Asesmen")
sZ = student_account(Z, f"asz-{sfx}@e2e.test", "Citra Asesmen")
sW = student_account(W, f"asw-{sfx}@e2e.test", "Dodi Sekolah B")

# ---------------------------------------------------------------- admin catalog
print("\n== admin: packages / vouchers / questions ==")
c, j = r("GET", AD + "/packages?page=1&limit=5", token=admin)
check("admin packages paginated", c == 200 and isinstance(j.get("data"), list) and j["meta"]["limit"] == 5, (c, j))
c, j = r("GET", AD + "/packages", token=st)
check("school account on admin routes -> 403", c == 403, (c, j))
base = {"name": "Paket E2E", "description": "x", "price": 100000, "student_quota": 50, "instrument_codes": ["TMB", "RIA", "PMK", "ADN"]}
c, j = r("POST", AD + "/packages", {**base, "code": "E2E-BAD-" + sfx, "instrument_codes": ["ALL"]}, admin)
check("package with instrument ALL -> 400 INVALID_INSTRUMENT", c == 400 and j.get("code") == "INVALID_INSTRUMENT", (c, j))
c, j = r("POST", AD + "/packages", {**base, "code": "E2E-BAD-" + sfx, "student_quota": 0}, admin)
check("package quota 0 -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/packages", {**base, "code": "E2E-BAD-" + sfx, "price": -1}, admin)
check("package negative price -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/packages", {**base, "code": "e2e-full-" + sfx}, admin)
PK_FULL = j.get("data", {}).get("id")
check("create bundle package (code upper-cased, is_bundle computed)", c == 201 and j["data"]["is_bundle"] and j["data"]["code"] == ("E2E-FULL-" + sfx).upper(), (c, j))
c, j = r("POST", AD + "/packages", {**base, "code": "E2E-FULL-" + sfx}, admin)
check("duplicate package code -> 409 DUPLICATE_CODE", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))
c, j = r("POST", AD + "/packages", {**base, "code": "E2E-RIA1-" + sfx, "price": 0, "student_quota": 1, "instrument_codes": ["RIA"]}, admin)
PK_RIA1 = j.get("data", {}).get("id")
check("create free RIA-only package (not bundle)", c == 201 and not j["data"]["is_bundle"], (c, j))
c, j = r("PATCH", AD + f"/packages/{PK_RIA1}/status", {"is_active": False}, admin)
check("PATCH package status {is_active:false}", c == 200 and j["data"]["is_active"] is False, (c, j))
c, j = r("PATCH", AD + f"/packages/{PK_RIA1}/status", {"is_active": True}, admin)
check("PATCH package status {is_active:true}", c == 200 and j["data"]["is_active"] is True, (c, j))
c, j = r("PUT", AD + f"/packages/{PK_RIA1}", {**base, "code": "E2E-FULL-" + sfx, "instrument_codes": ["RIA"]}, admin)
check("update package to another's code -> 409", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))

vb = {"name": "Voucher E2E", "discount_type": "percentage", "discount_value": 50, "max_usage": 5}
c, j = r("POST", AD + "/vouchers", {**vb, "code": "BAD" + sfx, "discount_value": 150}, admin)
check("voucher percentage 150 -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/vouchers", {**vb, "code": "BAD" + sfx, "valid_from": iso(now), "valid_until": iso(now - datetime.timedelta(days=1))}, admin)
check("voucher valid_until <= valid_from -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/vouchers", {**vb, "code": "free" + sfx, "discount_type": "free_claim", "discount_value": 0, "max_usage": 1}, admin)
check("free_claim voucher (discount_value 0 accepted, code upper-cased)", c == 201 and j["data"]["code"] == ("FREE" + sfx).upper(), (c, j))
c, j = r("POST", AD + "/vouchers", {**vb, "code": "PCT" + sfx}, admin)
check("percentage voucher 50%", c == 201, (c, j))
c, j = r("POST", AD + "/vouchers", {**vb, "code": "pct" + sfx}, admin)
check("duplicate voucher code (case-insensitive) -> 409 DUPLICATE_CODE", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))

c, j = r("GET", AD + "/questions?instrument_code=TMB&limit=100", token=admin)
qs = {q["item_code"]: q for q in j.get("data", [])}
check("TMB bank: 48 items, Sosial TMB-S1..S4 + spatial TMB-SP1..SP6, no TMB-S5",
      c == 200 and j["meta"]["total"] == 48 and all(f"TMB-SP{i}" in qs for i in range(1, 7)) and qs["TMB-S1"]["subtest"] == "part1_minat"
      and qs["TMB-SP1"]["subtest"] == "part2_bakat" and "TMB-S5" not in qs, (c, j.get("meta")))
c, j = r("GET", AD + "/questions?instrument_code=RIA&search=RIA-AC&limit=10", token=admin)
check("RIA attention checks: empty dimension, scored=false, expected 3",
      c == 200 and len(j["data"]) == 2 and all(q["dimension"] == "" and q["scored"] is False and q["attention_expected"] == 3 for q in j["data"]), (c, j))
c, j = r("GET", AD + "/questions?instrument_code=PMK&subtest=part_a_activities&limit=1", token=admin)
check("PMK boolean items: scale 0..1 with 2 labels", c == 200 and j["data"][0]["scale_min"] == 0 and j["data"][0]["scale_max"] == 1 and len(j["data"][0]["scale_labels"]) == 2, (c, j))
c, j = r("GET", AD + "/questions?instrument_code=ADN&subtest=module2_wellbeing&limit=20", token=admin)
check("ADN module 2 seeded with subscale dimensions (incl. waspada_kesejahteraan)", c == 200 and len(j["data"]) == 9
      and {q["dimension"] for q in j["data"]} >= {"motivasi", "kecemasan_belajar", "waspada_kesejahteraan"}, (c, j))
qb = {"instrument_code": "RIA", "subtest": "riasec_main", "item_code": "RIA-E2E-" + sfx, "question_text": "Butir uji", "question_type": "likert",
      "scale_min": 1, "scale_max": 5, "scale_labels": ["1", "2", "3", "4", "5"], "dimension": "R"}
c, j = r("POST", AD + "/questions", {**qb, "dimension": "V"}, admin)
check("question invalid dimension -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/questions", {**qb, "scale_labels": ["1", "2"]}, admin)
check("question scale_labels count mismatch -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/questions", {**qb, "subtest": "part2_bakat"}, admin)
check("question subtest of another instrument -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/questions", {"instrument_code": "TMB", "subtest": "part2_bakat", "item_code": "TMB-E2E-" + sfx, "question_text": "x",
                                     "question_type": "multiple_choice", "options": ["a", "b"], "correct_key": "D", "dimension": "S"}, admin)
check("multiple choice correct_key outside options -> 400", c == 400, (c, j))
c, j = r("POST", AD + "/questions", {**qb, "is_active": False}, admin)
QID = j.get("data", {}).get("id")
check("create question (inactive)", c == 201 and j["data"]["is_active"] is False, (c, j))
c, j = r("POST", AD + "/questions", {**qb}, admin)
check("duplicate item_code -> 409 DUPLICATE_CODE", c == 409 and j.get("code") == "DUPLICATE_CODE", (c, j))
c, j = r("DELETE", AD + f"/questions/{QID}", token=admin)
check("delete unused question -> hard delete", c == 200 and j.get("soft_deleted") is False, (c, j))
c, j = r("GET", AD + "/stats", token=admin)
check("admin stats wrapped in data", c == 200 and j["data"]["total_packages"] >= 2 and "pending_subscriptions" in j["data"], (c, j))

# ---------------------------------------------------------------- roles & identity
print("\n== roles & identity ==")
c, j = r("GET", SCH + "/me", token=st)
check("manager /me", c == 200 and j["data"]["kind"] == "manager" and j["data"]["can_manage"] and j["data"]["school_id"] == schoolA, (c, j))
c, j = req("GET", SCH + "/me", token=tBK)
check("Guru BK /me -> counselor", c == 200 and j["data"]["kind"] == "counselor" and not j["data"]["can_manage"], (c, j))
c, j = r("GET", SCH + "/sessions", token=tTR)
check("regular teacher -> 403 NOT_COUNSELOR", c == 403 and j.get("code") == "NOT_COUNSELOR", (c, j))
c, j = r("GET", SCH + "/sessions", token=sX)
check("student on school routes -> 403", c == 403, (c, j))
c, j = r("GET", STU + "/sessions", token=st)
check("manager on student routes -> 403 STUDENT_ONLY", c == 403 and j.get("code") == "STUDENT_ONLY", (c, j))
c, j = r("GET", STU + "/sessions", token=admin)
check("admin on student routes -> 403 STUDENT_ONLY", c == 403 and j.get("code") == "STUDENT_ONLY", (c, j))
# spoofed tenant header is ignored
rq = urllib.request.Request(BASE + SCH + "/me", headers={"Authorization": "Bearer " + st, "X-School-ID": schoolB})
js = json.loads(urllib.request.urlopen(rq).read())
check("X-School-ID header ignored (tenant from principal)", js["data"]["school_id"] == schoolA, js)
c, j = req("GET", "/internal/assessment/principal?user_id=1&role=admin")
check("gateway blocks /internal/* -> 404", c == 404, (c, j))
c, j = req("GET", "/instantion/%2e%2e/internal/assessment/principal?user_id=1&role=admin")
check("path traversal to /internal via gateway -> 404", c == 404, (c, j))

# ---------------------------------------------------------------- billing
print("\n== billing ==")
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": "FREE" + sfx, "package_id": PK_FULL}, tBK)
check("counselor claim -> 403 MANAGER_ONLY", c == 403 and j.get("code") == "MANAGER_ONLY", (c, j))
c, j = r("POST", SCH + "/purchase", {"package_id": PK_FULL}, st)
SUB_PAID = j.get("data", {}).get("id")
check("purchase paid package -> pending (no free quota)", c == 201 and j["data"]["status"] == "pending" and j["data"]["amount_due"] == 100000, (c, j))
c, j = r("GET", SCH + "/subscriptions", token=st)
check("pending purchase not counted in summary", c == 200 and j["summary"]["remaining_quota"] == 0, (c, j))
c, j = r("POST", SCH + "/sessions", {"subscription_id": SUB_PAID, "title": "x", "instrument_codes": ["RIA"], "target_type": "class", "class_ids": [c9a],
                                     "opens_at": iso(now), "closes_at": iso(now + datetime.timedelta(days=1))}, st)
check("session on pending subscription -> 400 INVALID_SUBSCRIPTION", c == 400 and j.get("code") == "INVALID_SUBSCRIPTION", (c, j))
c, j = r("GET", AD + "/subscriptions?status=pending", token=admin)
check("admin sees pending subscription with school_name", c == 200 and any(s["id"] == SUB_PAID and s.get("school_name") for s in j["data"]), (c, j))
c, j = r("PATCH", AD + f"/subscriptions/{SUB_PAID}/status", {"status": "active"}, admin)
check("admin approves purchase -> active", c == 200 and j["data"]["status"] == "active", (c, j))
c, j = r("PATCH", AD + f"/subscriptions/{SUB_PAID}/status", {"status": "rejected"}, admin)
check("re-review non-pending -> 409", c == 409, (c, j))
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": ("free" + sfx), "package_id": PK_FULL}, st)
check("claim free voucher -> active", c == 201 and j["data"]["status"] == "active" and j["data"]["voucher"]["code"] == ("FREE" + sfx).upper(), (c, j))
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": "FREE" + sfx, "package_id": PK_FULL}, st)
check("same school claims same voucher again -> 409 ALREADY_CLAIMED", c == 409 and j.get("code") == "ALREADY_CLAIMED", (c, j))
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": "FREE" + sfx, "package_id": PK_FULL}, stB)
check("voucher max_usage 1 reached -> 409 VOUCHER_EXHAUSTED", c == 409 and j.get("code") == "VOUCHER_EXHAUSTED", (c, j))
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": "PCT" + sfx, "package_id": PK_FULL}, stB)
check("percentage voucher -> pending with discounted amount_due", c == 201 and j["data"]["status"] == "pending" and j["data"]["amount_due"] == 50000, (c, j))
c, j = r("POST", SCH + "/claim-voucher", {"voucher_code": "NOPE" + sfx, "package_id": PK_FULL}, st)
check("unknown voucher -> 400", c == 400, (c, j))
c, j = r("POST", SCH + "/purchase", {"package_id": PK_RIA1}, st)
SUB_RIA1 = j.get("data", {}).get("id")
check("purchase price-0 package -> active", c == 201 and j["data"]["status"] == "active", (c, j))
c, j = r("GET", AD + "/vouchers/logs", token=admin)
check("voucher logs paginated with voucher {code,name}", c == 200 and "meta" in j and any(l["voucher"] and l["voucher"]["code"] == ("FREE" + sfx).upper() for l in j["data"]), (c, j))

# ---------------------------------------------------------------- sessions
print("\n== sessions ==")
c, j = r("GET", SCH + "/classes", token=tBK)
check("class picker (counselor)", c == 200 and {c9a, c9b} <= {x["id"] for x in j["data"]}, (c, j))
c, j = r("GET", SCH + f"/students?class_id={c9a}", token=tBK)
check("student picker for class 9A", c == 200 and {x["student_id"] for x in j["data"]} == {X, Y} and j["meta"]["total"] == 2, (c, j))
sess = {"subscription_id": SUB_PAID, "title": "Asesmen Kelas 9A", "description": "Uji e2e", "instrument_codes": ["RIA", "TMB", "PMK", "ADN"],
        "target_type": "class", "class_ids": [c9a], "opens_at": iso(now - datetime.timedelta(minutes=5)), "closes_at": iso(now + datetime.timedelta(days=1))}
c, j = r("POST", SCH + "/sessions", {**sess, "closes_at": iso(now - datetime.timedelta(days=1))}, tBK)
check("closes_at before opens_at -> 400", c == 400, (c, j))
c, j = r("POST", SCH + "/sessions", {**sess, "class_ids": [c9x]}, tBK)
check("other school's class -> 400 NO_TARGETS", c == 400 and j.get("code") == "NO_TARGETS", (c, j))
c, j = r("POST", SCH + "/sessions", {**sess, "subscription_id": SUB_RIA1}, tBK)
check("instrument not in package -> 400 INVALID_INSTRUMENT", c == 400 and j.get("code") == "INVALID_INSTRUMENT", (c, j))
c, j = r("POST", SCH + "/sessions", {**sess, "subscription_id": SUB_RIA1, "instrument_codes": ["RIA"]}, tBK)
check("2 targets > quota 1 -> 400 QUOTA_EXCEEDED", c == 400 and j.get("code") == "QUOTA_EXCEEDED", (c, j))
c, j = r("POST", SCH + "/sessions", sess, tBK)
SES = j.get("data", {}).get("id")
check("counselor creates class session (4 instruments, 2 targets)", c == 201 and j["data"]["stats"]["targeted"] == 2 and j["data"]["state"] == "open"
      and j["data"]["instrument_codes"] == ["TMB", "RIA", "PMK", "ADN"], (c, j))
c, j = r("POST", SCH + "/sessions", {**sess, "title": "Sesi Mendatang", "instrument_codes": ["RIA"], "opens_at": iso(now + datetime.timedelta(days=1)),
                                     "closes_at": iso(now + datetime.timedelta(days=2))}, st)
SES_UP = j.get("data", {}).get("id")
check("manager creates upcoming session", c == 201 and j["data"]["state"] == "upcoming", (c, j))
c, j = r("POST", SCH + "/sessions", {**sess, "title": "Sesi Siswa Z", "instrument_codes": ["ADN"], "target_type": "students", "student_ids": [Z, W]}, st)
SES_Z = j.get("data", {}).get("id")
check("students target: other school's student silently dropped", c == 201 and j["data"]["stats"]["targeted"] == 1, (c, j))
c, j = r("GET", SCH + "/sessions?page=1&limit=10", token=st)
check("sessions list paginated", c == 200 and j["meta"]["total"] == 3, (c, j))
c, j = r("GET", SCH + f"/sessions/{SES}", token=stB)
check("other school's session -> 404", c == 404, (c, j))

# ---------------------------------------------------------------- student access
print("\n== student access ==")
c, j = r("GET", STU + "/sessions", token=sX)
ids = [s["id"] for s in j.get("data", [])]
check("student X sees own targeted sessions only", c == 200 and set(ids) == {SES, SES_UP} and SES_Z not in ids, (c, j))
mine = [s for s in j.get("data", []) if s["id"] == SES][0] if SES in ids else {}
check("session instruments list with status not_started", [i["status"] for i in mine.get("instruments", [])] == ["not_started"] * 4
      and all(i["total_questions"] > 0 for i in mine["instruments"]), mine)
c, j = r("GET", STU + "/sessions", token=sZ)
check("student Z (9B) sees only the students-target session", c == 200 and [s["id"] for s in j["data"]] == [SES_Z], (c, j))
c, j = r("GET", STU + "/sessions", token=sW)
check("other school's student sees nothing", c == 200 and j["data"] == [], (c, j))
c, j = r("GET", STU + f"/sessions/{SES}/onboarding?instrument=RIA", token=sW)
check("other school's student onboarding -> 404", c == 404, (c, j))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": True}, sW)
check("other school's student start -> 404", c == 404, (c, j))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": True}, sZ)
check("non-target classmate start -> 404", c == 404, (c, j))
c, j = r("GET", STU + f"/sessions/{SES}/onboarding?instrument=RIA", token=sX)
d = j.get("data", {})
check("onboarding: instrument, terms, consent required, examples", c == 200 and d["instrument"]["code"] == "RIA" and d["consent"]["required"]
      and d["consent"]["accepted_at"] is None and d["terms_and_conditions"]["disclaimer"].startswith(DISCLAIMER_START) and d["interactive_examples"], (c, j))
c, j = r("POST", STU + f"/sessions/{SES_UP}/start", {"instrument_code": "RIA", "consent_accepted": True}, sX)
check("upcoming session start -> 409 SESSION_NOT_OPEN", c == 409 and j.get("code") == "SESSION_NOT_OPEN", (c, j))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": False}, sX)
check("start without consent -> 400 CONSENT_REQUIRED", c == 400 and j.get("code") == "CONSENT_REQUIRED", (c, j))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "XYZ", "consent_accepted": True}, sX)
check("start unknown instrument -> 400", c == 400, (c, j))


def used_quota(sub):
    c, j = req("GET", SCH + "/subscriptions", token=st)
    return [s for s in j["data"] if s["id"] == sub][0]["used_quota"]


q0 = used_quota(SUB_PAID)
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": True}, sX)
ria = j.get("data", {})
RIA_ATT = ria.get("attempt", {}).get("id")
qids = [q["id"] for q in ria.get("questions", [])]
check("start RIA -> 201 with 50 questions, attention checks at 17 & 41", c == 201 and len(qids) == 50 and qids[16] == "RIA-AC1" and qids[40] == "RIA-AC2", (c, qids[:20]))
check("no answer keys in attempt payload", "correct_key" not in json.dumps(ria) and ria["sections"] and ria["sections"][0]["scale_labels"], ria.get("sections"))
check("quota consumed on first start", used_quota(SUB_PAID) == q0 + 1, used_quota(SUB_PAID))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": True}, sX)
check("start again -> 200 resume same attempt", c == 200 and j["data"]["attempt"]["id"] == RIA_ATT, (c, j))
c, j = r("GET", STU + f"/sessions/{SES}/onboarding?instrument=RIA", token=sX)
check("consent stored (accepted_at)", c == 200 and j["data"]["consent"]["accepted_at"], (c, j))
check("  consent row in assessment_consents", psql(f"select count(*) from assessment_consents where session_id='{SES}' and student_id='{X}'", db="edupilot_assesment_db") == "1", "")
c, j = r("GET", STU + f"/attempts/{RIA_ATT}", token=sY)
check("other student reads attempt -> 404", c == 404, (c, j))
c, j = r("PUT", STU + f"/attempts/{RIA_ATT}/answers", {"answers": {"RIA-R1": 1}}, sY)
check("other student autosaves attempt -> 404", c == 404, (c, j))

# RIA answers per AC-01
ria_vals = {"I": 5, "A": 4, "S": 3}
answers = {}
for q in ria["questions"]:
    if q["id"].startswith("RIA-AC"):
        answers[q["id"]] = 3
    else:
        answers[q["id"]] = ria_vals.get(q["id"][4], 1)
first = dict(list(answers.items())[:20])
c, j = r("PUT", STU + f"/attempts/{RIA_ATT}/answers", {"answers": first, "rt_ms": {k: 2500 for k in first}}, sX)
check("autosave 20 answers", c == 200 and j["data"]["saved"] == 20, (c, j))
c, j = r("PUT", STU + f"/attempts/{RIA_ATT}/answers", {"answers": first}, sX)
check("autosave idempotent (upsert)", c == 200 and psql(f"select count(*) from assessment_responses where attempt_id='{RIA_ATT}'", db="edupilot_assesment_db") == "20", (c, j))
c, j = r("GET", STU + f"/attempts/{RIA_ATT}", token=sX)
check("resume shows saved answers", c == 200 and len(j["data"]["answers"]) == 20 and j["data"]["answers"]["RIA-AC1"] == 3 if "RIA-AC1" in first else len(j["data"]["answers"]) == 20, (c, len(j.get("data", {}).get("answers", {}))))
c, j = r("PUT", STU + f"/attempts/{RIA_ATT}/answers", {"answers": {"RIA-R1": 9}}, sX)
check("out-of-range answer -> 400 INVALID_ANSWER", c == 400 and j.get("code") == "INVALID_ANSWER", (c, j))
c, j = r("PUT", STU + f"/attempts/{RIA_ATT}/answers", {"answers": {"TMB-M1": 2}}, sX)
check("item of another instrument -> 400", c == 400, (c, j))
c, j = r("POST", STU + f"/attempts/{RIA_ATT}/submit", {"duration_sec": 600}, sX)
exp_missing = 48 - len([k for k in first if not k.startswith("RIA-AC")])
check("submit with >10% missing -> 422 INCOMPLETE + missing list", c == 422 and j.get("code") == "INCOMPLETE" and len(j.get("missing", [])) == exp_missing, (c, j))
psql(f"update assessment_attempts set started_at = now() - interval '20 minutes' where id='{RIA_ATT}'", db="edupilot_assesment_db")
c, j = r("POST", STU + f"/attempts/{RIA_ATT}/submit", {"answers": answers, "duration_sec": 900}, sX)
check("submit -> 202 queued", c == 202 and j["data"]["status"] == "queued", (c, j))
c, j = r("POST", STU + f"/attempts/{RIA_ATT}/submit", {"duration_sec": 900}, sX)
check("double submit -> 409 ALREADY_SUBMITTED", c == 409 and j.get("code") == "ALREADY_SUBMITTED", (c, j))
stt, RIA_RES = wait_done(sX, RIA_ATT)
check("RIA scored by worker -> completed", stt == "completed" and RIA_RES, stt)
c, j = r("GET", STU + f"/attempts/{RIA_ATT}/result", token=sX)
d = j.get("data", {})
check("RIA result per AC-01: IAS, Raw I=40 A=32 S=24, Pct I=100, consistency Tinggi",
      c == 200 and d["holland_code"] == "IAS" and d["scores"]["raw_scores"]["I"] == 40 and d["scores"]["raw_scores"]["A"] == 32
      and d["scores"]["raw_scores"]["S"] == 24 and d["scores"]["percentages"]["I"] == 100 and d["indices"]["consistency"] == "Tinggi", (c, j))
check("  interpretation (type sections, careers by C-index, disclaimer, versions)", d.get("interpretation", {}).get("summary") and len(d["interpretation"]["careers"]) >= 5
      and d["interpretation"]["careers"][0]["c_index"] == 18 and d["disclaimer"].startswith(DISCLAIMER_START) and d["instrument_version"] and d["config_version"], d.get("interpretation"))
check("  no forbidden words in report", not no_forbidden(d), no_forbidden(d))
check("  attempt response rows not duplicated by the worker", psql(f"select count(*) from assessment_responses where attempt_id='{RIA_ATT}'", db="edupilot_assesment_db") == "50", "")
c, j = r("GET", STU + f"/attempts/{RIA_ATT}/result", token=sY)
check("other student reads result -> 404", c == 404, (c, j))
c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": "RIA", "consent_accepted": True}, sX)
check("max_attempts reached -> 409 MAX_ATTEMPTS", c == 409 and j.get("code") == "MAX_ATTEMPTS", (c, j))


def run_instrument(tok, code, answer_fn, section=None):
    c, j = r("POST", STU + f"/sessions/{SES}/start", {"instrument_code": code, "consent_accepted": True}, tok)
    att = j.get("data", {})
    aid = att.get("attempt", {}).get("id")
    ans = {q["id"]: answer_fn(q) for q in att.get("questions", [])}
    psql(f"update assessment_attempts set started_at = now() - interval '25 minutes' where id='{aid}'", db="edupilot_assesment_db")
    body = {"answers": ans, "duration_sec": 1200}
    if section:
        body["section_started"] = section
    c2, j2 = r("POST", STU + f"/attempts/{aid}/submit", body, tok)
    stt, rid = wait_done(tok, aid)
    c3, j3 = r("GET", STU + f"/attempts/{aid}/result", token=tok)
    return c, att, c2, stt, j3.get("data", {}), aid


# TMB per AC-07: all interests 4; N & L correct; V & SP wrong
def tmb_ans(q):
    if q["type"] == "likert":
        return 4
    k = TMB_KEYS[q["id"]]
    if q["id"].startswith("TMB-N") or q["id"].startswith("TMB-L"):
        return k
    return "A" if k != "A" else "B"


c, att, c2, stt, d, TMB_ATT = run_instrument(sX, "TMB", tmb_ans, section="part2_bakat")
sec = {s["key"]: s for s in att.get("sections", [])}
check("TMB start: bakat section 1200 s, aptitude fixed order", c == 201 and sec.get("part2_bakat", {}).get("time_limit_seconds") == 1200
      and [q["id"] for q in att["questions"]][24] == "TMB-V1", (c, att.get("sections")))
check("quota consumed once per student per session (TMB start)", used_quota(SUB_PAID) == q0 + 1, used_quota(SUB_PAID))
check("TMB result per AC-07: IPA = 100, recommended IPA, IPS < IPA", stt == "completed" and d["recommended_track"] == "IPA" and d["scores"]["rumpun_scores"]["IPA"] == 100
      and d["scores"]["rumpun_scores"]["IPS"] < 100 and d["scores"]["minat_scores"]["M"] == 100 and d["scores"]["bakat_scores"]["SP" if False else "S"] == 0, (stt, d.get("scores")))
check("  TMB interpretation & no forbidden words", d.get("interpretation", {}).get("sections") and not no_forbidden(d), no_forbidden(d))

# PMK: totals S=22, A=15, E=14 → SAE
pmk_a, pmk_b, pmk_c = {"S": 3, "A": 2, "E": 2}, {"S": 3, "A": 2, "E": 1}, {"S": 3, "E": 2, "A": 1}
pmk_d = {"S": (7, 6), "A": (5, 5), "E": (5, 4)}


def pmk_ans(q):
    i = q["id"]
    if i.startswith("PMK-D"):
        t = i[-1]; row = int(i[5]) - 1
        return pmk_d.get(t, (1, 1))[row]
    part, t, n = i[4], i[6], int(i[7])
    lim = {"A": pmk_a, "B": pmk_b, "C": pmk_c}[part].get(t, 0)
    return n <= lim


c, att, c2, stt, d, PMK_ATT = run_instrument(sX, "PMK", pmk_ans)
check("PMK boolean answers accepted as JSON true/false", c == 201 and c2 == 202, (c, c2))
check("PMK result: code SAE, totals S=22 A=15 E=14", stt == "completed" and d["holland_code"] == "SAE" and d["scores"]["total_scores"]["S"] == 22
      and d["scores"]["total_scores"]["A"] == 15 and d["scores"]["total_scores"]["E"] == 14 and len(d["interpretation"]["careers"]) >= 5, (stt, d.get("scores")))
check("  PMK summary uses the career-code template", "Kode kariermu SAE" in d.get("interpretation", {}).get("summary", "") and not no_forbidden(d), d.get("interpretation", {}).get("summary"))

# ADN per AC-10 (V=17, A=12, R=10, K=9) + WEL1 = 4 (AC-12) + brief S/E
vark_vals = {"V": [4, 4, 3, 3, 3], "A": [3, 3, 2, 2, 2], "R": [2, 2, 2, 2, 2], "K": [2, 2, 2, 2, 1]}


def adn_ans(q):
    i = q["id"]
    if len(i) == 6 and i[4] in "VARK":
        return vark_vals[i[4]][int(i[5]) - 1]
    if i == "ADN-WEL1":
        return 4
    if i.startswith("ADN-K") and len(i) == 7:  # brief career ADN-KR1..KC2
        return {"S": 4, "E": 3}.get(i[5], 1)
    return 3


c, att, c2, stt, d, ADN_ATT = run_instrument(sX, "ADN", adn_ans)
check("ADN result (student): Visual dominan, brief SE", stt == "completed" and d["vark_category"] == "Visual dominan" and d["scores"]["vark_scores"]["V"] == 17
      and d["holland_code"] == "SE", (stt, d.get("vark_category"), d.get("scores")))
check("  student view: no FLAG_KESEJAHTERAAN / WEL1, neutral wellbeing_message, learning-style note",
      "FLAG_KESEJAHTERAAN" not in json.dumps(d) and "WEL1" not in json.dumps(d) and "staff_notes" not in json.dumps(d)
      and d.get("wellbeing_message") == "Terima kasih sudah menjawab dengan jujur. Guru BK siap mengobrol bila kamu mau."
      and "Gaya belajar adalah kecenderungan" in json.dumps(d, ensure_ascii=False), d)
check("  ADN no forbidden words", not no_forbidden(d), no_forbidden(d))

c, j = r("GET", STU + "/sessions", token=sX)
mine = [s for s in j["data"] if s["id"] == SES][0]
check("session instruments all completed with result ids", all(i["status"] == "completed" and i["result_id"] for i in mine["instruments"]), mine)
c, j = r("GET", STU + "/results", token=sX)
check("student result history (4 rows, session_title)", c == 200 and j["meta"]["total"] == 4 and all(x["session_title"] == "Asesmen Kelas 9A" for x in j["data"]), (c, j))

# student Y starts ADN (second quota unit), flat answers
c, att, c2, stt, dY, Y_ATT = run_instrument(sY, "ADN", lambda q: 2)
check("student Y ADN completed, no welfare flag", stt == "completed" and "wellbeing_message" not in dY, (stt, dY.get("flags")))
check("quota used = 2 (one per student per session)", used_quota(SUB_PAID) == q0 + 2, used_quota(SUB_PAID))

# ---------------------------------------------------------------- staff views
print("\n== staff: results, alerts, analytics ==")
c, j = r("GET", SCH + "/alerts?status=open", token=tBK)
alerts = j.get("data", [])
check("welfare alert visible to counselor", c == 200 and len(alerts) == 1 and alerts[0]["student_id"] == X and alerts[0]["flag"] == "FLAG_KESEJAHTERAAN", (c, j))
ALERT = alerts[0]["id"] if alerts else None
c, j = r("GET", SCH + "/alerts", token=st)
check("welfare alert visible to manager", c == 200 and j["meta"]["total"] == 1, (c, j))
c, j = r("GET", SCH + "/alerts", token=tTR)
check("regular teacher alerts -> 403", c == 403, (c, j))
c, j = r("GET", SCH + "/alerts", token=stB)
check("other school has no alerts", c == 200 and j["data"] == [], (c, j))
c, j = r("PATCH", SCH + f"/alerts/{ALERT}", {"status": "acknowledged", "note": "Sudah dihubungi"}, stB)
check("other school PATCH alert -> 404", c == 404, (c, j))
c, j = r("PATCH", SCH + f"/alerts/{ALERT}", {"status": "acknowledged", "note": "Sudah dihubungi"}, tBK)
check("counselor acknowledges alert", c == 200 and j["data"]["status"] == "acknowledged" and j["data"]["handled_by_name"] == "Bu BK Asesmen", (c, j))
c, j = r("PATCH", SCH + f"/alerts/{ALERT}", {"status": "done"}, tBK)
check("invalid alert status -> 400", c == 400, (c, j))
time.sleep(1)
check("counselor notified (notifications row, no answers in body)",
      psql(f"select count(*) from notifications where user_id={uidBK} and type='assessment_welfare_alert' and body not ilike '%WEL1%' and body not ilike '%Andi%'") == "1", "")

c, j = r("GET", SCH + f"/results?session_id={SES}", token=tBK)
check("results list for session (5 rows)", c == 200 and j["meta"]["total"] == 5 and all("report_summary" in x for x in j["data"]), (c, j))
c, j = r("GET", SCH + f"/results?flag=FLAG_KESEJAHTERAAN", token=st)
check("filter by flag", c == 200 and j["meta"]["total"] == 1 and j["data"][0]["instrument_code"] == "ADN", (c, j))
c, j = r("GET", SCH + f"/results?instrument_code=RIA&search=Andi", token=st)
check("filter by instrument + search", c == 200 and j["meta"]["total"] == 1 and j["data"][0]["holland_code"] == "IAS", (c, j))
adn_res = [x for x in req("GET", SCH + f"/results?instrument_code=ADN&search=Andi", token=st)[1]["data"]][0]["id"]
before = psql("select count(*) from assessment_audit_logs where action='view_result'", db="edupilot_assesment_db")
c, j = r("GET", SCH + f"/results/{adn_res}", token=tBK)
d = j.get("data", {})
check("staff result detail keeps FLAG_KESEJAHTERAAN + staff notes + student info", c == 200 and "FLAG_KESEJAHTERAAN" in d["flags"] and d["staff_notes"]
      and d["student"]["full_name"] == "Andi Asesmen" and d["student_name"] == "Andi Asesmen" and d["scores"]["wellbeing_items"]["ADN-WEL1"] == 4, (c, j))
after = psql("select count(*) from assessment_audit_logs where action='view_result'", db="edupilot_assesment_db")
check("individual result read is audited", int(after) == int(before) + 1, (before, after))
c, j = r("GET", SCH + f"/results/{adn_res}", token=stB)
check("other school's result -> 404", c == 404, (c, j))
c, j = r("GET", SCH + f"/results/{adn_res}", token=tTR)
check("regular teacher result -> 403", c == 403, (c, j))
code, blob, hdrs = req("GET", SCH + f"/results/export?session_id={SES}", token=st, want_raw=True)
statuses.append((code, "GET", "export"))
txt = blob.decode("utf-8-sig", "replace")
check("CSV export (header + 5 rows, audited)", code == 200 and "text/csv" in hdrs.get("Content-Type", "") and len(txt.strip().splitlines()) == 6 and "IAS" in txt
      and psql("select count(*) from assessment_audit_logs where action='export_results'", db="edupilot_assesment_db") == "1", (code, txt[:300]))
c, j = r("GET", SCH + f"/analytics?session_id={SES}", token=tBK)
a = j.get("data", {})
check("analytics computed from data", c == 200 and a["total_participants"] == 2 and a["vark_distribution"]["Visual"] == 1 and a["by_instrument"]["ADN"] == 2
      and a["riasec_primary"]["I"] == 1 and a["riasec_primary"]["S"] == 1 and a["track_distribution"].get("IPA") == 1 and a["welfare_alerts_open"] == 1
      and a["insights"]["primary_learning_style"].startswith(("Visual", "Multimodal", "Belum")) and a["socio_emotional"]["motivasi"], (c, j))
c, j = r("GET", SCH + "/analytics", token=stB)
check("analytics of empty school: zeros + data-derived insight", c == 200 and j["data"]["total_participants"] == 0 and "Belum ada data" in j["data"]["insights"]["primary_learning_style"], (c, j))
c, j = r("GET", SCH + f"/sessions/{SES}", token=st)
d = j.get("data", {})
tx = [t for t in d.get("targets", []) if t["student_id"] == X]
check("session detail targets + stats", c == 200 and d["stats"]["started"] == 2 and d["stats"]["completed"] == 1 and d["stats"]["flagged"] >= 1
      and tx and tx[0]["status"] == "completed" and tx[0]["instruments"]["RIA"] == "completed", (c, d.get("stats"), tx))

# ---------------------------------------------------------------- session lifecycle
print("\n== session lifecycle ==")
c, j = r("DELETE", SCH + f"/sessions/{SES}", token=st)
check("delete session with attempts -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = r("DELETE", SCH + f"/sessions/{SES_UP}", token=st)
check("delete session without attempts -> 200", c == 200, (c, j))
c, j = r("PATCH", SCH + f"/sessions/{SES_Z}/close", token=tBK)
check("close session", c == 200 and j["data"]["status"] == "closed" and j["data"]["state"] == "closed", (c, j))
c, j = r("POST", STU + f"/sessions/{SES_Z}/start", {"instrument_code": "ADN", "consent_accepted": True}, sZ)
check("start on closed session -> 404/409", c in (404, 409), (c, j))
c, j = r("GET", STU + "/sessions", token=sZ)
check("closed session hidden from student list", c == 200 and j["data"] == [], (c, j))

fives = [s for s in statuses if s[0] >= 500]
check("no 5xx responses in this run", not fives, fives)

passed = sum(1 for _, ok in results if ok)
print(f"\nRESULT {passed}/{len(results)} passed")
for n, ok in results:
    if not ok: print("FAILED:", n)
