package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// NullableJSON stores arbitrary JSON in a nullable jsonb column. Empty input,
// absent fields, and JSON null are stored as SQL NULL. Object/array payloads are
// kept verbatim. For backward compatibility a JSON *string* that itself contains
// valid JSON (e.g. "{\"a\":1}") is unwrapped to the inner document.
type NullableJSON json.RawMessage

func (j NullableJSON) isNull() bool {
	t := bytes.TrimSpace(j)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// Value implements driver.Valuer.
func (j NullableJSON) Value() (driver.Value, error) {
	if j.isNull() {
		return nil, nil
	}
	if !json.Valid(j) {
		return nil, errors.New("NullableJSON: invalid JSON")
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *NullableJSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = NullableJSON(v)
	default:
		return fmt.Errorf("NullableJSON: unsupported scan type %T", src)
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (j NullableJSON) MarshalJSON() ([]byte, error) {
	if j.isNull() {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (j *NullableJSON) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		*j = nil
		return nil
	}
	if t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return err
		}
		inner := bytes.TrimSpace([]byte(s))
		switch {
		case len(inner) == 0:
			*j = nil
		case json.Valid(inner) && (inner[0] == '{' || inner[0] == '['):
			*j = append((*j)[:0], inner...)
		default:
			*j = append((*j)[:0], t...) // keep as a JSON string value
		}
		return nil
	}
	*j = append((*j)[:0], t...)
	return nil
}

// GormDataType tells GORM which column type to use.
func (NullableJSON) GormDataType() string { return "jsonb" }

type RequestLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	RequestID string    `gorm:"type:varchar(128);index" json:"request_id"`
	Method    string    `gorm:"type:varchar(20)" json:"method"`
	Path      string    `gorm:"type:varchar(255)" json:"path"`
	Query     string    `gorm:"type:text" json:"query"`
	Status    int       `json:"status"`
	LatencyMs float64   `json:"latency_ms"`
	IP        string    `gorm:"type:varchar(100)" json:"ip"`
	UserAgent string    `gorm:"type:text" json:"user_agent"`
	ErrorMsg  string    `gorm:"type:text" json:"error_msg"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditLog struct {
	ID         uint         `gorm:"primaryKey;autoIncrement" json:"id"`
	Service    string       `gorm:"type:varchar(100)" json:"service"`
	Entity     string       `gorm:"type:varchar(100)" json:"entity"`
	EntityID   string       `gorm:"type:varchar(100)" json:"entity_id"`
	Action     string       `gorm:"type:varchar(50)" json:"action"`
	BeforeData NullableJSON `gorm:"type:jsonb" json:"before_data"`
	AfterData  NullableJSON `gorm:"type:jsonb" json:"after_data"`
	CreatedBy  string       `gorm:"type:varchar(100)" json:"created_by"`
	CreatedAt  time.Time    `json:"created_at"`
}
