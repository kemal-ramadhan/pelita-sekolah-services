package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type fakePgErr struct{ code string }

func (e fakePgErr) Error() string {
	return "ERROR: update or delete violates foreign key constraint (SQLSTATE " + e.code + ")"
}
func (e fakePgErr) SQLState() string { return e.code }

func TestRespondErrorMapsForeignKeyViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}
	for _, err := range []error{
		gorm.ErrForeignKeyViolated,
		fmt.Errorf("delete schedule: %w", gorm.ErrForeignKeyViolated),
		fmt.Errorf("wrapped: %w", fakePgErr{"23503"}),
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodDelete, "/x", nil)
		respondError(c, err)
		if w.Code != http.StatusConflict {
			t.Fatalf("%v: status %d", err, w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["code"] != "IN_USE" || body["error"] == "" {
			t.Fatalf("unexpected body %v", body)
		}
		if s, _ := body["error"].(string); len(s) == 0 || s == err.Error() {
			t.Fatalf("raw DB error leaked: %v", body)
		}
	}
	// Other SQLSTATEs stay 500.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	respondError(c, fakePgErr{"42P01"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
