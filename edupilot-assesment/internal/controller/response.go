package controller

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"edupilot-assesment/internal/apperr"
	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// respondError maps an error to a client-safe body; raw DB text is only logged.
func respondError(c *gin.Context, err error) {
	var ae *apperr.Error
	switch {
	case errors.As(err, &ae):
		body := gin.H{"error": ae.Message, "code": ae.Code}
		for k, v := range ae.Extra {
			body[k] = v
		}
		c.JSON(ae.Status, body)
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Data tidak ditemukan", "code": "NOT_FOUND"})
	case errors.Is(err, client.ErrUnavailable):
		config.Logger.Error("instantion call failed", zap.Error(err), zap.String("path", c.FullPath()))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Layanan data sekolah sedang tidak tersedia, coba lagi", "code": "UPSTREAM_UNAVAILABLE"})
	case isUniqueViolation(err):
		c.JSON(http.StatusConflict, gin.H{"error": "Data sudah ada", "code": "DUPLICATE"})
	default:
		config.Logger.Error("internal error", zap.Error(err), zap.String("path", c.FullPath()), zap.String("method", c.Request.Method))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Terjadi kesalahan pada server", "code": "INTERNAL_ERROR"})
	}
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &pg) && pg.Code == "23505")
}

// bindJSON decodes the body; on failure it writes 400 and returns false.
func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		msg := "Format request tidak valid"
		var ute *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			msg = "Body request kosong"
		case errors.As(err, &ute):
			msg = "Tipe data " + ute.Field + " tidak valid"
		case strings.Contains(err.Error(), "parsing time"):
			msg = "Format waktu tidak valid (gunakan RFC3339)"
		case strings.Contains(err.Error(), "invalid UUID"):
			msg = "Format UUID tidak valid"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "VALIDATION_ERROR"})
		return false
	}
	return true
}

func paramUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Data tidak ditemukan", "code": "NOT_FOUND"})
		return uuid.Nil, false
	}
	return id, true
}

func queryUUID(c *gin.Context, name string) (*uuid.UUID, bool) {
	s := strings.TrimSpace(c.Query(name))
	if s == "" {
		return nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " tidak valid", "code": "VALIDATION_ERROR"})
		return nil, false
	}
	return &id, true
}

// Page = contract §1 pagination (limit default 20, max 100).
type Page struct {
	Page, Limit int
	Search      string
}

func (p Page) Offset() int { return (p.Page - 1) * p.Limit }

func parsePage(c *gin.Context) Page {
	p := Page{Page: 1, Limit: 20, Search: strings.TrimSpace(c.Query("search"))}
	if v, err := strconv.Atoi(c.Query("page")); err == nil && v >= 1 {
		p.Page = v
	}
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v >= 1 {
		p.Limit = v
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if len(p.Search) > 100 {
		p.Search = p.Search[:100]
	}
	return p
}

func meta(p Page, total int64) gin.H {
	pages := 0
	if total > 0 {
		pages = int(math.Ceil(float64(total) / float64(p.Limit)))
	}
	return gin.H{"page": p.Page, "limit": p.Limit, "total": total, "total_pages": pages}
}

func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

func listOK(c *gin.Context, data any, p Page, total int64) {
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": meta(p, total), "total": total})
}

func dataOK(c *gin.Context, status int, data any, message string) {
	body := gin.H{"data": data}
	if message != "" {
		body["message"] = message
	}
	c.JSON(status, body)
}

// mirrorOK returns { data: obj, ...obj } for small objects the contract shows
// at the top level (/me, /stats, /analytics, status, save, submit).
func mirrorOK(c *gin.Context, status int, obj map[string]any) {
	body := gin.H{"data": obj}
	for k, v := range obj {
		body[k] = v
	}
	c.JSON(status, body)
}

func now() time.Time { return time.Now() }
