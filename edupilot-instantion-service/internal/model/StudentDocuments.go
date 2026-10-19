package model

import (
	"time"

	"github.com/google/uuid"
)

// StudentDocuments stores an uploaded file as a data URL in Data (API_CONTRACT §8.7).
// Legacy file_path / upload_at / status are nullable now. Lists must never select Data.
type StudentDocuments struct {
	ID                    uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	StudentID             uuid.UUID  `gorm:"type:uuid;not null;index" json:"student_id"`
	SchoolID              *uuid.UUID `gorm:"type:uuid;index" json:"-"`
	StudentDocumentTypeID uuid.UUID  `gorm:"type:uuid;not null" json:"document_type_id"`
	FileName              string     `gorm:"type:varchar(255)" json:"file_name"`
	FilePath              string     `gorm:"type:varchar(255)" json:"-"`
	Status                string     `gorm:"type:varchar(255)" json:"status"`
	UploadAt              string     `gorm:"type:varchar(255)" json:"-"`
	Data                  string     `gorm:"type:text" json:"-"`
	MimeType              string     `gorm:"type:varchar(100)" json:"mime_type"`
	SizeBytes             int64      `gorm:"not null;default:0" json:"size_bytes"`
	Notes                 string     `gorm:"type:text" json:"notes"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}
