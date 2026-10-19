package repository

import (
	"edupilot-gateway/internal/model"
	"gorm.io/gorm"
)

type LogRepository interface {
	SaveRequestLog(log *model.RequestLog) error
	SaveAuditLog(log *model.AuditLog) error
}

type logRepositoryImpl struct {
	db *gorm.DB
}

func NewLogRepository(db *gorm.DB) LogRepository {
	return &logRepositoryImpl{db: db}
}

func (r *logRepositoryImpl) SaveRequestLog(log *model.RequestLog) error {
	return r.db.Create(log).Error
}

func (r *logRepositoryImpl) SaveAuditLog(log *model.AuditLog) error {
	return r.db.Create(log).Error
}
