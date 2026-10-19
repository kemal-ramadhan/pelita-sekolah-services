package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestRequireRoles(t *testing.T) {
	for role, want := range map[string]int{"admin": 200, "super_admin": 200, "institution": 403, "": 403} {
		r := gin.New()
		r.GET("/x", func(c *gin.Context) { c.Set(CtxRole, role) }, RequireRoles(RoleAdmin, RoleSuperAdmin), func(c *gin.Context) { c.Status(200) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != want {
			t.Errorf("role %q: got %d want %d", role, w.Code, want)
		}
	}
}

func TestGatewayAuthAndClientIP(t *testing.T) {
	t.Setenv("GATEWAY_SECRET", "s3cret")
	r := gin.New()
	r.Use(GatewayAuthMiddleware())
	r.GET("/ip", func(c *gin.Context) { c.String(200, ClientIP(c)) })
	r.GET("/health", func(c *gin.Context) { c.String(200, ClientIP(c)) })

	do := func(path, secret, realIP string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "10.0.0.9:1234"
		if secret != "" {
			req.Header.Set("X-Gateway-Auth", secret)
		}
		if realIP != "" {
			req.Header.Set("X-Real-IP", realIP)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := do("/ip", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("missing header: %d", w.Code)
	}
	if w := do("/ip", "wrong", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong header: %d", w.Code)
	}
	if w := do("/ip", "s3cret", "1.2.3.4"); w.Code != 200 || w.Body.String() != "1.2.3.4" {
		t.Errorf("valid header: %d %s", w.Code, w.Body.String())
	}
	if w := do("/health", "", "1.2.3.4"); w.Code != 200 || w.Body.String() != "10.0.0.9" {
		t.Errorf("health: %d %s (X-Real-IP must be ignored without gateway auth)", w.Code, w.Body.String())
	}
}

func TestCORSAllowlist(t *testing.T) {
	t.Setenv("CORS_ALLOW_ORIGINS", "http://localhost:5173, http://localhost:5174")
	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://localhost:5174")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5174" || w.Header().Get("Vary") != "Origin" {
		t.Errorf("allowed origin not echoed: %v", w.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "http://evil.example")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("disallowed origin echoed")
	}
}

func TestTeacherRouteGuard(t *testing.T) {
	type tc struct {
		role, method, path string
		want               int
	}
	cases := []tc{
		{RoleTeacher, http.MethodGet, SchoolProfilePath, 200},
		{RoleTeacher, http.MethodGet, SchoolMePath, 200},
		{RoleTeacher, http.MethodPut, SchoolProfilePath, 403},
		{RoleTeacher, http.MethodPost, SchoolProfilePath, 403},
		{RoleTeacher, http.MethodPut, SchoolProfilePath + "/logo", 403},
		{RoleTeacher, http.MethodGet, "/instantion/school/teachers", 403},
		{RoleTeacher, http.MethodPost, "/instantion/school/teachers/:id/invite", 403},
		{RoleTeacher, http.MethodGet, SchoolMySchedulePath, 200},
		{RoleTeacher, http.MethodGet, "/instantion/school/schedules/grid", 200},
		{RoleTeacher, http.MethodGet, "/instantion/school/teachers/:id/subjects", 200},
		{RoleTeacher, http.MethodPost, "/instantion/school/teachers/:id/subjects", 403},
		{RoleTeacher, http.MethodPost, "/instantion/school/schedules", 403},
		{RoleTeacher, http.MethodPut, "/instantion/school/schedules/:id", 403},
		{RoleTeacher, http.MethodPost, "/instantion/school/schedules/check-conflict", 403},
		{RoleTeacher, http.MethodPost, "/instantion/school/subjects", 403},
		{RoleTeacher, http.MethodPost, "/instantion/school/schedule-periods/preset", 403},
		{RoleTeacher, http.MethodGet, "/instantion/school/subjects/:id/teachers", 403},
		{RoleTeacher, http.MethodGet, "/instantion/school/classes/:id/subjects", 403},
		{RoleInstitution, http.MethodPut, SchoolProfilePath, 200},
		{RoleCustomer, http.MethodGet, "/instantion/school/teachers", 200},
	}
	for _, c := range cases {
		r := gin.New()
		role := c.role
		r.Handle(c.method, c.path, func(ctx *gin.Context) { ctx.Set(CtxRole, role) }, TeacherRouteGuard(), func(ctx *gin.Context) { ctx.Status(200) })
		path := strings.Replace(c.path, ":id", "abc", 1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(c.method, path, nil))
		if w.Code != c.want {
			t.Errorf("%s %s %s: got %d want %d", c.role, c.method, c.path, w.Code, c.want)
		}
		if c.want == 403 && !strings.Contains(w.Body.String(), "FORBIDDEN_ROLE") {
			t.Errorf("%s %s: missing FORBIDDEN_ROLE code", c.method, c.path)
		}
	}
}
