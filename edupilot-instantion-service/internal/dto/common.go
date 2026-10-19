package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ---------- Pagination (API_CONTRACT.md §1) ----------

const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxLimit     = 100
)

type PageQuery struct {
	Page   int
	Limit  int
	Search string
}

func (q PageQuery) Offset() int { return (q.Page - 1) * q.Limit }

// ParsePageQuery applies defaults/clamping to raw query values.
func ParsePageQuery(page, limit, search string) PageQuery {
	q := PageQuery{Page: DefaultPage, Limit: DefaultLimit, Search: strings.TrimSpace(search)}
	if p, err := strconv.Atoi(page); err == nil && p >= 1 {
		q.Page = p
	}
	if l, err := strconv.Atoi(limit); err == nil && l >= 1 {
		q.Limit = l
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	if len(q.Search) > 100 {
		q.Search = q.Search[:100]
	}
	return q
}

type PageMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func NewPageMeta(q PageQuery, total int64) PageMeta {
	pages := 0
	if total > 0 {
		pages = int(math.Ceil(float64(total) / float64(q.Limit)))
	}
	return PageMeta{Page: q.Page, Limit: q.Limit, Total: total, TotalPages: pages}
}

// LikePattern escapes LIKE metacharacters and wraps the term in %...%.
func LikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// ---------- OptionalUUID ----------

// OptionalUUID accepts a uuid string, "" or null. "" and null mean "not provided".
type OptionalUUID struct {
	UUID  uuid.UUID
	Valid bool
}

var errInvalidUUID = errors.New("invalid uuid")

func (o *OptionalUUID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*o = OptionalUUID{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errInvalidUUID
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*o = OptionalUUID{}
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return errInvalidUUID
	}
	*o = OptionalUUID{UUID: id, Valid: true}
	return nil
}

func (o OptionalUUID) MarshalJSON() ([]byte, error) {
	if !o.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(o.UUID.String())
}

// Ptr returns nil when not provided.
func (o OptionalUUID) Ptr() *uuid.UUID {
	if !o.Valid {
		return nil
	}
	id := o.UUID
	return &id
}

// ---------- FlexBool ----------

// FlexBool accepts true/false, "true"/"false"/"1"/"0"/"ya"/"tidak", 1/0 or null.
type FlexBool bool

func (f *FlexBool) UnmarshalJSON(b []byte) error {
	s := strings.ToLower(strings.Trim(strings.TrimSpace(string(b)), `"`))
	switch s {
	case "true", "1", "yes", "ya", "y":
		*f = true
	case "false", "0", "no", "tidak", "n", "", "null":
		*f = false
	default:
		return errors.New("invalid boolean")
	}
	return nil
}

func (f FlexBool) String() string {
	if f {
		return "true"
	}
	return "false"
}

// ---------- string helpers ----------

// TrimPtr trims a *string and turns "" into nil.
func TrimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}
