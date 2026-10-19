package route

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// Registering routes must not panic (e.g. /teachers/import/template vs /teachers/:id).
func TestSetupRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_ENV", "production")
	r := gin.New()
	SetupRoutes(r)
	if len(r.Routes()) < 18 {
		t.Fatalf("unexpected route count %d", len(r.Routes()))
	}
}
