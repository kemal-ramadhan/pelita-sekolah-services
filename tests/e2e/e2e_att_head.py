# Shared helpers for e2e_attendance.py / e2e_counseling.py (exec'd from services/).
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


