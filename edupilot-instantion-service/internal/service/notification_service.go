package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// NotificationInput describes one in-app notification (API_CONTRACT §10.5).
type NotificationInput struct {
	UserID   uint
	SchoolID *uuid.UUID
	Type     string
	Title    string
	Body     string
	Data     map[string]any
}

// Notify stores a notification. Best effort: failures are logged and never
// fail the business operation that triggered it.
func Notify(ctx context.Context, in NotificationInput) {
	if in.UserID == 0 || config.DB == nil {
		return
	}
	data := "{}"
	if len(in.Data) > 0 {
		if b, err := json.Marshal(in.Data); err == nil {
			data = string(b)
		}
	}
	n := model.Notification{
		UserID:   in.UserID,
		SchoolID: in.SchoolID,
		Type:     in.Type,
		Title:    in.Title,
		Body:     in.Body,
		Data:     data,
	}
	if err := config.DB.WithContext(context.WithoutCancel(ctx)).Create(&n).Error; err != nil {
		config.Logger.Warn("failed to create notification", zap.Error(err), zap.String("type", in.Type), zap.Uint("user_id", in.UserID))
	}
}

func notificationResponse(n *model.Notification) dto.NotificationResponse {
	data := json.RawMessage(n.Data)
	if len(data) == 0 || !json.Valid(data) {
		data = json.RawMessage("{}")
	}
	return dto.NotificationResponse{
		ID:        n.ID,
		SchoolID:  n.SchoolID,
		Type:      n.Type,
		Title:     n.Title,
		Body:      n.Body,
		Data:      data,
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}

// ListNotifications returns the user's notifications, newest first.
func ListNotifications(ctx context.Context, userID uint, q dto.PageQuery) ([]dto.NotificationResponse, *dto.PageMeta, error) {
	db := config.DB.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", userID)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, nil, err
	}
	var rows []model.Notification
	if err := db.Order("created_at DESC, id DESC").Offset(q.Offset()).Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	out := make([]dto.NotificationResponse, 0, len(rows))
	for i := range rows {
		out = append(out, notificationResponse(&rows[i]))
	}
	pm := dto.NewPageMeta(q, total)
	return out, &pm, nil
}

func UnreadNotificationCount(ctx context.Context, userID uint) (int64, error) {
	var n int64
	err := config.DB.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).Count(&n).Error
	return n, err
}

// MarkNotificationRead marks one of the user's notifications read (idempotent).
func MarkNotificationRead(ctx context.Context, userID uint, id uuid.UUID) (*dto.NotificationResponse, error) {
	db := config.DB.WithContext(ctx)
	var n model.Notification
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&n).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Notifikasi tidak ditemukan")
		}
		return nil, err
	}
	if n.ReadAt == nil {
		now := time.Now()
		if err := db.Model(&n).Update("read_at", now).Error; err != nil {
			return nil, err
		}
		n.ReadAt = &now
	}
	resp := notificationResponse(&n)
	return &resp, nil
}

// MarkAllNotificationsRead returns the number of notifications updated.
func MarkAllNotificationsRead(ctx context.Context, userID uint) (int64, error) {
	res := config.DB.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).Update("read_at", time.Now())
	return res.RowsAffected, res.Error
}
