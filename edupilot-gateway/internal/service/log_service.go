package service

import (
	"edupilot-gateway/internal/model"
	"edupilot-gateway/internal/repository"
)

type LogService interface {
	SaveRequestLog(log *model.RequestLog) error
	SaveAuditLog(log *model.AuditLog) error
}

type logServiceImpl struct {
	repo repository.LogRepository
}

func NewLogService(repo repository.LogRepository) LogService {
	return &logServiceImpl{repo: repo}
}

func (s *logServiceImpl) SaveRequestLog(log *model.RequestLog) error {
	return s.repo.SaveRequestLog(log)
}

func (s *logServiceImpl) SaveAuditLog(log *model.AuditLog) error {
	return s.repo.SaveAuditLog(log)
}
