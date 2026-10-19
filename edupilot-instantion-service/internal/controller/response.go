package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func init() {
	// Report JSON field names (not Go struct names) in validation messages.
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	}
}

// respondError maps an error to a client-safe JSON response.
// Raw DB / internal error text is never returned to the client.
func respondError(c *gin.Context, err error) {
	var ae *apperr.Error
	switch {
	case errors.As(err, &ae):
		body := gin.H{"error": ae.Message}
		if ae.Code != "" {
			body["code"] = ae.Code
		}
		if len(ae.Details) > 0 {
			body["details"] = ae.Details
		} else if len(ae.Data) > 0 {
			body["details"] = ae.Data
		}
		c.JSON(ae.Status, body)
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Data tidak ditemukan", "code": "NOT_FOUND"})
	case errors.Is(err, gorm.ErrForeignKeyViolated) || apperr.IsForeignKeyViolation(err):
		config.Logger.Warn("foreign key violation",
			zap.Error(err),
			zap.String("path", c.FullPath()),
			zap.String("method", c.Request.Method),
		)
		c.JSON(http.StatusConflict, gin.H{"error": apperr.InUseMessage, "code": "IN_USE"})
	case errors.Is(err, gorm.ErrDuplicatedKey):
		c.JSON(http.StatusConflict, gin.H{"error": "Data sudah ada", "code": "DUPLICATE"})
	default:
		config.Logger.Error("internal error",
			zap.Error(err),
			zap.String("path", c.FullPath()),
			zap.String("method", c.Request.Method),
			zap.String("request_id", c.GetString("request_id")),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Terjadi kesalahan pada server", "code": "INTERNAL_ERROR"})
	}
}

// respondBindError converts JSON decode / validation errors to a readable 400.
func respondBindError(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": bindErrorMessage(err), "code": "VALIDATION_ERROR"})
}

func bindErrorMessage(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) && len(ve) > 0 {
		fe := ve[0]
		field := fe.Field()
		switch fe.Tag() {
		case "required":
			return fmt.Sprintf("%s wajib diisi", field)
		case "email":
			return fmt.Sprintf("%s harus berupa email yang valid", field)
		case "min":
			return fmt.Sprintf("%s minimal %s", field, fe.Param())
		case "max":
			return fmt.Sprintf("%s maksimal %s", field, fe.Param())
		case "oneof":
			return fmt.Sprintf("%s harus salah satu dari: %s", field, fe.Param())
		case "gt":
			return fmt.Sprintf("%s harus lebih besar dari %s", field, fe.Param())
		default:
			return fmt.Sprintf("%s tidak valid", field)
		}
	}
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return fmt.Sprintf("tipe data %s tidak valid", ute.Field)
	}
	if errors.Is(err, io.EOF) {
		return "Body request kosong"
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return "Body request terlalu besar"
	}
	// Custom unmarshalers (e.g. OptionalUUID) return safe messages.
	if strings.HasPrefix(err.Error(), "invalid uuid") {
		return "Format UUID tidak valid"
	}
	return "Format request tidak valid"
}

func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe) || strings.Contains(err.Error(), "request body too large")
}
