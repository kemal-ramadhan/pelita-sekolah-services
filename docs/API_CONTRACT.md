# EduPilot API Contract (v2 — hardening refactor, 2026-10-12)

Single source of truth shared by `edupilot-auth-service`, `edupilot-instantion-service`,
`edupilot-gateway`, `dashboard-admin-app`, and `dashboard-schools-app`.
All browser traffic goes through the gateway: `VITE_API_BASE_URL=http://localhost:8888`.
Paths below are gateway paths (no `/api/v1` prefix).

## 1. Conventions

- Error body: `{ "error": "<human message, Indonesian ok>", "code": "<OPTIONAL_MACHINE_CODE>" }`.
  Never return raw DB/GORM error text to the client (log it server-side instead).
- Paginated list response:
  `{ "message": "...", "data": [ ... ], "meta": { "page": 1, "limit": 10, "total": 42, "total_pages": 5 } }`
  Query: `page` (default 1, min 1), `limit` (default 10, max 100), `search` (optional).
- Timestamps: RFC3339. IDs: auth = uint, instantion = uuid.

## 2. Roles (`users.role`, the single source used for app access)

| role          | meaning                         | app                  |
|---------------|---------------------------------|----------------------|
| `super_admin` | platform owner                  | dashboard-admin-app  |
| `admin`       | platform staff                  | dashboard-admin-app  |
| `institution` | school account (created by admin or self-registered) | dashboard-schools-app |
| `customer`    | legacy self-registered school owner | dashboard-schools-app |

- Admin app: allow only `admin`, `super_admin`. Schools app: allow only `institution`, `customer`.
- `customers.role` is a free-form profile attribute only — never used for authorization.
- A `super_admin` is seeded on auth-service startup from `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD`
  (skip if unset or already exists). Abilities/roles for RBAC are seeded idempotently too.

## 3. JWT

- HS256 only (`jwt.WithValidMethods([]string{"HS256"})`); service refuses to start if `JWT_SECRET` is empty.
- Claims: `user_id` (uint), `email`, `role` (users.role), `typ` (`"access"` | `"refresh"`), `iat`, `exp`, `jti`.
- Every auth middleware (auth + instantion) accepts **only `typ=access`**.
- Revocation: Redis key `auth:revoked_before:user:<id>` = unix seconds. Any token with `iat` < that value is rejected
  (both services check it; instantion shares the same Redis). Set on logout and on password reset.
- Refresh tokens: stored in Redis per `jti` (`refresh_token:<user_id>:<jti>`, TTL = `JWT_REFRESH_EXPIRES`),
  rotated on every refresh (old jti deleted), so multiple devices work.

## 4. Gateway

- Proxies `/auth/*`, `/other/*`, `/customer/*` → auth-service; `/instantion/*` → instantion-service.
- Returns 404 for `/internal/*`, `/metrics`, and (when `APP_ENV=production`) `/docs/*`. No blind NoRoute fallback for those.
- On every proxied request: sets `X-Gateway-Auth: <GATEWAY_SECRET>`, overwrites `X-Real-IP` with the direct client IP,
  sets correct `X-Forwarded-Host/Proto`. Strips any incoming `X-Gateway-Auth`, `X-Internal-Secret`, `X-Real-IP`.
- Services: `GatewayAuthMiddleware` enforced when `GATEWAY_SECRET` is set (skip for `/health`). Rate limiter keys
  on `X-Real-IP` only when the gateway header validated, else `RemoteAddr`.
- Request logs never contain query strings with `token=`, request bodies, or Authorization headers.

## 5. Auth service endpoints

Public (rate limited):
- `POST /auth/register` `{email, password(min 8), full_name, phone?, school_name?}` →
  `201 { message, data: { email, requires_verification: true } }` — **no tokens**; role = `customer`.
  `is_import` and `role` are NOT accepted from the public body. OTP emailed.
- `POST /auth/verify` `{email, otp}` → `200 LoginResponse` (logs the user in). Max 5 attempts per OTP, then must resend.
- `POST /auth/resend-otp` `{email}` → always `200 { message }` (generic, no enumeration).
- `POST /auth/login` `{email, password}` → `200 LoginResponse`. Errors are generic ("Email atau password salah").
  Unverified → `403 { error, code: "EMAIL_NOT_VERIFIED" }` (frontend then routes to OTP screen and calls resend-otp).
- `POST /auth/refresh` `{refresh_token}` → `200 { access_token, refresh_token }` (rotated).
- `POST /auth/forgot-password` `{email}` → always `200` generic message.
- `POST /auth/reset-password` `{token, password(min 8)}` → `200`. Token is crypto-random, stored hashed, single use,
  revokes all sessions.
- `GET /auth/google/login`, `GET /auth/google/callback` (cookie domain from `COOKIE_DOMAIN`, random state).

`LoginResponse = { message, access_token, refresh_token, user: UserDTO }`

`UserDTO = { id, email, role, phone, photo_profile, cover, email_verified_at, customer: { id, full_name, school_name, birth_place, birth_date, gender, role } }`
(never: password, otp, tokens, 2FA secrets, google access token).

Authenticated (access token):
- `GET /auth/me` → `{ message, data: UserDTO, abilities: string[] }`
- `PUT /auth/profile` `{full_name, phone?, birth_place?, birth_date?(YYYY-MM-DD), gender?}` → `{ message, data: UserDTO }`
- `PUT /auth/photo-profile` multipart `photo`; `PUT /auth/cover` multipart `cover` — image/png|jpeg|webp only,
  ≤ 2 MB → `{ message, data: UserDTO }`
- `POST /auth/logout` `{refresh_token?}` → `200` (revokes all sessions for the user).

Admin only (`admin`/`super_admin`):
- `POST /auth/admin/users` `{email, password, full_name, phone?, role}` — `role` ∈ {`institution`,`customer`};
  only `super_admin` may also create `admin`. → `201 { message, data: { id, email, role } }`
- `PUT /auth/admin/users/:id/profile` (replaces old `/auth/profile/:id`), `PUT /auth/admin/users/:id/role`
  (replaces old `/auth/change-role`, validated against role list, cannot escalate above caller).
- `GET /other/statistik`, `GET /other/customers` (paginated), `GET /other/customers/:customer_id`.
- RBAC: `/auth/rbac/*` keep paths, but require admin role; grants scoped (cannot grant `manage_roles` unless super_admin).
- `/auth/reports` dummy endpoint removed.

Internal (service-to-service on docker network only, header `X-Internal-Secret: <INTERNAL_SERVICE_SECRET>`):
- `POST /internal/users` `{email, password, full_name, phone?, role}` → `201 { data: { id, email, role } }`
- `DELETE /internal/users/:id` → `200` (compensation when a later step fails).

## 6. Instantion service endpoints

Tenant resolution (middleware for `/instantion/school/*`): school = `institution_accounts.user_id = jwt.user_id`,
fallback `schools.customer_id = jwt.user_id`. No school → `404 { code: "SCHOOL_NOT_SET_UP" }`
(only `GET/POST /profile` work without a school). Inactive school → `403 { code: "SCHOOL_INACTIVE" }`.
Role must be `institution` or `customer`.

School (tenant):
- `GET /instantion/school/profile` → `{ data: School }`
- `POST /instantion/school/profile` (create once; 409 if exists) / `PUT /instantion/school/profile` — body `SchoolRequest`
  (`code`, `name`, `school_level` required; `school_level` ∈ `TK|SD|SMP|SMA|SMK|SLB|Lainnya`).

Teachers (tenant-scoped; every query filters `school_id`):
- `GET /instantion/school/teachers?page&limit&search` → paginated `Teacher[]`
- `GET /instantion/school/teachers/:id`, `PUT /instantion/school/teachers/:id`, `DELETE /instantion/school/teachers/:id`
- `POST /instantion/school/teachers` body:
  `{ full_name*, employee_code*, email?, phone?, number_type?, number?, employee_status?, join_date?, status?,
     subjects?: [{subject_id, academic_id, is_primary}], assignments?: [{employee_position_id, start_date, end_date, is_active}],
     schedules?: [{subject_id, class_id, day_of_week(1-7), start_time "HH:MM", end_time "HH:MM"}] }`
  Empty strings for optional uuid fields are treated as "not provided". All referenced ids must belong to the tenant school.
  `employee_code` unique per school (409).
- `POST /instantion/school/teachers/import` multipart `file` (.xlsx, ≤ 5 MB), `?dry_run=true` supported →
  `{ data: { total, created, failed, errors: [{row, message}] } }`. Uses the first sheet. Columns (header row 1):
  `full_name, employee_code, email, phone, number_type, number, employee_status, join_date`.
- `Teacher` JSON: `{ id, school_id, full_name, employee_code, email, phone, number_type, number, employee_type,
  employee_status, join_date, status, created_at, updated_at }`.

Admin (`admin`/`super_admin` only):
- `GET /instantion/admin/schools?page&limit&search&status=active|inactive` → paginated School[] (+ `account_count`)
- `POST /instantion/admin/schools` body `SchoolRequest + { account?: { email, password, full_name, phone? } }` →
  creates school, and when `account` is present creates the auth user via `/internal/users` (role `institution`)
  and links it — all-or-nothing (compensating delete on failure). → `201 { data: School }`
- `PUT /instantion/admin/schools/:id`, `PATCH /instantion/admin/schools/:id/status` `{ is_active: bool }` (required)
- `GET|POST|DELETE /instantion/admin/schools/:id/accounts` `{ user_id }` (user_id > 0)
- `GET /instantion/admin/stats` → `{ data: { total_schools, active_schools, inactive_schools, total_teachers } }`

## 7. v2.1 additions (2026-10-12): school logo + teacher accounts

### 7.1 Role `teacher`
- New `users.role` value `teacher` → allowed in **dashboard-schools-app** (alongside `institution`, `customer`).
- Teachers are created ONLY via the teacher-invite flow below (never via public register or `/auth/admin/users`).
- Tenant for a teacher: `employees.user_id = jwt.user_id` → that employee's `school_id`
  (institution/customer resolution from §6 unchanged). Inactive school → `SCHOOL_INACTIVE` as usual.
- Teacher permissions in instantion:
  - `GET /instantion/school/profile` → allowed (read-only). `POST/PUT /profile`, `PUT /profile/logo` → 403 `FORBIDDEN_ROLE`.
  - `/instantion/school/teachers*` (list/CRUD/import/invite) → 403 `FORBIDDEN_ROLE`.
  - `GET /instantion/school/me` → `{ data: Teacher }` own employee record (with subjects/assignments/schedules). 404 for non-teachers.
- On any authenticated instantion request by a teacher whose `employees.activated_at` is NULL, set it to now.

### 7.2 School logo (base64, same approach as auth photo/cover)
- New column `schools.logo` (`text`, nullable) holding a data URL `data:image/<type>;base64,...`. Exposed as `logo` in School JSON (contract §6 School response + admin list).
- `PUT /instantion/school/profile/logo` multipart field `logo` — png/jpeg/webp only (DetectContentType), ≤ 2 MB → `{ message, data: School }`. institution/customer only.
- `DELETE /instantion/school/profile/logo` → removes logo → `{ message, data: School }`.
- Route body limit for the logo upload: 3 MB.

### 7.3 Teacher invitation & activation
Teacher JSON gains: `user_id` (uint|null), `account_status` (`"none"` | `"invited"` | `"active"`), `invited_at`, `activated_at`.
- `none` = no user_id; `invited` = user_id set, activated_at null; `active` = activated_at set.

`POST /instantion/school/teachers` body gains `send_invite` (bool, default `true`). When `email` is present and
`send_invite` is true: inside the create flow, instantion calls auth `POST /internal/users/invite` and stores the returned
`user_id` + `invited_at` on the employee. If the employee insert/commit fails afterwards → compensating `DELETE /internal/users/:id`.
If auth returns 409 (email already used by another account) → whole create fails with 409 `{ code: "EMAIL_IN_USE" }`.
Invite email failures (SMTP) do NOT fail the create; auth returns `email_sent: false` and the teacher JSON is still returned
with `account_status: "invited"` plus top-level `warning` string in the response.

`POST /instantion/school/teachers/:id/invite` (institution/customer) — teacher must have an email:
- `none` → creates the account via `/internal/users/invite` (same as above) → 200 `{ data: Teacher }`
- `invited` → resend via `POST /internal/users/:id/resend-invite` (new token, old one invalidated) → 200
- `active` → 409 `{ code: "ALREADY_ACTIVE" }`
Rate limit: max 1 resend per teacher per 60 s (429).

`DELETE /instantion/school/teachers/:id` → also calls `DELETE /internal/users/:user_id` when user_id is set (best effort, logged).

Bulk import: unchanged (no invites sent automatically); teachers can be invited afterwards per row.

Auth internal (X-Internal-Secret):
- `POST /internal/users/invite` `{ email, full_name, phone?, role: "teacher", school_name }` →
  `201 { data: { id, email, role, email_sent: bool } }`. Creates user with NO usable password (empty hash), unverified,
  customer row (full_name, role "teacher"), activation token = 32 bytes crypto-random, stored as SHA-256 hash,
  expires in 72 h (`ACTIVATION_TOKEN_TTL_HOURS`, default 72). Sends Indonesian HTML email (escaped names) with
  button link `${FRONTEND_URL}/activate?token=<raw>` mentioning the school name. 409 if email exists.
- `POST /internal/users/:id/resend-invite` `{ school_name }` → new token + email; 409 `ALREADY_ACTIVE` if already activated.
- `DELETE /internal/users/:id` — existing; must work for teacher users.

Auth public (rate limited):
- `GET /auth/activate?token=` → `200 { data: { email, full_name } }` | `400 { code: "INVALID_OR_EXPIRED_TOKEN" }`
- `POST /auth/activate` `{ token, password (min 8) }` → sets password (bcrypt), sets email_verified_at, clears token
  (single use) → `200 LoginResponse` (auto login).
- Login for a user with an empty password hash → generic `INVALID_CREDENTIALS`.
- Forgot/reset password works for activated teachers as for anyone.

## 8. v2.2 (2026-10-12): Academic structure & Student management

All endpoints below live in instantion-service under `/instantion/school/*`, tenant-scoped (§6), role
`institution`/`customer` only (teachers → 403 `FORBIDDEN_ROLE`, extend the teacher allowlist with nothing).
Lists are paginated (§1) unless stated. Every write validates that every referenced id belongs to the tenant school.
Field naming = snake_case JSON. Dates `YYYY-MM-DD`. Existing tables are EXTENDED (additive columns; relax legacy
NOT NULL columns in preMigrate), never dropped. Data standard follows Dapodik (Kemendikbud) "Peserta Didik" so the data can
be exported for government reporting and university/PDSS registration.

### 8.1 Reference data (enums served by backend — frontend must not hardcode)
`GET /instantion/school/references` → `{ data: { <key>: [{ value, label }] } }` with keys:
`religions` (islam, kristen, katolik, hindu, buddha, konghucu, kepercayaan),
`genders` (L, P), `blood_types` (A, B, AB, O, unknown),
`citizenships` (WNI, WNA),
`special_needs` (Dapodik codes: none, A Tunanetra, B Tunarungu, C Tunagrahita ringan, C1 Tunagrahita sedang, D Tunadaksa ringan,
  D1 Tunadaksa sedang, E Tunalaras, F Tunawicara, H Hiperaktif, I Cerdas istimewa, J Bakat istimewa, K Kesulitan belajar,
  N Narkoba, O Indigo, P Down syndrome, Q Autis),
`residence_types` (orang_tua, wali, kos, asrama, panti_asuhan, pesantren, lainnya),
`transportations` (jalan_kaki, sepeda, sepeda_motor, mobil_pribadi, antar_jemput_sekolah, angkutan_umum, ojek, perahu, kereta_api, lainnya),
`education_levels` (tidak_sekolah, putus_sd, sd, smp, sma, d1, d2, d3, d4, s1, s2, s3, profesi),
`occupations` (tidak_bekerja, nelayan, petani, peternak, pns, tni_polri, guru_dosen, pegawai_swasta, wiraswasta, pedagang,
  buruh, pensiunan, tenaga_kesehatan, ibu_rumah_tangga, sudah_meninggal, lainnya),
`incomes` (tidak_berpenghasilan, lt_500k, 500k_999k, 1m_1_99m, 2m_4_99m, 5m_20m, gt_20m),
`registration_types` (siswa_baru, pindahan, kembali_bersekolah),
`student_statuses` (active, graduated, transferred_out, dropped_out, deceased, inactive),
`history_statuses` (active, moved, promoted, retained, graduated, transferred_out, withdrawn),
`education_stages` (TK, SD, MI, SMP, MTs, SMA, MA, SMK, MAK, SLB, Lainnya),
`curricula` (merdeka, k13, ktsp, lainnya),
`guardian_types` (father, mother, guardian),
`document_types` (seeded per school, see 8.7).

### 8.2 Academic years & semesters
Extend `academic_years`: `name` ("2025/2026", unique per school), `start_date`, `end_date`, `is_active` (exactly one active
per school — activating one deactivates others in a tx). Creating a year auto-creates semesters `Ganjil` (1) and `Genap` (2)
(extend `semesters`: `number`, `start_date?`, `end_date?`, `is_active`).
- `GET /academic-years` (not paginated, sorted desc) · `POST /academic-years` · `PUT /academic-years/:id` ·
  `DELETE /academic-years/:id` (409 `IN_USE` if classes exist) · `POST /academic-years/:id/activate`
- `POST /semesters/:id/activate` (also activates its year)
- `AcademicYear` JSON: `{ id, name, start_date, end_date, is_active, semesters: [{ id, number, name, start_date, end_date, is_active }], class_count, student_count }`

### 8.3 Grade levels (tingkat / jenjang kelas)
New table `grade_levels`: `school_id, education_stage, number (int, ordinal used for sorting & promotion), name ("Kelas 7"),
short_name ("7" or "VII"), is_final (graduating level), sort_order`. Unique (school_id, number).
Covers SD 1–6, SMP 7–9 or 1–3, SMA/SMK 10–12 or 1–3, SMK 4-year 10–13, TK A/B, etc.
- `GET /grade-levels` (not paginated, ordered) · `POST` · `PUT /:id` · `DELETE /:id` (409 `IN_USE` if classes reference it)
- `POST /grade-levels/preset` `{ preset, numbering: "arabic"|"roman", replace: bool }` where preset ∈
  `SD`(1–6), `MI`(1–6), `SMP`(7–9), `MTs`(7–9), `SMP_LOCAL`(1–3), `SMA`(10–12), `MA`(10–12), `SMA_LOCAL`(1–3),
  `SMK`(10–12), `SMK_4Y`(10–13), `SMK_LOCAL`(1–3), `TK`(A, B). Last level gets `is_final=true`. `replace=false` → 409 if levels exist.
- `GradeLevel` JSON: `{ id, education_stage, number, name, short_name, is_final, sort_order, class_count }`

### 8.4 Majors (jurusan / peminatan / program keahlian) — optional, for SMA/MA/SMK
New table `majors`: `school_id, code, name, description, is_active`. CRUD: `GET /majors` (not paginated), `POST`, `PUT /:id`, `DELETE /:id` (409 if used).

### 8.5 Classes (rombongan belajar / rombel)
Extend `classes` (keep table; legacy `semester_id`, `class_code`, `class_description` become nullable):
`academic_year_id*, grade_level_id*, major_id?, name* ("7A", "X TKJ 1", "1"), alias? ("Ibnu Sina"),
homeroom_employee_id? (wali kelas, must be a teacher of this school), capacity? (int), room?, curriculum?, is_active`.
`display_name` (computed, not stored): `name` + (alias ? " - " + alias : "") → "7A - Ibnu Sina".
Unique (school_id, academic_year_id, lower(name)) among non-deleted. Soft delete.
- `GET /classes?academic_year_id=&grade_level_id=&major_id=&search=&page&limit` (default academic year = active one;
  `limit=0`… not allowed; use `all=true` to get a non-paginated list for pickers) 
- `POST /classes` · `PUT /classes/:id` · `DELETE /classes/:id` (409 `HAS_STUDENTS` if active members) · `GET /classes/:id`
- `POST /classes/bulk` `{ academic_year_id, grade_level_id, major_id?, names: ["7A","7B"], aliases?: ["Ibnu Sina","Al-Farabi"], capacity? }`
- `POST /classes/copy` `{ from_academic_year_id, to_academic_year_id }` → copies class structure (names, aliases, levels, majors,
  capacity, room; homeroom optional flag `include_homeroom`) → `{ data: { created, skipped } }`
- `Class` JSON: `{ id, academic_year_id, academic_year_name, grade_level: GradeLevel(short), major?: {id,code,name},
  name, alias, display_name, homeroom: { employee_id, full_name } | null, capacity, room, curriculum, is_active,
  student_count, male_count, female_count }`
- Roster: `GET /classes/:id/students?search&page&limit` → paginated `StudentSummary` (members with active history in this class).

### 8.6 Students (peserta didik)
Extend `students` (legacy `customer_id`, `student_code` nullable; keep `nis`, `nisn`). Soft delete.
Uniques per school among non-deleted (partial indexes, guarded): `nisn` (if not empty), `nis` (if not empty), `nik` (if not empty).
Validation: nisn = 10 digits, nik/no_kk/guardian nik = 16 digits, phone digits/+ 8–15, email format, birth_date not in future,
numeric ranges sane. Required on create: `full_name, gender, birth_place, birth_date, religion`. Everything else optional
(the UI shows a completeness % — `completeness` computed server-side over a defined list of Dapodik-important fields).

`Student` (detail) JSON — groups are flat columns in DB, nested only in JSON for readability:
```
{ id, school_id, status, completeness (0-100),
  identity: { full_name, nickname, gender, nisn, nis, nik, no_kk, birth_place, birth_date, birth_certificate_no,
              religion, citizenship, country, special_needs, blood_type, photo (data URL|null), child_order, siblings_count,
              hobby, aspiration },
  contact:  { phone, mobile_phone, email },
  address:  { street, rt, rw, hamlet, village, district, city, province, postal_code, latitude, longitude,
              residence_type, transportation, distance_km, travel_time_minutes },
  physical: { height_cm, weight_kg, head_circumference_cm },
  welfare:  { kps_pkh_receiver, kps_pkh_number, kip_receiver, kip_number, kip_name, kks_number, pip_eligible, pip_reason,
              bank_name, bank_account_number, bank_account_name },
  registration: { registration_type, entry_date, entry_grade_level_id, previous_school_name, previous_school_npsn,
              previous_skhun_number, previous_diploma_number, exam_participant_number },
  exit:     { exit_date, exit_reason, exit_destination, graduation_certificate_number },
  guardians: [ { id, type (father|mother|guardian), name, nik, birth_year, education, occupation, income, special_needs,
                 phone, email, address, relationship (for guardian), is_alive } ],
  achievements: [ { id, name, type (akademik|non_akademik), level (sekolah|kecamatan|kabupaten|provinsi|nasional|internasional),
                    rank, year, organizer } ],
  scholarships: [ { id, name, type (anak_berprestasi|anak_miskin|pendidikan|unggulan|lainnya), provider, start_year, end_year, amount } ],
  current_class: { class_id, display_name, grade_level_name, academic_year_name } | null,
  created_at, updated_at }
```
`StudentSummary` (list/roster): `{ id, full_name, nickname, gender, nisn, nis, birth_place, birth_date, status, photo_thumb? (omit; use initials),
  current_class: {...}|null, completeness }`.

Endpoints:
- `GET /students?search&status&gender&academic_year_id&grade_level_id&class_id&unassigned=true&page&limit&sort` (search over
  full_name, nisn, nis, nik) → paginated StudentSummary
- `GET /students/:id` → Student · `POST /students` (body = Student JSON groups, guardians/achievements/scholarships arrays,
  optional `class_id` to place immediately in active year) · `PUT /students/:id` (full replace of child arrays; arrays omitted = unchanged)
  · `DELETE /students/:id` (soft)
- `PUT /students/:id/photo` multipart `photo` (png/jpeg/webp ≤ 2 MB → data URL) · `DELETE /students/:id/photo`
- `PATCH /students/:id/status` `{ status, exit_date?, exit_reason?, exit_destination? }` — leaving statuses close the active
  class history with the matching history status.
- `GET /students/stats` → `{ data: { total, active, male, female, by_grade_level: [{grade_level_id, name, male, female, total}],
  unassigned, incomplete (completeness < 80) } }` (active year)

### 8.7 Documents (berkas siswa; base64 in DB like logos)
Extend `student_document_types` (seed idempotently per school on first access: Kartu Keluarga, Akta Kelahiran, Ijazah, SKHUN/SHUN,
KIP/KPS/PKH, Pas Foto, Rapor, Surat Pindah, Lainnya; `is_required` becomes bool) and `student_documents`:
`data` (text data URL), `mime_type`, `size_bytes`, `notes`; `file_path` nullable.
- `GET /students/:id/documents` → list WITHOUT data · `POST /students/:id/documents` multipart `file` + `document_type_id` + `notes?`
  (pdf/png/jpeg ≤ 2 MB; route limit 3 MB) · `GET /students/:id/documents/:docId/download` → binary with Content-Disposition ·
  `DELETE /students/:id/documents/:docId` · `GET /document-types`.

### 8.8 Class history, placement & promotion
New table `student_class_histories`: `school_id, student_id, class_id, academic_year_id, grade_level_id, status (history_statuses),
start_date, end_date?, notes, created_by_user_id, created_at`. At most ONE `active` row per student (partial unique index).
- `GET /students/:id/history` → ordered list `{ id, class: {id, display_name}, academic_year_name, grade_level_name, status, start_date, end_date, notes }`
- `POST /students/:id/placement` `{ class_id, start_date?, notes? }` → closes current active row (status `moved` if same academic
  year, else `promoted`) and opens a new active row. 409 `CLASS_FULL` if capacity reached (unless `force=true`).
- `POST /classes/:id/members` `{ student_ids: [...], start_date?, notes? }` (bulk placement; same rules per student) →
  `{ data: { placed, skipped: [{student_id, reason}] } }`
- `DELETE /classes/:id/members` `{ student_ids: [...] }` → closes active rows with status `withdrawn` (student becomes unassigned).
- `POST /promotions` (kenaikan kelas / kelulusan, single tx) `{ to_academic_year_id, items: [{ student_id, action: "promote"|"retain"|"graduate",
  to_class_id? }] }` — promote/retain require `to_class_id` in `to_academic_year_id`; promote target grade number must be > current,
  retain = same grade number; graduate requires current grade `is_final` (or `force`), closes history `graduated` and sets student
  status `graduated` + exit_date. → `{ data: { promoted, retained, graduated, errors: [{student_id, message}] } }`
- `GET /promotions/preview?from_class_id=&to_academic_year_id=` → roster with suggested action & suggested target class
  (same name pattern at next grade if found).

### 8.9 Import / export
- `GET /students/import/template` → .xlsx with sheet "Siswa" (header row + one example row) and a sheet "Referensi" listing allowed
  enum values. Columns (Indonesian headers mapped to fields, required marked `*`): Nama Lengkap*, Nama Panggilan, Jenis Kelamin* (L/P),
  NISN, NIS, NIK, No KK, Tempat Lahir*, Tanggal Lahir*, No Akta Lahir, Agama*, Kewarganegaraan, Kebutuhan Khusus, Golongan Darah,
  Anak Ke, Jumlah Saudara, Alamat, RT, RW, Dusun, Kelurahan/Desa, Kecamatan, Kabupaten/Kota, Provinsi, Kode Pos, Jenis Tinggal,
  Alat Transportasi, Jarak (km), Waktu Tempuh (menit), Telepon, HP, Email, Tinggi (cm), Berat (kg), Lingkar Kepala (cm),
  Penerima KPS/PKH (Ya/Tidak), No KPS/PKH, Penerima KIP, No KIP, Nama di KIP, No KKS, Jenis Pendaftaran, Tanggal Masuk,
  Sekolah Asal, NPSN Sekolah Asal, No Peserta Ujian, No Seri Ijazah Sebelumnya, No SKHUN Sebelumnya,
  Nama Ayah, NIK Ayah, Tahun Lahir Ayah, Pendidikan Ayah, Pekerjaan Ayah, Penghasilan Ayah, HP Ayah,
  Nama Ibu, NIK Ibu, Tahun Lahir Ibu, Pendidikan Ibu, Pekerjaan Ibu, Penghasilan Ibu, HP Ibu,
  Nama Wali, NIK Wali, Tahun Lahir Wali, Pendidikan Wali, Pekerjaan Wali, Penghasilan Wali, HP Wali, Hubungan Wali,
  Kelas (matches class name or display_name in the active academic year; optional).
  Enum cells accept the label or the value, case-insensitive. Dates: YYYY-MM-DD, DD/MM/YYYY or Excel serial.
- `POST /students/import?dry_run=true|false&mode=create|upsert` multipart `file` (.xlsx ≤ 10 MB, ≤ 5000 rows; route limit 11 MB) →
  `{ data: { total, created, updated, failed, dry_run, errors: [{ row, column?, message }] } }`. `upsert` matches by NISN, then NIS.
  Valid rows are written in one transaction; class placement included.
- `GET /students/export?<same filters as list>&format=xlsx` → .xlsx with the same columns as the template + Status + Kelas Saat Ini
  (Dapodik-friendly). Max 10000 rows.

## 9. v2.3 (2026-10-13): Attendance (presensi) — rules for the user-built module

The routes under `/instantion/school/attendance/*` (router.go "Attendance System") and their JSON shapes are KEPT
(frontend already uses them). Additions must be optional fields/new routes. Required behaviour:

- **Time zone:** all dates/times (today, late minutes, token expiry windows, "now" comparisons) use the school's
  `timezone` (default `Asia/Jakarta`), never the container's UTC. Containers have tzdata.
- **Class membership** = active row in `student_class_histories` (there is NO `students.class_id`).
- **Who may do what:**
  - managers (institution/customer): config, shifts, class-shifts, daily list/summary/manual, live monitor, view any session.
  - teacher: own daily check-in/out (`/attendance/me/*`), own teaching sessions only (schedule.teacher = own employee):
    start / end / refresh token / detail / save students. Detail & save for other teachers' sessions → 403.
  - student (see §10): only `/instantion/student/attendance/*`. The old `POST /instantion/school/attendance/claim-token`
    is REMOVED (replaced by §10.5) and removed from the teacher allowlist.
- **GPS geofence:** haversine distance to `school_latitude/longitude`; inside `radius_meters` required for method `gps`.
  Reject missing/invalid coords (lat −90..90, lng −180..180). Optional `accuracy` (meters) accepted; if accuracy > 2×radius → 400
  `LOW_GPS_ACCURACY`. Store distance.
- **Special conditions (teachers only):** methods `pjj`, `wfh`, `dinas_luar` skip the geofence but require `notes` and are
  stored with status `remote` / `business_trip` accordingly (config flag `allow_remote_check_in`, default true).
- **Late tolerance:** status `late` when check-in > target + tolerance; `late_minutes` counted from target.
  Teacher target: `teacher_schedule_mode=first_schedule` → start of the teacher's first schedule that day (active year/semester,
  weekday in school tz), fallback to shift/default time; `fixed` → default/shift time. Student target: class shift → default shift → config.
- **Check-out:** only after check-in; early when before shift `check_out_start_time`/default → `early_minutes`.
  Idempotency: double check-in/out → 409 `ALREADY_CHECKED_IN` / `ALREADY_CHECKED_OUT`. Unique (school_id, date, employee_id) and
  (school_id, date, student_id) partial indexes.
- **Teaching sessions:** start only for a schedule on today's weekday, within [start−15 min, end]; one session per
  (schedule_id, date) (unique index); `late` vs schedule start + config tolerance; token = 6 chars from an unambiguous alphabet
  (no 0/O/1/I), unique among active sessions of the school, expires after `code_valid_minutes`; refresh rotates it.
  Ending a session closes token. Session student list = active class members; teacher batch save upserts per student.
- **QR payload** (shown on the projector): `${STUDENT_APP_URL}/attendance/claim?code=<TOKEN>`; frontend generates the QR
  locally (npm `qrcode`), never via third-party APIs. `STUDENT_APP_URL` exposed to the schools app as `VITE_STUDENT_APP_URL`.
- Lists → `[]` never null; tenant checks on every id; no raw DB errors; Indonesian messages.

## 10. v2.3: Student accounts, join-school & dashboard-students-app

### 10.1 Identity
- A student account = `users.role = "customer"` AND `customers.role = "students"` (user decision).
- JWT gains claim `customer_role` (value of customers.role; `""` if none). Instantion puts it in context.
  **Student principal** = role `customer` + customer_role `students`.
- Student principals: 403 `FORBIDDEN_ROLE` on `/instantion/school/*` and `/instantion/admin/*`; only `/instantion/student/*`.
  Non-students → 403 on `/instantion/student/*`.
- dashboard-schools-app must refuse login for student accounts (message: "Akun siswa, silakan masuk lewat aplikasi siswa").
  dashboard-students-app accepts ONLY student accounts.

### 10.2 Auth changes
- `POST /auth/register` gains optional `app: "schools" | "students"` (default schools). `students` → customers.role = `students`,
  `school_name` ignored. Everything else (OTP verify, resend, login, refresh, logout, forgot/reset) unchanged.
- Emails with links (reset password, activation) use `STUDENT_FRONTEND_URL` (default `http://localhost:5175`) when the
  account's customers.role = `students`, else `FRONTEND_URL`.
- Internal invite `POST /internal/users/invite` accepts `role: "student"` → users.role `customer`, customers.role `students`,
  link to `${STUDENT_FRONTEND_URL}/activate?token=` (email template says "akun siswa").
- CORS allowlists of all services add `http://localhost:5175`.

### 10.3 Linking students to accounts
- `students` gains `user_id *uint` (partial unique where not null), `invited_at`, `activated_at`; Student JSON & StudentSummary add
  `user_id`, `account_status` (`none` | `invited` | `active`).
- School invites a student: `POST /instantion/school/students/:id/invite` (student must have `contact.email`), same states/limits as teacher
  invite §7.3 (409 `ALREADY_ACTIVE`, 429, `EMAIL_IN_USE`). Deleting a student unlinks (does NOT delete the account; the student just loses the school).
- First authenticated `/instantion/student/*` request sets `activated_at` if null.

### 10.4 Join school (self-registered students)
- `schools` gains `allow_student_join bool default true` (in School JSON + SchoolRequest, editable in Profil Sekolah).
- Table `school_join_requests`: `id, school_id, user_id, email, full_name, nisn, birth_date, message, status (pending|approved|rejected|cancelled),
  student_id?, reviewed_by_user_id?, reviewed_at?, reject_reason, created_at, updated_at`. One pending per user (partial unique).
- Student: `POST /instantion/student/join` `{ school_code, full_name, nisn?, birth_date?, message? }` → 201 `{ data: JoinRequest }`.
  Errors: 404 `SCHOOL_NOT_FOUND` (also for inactive or join disabled — same message), 409 `ALREADY_LINKED`, 409 `REQUEST_PENDING`.
  `DELETE /instantion/student/join` cancels the pending request. Rate limit 10/hour/user.
- School (managers): `GET /instantion/school/join-requests?status=pending|approved|rejected&search&page&limit` → paginated
  `JoinRequest` + `suggestions: [StudentSummary]` (same school, no user_id, matching NISN or same normalized name + birth_date).
  `GET /instantion/school/join-requests/count?status=pending` → `{ data: { count } }` (sidebar badge).
  `POST /instantion/school/join-requests/:id/approve` `{ student_id }` (link existing student without account) **or**
  `{ create: { full_name, gender, birth_place, birth_date, religion, nisn?, class_id? } }` (creates a student via the normal
  student-create validation) → links `students.user_id`, sets `activated_at`, request `approved`, notification to the user.
  `POST /instantion/school/join-requests/:id/reject` `{ reason }` → notification.
- `JoinRequest` JSON: `{ id, school: { id, name, code, logo }, email, full_name, nisn, birth_date, message, status, reject_reason,
  student_id, created_at, reviewed_at }`.

### 10.5 Student endpoints (`/instantion/student/*`, student principal only)
- `GET /me` → `{ data: { status: "no_school"|"pending"|"active", school: {id,name,code,logo,timezone}|null,
  student: StudentSummary|null (incl. current_class, photo), pending_request: JoinRequest|null } }`
- `GET /attendance/today` → `{ data: { date, now, school_location: { latitude, longitude, radius_meters, location_name },
  target_check_in_time, target_check_out_time, late_tolerance_minutes, daily_attendance: DailyAttendance|null,
  is_checked_in, is_checked_out, sessions: [ { schedule_id, subject_name, teacher_name, start_time, end_time, room,
  session_id|null, session_status, my_status: "present"|"sick"|"permission"|"absent"|"dispensation"|null, my_check_in_time } ] } }`
  (status ≠ active → 403 `NOT_ENROLLED`).
- `POST /attendance/check-in` / `POST /attendance/check-out` `{ latitude, longitude, accuracy? }` → `{ data: DailyAttendance }`
  (GPS only for students; errors `OUTSIDE_GEOFENCE` with `details.distance_m` & `details.radius_m`, `LOW_GPS_ACCURACY`,
  `ALREADY_CHECKED_IN/OUT`, `NOT_CHECKED_IN`).
- `POST /attendance/claim` `{ code, latitude?, longitude? }` → `{ data: { session_id, subject_name, class_name, status, check_in_time } }`.
  Code case-insensitive; must be an in-progress session of the student's current class, unexpired; idempotent (second claim → 200 same data);
  if the teacher already marked a non-present status, the claim sets `present`. Errors `INVALID_CODE` (404), `CODE_EXPIRED` (410),
  `NOT_IN_CLASS` (403). Rate limit 10/min/user.
- `GET /attendance/history?month=YYYY-MM` → `{ data: { summary: { present, late, sick, permission, absent }, daily: [DailyAttendance],
  sessions: [ { date, subject_name, teacher_name, start_time, status, check_in_method } ] } }`
- `GET /schedule/today` is NOT added (sessions are in /attendance/today).
- Notifications (table `notifications`: `id, user_id (auth user), school_id?, type, title, body, data jsonb, read_at, created_at`):
  `GET /notifications?page&limit` (paginated, newest first), `GET /notifications/unread-count` → `{ data: { count } }`,
  `POST /notifications/:id/read`, `POST /notifications/read-all`. Types created by the backend:
  `join_approved`, `join_rejected`, `account_linked` (invite activated / approved), `attendance_marked` (teacher saved a non-present
  session status, or a manager recorded/corrected the student's daily attendance), `late_check_in`.

### 10.6 dashboard-students-app (new, `apps/dashboard-students-app`, port 5175)
Same stack as dashboard-schools-app (Vue 3 + TS + Vite + Tailwind v4 + Pinia, same api.ts refresh logic & design tokens),
mobile-first (max-width ~480px centered on desktop, safe-area insets, PWA-like). Pages: login, register (app=students), verify OTP,
forgot/reset password, activate (invite), Beranda, Activity (history), Presensi (center FAB: Presensi Sekolah GPS +
Presensi Kelas via QR scan / 6-char code), Notifikasi (badge), Profil (account + join school / pending status + logout).
Only features backed by existing endpoints.

## 11. v2.4 (2026-10-13): Employee positions (jabatan) & period-based teacher schedules
- `GET|POST /instantion/school/employee-positions`, `PUT|DELETE /instantion/school/employee-positions/:id` (managers only).
  Position JSON `{ id, position, category, position_code, assignment_count }`. 20 standard positions seeded per school on first GET.
  Blank `position_code` auto-generated from initials. 409 `DUPLICATE_NAME` / `DUPLICATE_CODE` / `IN_USE`.
- Teacher form schedules (`TeacherScheduleReq`) accept optional `period_id` (tenant, non-break, active, valid for that day); times come
  from the period. Teacher `GET /:id` returns `period_id` on schedules and the position name on assignments.
- **Schedule archive (2026-10-14):** a schedule that already has teaching sessions (`class_teaching_sessions.schedule_id`) is never
  hard-deleted. Every path that deletes schedules (DELETE `/schedules/:id`, teacher delete, teacher form schedule replacement, class delete)
  archives it instead (`teacher_schedules.deleted_at`, soft delete); unreferenced rows are still hard-deleted. Archived schedules are
  invisible everywhere (lists, grid, conflict checks, JP counts, teacher form, `/me/schedule`, teaching/today, live monitor, student
  attendance today) and do not block period delete / period preset / subject delete (their `schedule_period_id` is cleared). Session detail
  (`GET /attendance/teaching/:id`) still returns the archived `schedule` and the (possibly soft-deleted) class/subject/teacher.
- **FK violations:** any Postgres foreign-key violation (SQLSTATE 23503) that reaches the API is returned as
  `409 { error: "Data tidak dapat dihapus atau diubah karena masih digunakan oleh data lain", code: "IN_USE" }`, never 500.

## 12. v2.5 (2026-10-14): Bimbingan Konseling (BK)

All BK data is confidential. Routes under `/instantion/school/counseling/*` (school group) and `/instantion/student/counseling/*`
(student group, enrolled students only). Lists are `[]` never null; list responses are `{ data, total, meta }`
(`page` default 1, `limit` default 50, max 100, unless noted). Dates/times use the school time zone (§9).

### 12.1 Roles & access (enforced in the controller/service, not only by the teacher allowlist)
- **Guru BK (counselor)** = role `teacher` whose employee has `is_counselor = true` (Employee/Teacher JSON field `is_counselor`).
- **Regular teacher** (no flag): `403 NOT_COUNSELOR` on every `/counseling/*` route (the allowlist lets the role through; the flag is checked per request).
- **Managers** (`institution`/`customer`): `GET /summary` (school-wide counts only), `GET /counselors`, `POST /teachers/:id/toggle`,
  `GET /events`, `GET /events/:id`, `POST /events/:id/attend`. Everything containing case data — sessions, outcomes (confidential notes),
  journals, student picker, event create/conclusion — is `403 COUNSELOR_ONLY` for managers.
- **Counselors** see and act only on sessions **assigned to them** (`counselor_id` = own employee; other counselors' sessions →
  `403 NOT_ASSIGNED`); counselors do NOT see each other's sessions or journals (`counselor_id`/`all` query params are ignored).
  Events are visible to all counselors of the school; only the creating counselor may conclude (`403 NOT_OWNER`).
  Counselors cannot toggle (`403 FORBIDDEN_ROLE`).
- **Students** see only sessions where they are initiator or participant (others → 404), never `confidential_notes`, outcome
  attachments, or other students' personal data; non-initiator participants don't see the initiator's `description`.
- Every id is tenant-checked (other school → 404).

### 12.2 School routes (`/instantion/school/counseling`)
| Method & path | Who | Notes |
|---|---|---|
| `GET /summary` | counselor (own counts), manager (school) | `{ total_counselors, pending_requests_count, scheduled_sessions_count (scheduled + rescheduled), completed_sessions_count, upcoming_events_count, total_journal_entries }` (not wrapped in `data`) |
| `GET /counselors` | counselor, manager | `{ data: Teacher[] }` (`is_counselor = true`) |
| `GET /students?search=&class_id=&page&limit` | counselor | **new**. Student picker: active students of the school. `{ data: [{ id, full_name, nis, class: { id, name } \| null }], total, meta }`, `limit` default 20, max 50; `class.name` = display name (`name - alias`); search on name/NIS/NISN |
| `POST /teachers/:id/toggle` `{ is_counselor }` | manager | employee of the tenant (404 otherwise). Removing the flag while the counselor has pending/scheduled/rescheduled sessions → `409 HAS_OPEN_SESSIONS` |
| `GET /sessions?status&counseling_type&date_from&date_to&class_id&student_id&search&page&limit` | counselor (own) | `{ data: CounselingSession[], total, meta }` |
| `POST /sessions` | counselor | `CounselingSessionRequest` with `initiator_student_id` (active student of the school); assigned to the caller (`counselor_id` ≠ caller → 400); starts `scheduled`; participants notified |
| `GET /sessions/:id` | assigned counselor | |
| `PUT /sessions/:id/status` | assigned counselor | see 12.4 |
| `POST /sessions/:id/outcome` | assigned counselor | `CounselingOutcomeRequest`; only for scheduled/rescheduled sessions (or to edit a completed one); sets status `completed`; `create_journal_entry` creates a journal that never copies the confidential notes. Returns `{ data: CounselingOutcome }` (incl. `confidential_notes`) |
| `GET /events?status&page&limit` / `GET /events/:id` | counselor, manager | `CounselingEvent` + `attendances[]` + `attendance_count` |
| `POST /events` | counselor | `event_date` ≥ today (school tz), ≤ 1 year; `target_audience` ∈ all, students, teachers, specific_grade; blank `category` → "Lainnya"; status `published` |
| `POST /events/:id/attend` `{ notes? }` | counselor (`user_type` teacher), manager (`institution`) | see 12.5 |
| `POST /events/:id/conclusion` `{ conclusion_notes*, attachment_urls? }` | owning counselor | event must have started (`409 EVENT_NOT_STARTED`), not cancelled/draft; sets `completed` (re-conclusion edits) |
| `GET/POST /journals`, `PUT/DELETE /journals/:id` | counselor (owner) | list = own only; others' journal → `403 NOT_OWNER`; `service_type` ∈ orientasi, informasi, konseling_individual, bimbingan_kelompok, konseling_kelompok, bimbingan_klasikal, mediasi, home_visit, konferensi_kasus, kolaborasi, administrasi, layanan_responsif, lainnya; optional `counseling_session_id` must be own session, `event_id` a school event. On PUT, absent `attachment_urls` keeps them, `[]` clears them |

`attachment_urls`: ≤ 10 http(s) URLs, ≤ 500 chars each (stored as a JSON-array string).

**CounselingSession (staff)**: `{ id, school_id, counselor_id, initiator_student_id, class_id?, counseling_type, topic, description,
requested_date, start_time, end_time, place, status, reschedule_reason?, created_at, updated_at, counselor: Teacher,
initiator_student: { id, full_name, nis, nisn, gender, status }, class: { id, name, class_name }, participants: [{ id, school_id,
counseling_session_id, student_id, is_initiator, attendance_status, full_name, created_at, updated_at, student: {id, full_name, nis, nisn, gender, status} }],
outcome?: CounselingOutcome }`.

### 12.3 Student routes (`/instantion/student/counseling`, `RequireEnrolledStudent`)
- `GET /counselors` → `{ data: [{ id, full_name }] }`.
- `GET /peers?search=` → `{ data: [{ id, full_name, class_name }] }` (max 20). Without search (or < 2 chars): active classmates; with
  search: active, enrolled schoolmates by name/NIS. Never self, never counseling data.
- `GET /sessions?status&page&limit` → own sessions; `POST /sessions` (rate limit 10/hour/user): `counseling_type` individual (no
  participants) or group (1–10 `participant_student_ids`, active students of the school); `counselor_id` optional (must be a Guru BK
  of the school; default = counselor with fewest open sessions; none → `409 NO_COUNSELOR`); slot not in the past (school tz), ≤ 1 year;
  max 3 pending requests (`409 TOO_MANY_PENDING`); status `pending`; class = active class.
- `GET /sessions/:id` → 404 unless participant.
- **StudentCounselingSession**: `{ id, counselor_id, initiator_student_id, is_initiator, counseling_type, topic, description ("" unless
  initiator), requested_date, start_time, end_time, place, status, reschedule_reason?, created_at, updated_at, counselor: {id, full_name},
  class: {id, name, class_name}, participants: [{ student_id, is_initiator, full_name, student: {id, full_name} }],
  outcome?: { student_feedback, outcome_status, follow_up_plan?, follow_up_date? } (only once completed) }`.
- `GET /events?scope=upcoming|past&page&limit` → published events with audience ≠ teachers. Default order: upcoming (event_date ≥ today)
  ASC first, then past DESC; `scope=upcoming` / `scope=past` filter. Items `{ id, title, category, description, event_date, start_time,
  end_time, location, target_audience, status, counselor: {id, full_name}, attended, my_attendance: {id, event_id, student_id, check_in_time,
  created_at} | null, attendances: [own check-in only] }`.
- `POST /events/:id/attend` → `{ data: { id, event_id, student_id, check_in_time, created_at } }`.

### 12.4 Session status rules
`pending → scheduled | rescheduled | cancelled`; `scheduled → rescheduled | completed | cancelled`;
`rescheduled → scheduled | rescheduled | completed | cancelled`; `completed`, `cancelled` are final. Anything else →
`409 INVALID_STATUS_TRANSITION`. `scheduled`/`rescheduled` accept `new_date`, `new_start_time`, `new_end_time`, `new_place`; the resulting
slot must not be in the past (school tz). `rescheduled` requires `reschedule_reason` and at least one new value. `cancelled` stores an
optional reason in `reschedule_reason`. `outcome_status = needs_followup` requires `follow_up_plan`; `follow_up_date` ≥ session date.

### 12.5 Events attendance
Only `published` events (`409 EVENT_NOT_OPEN`), on the event date in school tz (`409 EVENT_NOT_TODAY`), from 30 min before start until
end (`409 EVENT_NOT_OPEN`); students not for `teachers` events, counselors not for `students` events (`403 NOT_EVENT_AUDIENCE`).
Idempotent (second check-in returns the first, unique indexes per attendee). `check_in_time` is school-tz `HH:MM:SS`.

### 12.6 Notifications (to every linked student account in the session, §10.5 table)
`counseling_scheduled`, `counseling_rescheduled` (with reason), `counseling_cancelled` (with reason), `counseling_completed`.
`data = { session_id, status }`. Bodies contain date/time/place only — never topic, description or notes.

## 13. v2.6 (2026-10-16): Asesmen Psikologi (service `edupilot-assesment`, PRD `docs/assesment-psikologi.md`)

Gateway prefix `/assessment/*` → `edupilot-assesment:8080` (own DB `edupilot_assesment_db`). §1 conventions apply: errors
`{ error, code }`, never raw DB text; paginated lists `{ data, meta: { page, limit, total, total_pages } }` (limit default 20, max 100);
lists are `[]` never null. Instruments: `TMB`, `RIA`, `PMK`, `ADN`. Results are **screening, not diagnosis** (PRD §0, §10.3–10.4).

### 13.1 Identity & tenant (no client-supplied ids)
- `X-School-ID` / `X-Student-ID` headers are **ignored**; no mock/fallback ids. Identity comes from the JWT (§3) + instantion.
- instantion exposes internal routes (gateway already 404s `/internal/*`), protected by `X-Gateway-Auth` = `GATEWAY_SECRET`
  (constant-time compare) and called by assessment at `INSTANTION_INTERNAL_URL` (default `http://instantion-service:<port>`):
  - `GET /internal/assessment/principal?user_id=&role=&customer_role=` →
    `{ kind: "manager"|"counselor"|"teacher"|"student"|"none", school_id, school_name, school_active, employee_id?, is_counselor,
       student_id?, student_name?, student_status?, class_id?, class_name? }`
    (manager = `institution`/`customer` tenant §6; teacher via employees.user_id §7.1; student via students.user_id §10, enrolled only).
  - `POST /internal/assessment/students` `{ school_id, class_ids?: uuid[], student_ids?: uuid[] }` → `{ data: [{ student_id, user_id?,
    full_name, nis, nisn, gender, class_id?, class_name? }] }` — active students of that school only (others silently dropped).
  - `GET /internal/assessment/classes?school_id=` → `{ data: [{ id, name, grade_level?, student_count }] }` (active academic year first).
  - `POST /internal/assessment/notify` `{ school_id, user_ids?: uint[], to_counselors?: bool, type, title, body, data }` → creates
    notifications (§10.5 table) for the given users and/or every Guru BK of the school.
- Assessment caches the principal in Redis 60 s (`asm:principal:<user_id>:<iat>`). `kind = none`, inactive school → `403 SCHOOL_NOT_SET_UP` / `SCHOOL_INACTIVE`.
- `student_id` everywhere = instantion `students.id` (uuid). Never derived from the auth user id.
- Missing/wrong `X-Gateway-Auth` on assessment → `502 UNAUTHORIZED_GATEWAY` (server misconfiguration). All apps treat a response with code
  `UNAUTHORIZED_GATEWAY` (any status) as a server error: no token refresh, no logout.

### 13.2 Roles
| Group | Who | Else |
|---|---|---|
| `/assessment/admin/*` | `admin`, `super_admin` | 403 `FORBIDDEN` |
| `/assessment/school/*` | managers + Guru BK (counselor); billing routes (claim/purchase) managers only (`403 MANAGER_ONLY`) | regular teacher → `403 NOT_COUNSELOR`, student → 403 |
| `/assessment/student/*` | enrolled student accounts only | others → `403 STUDENT_ONLY`, not enrolled → `403 NOT_ENROLLED` |
Every id is tenant-checked (other school's session/result/alert → 404). Individual result reads by staff are written to `assessment_audit_logs`
(`actor_user_id, actor_kind, action, target_student_id, result_id, ip, at`) (PRD FR-18).

### 13.3 Admin routes (`/assessment/admin`)
- `GET/POST /packages`, `PUT /packages/:id`, `PATCH /packages/:id/status` — `instrument_codes` non-empty subset of the 4 codes (400 `INVALID_INSTRUMENT`),
  `code` unique (409 `DUPLICATE_CODE`), `student_quota > 0`, `price ≥ 0`. `is_bundle` = more than one instrument (server-computed).
- `GET/POST /vouchers`, `PATCH /vouchers/:id/status`, `GET /vouchers/logs` — `valid_until > valid_from` (400), `percentage` 1–100,
  code stored upper-case, unique (409). Logs include `{ voucher: {code,name}, school_id, school_name?, subscription_id, claimed_at }`.
- `GET /subscriptions?status=&page&limit` → school subscriptions (`package`, `school_name?`); `PATCH /subscriptions/:id/status`
  `{ status: "active"|"rejected" }` approves/rejects a `pending` purchase.
- `GET /stats` → `{ total_packages, active_packages, total_vouchers, active_vouchers, total_claims, total_subscriptions, pending_subscriptions,
  total_attempts, completed_results, total_questions }`.
- Question bank `GET /questions?instrument_code&subtest&search&page&limit`, `GET/PUT/DELETE /questions/:id`, `POST /questions`.
  `item_code` unique (409); likert needs `scale_min < scale_max` and `len(scale_labels) = max-min+1`; multiple_choice needs 2–5 options and
  `correct_key` within them; `dimension` must be valid for the instrument/subtest. DELETE of an item that has responses → soft (is_active=false).

### 13.4 School routes (`/assessment/school`)
- `GET /me` → `{ kind, can_manage (manager), is_counselor, school_id, school_name }`.
- `GET /subscriptions` → `{ data: Subscription[], summary: { total_quota, used_quota, remaining_quota, active_packages } }` (summary over `active` only;
  expired/depleted computed on read). `GET /packages` → active packages.
- `POST /claim-voucher` `{ voucher_code, package_id }` (manager) — same school cannot claim the same voucher twice (409 `ALREADY_CLAIMED`);
  `free_claim` → active subscription; `percentage`/`fixed_amount` → `pending` (discounted price recorded in `amount_due`). Usage increment is atomic
  (`used_count < max_usage` in the UPDATE; 409 `VOUCHER_EXHAUSTED`).
- `POST /purchase` `{ package_id }` (manager) — price 0 → `active`; otherwise `pending` until admin approves (no simulated free purchase).
- `GET /classes` → class picker (from §13.1).
- Sessions: `GET /sessions?status&page&limit`, `GET /sessions/:id`, `POST /sessions`, `PATCH /sessions/:id/close`, `DELETE /sessions/:id` (only without attempts, else 409 `IN_USE`).
  `CreateSessionReq { subscription_id, title, description?, instrument_codes: string[] (subset of the package's), target_type: class|random_students|students,
  class_ids?, student_ids?, random_count?, opens_at, closes_at (> opens_at), max_attempts? (default 1) }`. Targets are materialised at creation via
  `/internal/assessment/students` (random = random sample of the classes' students). 0 targets → 400 `NO_TARGETS`. Quota is checked against the
  number of targets (400 `QUOTA_EXCEEDED`) and consumed **once per student per session** on first start (atomic conditional UPDATE).
  Session JSON: `{ id, title, description, instrument_codes, target_type, class_ids, opens_at, closes_at, max_attempts, status (published|closed),
  state (upcoming|open|closed, computed), subscription: { id, package: {id,name,code} }, stats: { targeted, started, completed, flagged }, created_at }`.
  Detail adds `targets: [{ student_id, full_name, nis, class_name, status (assigned|in_progress|completed), instruments: { RIA: "completed", … } }]`.
- Results: `GET /results?session_id&class_id&instrument_code&flag&search&page&limit` → `{ data: [ResultRow], meta }`,
  `ResultRow = { id, attempt_id, session_id, student_id, student_name, nis, class_id, class_name, instrument_code, holland_code?, vark_category?,
  recommended_track?, flags: string[], validity_flags: string[], report_summary, created_at }`. `GET /results/:id` → full result (§13.6) + student info (audited).
  `GET /results/export?session_id&class_id&instrument_code` → CSV (processed scores; audited).
- `GET /analytics?session_id&class_id` → `{ total_participants, by_instrument: {RIA: n,…}, vark_distribution: { Visual, Auditori, "Baca-Tulis", Kinestetik, Multimodal, "Belum jelas" },
  riasec_primary: {R..C}, top_codes: [{ code, count }], track_distribution: { IPA: n, … }, socio_emotional: { motivasi: {kuat, berkembang, perlu_dukungan}, … },
  welfare_alerts_open, insights: { primary_learning_style, teaching_recommendation } }` — insights derived from the data, never hard-coded.
- Welfare alerts (PRD §9.3, FR-19): `GET /alerts?status=open|acknowledged|resolved&page&limit`, `PATCH /alerts/:id` `{ status, note? }`.
  Alert = `{ id, student_id, student_name, class_name, result_id, session_id, flag, status, note, created_at, handled_by_name?, handled_at? }`.
  Created by the scoring worker for `FLAG_KESEJAHTERAAN` and notified to every Guru BK via `/internal/assessment/notify` (title/body never contain answers).

### 13.5 Student routes (`/assessment/student`)
- `GET /sessions` → sessions where the caller is a target (published, own school): `{ data: [{ id, title, description, opens_at, closes_at, state,
  instruments: [{ code, name, estimated_minutes, total_questions, status: not_started|in_progress|queued|processing|completed|failed, attempt_id?, result_id? }] }] }`.
- `GET /sessions/:id/onboarding?instrument=RIA` → `{ session, instrument: { code, name, description, estimated_minutes, total_questions, sections: [{ key, title,
  instruction, scale_labels?, time_limit_seconds? }] }, terms_and_conditions: { title, points[], disclaimer }, consent: { required: true, accepted_at? },
  interactive_examples[] }`.
- `POST /sessions/:id/start` `{ instrument_code, consent_accepted: true }` → `{ data: AttemptState }` (201 new / 200 resume). Rules: session `open`
  (409 `SESSION_NOT_OPEN`), instrument in session (400), consent required (400 `CONSENT_REQUIRED`, stored in `assessment_consents`), `max_attempts`
  per instrument (409 `MAX_ATTEMPTS`), quota (409 `QUOTA_EXHAUSTED`). One `in_progress` attempt per (session, student, instrument) (unique index; no duplicates).
- `GET /attempts/:id` → `AttemptState = { attempt: { id, session_id, instrument_code, status, started_at, seed }, sections: [{ key, title, instruction,
  scale_labels?, time_limit_seconds?, started_at? }], questions: [{ id, section, text, type (likert|multiple_choice|boolean), scale_min?, scale_max?,
  scale_labels?, options?: [{key,text}] }], answers: { item_id: value }, server_time }`. Question order is deterministic per attempt seed; RIA interleaved
  R,I,A,S,E,C with attention checks at positions 17 and 41; TMB part 2 (bakat) fixed order with `time_limit_seconds = 1200`. Answer keys are never sent.
- `PUT /attempts/:id/answers` `{ answers: { item_id: value }, rt_ms?: { item_id: ms }, section_started?: "part2_bakat" }` → `{ saved, server_time }` (autosave, upsert, idempotent).
  Bakat answers saved after section start + 20 min + 60 s grace are ignored.
- `POST /attempts/:id/submit` `{ answers?, duration_sec }` → `202 { attempt_id, status: "queued" }`; resubmit of a non-`in_progress` attempt → 409 `ALREADY_SUBMITTED`;
  > 10 % scored items missing → 422 `INCOMPLETE` `{ missing: [item_id] }`.
- `GET /attempts/:id/status` → `{ attempt_id, status, result_id? }`. `GET /attempts/:id/result` → StudentResult. `GET /results` → own history.
- Only the owning student may read an attempt/result (others → 404).

### 13.6 Result payload
`{ id, attempt_id, session_id, instrument_code, holland_code?, vark_category?, recommended_track?, scores, indices, recommendations,
validity_flags[], flags[], report_summary, interpretation: { summary, sections: [{ title, body, items?: string[] }], careers?: [{ title, code, c_index }],
study_tips?: string[] }, disclaimer, instrument_version, config_version, created_at }`.
- **StudentResult** strips `FLAG_KESEJAHTERAAN`, socio-emotional raw levels for `WEL1`, and any staff-only notes; if the welfare flag is set it adds
  `wellbeing_message = "Terima kasih sudah menjawab dengan jujur. Guru BK siap mengobrol bila kamu mau."`. The disclaimer (PRD §10.4) is always present.
- Scoring follows PRD §6–9 exactly (AC-01…AC-12 are unit tests); thresholds/weights in `internal/data/*.json` config, version saved on each result.
- TMB item ids: aptitude spatial items are `TMB-SP1..SP6` (PRD reuses `TMB-S1..S4` for Sosial interest; ids must be unique).

### 13.7 Pinned implementation details (2026-10-16, backend audit)
**Envelopes.** Every single object is `{ data: … }` (+ `message` on writes); lists are `{ data: [], meta, total }`. The small objects the
sections above show at top level (`/school/me`, `/admin/stats`, `/school/analytics`, attempt `status`, `save`, `submit`) are returned as
`{ data: X, ...X }` (both forms valid). Errors: `{ error, code }` + extra top-level fields (422 `INCOMPLETE` → `missing: [item_id]`,
`QUOTA_EXCEEDED` → `remaining_quota`).

**Additions.** `GET /assessment/school/students?search&class_id&page&limit` (managers + Guru BK, target picker) →
`{ data: [{ student_id, full_name, nis, class_id, class_name }], meta }`. `GET /internal/assessment/principal` also returns
`display_name` (employee/student/school name; used for `handled_by_name`); `/internal/assessment/classes` rows add `academic_year_name`,
`is_active_year`. `GET /analytics` without filters = whole school; `insights` may add `dominant_interest`.

**Admin (§13.3).** `PATCH /packages|vouchers/:id/status` body `{ is_active }` sets (absent body toggles) → `{ message, data }`.
`DELETE /questions/:id` → `{ message, soft_deleted }`. `GET /packages|vouchers|vouchers/logs|subscriptions|questions` accept `page&limit(≤100)&search`.
Duplicates → 409 `DUPLICATE_CODE` (packages, vouchers, questions; case-insensitive). Package/voucher codes are stored upper-case.
`free_claim` accepts `discount_value` 0. Question rules: `options` = string[] (index → A–E); boolean → `scale_min 0, scale_max 1`, 2 labels
(default `["Tidak","Ya"]`); multiple_choice → `scale 0/0, scale_labels []`, `correct_key` A–E within options; others `correct_key ""`.
`question_type` must match the subtest (`part2_bakat` MC; `part_a/b/c` boolean; others likert). RIA attention checks: empty `dimension`
(subtest stored as `attention_check`, `scored=false`, `attention_expected` default 3). Rejecting a voucher-claimed pending subscription frees
one voucher use.

**Subtests / dimensions.** TMB `part1_minat` (M,T,S,E,B,K) · `part2_bakat` (V,N,L,S; spatial items `TMB-SP1..SP6`) · RIA `riasec_main`
(R..C) · `attention_check` ("") · PMK `part_a_activities|part_b_competencies|part_c_occupations|part_d_self_estimates` (R..C) · ADN
`module1_vark` (V,A,R,K) · `module2_wellbeing` (`motivasi, efikasi_diri, regulasi_belajar, kecemasan_belajar, dukungan_sosial,
waspada_kesejahteraan`) · `module3_career` (R..C).

**Student flow.** Answers: likert int, multiple choice `"A".."E"`, boolean `true/false` (also 1/0, "1"/"0"). Unknown item / out of range →
400 `INVALID_ANSWER`. `section_started` is a single section key (only `part2_bakat` is timed; the first aptitude answer also starts it).
Save/submit allowed until `closes_at + 30 min` (in-progress attempts), else 409 `SESSION_NOT_OPEN`. `max_attempts` counts attempts that are
not `failed`. Consent is per (session, student, instrument); once stored, `consent_accepted` may be omitted on resume. Result not ready →
404 `RESULT_NOT_READY`. `GET /student/results` rows `{ id, attempt_id, session_id, session_title, instrument_code, holland_code,
recommended_track, vark_category, report_summary, created_at }`. Onboarding `interactive_examples[]` =
`{ type, title, instruction, sample_question, options: [{key,text}], correct_option?, hint }`. Scoring failures are retried 3× then the
attempt becomes `failed`.

**Result keys (§13.6).** `scores` / `indices` per instrument:
- RIA: scores `raw_scores, percentages, levels, ipsative_scores`; indices `order, code_display ("I/A" on ties), tie_flag, consistency,
  consistency_distance, differentiation, differentiation_category (Jelas|Sedang|Profil datar), amplitude, things_people, data_ideas,
  ranking_basis (raw|ipsatif), attention_failed`; recommendations `careers`.
- TMB: scores `minat_scores (%), minat_sum, bakat_scores (correct count), bakat_total, bakat_percentages, bakat_categories,
  rumpun_scores, rumpun_categories` (keys IPA, IPS, Bahasa, Vokasi, Seni); indices `ranking, equal_alternatives, rumpun_flags`;
  recommendations `primary, alternatives, equal_alternatives`. `recommended_track` = "" when `PROFIL_BELUM_JELAS`.
- PMK: scores `part_a, part_b, part_c, part_d, total_scores, percentages, minat_pct, kompetensi_pct`; indices `order, code_display, tie_flag,
  consistency, differentiation, differentiation_category (Jelas|Sedang|Datar), gap, gap_flags {type: flag}, image_over_activity`.
- ADN: scores `vark_scores, socio_emotional_scores, socio_emotional_levels (Kuat|Berkembang|Perlu dukungan; kecemasan reversed),
  brief_career_scores, brief_levels, wellbeing_items (staff only)`; indices `vark_type (unimodal|bimodal|trimodal|multimodal|belum_jelas),
  vark_dominant, brief_order, brief_code_display, tie_flag`; recommendations `vark_dominant, study_tips, staff_notes (staff only)`.
  `vark_category` labels: "Visual dominan", "Bimodal Visual + Auditori", "Trimodal …", "Multimodal", "Belum jelas"; `holland_code` = 2-letter brief code.
- Staff results also carry `staff_notes[]`; StudentResult removes `staff_notes`, `recommendations.staff_notes`, `scores.wellbeing_items`,
  `FLAG_KESEJAHTERAAN`, `student_name`, `nis`.

**Flag codes.** `validity_flags`: `RESPON_TIDAK_VALID_POTENSIAL` (attention check), `PROFIL_DATAR` (raw range < 4), `POLA_JAWABAN_BERUNTUN`
(≥ 20 identical in a row), `WAKTU_TERLALU_CEPAT` (< 2 s/item or < 40 % of the median; RIA/PMK/ADN). Missing data (> 10 % of scored items,
aptitude excluded) is not a flag: submit is refused with 422 `INCOMPLETE`. `flags`: `TIE_FLAG`, TMB `PENGUATAN_KEMAMPUAN`, `EKSPLORASI_MINAT`,
`PROFIL_BELUM_JELAS`, `DUA_ALTERNATIF_SETARA`; PMK `MINAT_TINGGI_KOMPETENSI_BELUM`, `KOMPETENSI_TINGGI_MINAT_RENDAH`; ADN
`KECEMASAN_BELAJAR_TINGGI`, `FLAG_KESEJAHTERAAN` (staff only). Notification type for welfare alerts: `assessment_welfare_alert`.

**Billing.** A voucher whose discounted price is 0 (100 % / fixed ≥ price) yields an `active` subscription; any amount due → `pending`.

Changelog: 2026-10-16 — §13.7 added (envelopes, `/school/students`, principal `display_name`, admin request/response details, result keys,
flag names, grace/consent/retry rules) after the backend audit; no existing §13 route or field was removed.
