exec(open("tests/e2e/e2e_att_head.py").read())
import base64, datetime
from zoneinfo import ZoneInfo

WIB = ZoneInfo("Asia/Jakarta")
now = datetime.datetime.now(WIB)
if now.hour < 1 or now.hour >= 22:
    print("WARNING: run near midnight WIB; time-window checks may be skewed")


def hm(dt):
    return dt.strftime("%H:%M")


def minutes_of(s):
    h, m = s.split(":")[:2]
    return int(h) * 60 + int(m)


TODAY = now.strftime("%Y-%m-%d")
DOW = now.isoweekday()
ALPH = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
LAT, LNG = -6.2, 106.816666
IN = {"latitude": -6.2003, "longitude": 106.8167, "accuracy": 20}
OUT = {"latitude": -6.21, "longitude": 106.816666}
A = "/instantion/school/attendance"
STU = "/instantion/student"


def claims(tok):
    p = tok.split(".")[1]; p += "=" * (-len(p) % 4)
    return json.loads(base64.urlsafe_b64decode(p))


st = mk_school("ATT-" + sfx, "SMP", f"att-{sfx}@e2e.test")
stB = mk_school("ATB-" + sfx, "SD", f"atb-{sfx}@e2e.test")
T1 = mk_teacher(st, "Guru Satu", "T1-" + sfx)
T2 = mk_teacher(st, "Guru Dua", "T2-" + sfx)
T3 = mk_teacher(st, "Guru Tiga", "T3-" + sfx)
yr, grades = setup_school(st, "SMP", "2025/2026")
c, j = req("POST", S + "/classes/bulk", {"academic_year_id": yr["id"], "grade_level_id": grades[7], "names": ["7A", "7B"]}, st)
c, j = req("GET", S + "/classes?all=true", token=st)
cls = {x["name"]: x["id"] for x in j.get("data", [])}
c7a, c7b = cls["7A"], cls["7B"]
c, j = req("POST", S + "/subjects/preset", {"preset": "SMP"}, st)
codes = {x["subject_code"]: x["id"] for x in j.get("data", [])}
MAT, IPA = codes["MAT"], codes["IPA"]

# teacher account for T2
temail = f"tt-{sfx}@e2e.test"
c, j = req("POST", "/auth/admin/users", {"email": temail, "password": "TeacherPass123", "full_name": "Guru Dua", "role": "institution"}, admin)
tuid = j.get("data", {}).get("id")
psql(f"update users set role='teacher', email_verified_at=now() where id={tuid}", db="edupilot_auth_db")
psql(f"update employees set user_id={tuid} where id='{T2}'")
c, j = req("POST", "/auth/login", {"email": temail, "password": "TeacherPass123"})
tt = j.get("access_token")
check("teacher T2 login", c == 200 and tt, (c, j))

# students
def mk_student(name, nisn, cls_id, birth="2012-03-04", email=None):
    b = {"identity": {"full_name": name, "gender": "L", "birth_place": "Bogor", "birth_date": birth, "religion": "islam", "nisn": nisn}}
    if email: b["contact"] = {"email": email}
    b["class_id"] = cls_id
    c, j = req("POST", S + "/students", b, st)
    check(f"student create {name}", c == 201 and j["data"]["account_status"] == "none" and j["data"]["user_id"] is None, (c, j))
    return j.get("data", {}).get("id")

X = mk_student("Andi Pratama", "0012345678", c7a)
Y = mk_student("Budi Santoso", "0012345679", c7b)
W = mk_student("Citra Lestari", "0012345680", c7b, birth="2012-07-07")

# ---------- config ----------
target_in = hm(now - datetime.timedelta(minutes=30))
target_out = hm(now + datetime.timedelta(minutes=60))
c, j = req("PUT", A + "/config", {"school_latitude": LAT, "school_longitude": LNG, "radius_meters": 150, "default_check_in_time": target_in,
           "default_check_out_time": target_out, "late_tolerance_minutes": 10, "teacher_schedule_mode": "fixed", "code_valid_minutes": 30}, st)
check("config update", c == 200 and j["data"]["late_tolerance_minutes"] == 10 and j["data"]["allow_remote_check_in"] is True, (c, j))
c, j = req("PUT", A + "/config", {"radius_meters": 150}, st)
check("config partial update keeps tolerance/self-check flag", c == 200 and j["data"]["late_tolerance_minutes"] == 10 and j["data"]["allow_self_check_in_class"] is True, (c, j))
c, j = req("PUT", A + "/config", {"school_latitude": 95}, st)
check("config invalid latitude -> 400", c == 400, (c, j))
c, j = req("PUT", A + "/config", {"default_check_in_time": "25:00"}, st)
check("config invalid time -> 400", c == 400, (c, j))

# ---------- teacher daily ----------
c, j = req("GET", A + "/me/today", token=tt)
check("teacher me/today before check-in: daily_attendance null, tz, target", c == 200 and j["data"]["daily_attendance"] is None
      and j["data"]["timezone"] == "Asia/Jakarta" and j["data"]["target_check_in_time"] == target_in and j["data"]["target_check_out_time"] == target_out, (c, j))
c, j = req("POST", A + "/me/check-in", {"method": "gps", **OUT}, tt)
check("teacher check-in outside geofence -> 400 OUTSIDE_GEOFENCE + details", c == 400 and j.get("code") == "OUTSIDE_GEOFENCE"
      and j["details"]["radius_m"] == 150 and j["details"]["distance_m"] > 1000, (c, j))
c, j = req("POST", A + "/me/check-in", {"method": "gps", "latitude": -6.2, "longitude": 106.816666, "accuracy": 400}, tt)
check("teacher low accuracy -> LOW_GPS_ACCURACY", c == 400 and j.get("code") == "LOW_GPS_ACCURACY", (c, j))
c, j = req("POST", A + "/me/check-in", {"method": "gps", "latitude": 91, "longitude": 0}, tt)
check("teacher invalid coords -> 400", c == 400, (c, j))
c, j = req("POST", A + "/me/check-in", {"method": "gps", **IN}, tt)
d = j.get("data", {})
cin = d.get("check_in_time") or "00:00:00"
lag = minutes_of(cin) - minutes_of(target_in)
check("teacher check-in inside -> late, minutes from target, WIB clock", c == 200 and d["status"] == "late" and d["check_in_status"] == "late"
      and 29 <= d["late_minutes"] <= 32 and d["late_minutes"] == lag and abs(minutes_of(cin) - minutes_of(hm(datetime.datetime.now(WIB)))) <= 2
      and d["check_in_distance"] is not None, (c, j))
c, j = req("POST", A + "/me/check-in", {"method": "gps", **IN}, tt)
check("double check-in -> 409 ALREADY_CHECKED_IN", c == 409 and j.get("code") == "ALREADY_CHECKED_IN", (c, j))
c, j = req("GET", A + "/me/today", token=tt)
check("me/today after check-in: object", c == 200 and j["data"]["is_checked_in"] and j["data"]["daily_attendance"]["id"], (c, j))
c, j = req("POST", A + f"/me/check-in?employee_id={T1}", {"method": "pjj"}, st)
check("pjj without notes -> 400", c == 400, (c, j))
c, j = req("POST", A + f"/me/check-in?employee_id={T1}", {"method": "pjj", "notes": "Mengajar daring"}, st)
check("pjj with notes -> status remote", c == 200 and j["data"]["status"] == "remote" and j["data"]["check_in_method"] == "pjj", (c, j))
c, j = req("POST", A + "/me/check-in?employee_id=" + str(uuid.uuid4()), {"method": "pjj", "notes": "x"}, st)
check("manager employee_id of another tenant/unknown -> 404", c == 404, (c, j))
c, j = req("POST", A + f"/me/check-out?employee_id={T3}", {"method": "gps", **IN}, st)
check("check-out without check-in -> NOT_CHECKED_IN", c == 400 and j.get("code") == "NOT_CHECKED_IN", (c, j))
c, j = req("POST", A + "/me/check-out", {"method": "gps", **IN}, tt)
check("teacher check-out early", c == 200 and j["data"]["check_out_status"] == "early" and 58 <= j["data"]["early_minutes"] <= 61, (c, j))
c, j = req("POST", A + "/me/check-out", {"method": "gps", **IN}, tt)
check("double check-out -> 409 ALREADY_CHECKED_OUT", c == 409 and j.get("code") == "ALREADY_CHECKED_OUT", (c, j))

# ---------- schedules today ----------
s_start = hm(now - datetime.timedelta(minutes=5)); s_end = hm(now + datetime.timedelta(minutes=55))
def mk_sched(cls_id, sub, t, day, start, end):
    c, j = req("POST", S + "/schedules", {"class_id": cls_id, "subject_id": sub, "teacher_id": t, "day_of_week": day, "start_time": start, "end_time": end}, st)
    check(f"schedule {start}-{end} day {day}", c == 201, (c, j))
    return j.get("data", {}).get("id")
S1 = mk_sched(c7a, MAT, T2, DOW, s_start, s_end)
S2 = mk_sched(c7b, IPA, T1, DOW, s_start, s_end)
S3 = mk_sched(c7a, IPA, T2, DOW % 7 + 1, s_start, s_end)  # tomorrow
future = now + datetime.timedelta(minutes=70)
S4 = None
if future.date() == now.date() and (now + datetime.timedelta(minutes=110)).date() == now.date():
    S4 = mk_sched(c7a, IPA, T2, DOW, hm(future), hm(now + datetime.timedelta(minutes=110)))

c, j = req("PUT", A + "/config", {"teacher_schedule_mode": "first_schedule"}, st)
c, j = req("GET", A + "/me/today", token=tt)
check("first_schedule: target = first schedule today (active year/semester, WIB weekday)", c == 200 and j["data"]["first_schedule_time"] == s_start and j["data"]["target_check_in_time"] == s_start, (c, j))
c, j = req("PUT", A + "/config", {"teacher_schedule_mode": "fixed"}, st)

# ---------- teaching sessions ----------
c, j = req("POST", A + "/teaching/start", {"schedule_id": S1, "topic": "Pecahan"}, tt)
sess = j.get("data", {}); SES1 = sess.get("id"); tok1 = sess.get("session_token", "")
check("T2 start session S1 -> in_progress, on_time, 6-char token", c == 200 and sess["status"] == "in_progress" and sess["teacher_check_in_status"] == "on_time"
      and len(tok1) == 6 and all(ch in ALPH for ch in tok1), (c, j))
c, j = req("POST", A + "/teaching/start", {"schedule_id": S1}, tt)
check("start again -> same session (one per schedule/date)", c == 200 and j["data"]["id"] == SES1 and j["data"]["session_token"] == tok1, (c, j))
c, j = req("POST", A + "/teaching/start", {"schedule_id": S3}, tt)
check("start schedule of another weekday -> SCHEDULE_NOT_TODAY", c == 400 and j.get("code") == "SCHEDULE_NOT_TODAY", (c, j))
if S4:
    c, j = req("POST", A + "/teaching/start", {"schedule_id": S4}, tt)
    check("start before window (start-15) -> OUTSIDE_SESSION_WINDOW", c == 400 and j.get("code") == "OUTSIDE_SESSION_WINDOW", (c, j))
c, j = req("POST", A + "/teaching/start", {"schedule_id": S2}, tt)
check("T2 start T1's schedule -> 404", c == 404, (c, j))
c, j = req("POST", A + f"/teaching/start?employee_id={T1}", {"schedule_id": S2}, st)
SES2 = j.get("data", {}).get("id"); tok2 = j.get("data", {}).get("session_token")
check("manager starts T1's session S2", c == 200 and tok2 and tok2 != tok1, (c, j))
c, j = req("GET", A + f"/teaching/{SES2}", token=tt)
check("teacher detail of other teacher's session -> 403", c == 403, (c, j))
c, j = req("PUT", A + f"/teaching/{SES2}/students", {"students": []}, tt)
check("teacher save other teacher's session -> 403", c == 403, (c, j))
c, j = req("POST", A + f"/teaching/{SES2}/token", token=tt)
check("teacher refresh other teacher's token -> 403", c == 403, (c, j))
c, j = req("GET", A + f"/teaching/{SES1}", token=stB)
check("other tenant session detail -> 404", c == 404, (c, j))
c, j = req("GET", A + f"/teaching/{SES1}", token=tt)
studs = j.get("data", {}).get("students", [])
check("session detail students = active 7A members", c == 200 and [s["student_id"] for s in studs] == [X], (c, j))
c, j = req("POST", A + f"/teaching/{SES1}/token", token=tt)
tok1b = j.get("data", {}).get("session_token")
check("refresh rotates token", c == 200 and tok1b and tok1b != tok1, (c, j))
c, j = req("GET", A + "/teaching/today", token=tt)
row = [x for x in j.get("data", []) if x["schedule_id"] == S1]
check("teaching/today: S1 in_progress, students_total=1 (class history)", c == 200 and row and row[0]["session_status"] == "in_progress" and row[0]["students_total"] == 1, (c, j))
c, j = req("PUT", A + f"/teaching/{SES1}/journal", {"topic": "Pecahan campuran", "notes": "Latihan"}, tt)
check("NEW journal update", c == 200 and j["data"]["teaching_topic"] == "Pecahan campuran" and j["data"]["teacher_check_in_time"] == sess["teacher_check_in_time"], (c, j))

# ---------- student registration / join ----------
def register_student(email, name):
    c, j = req("POST", "/auth/register", {"email": email, "password": "StudentPass123", "full_name": name, "app": "students"})
    check(f"register student {email}", c == 201, (c, j))
    psql(f"update users set email_otp=123456, email_otp_expires_at=now()+interval '10 minutes', email_otp_attempt=0 where email='{email}'", db="edupilot_auth_db")
    c, j = req("POST", "/auth/verify", {"email": email, "otp": "123456"})
    if c != 200:
        c, j = req("POST", "/auth/verify", {"email": email, "otp": 123456})
    check(f"verify OTP {email}", c == 200 and j.get("access_token"), (c, j))
    return j.get("access_token")

semail = f"stu-{sfx}@e2e.test"
sk = register_student(semail, "Andi Pratama")
check("student JWT customer_role=students", claims(sk).get("customer_role") == "students" and claims(sk).get("role") == "customer", claims(sk))
c, j = req("GET", S + "/profile", token=sk)
check("student token on /instantion/school/* -> 403 FORBIDDEN_ROLE", c == 403 and j.get("code") == "FORBIDDEN_ROLE", (c, j))
c, j = req("GET", "/instantion/admin/stats", token=sk)
check("student token on /instantion/admin/* -> 403", c == 403, (c, j))
c, j = req("GET", STU + "/me", token=st)
check("school token on /instantion/student/* -> 403", c == 403 and j.get("code") == "FORBIDDEN_ROLE", (c, j))
c, j = req("GET", STU + "/me", token=tt)
check("teacher token on /instantion/student/* -> 403", c == 403, (c, j))
c, j = req("GET", STU + "/me", token=sk)
check("student me -> no_school", c == 200 and j["data"]["status"] == "no_school" and j["data"]["student"] is None, (c, j))
c, j = req("GET", STU + "/attendance/today", token=sk)
check("attendance without school -> 403 NOT_ENROLLED", c == 403 and j.get("code") == "NOT_ENROLLED", (c, j))
c, j = req("GET", STU + "/notifications", token=sk)
check("notifications without school -> [] paginated", c == 200 and j["data"] == [] and j["meta"]["total"] == 0, (c, j))
c, j = req("POST", STU + "/join", {"school_code": "NOPE-" + sfx, "full_name": "Andi"}, sk)
check("join unknown code -> 404 SCHOOL_NOT_FOUND", c == 404 and j.get("code") == "SCHOOL_NOT_FOUND", (c, j))
c, j = req("POST", STU + "/join", {"school_code": "att-" + sfx, "full_name": "Andi Pratama", "nisn": "123"}, sk)
check("join invalid nisn -> 400", c == 400, (c, j))
c, j = req("POST", STU + "/join", {"school_code": "att-" + sfx, "full_name": "Andi  Pratama", "nisn": "0012345678", "birth_date": "2012-03-04", "message": "Halo"}, sk)
JR = j.get("data", {}).get("id")
check("join with school code (case-insensitive) -> 201 pending", c == 201 and j["data"]["status"] == "pending" and j["data"]["school"]["code"] == "ATT-" + sfx and j["data"]["email"] == semail, (c, j))
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "Andi"}, sk)
check("second join -> 409 REQUEST_PENDING", c == 409 and j.get("code") == "REQUEST_PENDING", (c, j))
c, j = req("GET", STU + "/me", token=sk)
check("me -> pending with pending_request", c == 200 and j["data"]["status"] == "pending" and j["data"]["pending_request"]["id"] == JR, (c, j))
c, j = req("GET", S + "/join-requests/count?status=pending", token=st)
check("school join-requests count = 1", c == 200 and j["data"]["count"] == 1, (c, j))
c, j = req("GET", S + "/join-requests/count", token=stB)
check("other school count = 0", c == 200 and j["data"]["count"] == 0, (c, j))
c, j = req("GET", S + "/join-requests?status=pending", token=st)
item = (j.get("data") or [{}])[0]
check("school sees request + suggestion (NISN match)", c == 200 and item.get("id") == JR and [s["id"] for s in item.get("suggestions", [])] == [X] and j["meta"]["total"] == 1, (c, j))
c, j = req("GET", S + "/join-requests", token=tt)
check("teacher -> join-requests 403", c == 403, (c, j))
c, j = req("POST", S + f"/join-requests/{JR}/approve", {"student_id": X}, stB)
check("other school approve -> 404", c == 404, (c, j))
c, j = req("POST", S + f"/join-requests/{JR}/approve", {}, st)
check("approve without student_id/create -> 400", c == 400, (c, j))
c, j = req("POST", S + f"/join-requests/{JR}/approve", {"student_id": X}, st)
check("approve linking existing student", c == 200 and j["data"]["status"] == "approved" and j["data"]["student_id"] == X, (c, j))
c, j = req("POST", S + f"/join-requests/{JR}/approve", {"student_id": X}, st)
check("approve again -> 409", c == 409, (c, j))
c, j = req("GET", STU + "/me", token=sk)
d = j.get("data", {})
check("student me -> active with student, class, school tz", c == 200 and d["status"] == "active" and d["student"]["id"] == X and d["student"]["account_status"] == "active"
      and d["student"]["current_class"]["class_id"] == c7a and d["school"]["timezone"] == "Asia/Jakarta", (c, j))
c, j = req("GET", S + f"/students/{X}", token=st)
check("school sees student account_status active + user_id", c == 200 and j["data"]["account_status"] == "active" and j["data"]["user_id"], (c, j))
c, j = req("GET", S + "/students?search=Andi", token=st)
check("StudentSummary has account_status", c == 200 and j["data"][0]["account_status"] == "active", (c, j))
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "Andi"}, sk)
check("join when linked -> 409 ALREADY_LINKED", c == 409 and j.get("code") == "ALREADY_LINKED", (c, j))

# ---------- student attendance ----------
c, j = req("GET", STU + "/attendance/today", token=sk)
d = j.get("data", {})
srow = [s for s in d.get("sessions", []) if s["schedule_id"] == S1]
check("student today: location, targets, S1 session in_progress, my_status null", c == 200 and d["school_location"]["radius_meters"] == 150 and d["daily_attendance"] is None
      and d["target_check_in_time"] == target_in and srow and srow[0]["session_status"] == "in_progress" and srow[0]["my_status"] is None and srow[0]["session_id"] == SES1, (c, j))
c, j = req("POST", STU + "/attendance/check-out", IN, sk)
check("student check-out before check-in -> NOT_CHECKED_IN", j.get("code") == "NOT_CHECKED_IN", (c, j))
c, j = req("POST", STU + "/attendance/check-in", OUT, sk)
check("student check-in outside -> OUTSIDE_GEOFENCE", c == 400 and j.get("code") == "OUTSIDE_GEOFENCE" and "distance_m" in j.get("details", {}), (c, j))
c, j = req("POST", STU + "/attendance/check-in", {}, sk)
check("student check-in without GPS -> 400", c == 400, (c, j))
c, j = req("POST", STU + "/attendance/check-in", IN, sk)
check("student check-in -> late (WIB), class set", c == 200 and j["data"]["status"] == "late" and 29 <= j["data"]["late_minutes"] <= 32 and j["data"]["class_id"] == c7a, (c, j))
c, j = req("POST", STU + "/attendance/check-in", IN, sk)
check("student double check-in -> 409", c == 409 and j.get("code") == "ALREADY_CHECKED_IN", (c, j))
c, j = req("POST", STU + "/attendance/claim", {"code": tok1b.lower()}, sk)
check("claim valid code (lowercase) -> present", c == 200 and j["data"]["status"] == "present" and j["data"]["session_id"] == SES1 and j["data"]["subject_name"] and j["data"]["class_name"] == "7A", (c, j))
first_time = j.get("data", {}).get("check_in_time")
c, j = req("POST", STU + "/attendance/claim", {"code": tok1b}, sk)
check("claim again -> 200 idempotent same data", c == 200 and j["data"]["check_in_time"] == first_time, (c, j))
c, j = req("POST", STU + "/attendance/claim", {"code": tok1}, sk)
check("claim rotated old code -> 404 INVALID_CODE", c == 404 and j.get("code") == "INVALID_CODE", (c, j))
c, j = req("POST", STU + "/attendance/claim", {"code": tok2}, sk)
check("claim other class code -> 403 NOT_IN_CLASS", c == 403 and j.get("code") == "NOT_IN_CLASS", (c, j))
c, j = req("POST", STU + "/attendance/claim", {"code": "ZZZZZZ"}, sk)
check("claim invalid code -> 404", c == 404 and j.get("code") == "INVALID_CODE", (c, j))
# teacher re-saves present -> keeps claim time/method
c, j = req("PUT", A + f"/teaching/{SES1}/students", {"students": [{"student_id": X, "status": "present"}]}, tt)
c, j = req("GET", A + f"/teaching/{SES1}", token=tt)
sx = j["data"]["students"][0]
check("teacher re-save present keeps check_in_time & method", sx["check_in_method"] == "unique_code" and sx["check_in_time"] == first_time, sx)
c, j = req("PUT", A + f"/teaching/{SES1}/students", {"students": [{"student_id": Y, "status": "present"}]}, tt)
check("batch save non-member -> 400", c == 400, (c, j))
c, j = req("PUT", A + f"/teaching/{SES1}/students", {"students": [{"student_id": X, "status": "sick", "notes": "Demam"}]}, tt)
check("teacher batch save sick", c == 200, (c, j))
psql(f"update class_teaching_sessions set token_expires_at = now() - interval '1 minute' where id='{SES1}'")
c, j = req("POST", STU + "/attendance/claim", {"code": tok1b}, sk)
check("claim expired code -> 410 CODE_EXPIRED", c == 410 and j.get("code") == "CODE_EXPIRED", (c, j))
c, j = req("POST", A + f"/teaching/{SES1}/token", token=tt)
tok1c = j.get("data", {}).get("session_token")
c, j = req("POST", STU + "/attendance/claim", {"code": tok1c}, sk)
check("claim after teacher marked sick -> present", c == 200 and j["data"]["status"] == "present", (c, j))
c, j = req("GET", STU + "/attendance/today", token=sk)
srow = [s for s in j["data"]["sessions"] if s["schedule_id"] == S1]
check("today: my_status present, checked in", srow[0]["my_status"] == "present" and j["data"]["is_checked_in"], (c, j))
c, j = req("GET", A + "/live-monitor", token=st)
rows = {x["schedule_id"]: x for x in j.get("data", [])}
check("live monitor: S1 present=1 total=1, S2 total=2 (history-based)", c == 200 and rows[S1]["students_present"] == 1 and rows[S1]["students_total"] == 1 and rows[S2]["students_total"] == 2, (c, j))
c, j = req("POST", STU + "/attendance/check-out", IN, sk)
check("student check-out early", c == 200 and j["data"]["check_out_status"] == "early", (c, j))
c, j = req("GET", STU + "/attendance/history?month=" + now.strftime("%Y-%m"), token=sk)
d = j.get("data", {})
check("history: summary late=1, daily 1, sessions 1", c == 200 and d["summary"]["late"] == 1 and len(d["daily"]) == 1 and len(d["sessions"]) == 1 and d["sessions"][0]["status"] == "present", (c, j))
c, j = req("GET", STU + "/attendance/history?month=2026-13", token=sk)
check("history bad month -> 400", c == 400, (c, j))

# manual record
c, j = req("POST", A + "/daily/manual", {"date": "2026-10-01", "user_type": "student", "student_id": X, "status": "permission", "notes": "Acara keluarga"}, st)
check("manual record student (class from history)", c == 200 and j["data"]["class_id"] == c7a, (c, j))
c, j = req("POST", A + "/daily/manual", {"date": "2026-10-01", "user_type": "student", "student_id": X, "status": "absent"}, st)
check("manual record correction updates same row", c == 200 and j["data"]["status"] == "absent", (c, j))
c, j = req("POST", A + "/daily/manual", {"date": "2026-10-01", "user_type": "student", "student_id": X, "status": "bolos"}, st)
check("manual invalid status -> 400", c == 400, (c, j))
c, j = req("POST", A + "/daily/manual", {"date": "01-10-2026", "user_type": "teacher", "employee_id": T1, "status": "sick"}, st)
check("manual invalid date -> 400", c == 400, (c, j))
c, j = req("POST", A + "/daily/manual", {"date": "2026-10-01", "user_type": "student", "student_id": X, "status": "sick"}, stB)
check("manual other-tenant student -> 404", c == 404, (c, j))

# notifications
c, j = req("GET", STU + "/notifications?limit=50", token=sk)
types = [n["type"] for n in j.get("data", [])]
check("notifications: join_approved, late_check_in, attendance_marked", c == 200 and {"join_approved", "late_check_in", "attendance_marked"} <= set(types), types)
c, j = req("GET", STU + "/notifications/unread-count", token=sk)
unread = j.get("data", {}).get("count", 0)
nid = req("GET", STU + "/notifications", token=sk)[1]["data"][0]["id"]
c, j = req("POST", STU + f"/notifications/{nid}/read", token=sk)
c2, j2 = req("GET", STU + "/notifications/unread-count", token=sk)
check("read one decrements unread", c == 200 and j["data"]["read_at"] and j2["data"]["count"] == unread - 1, (c, j, j2))
c, j = req("POST", STU + "/notifications/read-all", token=sk)
c2, j2 = req("GET", STU + "/notifications/unread-count", token=sk)
check("read-all -> 0 unread", c == 200 and j2["data"]["count"] == 0, (c, j2))
c, j = req("POST", STU + f"/notifications/{uuid.uuid4()}/read", token=sk)
check("read unknown notification -> 404", c == 404, (c, j))

# ---------- reject / cancel / approve-create ----------
s2email = f"stu2-{sfx}@e2e.test"
sk2 = register_student(s2email, "Citra Lestari")
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "citra lestari", "birth_date": "2012-07-07"}, sk2)
JR2 = j.get("data", {}).get("id")
c, j = req("GET", S + "/join-requests?status=pending", token=st)
it = [x for x in j.get("data", []) if x["id"] == JR2]
check("suggestion by normalized name + birth date", it and [s["id"] for s in it[0]["suggestions"]] == [W], (c, j))
c, j = req("POST", S + f"/join-requests/{JR2}/reject", {"reason": "Data tidak sesuai"}, st)
check("reject -> rejected", c == 200 and j["data"]["status"] == "rejected" and j["data"]["reject_reason"] == "Data tidak sesuai", (c, j))
c, j = req("GET", STU + "/me", token=sk2)
check("rejected student me -> no_school", c == 200 and j["data"]["status"] == "no_school", (c, j))
c, j = req("GET", STU + "/notifications", token=sk2)
check("join_rejected notification", any(n["type"] == "join_rejected" for n in j.get("data", [])), (c, j))
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "Citra Lestari"}, sk2)
c, j = req("DELETE", STU + "/join", token=sk2)
check("cancel pending join", c == 200, (c, j))
c, j = req("DELETE", STU + "/join", token=sk2)
check("cancel again -> 404", c == 404, (c, j))
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "Citra Baru"}, sk2)
JR3 = j.get("data", {}).get("id")
c, j = req("POST", S + f"/join-requests/{JR3}/approve", {"student_id": X}, st)
check("approve linking already-linked student -> 409", c == 409, (c, j))
c, j = req("POST", S + f"/join-requests/{JR3}/approve", {"create": {"full_name": "Citra Baru", "gender": "P", "birth_place": "Depok", "birth_date": "2012-08-08", "religion": "islam", "class_id": c7b}}, st)
check("approve with create -> new student linked", c == 200 and j["data"]["student_id"] and j["data"]["student_id"] not in (X, W), (c, j))
c, j = req("GET", STU + "/me", token=sk2)
check("approved(create) student active in 7B, email copied", c == 200 and j["data"]["status"] == "active" and j["data"]["student"]["current_class"]["class_id"] == c7b, (c, j))

# ---------- summary ----------
c, j = req("GET", A + "/daily/summary?user_type=student", token=st)
check("summary students: expected = active students with class (4)", c == 200 and j["data"]["total_expected"] == 4 and j["data"]["late_count"] == 1, (c, j))
c, j = req("GET", A + "/daily/summary?user_type=teacher", token=st)
check("summary teachers: expected 3, late 1, remote 1, business_trip_count field", c == 200 and j["data"]["total_expected"] == 3 and j["data"]["late_count"] == 1 and j["data"]["remote_count"] == 1 and "business_trip_count" in j["data"], (c, j))
c, j = req("GET", A + "/daily?date=" + TODAY + "&user_type=student", token=st)
check("daily list", c == 200 and len(j["data"]) == 1, (c, j))
c, j = req("GET", A + "/daily?date=" + TODAY + "&status=sick", token=st)
check("daily list empty -> []", c == 200 and j["data"] == [], (c, j))

# ---------- student invite ----------
Z = mk_student("Dewi Invite", "0012345681", c7b, email=f"inv-{sfx}@e2e.test")
c, j = req("POST", S + f"/students/{Y}/invite", token=st)
check("invite student without email -> 400 EMAIL_REQUIRED", c == 400 and j.get("code") == "EMAIL_REQUIRED", (c, j))
c, j = req("POST", S + f"/students/{Z}/invite", token=st)
check("invite student (e2e.test) -> invited", c == 200 and j["data"]["account_status"] == "invited" and j["data"]["user_id"], (c, j))
def raw_post(path, tok):
    r = urllib.request.Request(BASE + path, data=b"", method="POST", headers={"Authorization": "Bearer " + tok})
    try:
        with urllib.request.urlopen(r, timeout=30) as resp: return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e: return e.code, json.loads(e.read() or b"{}")
c, j = raw_post(S + f"/students/{Z}/invite", st)
check("re-invite within 60s -> 429 (no retry)", c == 429 and j.get("code") == "RATE_LIMITED", (c, j))
c, j = req("POST", S + f"/students/{X}/invite", token=st)
check("invite active student -> 400/409", c in (400, 409), (c, j))
role = psql(f"select u.role||'/'||coalesce(c.role,'') from users u left join customers c on c.user_id=u.id where u.email='inv-{sfx}@e2e.test'", db="edupilot_auth_db")
check("invited account is customer/students", role == "customer/students", role)

# ---------- end session / shifts / misc ----------
c, j = req("POST", A + f"/teaching/{SES1}/end", {"topic": "Selesai"}, tt)
check("end session closes token", c == 200 and j["data"]["status"] == "completed" and not j["data"].get("session_token"), (c, j))
c, j = req("POST", A + f"/teaching/{SES1}/end", {}, tt)
check("end again -> 409", c == 409, (c, j))
c, j = req("POST", A + "/teaching/start", {"schedule_id": S1}, tt)
check("restart completed -> 409 SESSION_COMPLETED", c == 409, (c, j))
c, j = req("POST", "/instantion/school/attendance/claim-token", {"token": "ABCDEF"}, tt)
check("old claim-token route removed (404)", c == 404, (c, j))

c, j = req("POST", A + "/shifts", {"name": "Pagi", "start_time": "7:00", "end_time": "12:00"}, st)
check("shift invalid HH:MM -> 400", c == 400, (c, j))
c, j = req("POST", A + "/shifts", {"name": "Pagi", "start_time": "07:00", "end_time": "12:00", "is_default": True}, st)
SH1 = j.get("data", {}).get("id")
check("shift create default (tolerance default 15)", c == 201 and j["data"]["late_tolerance_minutes"] == 15 and j["data"]["check_out_start_time"] == "12:00", (c, j))
c, j = req("POST", A + "/shifts", {"name": "Siang", "start_time": "12:30", "end_time": "17:00", "late_tolerance_minutes": 0, "is_default": True}, st)
SH2 = j.get("data", {}).get("id")
c, j = req("GET", A + "/shifts", token=st)
defs = [s["name"] for s in j["data"] if s["is_default"]]
check("only one default shift", defs == ["Siang"] and [s for s in j["data"] if s["name"] == "Siang"][0]["late_tolerance_minutes"] == 0, (c, j))
c, j = req("POST", A + "/shifts", {"name": "pagi", "start_time": "07:00", "end_time": "12:00"}, st)
check("duplicate shift name -> 409", c == 409, (c, j))
c, j = req("POST", A + "/class-shifts", {"class_id": c7a, "shift_id": SH1}, st)
check("assign class shift", c == 200 and j["data"]["shift_id"] == SH1, (c, j))
c, j = req("GET", STU + "/attendance/today", token=sk)
check("student target follows class shift", j["data"]["target_check_in_time"] == "07:00", (c, j))
c, j = req("POST", A + "/class-shifts", {"class_id": c7a, "shift_id": str(uuid.uuid4())}, st)
check("assign unknown shift -> 404", c == 404, (c, j))
c, j = req("DELETE", A + f"/shifts/{SH1}", token=st)
check("delete assigned shift -> 409 IN_USE", c == 409 and j.get("code") == "IN_USE", (c, j))
c, j = req("DELETE", A + f"/class-shifts/{c7a}", token=st)
check("NEW unassign class shift", c == 200, (c, j))
c, j = req("DELETE", A + f"/class-shifts/{c7a}", token=st)
check("unassign again -> 404", c == 404, (c, j))
c, j = req("DELETE", A + f"/shifts/{SH1}", token=st)
check("delete shift", c == 200, (c, j))
c, j = req("GET", A + "/class-shifts", token=st)
check("class-shifts [] not null", c == 200 and j["data"] == [], (c, j))

# deleting a student unlinks the account
c, j = req("DELETE", S + f"/students/{X}", token=st)
c, j = req("GET", STU + "/me", token=sk)
check("deleted student -> account unlinked (no_school)", c == 200 and j["data"]["status"] == "no_school", (c, j))

# ---------- smoke earlier flows ----------
c, j = req("GET", S + "/teachers", token=st); check("smoke: teachers list", c == 200 and j["meta"]["total"] == 3, (c, j))
c, j = req("GET", S + "/schedules/grid?class_id=" + c7a, token=st); check("smoke: timetable grid", c == 200, (c, j))
c, j = req("GET", S + "/students/stats", token=st); check("smoke: student stats", c == 200, (c, j))
c, j = req("GET", S + "/profile", token=st); check("smoke: profile has allow_student_join", c == 200 and j["data"]["allow_student_join"] is True, (c, j))
prof = j["data"]
c, j = req("PUT", S + "/profile", {"code": prof["code"], "name": prof["name"], "school_level": prof["school_level"], "allow_student_join": False, "timezone": "Mars/Base"}, st)
check("profile invalid timezone -> 400", c == 400, (c, j))
c, j = req("PUT", S + "/profile", {"code": prof["code"], "name": prof["name"], "school_level": prof["school_level"], "allow_student_join": False}, st)
check("disable allow_student_join", c == 200 and j["data"]["allow_student_join"] is False, (c, j))
c, j = req("POST", STU + "/join", {"school_code": "ATT-" + sfx, "full_name": "Andi"}, sk)
check("join disabled school -> 404 SCHOOL_NOT_FOUND", c == 404 and j.get("code") == "SCHOOL_NOT_FOUND", (c, j))
c, j = req("GET", "/instantion/admin/stats", token=admin); check("smoke: admin stats", c == 200, (c, j))

passed = sum(1 for _, ok in results if ok)
print(f"\nRESULT {passed}/{len(results)} passed")
for n, ok in results:
    if not ok: print("FAILED:", n)
