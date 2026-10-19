# E2E: schedule archive bug (FK from class_teaching_sessions) + Bimbingan Konseling (API_CONTRACT §12).
# Run from services/:  python3 tests/e2e/e2e_counseling.py
exec(open("tests/e2e/e2e_att_head.py").read())
import base64, datetime
from zoneinfo import ZoneInfo

WIB = ZoneInfo("Asia/Jakarta")
now = datetime.datetime.now(WIB)
TODAY = now.strftime("%Y-%m-%d")
TOMORROW = (now + datetime.timedelta(days=1)).strftime("%Y-%m-%d")
YESTERDAY = (now - datetime.timedelta(days=1)).strftime("%Y-%m-%d")
DOW = now.isoweekday()
A = "/instantion/school/attendance"
B = S + "/counseling"
STU = "/instantion/student"
SC = STU + "/counseling"
statuses = []  # every response code seen, to assert "no 500"


def r(method, path, body=None, token=None):
    c, j = req(method, path, body, token)
    statuses.append((c, method, path))
    return c, j


def hm(dt):
    return dt.strftime("%H:%M")


def claims(tok):
    p = tok.split(".")[1]; p += "=" * (-len(p) % 4)
    return json.loads(base64.urlsafe_b64decode(p))


def psql_err(sql, db="edupilot_instantion_db"):
    p = subprocess.run(COMPOSE + ["exec", "-T", "db_auth", "psql", "-U", "postgres", "-d", db, "-v", "ON_ERROR_STOP=1", "-tAc", sql], capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def teacher_account(emp_id, email, name):
    c, j = req("POST", "/auth/admin/users", {"email": email, "password": "TeacherPass123", "full_name": name, "role": "institution"}, admin)
    uid = j.get("data", {}).get("id")
    psql(f"update users set role='teacher', email_verified_at=now() where id={uid}", db="edupilot_auth_db")
    psql(f"update employees set user_id={uid} where id='{emp_id}'")
    c, j = req("POST", "/auth/login", {"email": email, "password": "TeacherPass123"})
    check(f"teacher login {name}", c == 200 and j.get("access_token"), (c, j))
    return j.get("access_token")


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


st = mk_school("BKA-" + sfx, "SMP", f"bka-{sfx}@e2e.test")
stB = mk_school("BKB-" + sfx, "SMP", f"bkb-{sfx}@e2e.test")
BK1 = mk_teacher(st, "Bu Konselor Satu", "BK1-" + sfx)
BK2 = mk_teacher(st, "Pak Konselor Dua", "BK2-" + sfx)
TR = mk_teacher(st, "Guru Biasa", "TR-" + sfx)
BKB = mk_teacher(stB, "Konselor Sekolah B", "BKB-" + sfx)
yr, grades = setup_school(st, "SMP", "2025/2026")
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": yr["id"], "grade_level_id": grades[7], "names": ["7A", "7B", "7C"]}, st)
c, j = req("GET", S + "/classes?all=true", token=st)
cls = {x["name"]: x["id"] for x in j.get("data", [])}
c7a, c7b, c7c = cls["7A"], cls["7B"], cls["7C"]
c, j = req("POST", S + "/subjects/preset", {"preset": "SMP"}, st)
codes = {x["subject_code"]: x["id"] for x in j.get("data", [])}
MAT, IPA = codes["MAT"], codes["IPA"]


def mk_student(tok, name, nisn, cls_id):
    b = {"identity": {"full_name": name, "gender": "L", "birth_place": "Bogor", "birth_date": "2012-03-04", "religion": "islam", "nisn": nisn, "nik": "3201" + nisn[-12:].rjust(12, "0")}, "class_id": cls_id}
    c, j = req("POST", S + "/students", b, tok)
    check(f"student create {name}", c == 201, (c, j))
    return j.get("data", {}).get("id")


X = mk_student(st, "Andi Pratama", "0012345601", c7a)
Y = mk_student(st, "Bayu Saputra", "0012345602", c7a)
Z = mk_student(st, "Citra Lestari", "0012345603", c7b)

# =====================================================================
# (A) Schedule delete bug: schedule with teaching sessions
# =====================================================================
print("\n== schedule archive ==")
c, j = r("POST", S + "/schedule-periods", {"name": "Jam Malam", "period_order": 90, "start_time": "23:00", "end_time": "23:45"}, st)
check("late-night period 23:00-23:45", c == 201, (c, j)); PN = j.get("data", {}).get("id")
c, j = r("POST", S + "/schedules", {"class_id": c7c, "subject_id": IPA, "teacher_id": TR, "day_of_week": DOW, "schedule_period_id": PN}, st)
check("schedule on 23:00 period", c == 201 and j["data"]["start_time"] == "23:00", (c, j)); SN = j.get("data", {}).get("id")
# The user tested attendance at night: simulate the session row a 23:00 teaching session leaves behind.
psql(f"""insert into class_teaching_sessions (school_id, schedule_id, date, class_id, subject_id, teacher_id, scheduled_start_time, scheduled_end_time, status, teacher_check_in_time)
         select school_id, id, '{YESTERDAY}', class_id, subject_id, employee_id, start_time, end_time, 'completed', '23:01:00' from teacher_schedules where id='{SN}'""")
rc, out = psql_err(f"delete from teacher_schedules where id='{SN}'")
check("repro: raw hard delete violates FK 23503 (root cause of the old 500)", rc != 0 and "foreign key" in out.lower(), out[:300])
c, j = r("DELETE", S + f"/schedules/{SN}", token=st)
check("DELETE 23:00 schedule with session history -> 200 (was 500)", c == 200, (c, j))
check("  schedule archived (deleted_at set), session kept",
      psql(f"select count(*) from teacher_schedules where id='{SN}' and deleted_at is not null") == "1"
      and psql(f"select count(*) from class_teaching_sessions where schedule_id='{SN}'") == "1", "")
c, j = r("GET", S + f"/schedules?class_id={c7c}", token=st)
check("  archived schedule hidden from list", c == 200 and SN not in [x["id"] for x in j["data"]], (c, j))
c, j = r("GET", S + f"/schedules/grid?class_id={c7c}", token=st)
check("  archived schedule hidden from grid", c == 200 and SN not in json.dumps(j), "")
c, j = r("DELETE", S + f"/schedules/{SN}", token=st)
check("  delete again -> 404", c == 404, (c, j))
c, j = r("POST", S + "/schedules/check-conflict", {"class_id": c7c, "subject_id": IPA, "teacher_id": TR, "day_of_week": DOW, "schedule_period_id": PN}, st)
check("  archived schedule no longer conflicts", c == 200 and j["data"]["has_conflict"] is False, (c, j))
c, j = r("DELETE", S + f"/schedule-periods/{PN}", token=st)
check("  period used only by archived schedule -> deletable 200", c == 200, (c, j))

# live session today via API, then delete schedule
s_start = hm(now - datetime.timedelta(minutes=5)); s_end = hm(now + datetime.timedelta(minutes=50))
if s_end <= s_start:
    s_start, s_end = "00:00", "23:59"
c, j = r("POST", S + "/schedules", {"class_id": c7c, "subject_id": MAT, "teacher_id": TR, "day_of_week": DOW, "start_time": s_start, "end_time": s_end}, st)
check("schedule now", c == 201, (c, j)); SNOW = j.get("data", {}).get("id")
c, j = r("POST", A + f"/teaching/start?employee_id={TR}", {"schedule_id": SNOW, "topic": "Aljabar"}, st)
SES = j.get("data", {}).get("id")
check("manager starts teaching session", c == 200 and SES, (c, j))
c, j = r("DELETE", S + f"/schedules/{SNOW}", token=st)
check("DELETE schedule with today's session -> 200", c == 200, (c, j))
c, j = r("GET", A + f"/teaching/{SES}", token=st)
d = j.get("data", {})
check("  session history still resolves subject/class/schedule", c == 200 and d["session"]["subject"]["name"] and d["session"]["class"]["name"] == "7C"
      and d["schedule"] and d["schedule"]["id"] == SNOW, (c, j))
c, j = r("GET", A + f"/teaching/today?employee_id={TR}", token=st)
check("  teaching/today ignores archived schedule", c == 200 and SNOW not in [x.get("schedule_id") for x in j.get("data", [])], (c, j))
c, j = r("GET", A + "/live-monitor", token=st)
check("  live monitor ignores archived schedule", c == 200 and SNOW not in json.dumps(j), "")
c, j = r("POST", S + "/schedules", {"class_id": c7c, "subject_id": MAT, "teacher_id": TR, "day_of_week": DOW, "start_time": s_start, "end_time": s_end}, st)
check("  same slot can be re-created", c == 201, (c, j)); SNOW2 = j.get("data", {}).get("id")
c, j = r("POST", A + f"/teaching/start?employee_id={TR}", {"schedule_id": SNOW2}, st)
# teacher form replacement drops it -> archived, not 500
c, j = r("GET", S + f"/teachers/{TR}", token=st)
tdet = j.get("data", {})
c, j = r("PUT", S + f"/teachers/{TR}", {"full_name": "Guru Biasa", "employee_code": "TR-" + sfx, "schedules": []}, st)
check("teacher form clears schedules (one with session) -> 200", c == 200, (c, j))
check("  form-dropped schedule archived", psql(f"select count(*) from teacher_schedules where id='{SNOW2}' and deleted_at is not null") == "1", "")
# class delete with archived schedules
c, j = r("POST", S + "/schedules", {"class_id": c7c, "subject_id": IPA, "teacher_id": BK2, "day_of_week": DOW % 7 + 1, "start_time": "08:00", "end_time": "08:40"}, st)
SPLAIN = j.get("data", {}).get("id")
c, j = r("DELETE", S + f"/classes/{c7c}", token=st)
check("class delete (archived + plain schedules) -> 2xx", c in (200, 204), (c, j))
check("  unreferenced schedule hard-deleted", psql(f"select count(*) from teacher_schedules where id='{SPLAIN}'") == "0", "")
c, j = r("GET", A + f"/teaching/{SES}", token=st)
check("  session detail after class delete still has class name", c == 200 and j["data"]["session"]["class"]["name"] == "7C", (c, j))
# generic FK mapping: deleting something still referenced -> 409 IN_USE (never 500)
c, j = r("DELETE", S + f"/subjects/{MAT}", token=st)
check("subject delete with only archived schedules -> 200 (soft delete)", c == 200, (c, j))

# =====================================================================
# (B) Bimbingan Konseling
# =====================================================================
print("\n== BK setup ==")
tBK1 = teacher_account(BK1, f"bk1-{sfx}@e2e.test", "Bu Konselor Satu")
tBK2 = teacher_account(BK2, f"bk2-{sfx}@e2e.test", "Pak Konselor Dua")
tTR = teacher_account(TR, f"tr-{sfx}@e2e.test", "Guru Biasa")
tBKB = teacher_account(BKB, f"cnsb-{sfx}@e2e.test", "Konselor Sekolah B")
skX = student_account(X, f"sx-{sfx}@e2e.test", "Andi Pratama")
skY = student_account(Y, f"sy-{sfx}@e2e.test", "Bayu Saputra")
skZ = student_account(Z, f"sz-{sfx}@e2e.test", "Citra Lestari")

FAKE = str(uuid.uuid4())
school_routes = [
    ("GET", "/summary"), ("GET", "/counselors"), ("GET", "/students"), ("GET", "/sessions"), ("POST", "/sessions"), ("GET", f"/sessions/{FAKE}"),
    ("PUT", f"/sessions/{FAKE}/status"), ("POST", f"/sessions/{FAKE}/outcome"), ("GET", "/events"), ("POST", "/events"),
    ("GET", f"/events/{FAKE}"), ("POST", f"/events/{FAKE}/attend"), ("POST", f"/events/{FAKE}/conclusion"),
    ("GET", "/journals"), ("POST", "/journals"), ("PUT", f"/journals/{FAKE}"), ("DELETE", f"/journals/{FAKE}"),
]
print("\n== access: regular teacher / before flag ==")
bad = []
for m, p in school_routes:
    c, j = r(m, B + p, {} if m in ("POST", "PUT") else None, tTR)
    if not (c == 403 and j.get("code") == "NOT_COUNSELOR"):
        bad.append((m, p, c, j.get("code")))
check(f"regular teacher -> 403 NOT_COUNSELOR on all {len(school_routes)} counseling routes", not bad, bad)
c, j = r("POST", B + f"/teachers/{TR}/toggle", {"is_counselor": True}, tTR)
check("regular teacher toggle -> 403", c == 403, (c, j))
c, j = r("GET", B + "/sessions", token=tBK1)
check("BK1 before being flagged -> 403", c == 403 and j.get("code") == "NOT_COUNSELOR", (c, j))

print("\n== toggle ==")
c, j = r("POST", B + f"/teachers/{BK1}/toggle", {"is_counselor": True}, st)
check("manager toggles BK1 -> is_counselor true", c == 200 and j["data"]["is_counselor"] is True, (c, j))
c, j = r("POST", B + f"/teachers/{BK2}/toggle", {"is_counselor": True}, st)
check("manager toggles BK2", c == 200 and j["data"]["is_counselor"] is True, (c, j))
c, j = r("POST", B + f"/teachers/{BKB}/toggle", {"is_counselor": True}, st)
check("toggle teacher of another school -> 404", c == 404, (c, j))
c, j = r("POST", B + f"/teachers/{BKB}/toggle", {"is_counselor": True}, stB)
check("school B toggles own counselor", c == 200, (c, j))
c, j = r("POST", B + f"/teachers/{FAKE}/toggle", {"is_counselor": True}, st)
check("toggle unknown -> 404", c == 404, (c, j))
c, j = r("POST", B + f"/teachers/{BK2}/toggle", {"is_counselor": True}, tBK1)
check("counselor toggle -> 403 (managers only)", c == 403, (c, j))
c, j = r("GET", S + "/me", token=tBK1)
check("BK1 /me is_counselor true", c == 200 and j["data"]["is_counselor"] is True, (c, j))

print("\n== student: counselors & peers ==")
c, j = r("GET", SC + "/counselors", token=skX)
check("student counselors -> {id, full_name} only", c == 200 and {x["id"] for x in j["data"]} == {BK1, BK2}
      and all(set(x) == {"id", "full_name"} for x in j["data"]), (c, j))
c, j = r("GET", SC + "/peers", token=skX)
check("peers default -> active classmates only, minimal keys", c == 200 and [x["id"] for x in j["data"]] == [Y]
      and set(j["data"][0]) == {"id", "full_name", "class_name"} and j["data"][0]["class_name"] == "7A", (c, j))
c, j = r("GET", SC + "/peers?search=Citra", token=skX)
check("peers search -> schoolmate in other class, no counseling/personal data", c == 200 and [x["id"] for x in j["data"]] == [Z]
      and "nik" not in json.dumps(j) and "counsel" not in json.dumps(j), (c, j))
c, j = r("GET", SC + "/peers?search=Andi", token=skX)
check("peers never include self", c == 200 and X not in [x["id"] for x in j["data"]], (c, j))

print("\n== student requests ==")
slot = {"requested_date": TOMORROW, "start_time": "09:00", "end_time": "10:00"}
c, j = r("POST", SC + "/sessions", {"counselor_id": BK1, "counseling_type": "individual", "topic": "Motivasi belajar", "description": "Cerita pribadi Andi", **slot}, skX)
s1 = j.get("data", {}); S1 = s1.get("id")
check("X requests individual with BK1 -> 201 pending", c == 201 and s1["status"] == "pending" and s1["counselor"]["id"] == BK1
      and set(s1["counselor"]) == {"id", "full_name"} and "confidential_notes" not in json.dumps(j) and s1["is_initiator"] is True, (c, j))
c, j = r("POST", SC + "/sessions", {"counseling_type": "individual", "topic": "x", "participant_student_ids": [Y], **slot}, skX)
check("individual with participants -> 400", c == 400, (c, j))
c, j = r("POST", SC + "/sessions", {"counseling_type": "group", "topic": "x", **slot}, skX)
check("group without peers -> 400", c == 400, (c, j))
c, j = r("POST", SC + "/sessions", {"counseling_type": "individual", "topic": "x", "requested_date": YESTERDAY, "start_time": "09:00", "end_time": "10:00"}, skX)
check("past date -> 400", c == 400, (c, j))
c, j = r("POST", SC + "/sessions", {"counseling_type": "classical", "topic": "x", **slot}, skX)
check("student classical -> 400", c == 400, (c, j))
c, j = r("POST", SC + "/sessions", {"counselor_id": TR, "counseling_type": "individual", "topic": "x", **slot}, skX)
check("counselor_id = non-counselor teacher -> 404", c == 404, (c, j))
c, j = r("POST", SC + "/sessions", {"counselor_id": BKB, "counseling_type": "individual", "topic": "x", **slot}, skX)
check("counselor_id of another school -> 404", c == 404, (c, j))
c, j = r("POST", SC + "/sessions", {"counseling_type": "group", "topic": "x", "participant_student_ids": [FAKE], **slot}, skX)
check("unknown participant -> 400", c == 400, (c, j))
c, j = r("POST", SC + "/sessions", {"counselor_id": BK2, "counseling_type": "group", "topic": "Kerja kelompok", "description": "Rahasia pengaju", "participant_student_ids": [Y], **slot}, skX)
S2 = j.get("data", {}).get("id")
check("X requests group with Y (BK2) -> 201", c == 201 and len(j["data"]["participants"]) == 2, (c, j))

c, j = r("GET", SC + "/sessions", token=skX)
check("X lists own 2 sessions (+total/meta)", c == 200 and {x["id"] for x in j["data"]} == {S1, S2} and j["total"] == 2 and j["meta"]["total"] == 2, (c, j))
c, j = r("GET", SC + "/sessions", token=skY)
check("Y sees only the group session (participant), description hidden", c == 200 and [x["id"] for x in j["data"]] == [S2]
      and j["data"][0]["description"] == "" and j["data"][0]["is_initiator"] is False, (c, j))
c, j = r("GET", SC + f"/sessions/{S1}", token=skY)
check("Y GET X's individual session -> 404", c == 404, (c, j))
c, j = r("GET", SC + f"/sessions/{S1}", token=skZ)
check("Z GET X's session -> 404", c == 404, (c, j))
c, j = r("GET", SC + "/sessions", token=skZ)
check("Z list -> []", c == 200 and j["data"] == [], (c, j))

print("\n== manager access ==")
c, j = r("GET", B + "/sessions", token=st)
check("manager sessions list -> 403 COUNSELOR_ONLY", c == 403 and j.get("code") == "COUNSELOR_ONLY", (c, j))
c, j = r("GET", B + f"/sessions/{S1}", token=st)
check("manager session detail -> 403", c == 403, (c, j))
c, j = r("POST", B + f"/sessions/{S1}/outcome", {"confidential_notes": "x"}, st)
check("manager outcome -> 403", c == 403, (c, j))
c, j = r("GET", B + "/journals", token=st)
check("manager journals -> 403", c == 403, (c, j))
c, j = r("GET", B + "/summary", token=st)
check("manager summary -> school-wide counts only", c == 200 and j["pending_requests_count"] == 2 and j["total_counselors"] == 2, (c, j))
c, j = r("GET", B + "/counselors", token=st)
check("manager counselors list", c == 200 and {x["id"] for x in j["data"]} == {BK1, BK2}, (c, j))

print("\n== counselor student picker ==")
c, j = r("GET", B + "/students?search=andi", token=tBK1)
check("BK1 student picker search -> minimal {id, full_name, nis, class{id,name}}", c == 200 and [x["id"] for x in j["data"]] == [X]
      and set(j["data"][0]) == {"id", "full_name", "nis", "class"} and j["data"][0]["class"] == {"id": c7a, "name": "7A"} and j["total"] == 1, (c, j))
c, j = r("GET", B + f"/students?class_id={c7a}&limit=500", token=tBK1)
check("  class filter + limit capped at 50", c == 200 and {x["id"] for x in j["data"]} == {X, Y} and j["meta"]["limit"] == 50, (c, j))
c, j = r("GET", B + "/students", token=st)
check("  manager picker -> 403", c == 403, (c, j))
c, j = r("GET", B + "/students", token=tBKB)
check("  other school counselor sees none of A's students", c == 200 and X not in json.dumps(j), (c, j))

print("\n== counselor workflow ==")
c, j = r("GET", B + "/sessions", token=tBK1)
check("BK1 lists only own sessions", c == 200 and [x["id"] for x in j["data"]] == [S1], (c, j))
c, j = r("GET", B + "/sessions?all=true", token=tBK1)
check("BK1 all=true still own only", c == 200 and [x["id"] for x in j["data"]] == [S1], (c, j))
c, j = r("GET", B + f"/sessions/{S2}", token=tBK1)
check("BK1 GET BK2's session -> 403 NOT_ASSIGNED", c == 403 and j.get("code") == "NOT_ASSIGNED", (c, j))
c, j = r("GET", B + f"/sessions/{S1}", token=tBKB)
check("other school's counselor -> 404", c == 404, (c, j))
c, j = r("GET", B + f"/sessions/{S1}", token=tBK1)
ini = j.get("data", {}).get("initiator_student", {})
check("BK1 detail: minimal student info (no NIK)", c == 200 and ini.get("id") == X and "nik" not in ini and "3201" not in json.dumps(j), (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "completed"}, tBK1)
check("pending -> completed -> 409 INVALID_STATUS_TRANSITION", c == 409 and j.get("code") == "INVALID_STATUS_TRANSITION", (c, j))
c, j = r("POST", B + f"/sessions/{S1}/outcome", {"confidential_notes": "x", "student_feedback": "y"}, tBK1)
check("outcome on pending -> 409", c == 409, (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "scheduled", "new_date": YESTERDAY}, tBK1)
check("schedule into the past (school tz) -> 400", c == 400, (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "scheduled", "new_place": "Ruang BK 1"}, tBK1)
check("BK1 schedules -> scheduled", c == 200 and j["data"]["status"] == "scheduled" and j["data"]["place"] == "Ruang BK 1", (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "scheduled"}, tBK2)
check("BK2 changes BK1's session -> 403", c == 403, (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "rescheduled", "new_start_time": "10:00", "new_end_time": "11:00"}, tBK1)
check("reschedule without reason -> 400", c == 400, (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "rescheduled", "reschedule_reason": "Rapat guru", "new_start_time": "10:00", "new_end_time": "11:00"}, tBK1)
check("reschedule -> rescheduled with new time", c == 200 and j["data"]["status"] == "rescheduled" and j["data"]["start_time"] == "10:00", (c, j))
c, j = r("POST", B + f"/sessions/{S1}/outcome", {"outcome_status": "needs_followup"}, tBK1)
check("needs_followup without plan -> 400", c == 400, (c, j))
c, j = r("POST", B + f"/sessions/{S1}/outcome", {"confidential_notes": "CATATAN-RAHASIA-XYZ", "student_feedback": "Tetap semangat",
          "outcome_status": "resolved", "create_journal_entry": True}, tBK1)
check("BK1 saves outcome -> 200", c == 200 and j["data"]["confidential_notes"] == "CATATAN-RAHASIA-XYZ", (c, j))
c, j = r("GET", B + f"/sessions/{S1}", token=tBK1)
check("  session completed, counselor sees notes", c == 200 and j["data"]["status"] == "completed" and j["data"]["outcome"]["confidential_notes"] == "CATATAN-RAHASIA-XYZ", (c, j))
c, j = r("PUT", B + f"/sessions/{S1}/status", {"status": "cancelled"}, tBK1)
check("completed -> cancelled -> 409", c == 409, (c, j))
c, j = r("GET", SC + f"/sessions/{S1}", token=skX)
check("X sees feedback, never confidential notes", c == 200 and j["data"]["outcome"]["student_feedback"] == "Tetap semangat"
      and "CATATAN-RAHASIA" not in json.dumps(j) and "confidential_notes" not in json.dumps(j), (c, j))
c, j = r("GET", SC + "/sessions", token=skX)
check("X list never contains confidential notes", c == 200 and "CATATAN-RAHASIA" not in json.dumps(j), "")
c, j = r("GET", STU + "/notifications", token=skX)
types = [n["type"] for n in j.get("data", [])]
check("X notified: scheduled, rescheduled, completed", c == 200 and {"counseling_scheduled", "counseling_rescheduled", "counseling_completed"} <= set(types)
      and "CATATAN-RAHASIA" not in json.dumps(j), (c, types))
c, j = r("PUT", B + f"/sessions/{S2}/status", {"status": "cancelled", "reschedule_reason": "Bentrok ujian"}, tBK2)
check("BK2 cancels group session", c == 200 and j["data"]["status"] == "cancelled", (c, j))
c, j = r("GET", STU + "/notifications", token=skY)
check("participant Y notified of cancellation", c == 200 and "counseling_cancelled" in [n["type"] for n in j["data"]], (c, j))

c, j = r("POST", B + "/sessions", {"initiator_student_id": Z, "counseling_type": "case_handling", "topic": "Kasus kehadiran", **slot}, tBK1)
S3 = j.get("data", {}).get("id")
check("BK1 creates session for Z -> 201 scheduled, assigned to BK1", c == 201 and j["data"]["status"] == "scheduled" and j["data"]["counselor_id"] == BK1, (c, j))
c, j = r("POST", B + "/sessions", {"initiator_student_id": Z, "counselor_id": BK2, "counseling_type": "individual", "topic": "x", **slot}, tBK1)
check("BK1 creating for BK2 -> 400", c == 400, (c, j))
c, j = r("POST", B + "/sessions", {"initiator_student_id": FAKE, "counseling_type": "individual", "topic": "x", **slot}, tBK1)
check("unknown initiator -> 404", c == 404, (c, j))
c, j = r("GET", STU + "/notifications", token=skZ)
check("Z notified of scheduled session", c == 200 and "counseling_scheduled" in [n["type"] for n in j["data"]], (c, j))
c, j = r("POST", B + f"/teachers/{BK1}/toggle", {"is_counselor": False}, st)
check("remove BK1 flag with open session -> 409 HAS_OPEN_SESSIONS", c == 409 and j.get("code") == "HAS_OPEN_SESSIONS", (c, j))
c, j = r("GET", B + "/summary", token=tBK1)
check("BK1 summary = own counts", c == 200 and j["completed_sessions_count"] == 1 and j["scheduled_sessions_count"] == 1 and j["pending_requests_count"] == 0, (c, j))

print("\n== journals ==")
c, j = r("GET", B + "/journals", token=tBK1)
auto = j.get("data", [])
check("auto journal from outcome, no confidential notes copied", c == 200 and len(auto) == 1 and auto[0]["counseling_session_id"] == S1
      and "CATATAN-RAHASIA" not in json.dumps(j) and j["total"] == 1, (c, j))
jb = {"date": TODAY, "service_type": "informasi", "activity_title": "Info SNBP", "activity_summary": "Sosialisasi"}
c, j = r("POST", B + "/journals", {**jb, "service_type": "ngawur"}, tBK1)
check("journal invalid service_type -> 400", c == 400, (c, j))
c, j = r("POST", B + "/journals", {**jb, "counseling_session_id": S2}, tBK1)
check("journal linking BK2's session -> 403", c == 403, (c, j))
c, j = r("POST", B + "/journals", {**jb, "attachment_urls": ["javascript:alert(1)"]}, tBK1)
check("journal bad attachment url -> 400", c == 400, (c, j))
c, j = r("POST", B + "/journals", jb, tBK1)
J1 = j.get("data", {}).get("id")
check("BK1 creates journal", c == 201 and j["data"]["counselor_id"] == BK1, (c, j))
c, j = r("PUT", B + f"/journals/{J1}", {**jb, "activity_title": "Hack"}, tBK2)
check("BK2 update BK1's journal -> 403 NOT_OWNER", c == 403 and j.get("code") == "NOT_OWNER", (c, j))
c, j = r("DELETE", B + f"/journals/{J1}", token=tBK2)
check("BK2 delete BK1's journal -> 403", c == 403, (c, j))
c, j = r("GET", B + "/journals", token=tBK2)
check("BK2 does not see BK1's journals", c == 200 and j["data"] == [], (c, j))
c, j = r("PUT", B + f"/journals/{J1}", {**jb, "activity_title": "Info SNBP 2027", "attachment_urls": ["https://example.com/a.pdf"]}, tBK1)
check("BK1 updates own journal (+attachment)", c == 200 and j["data"]["activity_title"] == "Info SNBP 2027" and "a.pdf" in j["data"]["attachment_urls"], (c, j))
c, j = r("PUT", B + f"/journals/{J1}", jb, tBK1)
check("  absent attachment_urls keeps attachments", c == 200 and "a.pdf" in j["data"]["attachment_urls"], (c, j))
c, j = r("PUT", B + f"/journals/{J1}", {**jb, "attachment_urls": []}, tBK1)
check("  empty attachment_urls clears them", c == 200 and j["data"]["attachment_urls"] == "[]", (c, j))
c, j = r("PUT", B + f"/journals/{J1}", jb, tBKB)
check("other school's counselor update -> 404", c == 404, (c, j))
c, j = r("DELETE", B + f"/journals/{J1}", token=tBK1)
check("BK1 deletes own journal", c == 200, (c, j))
c, j = r("GET", B + "/journals?limit=500", token=tBK1)
check("journal list limit capped at 100", c == 200 and j["meta"]["limit"] == 100, (c, j))

print("\n== events ==")
ev = {"title": "Bimbingan Klasikal Karir", "category": "Bimbingan Klasikal", "event_date": TODAY, "start_time": "00:00", "end_time": "23:59", "location": "Aula"}
c, j = r("POST", B + "/events", ev, st)
check("manager create event -> 403", c == 403, (c, j))
c, j = r("POST", B + "/events", {**ev, "event_date": YESTERDAY}, tBK1)
check("event in the past -> 400", c == 400, (c, j))
c, j = r("POST", B + "/events", {**ev, "target_audience": "parents"}, tBK1)
check("invalid audience -> 400", c == 400, (c, j))
c, j = r("POST", B + "/events", ev, tBK1)
E1 = j.get("data", {}).get("id")
check("BK1 creates event", c == 201 and j["data"]["status"] == "published" and j["data"]["counselor"]["id"] == BK1, (c, j))
c, j = r("POST", B + "/events", {**ev, "title": "Rapat BK", "target_audience": "teachers"}, tBK1)
E2 = j.get("data", {}).get("id")
c, j = r("POST", B + "/events", {**ev, "title": "Seminar Besok", "event_date": TOMORROW}, tBK1)
E3 = j.get("data", {}).get("id")
c, j = r("POST", SC + f"/events/{E1}/attend", {}, skX)
A1 = j.get("data", {}).get("id")
check("X attends event", c == 200 and A1 and j["data"]["check_in_time"], (c, j))
c, j = r("POST", SC + f"/events/{E1}/attend", {}, skX)
check("X attends again -> idempotent same row", c == 200 and j["data"]["id"] == A1, (c, j))
c, j = r("POST", SC + f"/events/{E2}/attend", {}, skX)
check("student attends teachers-only event -> 403", c == 403, (c, j))
c, j = r("POST", SC + f"/events/{E3}/attend", {}, skX)
check("attend tomorrow's event -> 409 EVENT_NOT_TODAY", c == 409 and j.get("code") == "EVENT_NOT_TODAY", (c, j))
c, j = r("GET", SC + "/events", token=skX)
evs = {e["id"]: e for e in j.get("data", [])}
check("student events: no teacher-only event, own attendance only", c == 200 and E2 not in evs and evs[E1]["attended"] is True
      and len(evs[E1]["attendances"]) == 1 and evs[E3]["attended"] is False and evs[E3]["attendances"] == [], (c, j))
check("  default order: upcoming ASC (today before tomorrow)", c == 200 and [e for e in [x["id"] for x in j["data"]] if e in (E1, E3)] == [E1, E3], "")
c, j = r("GET", SC + "/events?scope=past", token=skX)
check("  scope=past -> []", c == 200 and j["data"] == [], (c, j))
c, j = r("GET", SC + "/events?scope=bogus", token=skX)
check("  scope invalid -> 400", c == 400, (c, j))
c, j = r("GET", SC + "/events", token=skY)
check("Y events: attended false, no other students' attendance", c == 200 and all(e["attendances"] == [] for e in j["data"]) and "Andi" not in json.dumps(j), (c, j))
c, j = r("POST", B + f"/events/{E1}/attend", {}, tBK2)
check("counselor BK2 attends (teacher)", c == 200 and j["data"]["user_type"] == "teacher", (c, j))
c, j = r("POST", B + f"/events/{E1}/attend", {}, st)
check("manager attends (institution)", c == 200 and j["data"]["user_type"] == "institution", (c, j))
c, j = r("POST", B + f"/events/{E1}/attend", {}, st)
check("manager attends again -> same row", c == 200, (c, j))
c, j = r("GET", B + f"/events/{E1}", token=st)
check("manager event detail: 3 attendees, minimal student info", c == 200 and j["data"]["attendance_count"] == 3 and "3201" not in json.dumps(j), (c, j))
c, j = r("GET", B + f"/events/{E1}", token=stB)
check("other school event detail -> 404", c == 404, (c, j))
c, j = r("POST", B + f"/events/{E1}/conclusion", {"conclusion_notes": "x"}, tBK2)
check("BK2 conclude BK1's event -> 403", c == 403, (c, j))
c, j = r("POST", B + f"/events/{E1}/conclusion", {}, tBK1)
check("conclusion without notes -> 400", c == 400, (c, j))
c, j = r("POST", B + f"/events/{E3}/conclusion", {"conclusion_notes": "x"}, tBK1)
check("conclude event not started -> 409", c == 409, (c, j))
c, j = r("POST", B + f"/events/{E1}/conclusion", {"conclusion_notes": "Siswa antusias"}, tBK1)
check("BK1 concludes event -> completed", c == 200 and j["data"]["status"] == "completed", (c, j))
c, j = r("POST", SC + f"/events/{E1}/attend", {}, skY)
check("attend completed event -> 409", c == 409, (c, j))
c, j = r("GET", B + "/events?status=completed", token=tBK1)
check("events list filter + total", c == 200 and [e["id"] for e in j["data"]] == [E1] and j["total"] == 1, (c, j))

print("\n== isolation & no 500 ==")
c, j = r("GET", B + "/sessions", token=tBKB)
check("school B counselor sees none of school A's sessions", c == 200 and j["data"] == [], (c, j))
c, j = r("GET", B + "/events", token=stB)
check("school B events empty", c == 200 and j["data"] == [], (c, j))
c, j = r("GET", B + "/summary", token=tTR)
check("regular teacher summary -> 403", c == 403, (c, j))
fives = [s for s in statuses if s[0] >= 500]
check("no 5xx responses in this run", not fives, fives)

passed = sum(1 for _, ok in results if ok)
print(f"\nRESULT {passed}/{len(results)} passed")
for n, ok in results:
    if not ok: print("FAILED:", n)
