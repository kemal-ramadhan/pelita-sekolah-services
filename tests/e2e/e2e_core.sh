#!/bin/bash
B=http://127.0.0.1:18888
J='Content-Type: application/json'
t(){ printf '%-48s %s\n' "$1" "$2"; }
code(){ curl -s -o /tmp/e2e_body -w '%{http_code}' "$@"; }
t "gateway /health" $(code $B/health)
t "/other/customers no auth (exp 401)" $(code $B/other/customers)
t "/internal/users via gateway (exp 404)" $(code -X POST $B/internal/users)
t "/metrics via gateway (exp 404)" $(code $B/metrics)
L=$(curl -s -X POST $B/auth/login -H "$J" -d "{\"email\":\"admin@edupilot.local\",\"password\":\"$PW\"}")
AT=$(echo "$L" | jq -r .access_token); RT=$(echo "$L" | jq -r .refresh_token)
t "admin login role" "$(echo "$L" | jq -r .user.role)"
t "wrong password msg" "$(curl -s -X POST $B/auth/login -H "$J" -d '{"email":"admin@edupilot.local","password":"xxxxxxxx"}' | jq -c .)"
ME=$(curl -s $B/auth/me -H "Authorization: Bearer $AT")
t "/me leaks otp/google token? (exp none)" "$(echo "$ME" | grep -oE 'email_otp|google_access_token|two_factor' | sort -u | tr '\n' ' ')none"
t "/other/customers as admin" $(code "$B/other/customers?limit=500" -H "Authorization: Bearer $AT"); t "  meta" "$(jq -c .meta /tmp/e2e_body)"
t "admin stats" "$(curl -s $B/instantion/admin/stats -H "Authorization: Bearer $AT" | jq -c .data)"
S=$(curl -s -X POST $B/instantion/admin/schools -H "$J" -H "Authorization: Bearer $AT" -d '{"code":"E2E01","name":"SMA E2E","school_level":"SMA","account":{"email":"sekolah@e2e.test","password":"Rahasia123!","full_name":"Operator E2E"}}')
SID=$(echo "$S" | jq -r .data.id); t "create school+account" "$SID"
t "create without school_level (exp 400)" $(code -X POST $B/instantion/admin/schools -H "$J" -H "Authorization: Bearer $AT" -d '{"code":"X","name":"X"}')
t "dup account email -> compensation (exp 409/400)" $(code -X POST $B/instantion/admin/schools -H "$J" -H "Authorization: Bearer $AT" -d '{"code":"E2E02","name":"Dup","school_level":"SD","account":{"email":"sekolah@e2e.test","password":"Rahasia123!","full_name":"x"}}')
t "  school E2E02 not left behind (exp 0)" "$(curl -s "$B/instantion/admin/schools?search=E2E02" -H "Authorization: Bearer $AT" | jq '.meta.total')"
SL=$(curl -s -X POST $B/auth/login -H "$J" -d '{"email":"sekolah@e2e.test","password":"Rahasia123!"}'); ST=$(echo "$SL" | jq -r .access_token); SR=$(echo "$SL" | jq -r .refresh_token)
t "school login role" "$(echo "$SL" | jq -r .user.role)"
t "school GET profile (linked)" "$(curl -s $B/instantion/school/profile -H "Authorization: Bearer $ST" | jq -r '.data.name // .error')"
t "school -> admin endpoint (exp 403)" $(code $B/instantion/admin/schools -H "Authorization: Bearer $ST")
t "school -> /auth/admin/users (exp 403)" $(code -X POST $B/auth/admin/users -H "$J" -H "Authorization: Bearer $ST" -d '{"email":"h@x.id","password":"12345678","full_name":"h","role":"admin"}')
t "create teacher" $(code -X POST $B/instantion/school/teachers -H "$J" -H "Authorization: Bearer $ST" -d '{"full_name":"Budi Guru","employee_code":"G001","email":"budi@e2e.test","subjects":[{"subject_id":"","academic_id":""}]}')
t "  body" "$(jq -c '.data|{full_name,employee_code}? // .' /tmp/e2e_body)"
t "duplicate teacher code (exp 409)" $(code -X POST $B/instantion/school/teachers -H "$J" -H "Authorization: Bearer $ST" -d '{"full_name":"X","employee_code":"G001"}')
t "list teachers" "$(curl -s "$B/instantion/school/teachers?search=budi" -H "Authorization: Bearer $ST" | jq -c .meta)"
t "import template" $(code $B/instantion/school/teachers/import/template -H "Authorization: Bearer $ST")
t "refresh token used as access (exp 401)" $(code $B/auth/me -H "Authorization: Bearer $SR")
R2=$(curl -s -X POST $B/auth/refresh -H "$J" -d "{\"refresh_token\":\"$SR\"}"); NR=$(echo "$R2" | jq -r .refresh_token); NA=$(echo "$R2" | jq -r .access_token)
t "refresh rotated" "$([ "$NR" != "$SR" ] && [ "$NR" != null ] && echo yes || echo NO)"
t "reuse old refresh (exp 401)" $(code -X POST $B/auth/refresh -H "$J" -d "{\"refresh_token\":\"$SR\"}")
t "deactivate school" $(code -X PATCH $B/instantion/admin/schools/$SID/status -H "$J" -H "Authorization: Bearer $AT" -d '{"is_active":false}')
t "inactive school teachers (exp 403 SCHOOL_INACTIVE)" "$(curl -s $B/instantion/school/teachers -H "Authorization: Bearer $NA" | jq -r .code)"
t "PATCH status {} (exp 400)" $(code -X PATCH $B/instantion/admin/schools/$SID/status -H "$J" -H "Authorization: Bearer $AT" -d '{}')
sleep 1
t "logout" $(code -X POST $B/auth/logout -H "Authorization: Bearer $NA")
t "access after logout auth (exp 401)" $(code $B/auth/me -H "Authorization: Bearer $NA")
t "access after logout instantion (exp 401)" $(code $B/instantion/school/profile -H "Authorization: Bearer $NA")
REG=$(curl -s -X POST $B/auth/register -H "$J" -d '{"email":"baru@e2e.test","password":"Rahasia123!","full_name":"Baru","is_import":true,"role":"admin"}')
t "register (no tokens)" "$(echo "$REG" | jq -c '{has_token:(.access_token!=null), data}')"
t "login unverified (exp 403 EMAIL_NOT_VERIFIED)" "$(curl -s -X POST $B/auth/login -H "$J" -d '{"email":"baru@e2e.test","password":"Rahasia123!"}' | jq -r .code)"
t "verify wrong otp x6 -> last" "$(for i in 1 2 3 4 5 6; do curl -s -X POST $B/auth/verify -H "$J" -d '{"email":"baru@e2e.test","otp":"000001"}' | jq -r .error; done | tail -1)"
t "forgot unknown email (generic 200)" $(code -X POST $B/auth/forgot-password -H "$J" -d '{"email":"tidakada@e2e.test"}')
t "CORS preflight 5173" "$(curl -s -o /dev/null -D - -X OPTIONS $B/auth/login -H 'Origin: http://localhost:5173' -H 'Access-Control-Request-Method: POST' | grep -i '^access-control-allow-origin' | tr -d '\r')"
t "CORS evil origin" "$(curl -s -o /dev/null -D - -X OPTIONS $B/auth/login -H 'Origin: http://evil.test' -H 'Access-Control-Request-Method: POST' | grep -ci '^access-control-allow-origin')"
