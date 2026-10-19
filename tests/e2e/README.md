# E2E tests (isolated stack)

Runs against a throwaway Docker stack — never touches `services/postgres-data`.

```
cd services
docker compose -p edupilot-e2e -f docker-compose.yml -f tests/e2e/override.e2e.yml up -d --build
python3 tests/e2e/e2e_students.py      # students, classes, import/export, promotions
python3 tests/e2e/e2e_schedules.py     # subjects, periods, schedules, conflicts
python3 tests/e2e/e2e_attendance.py    # attendance, student accounts, join school, notifications
python3 tests/e2e/e2e_positions.py     # employee positions, teacher form assignments
python3 tests/e2e/e2e_counseling.py    # schedule archive (FK) bug + Bimbingan Konseling access/flows
python3 tests/e2e/e2e_assessment.py    # Asesmen Psikologi (§13): catalog, billing, sessions, 4 instruments scored, welfare alerts
docker compose -p edupilot-e2e -f docker-compose.yml -f tests/e2e/override.e2e.yml down -v
```

Gateway is published on `127.0.0.1:18888`. Scripts read `SEED_ADMIN_PASSWORD` from
`edupilot-auth-service/.env`. Teacher/student invite tests send real email to `@e2e.test`
addresses (they bounce) — keep those to a minimum.

`e2e_att_head.py` holds the shared helpers exec'd by `e2e_attendance.py`, `e2e_counseling.py` and `e2e_assessment.py`.
The override loads `edupilot-instantion-service/.env` after `edupilot-assesment/.env` for the assessment container, so the
isolated stack shares JWT_SECRET / GATEWAY_SECRET with the other services. The auth register limit is per IP;
if a re-run is throttled, `docker exec edupilot-e2e-redis-1 redis-cli del rl:register:172.19.0.1`.
