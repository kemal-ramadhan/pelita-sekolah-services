package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StudentDocumentService struct {
	db *gorm.DB
}

func NewStudentDocumentService(db *gorm.DB) *StudentDocumentService {
	return &StudentDocumentService{db: db}
}

func (s *StudentDocumentService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

// ensureStudent verifies the student belongs to the tenant (and is not deleted).
func ensureStudent(db *gorm.DB, schoolID, studentID uuid.UUID) error {
	var n int64
	if err := db.Model(&model.Students{}).Where("school_id = ? AND id = ?", schoolID, studentID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("Siswa tidak ditemukan")
	}
	return nil
}

func docTypeResponse(dt model.StudentDocumentType) *dto.DocumentTypeResponse {
	return &dto.DocumentTypeResponse{ID: dt.ID, Name: dt.Name, Description: dt.Description, IsRequired: dt.IsRequired, SortOrder: dt.SortOrder}
}

// GetStudentDocuments lists a student's documents WITHOUT the data column.
func (s *StudentDocumentService) GetStudentDocuments(ctx context.Context, schoolID, studentID uuid.UUID) ([]dto.StudentDocumentResponse, error) {
	db := s.conn(ctx)
	if err := ensureStudent(db, schoolID, studentID); err != nil {
		return nil, err
	}
	var docs []model.StudentDocuments
	if err := db.Select("id, student_id, student_document_type_id, file_name, mime_type, size_bytes, notes, created_at").
		Where("student_id = ? AND (school_id = ? OR school_id IS NULL)", studentID, schoolID).
		Order("created_at DESC").Find(&docs).Error; err != nil {
		return nil, err
	}

	var types []model.StudentDocumentType
	if err := db.Where("school_id = ?", schoolID).Find(&types).Error; err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]model.StudentDocumentType{}
	for _, t := range types {
		byID[t.ID] = t
	}

	result := make([]dto.StudentDocumentResponse, 0, len(docs))
	for _, d := range docs {
		r := dto.StudentDocumentResponse{
			ID: d.ID, DocumentTypeID: d.StudentDocumentTypeID, FileName: d.FileName, MimeType: d.MimeType,
			SizeBytes: d.SizeBytes, Notes: d.Notes, CreatedAt: d.CreatedAt.Format(time.RFC3339),
		}
		if dt, ok := byID[d.StudentDocumentTypeID]; ok {
			r.DocumentTypeName = dt.Name
			r.DocumentType = docTypeResponse(dt)
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *StudentDocumentService) CreateStudentDocument(ctx context.Context, schoolID, studentID, docTypeID uuid.UUID, fileName, mimeType, notes, dataURL string, sizeBytes int64) (*dto.StudentDocumentResponse, error) {
	db := s.conn(ctx)
	if err := ensureStudent(db, schoolID, studentID); err != nil {
		return nil, err
	}
	notes = strings.TrimSpace(notes)
	if len([]rune(notes)) > 1000 {
		return nil, apperr.Validation("Catatan maksimal 1000 karakter", map[string]string{"notes": "Catatan maksimal 1000 karakter"})
	}
	var dt model.StudentDocumentType
	if err := db.Where("school_id = ? AND id = ?", schoolID, docTypeID).First(&dt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.Validation("Jenis dokumen tidak ditemukan", map[string]string{"document_type_id": "Jenis dokumen tidak ditemukan"})
		}
		return nil, err
	}

	doc := model.StudentDocuments{
		StudentID: studentID, SchoolID: &schoolID, StudentDocumentTypeID: docTypeID, FileName: fileName,
		MimeType: mimeType, SizeBytes: sizeBytes, Notes: notes, Data: dataURL, Status: "uploaded",
		UploadAt: time.Now().Format(time.RFC3339),
	}
	if err := db.Create(&doc).Error; err != nil {
		return nil, err
	}
	return &dto.StudentDocumentResponse{
		ID: doc.ID, DocumentTypeID: doc.StudentDocumentTypeID, DocumentTypeName: dt.Name, DocumentType: docTypeResponse(dt),
		FileName: doc.FileName, MimeType: doc.MimeType, SizeBytes: doc.SizeBytes, Notes: doc.Notes,
		CreatedAt: doc.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (s *StudentDocumentService) GetDocumentForDownload(ctx context.Context, schoolID, studentID, docID uuid.UUID) ([]byte, string, string, error) {
	db := s.conn(ctx)
	if err := ensureStudent(db, schoolID, studentID); err != nil {
		return nil, "", "", err
	}
	var doc model.StudentDocuments
	if err := db.Where("id = ? AND student_id = ? AND (school_id = ? OR school_id IS NULL)", docID, studentID, schoolID).
		First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", "", apperr.NotFound("Dokumen tidak ditemukan")
		}
		return nil, "", "", err
	}

	// data:<mime>;base64,<data>. Legacy rows (file_path only) have no data.
	commaIdx := strings.Index(doc.Data, ",")
	if !strings.HasPrefix(doc.Data, "data:") || commaIdx == -1 {
		return nil, "", "", apperr.NotFound("Berkas dokumen tidak tersedia")
	}
	decoded, err := base64.StdEncoding.DecodeString(doc.Data[commaIdx+1:])
	if err != nil {
		return nil, "", "", fmt.Errorf("decode document base64: %w", err)
	}
	mimeType := doc.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	fileName := doc.FileName
	if fileName == "" {
		fileName = "dokumen"
	}
	return decoded, fileName, mimeType, nil
}

func (s *StudentDocumentService) DeleteDocument(ctx context.Context, schoolID, studentID, docID uuid.UUID) error {
	db := s.conn(ctx)
	if err := ensureStudent(db, schoolID, studentID); err != nil {
		return err
	}
	res := db.Where("id = ? AND student_id = ? AND (school_id = ? OR school_id IS NULL)", docID, studentID, schoolID).
		Delete(&model.StudentDocuments{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("Dokumen tidak ditemukan")
	}
	return nil
}
