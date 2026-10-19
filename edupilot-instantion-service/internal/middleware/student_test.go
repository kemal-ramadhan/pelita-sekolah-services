package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func studentGuardRouter(role, customerRole string, mw gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxRole, role)
		c.Set(CtxCustomerRole, customerRole)
		c.Next()
	}, mw)
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestStudentPrincipalGuards(t *testing.T) {
	cases := []struct {
		role, cr             string
		rejectWant, onlyWant int
	}{
		{RoleCustomer, CustomerRoleStudents, http.StatusForbidden, http.StatusOK},
		{RoleCustomer, "", http.StatusOK, http.StatusForbidden},
		{RoleInstitution, CustomerRoleStudents, http.StatusOK, http.StatusForbidden},
		{RoleTeacher, "teacher", http.StatusOK, http.StatusForbidden},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		studentGuardRouter(c.role, c.cr, RejectStudentPrincipal()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != c.rejectWant {
			t.Fatalf("reject %s/%s: %d", c.role, c.cr, w.Code)
		}
		w = httptest.NewRecorder()
		studentGuardRouter(c.role, c.cr, RequireStudentPrincipal()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != c.onlyWant {
			t.Fatalf("require %s/%s: %d", c.role, c.cr, w.Code)
		}
	}
}

func TestClaimTokenRemovedFromTeacherAllowlist(t *testing.T) {
	if teacherAllowedRoutes[http.MethodPost+" /instantion/school/attendance/claim-token"] {
		t.Fatal("claim-token must not be allowed")
	}
	if teacherAllowedRoutes[http.MethodGet+" /instantion/school/join-requests"] {
		t.Fatal("join requests are managers only")
	}
}
