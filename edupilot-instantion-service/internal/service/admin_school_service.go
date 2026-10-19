package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// UserProvisioner creates/deletes auth users (implemented by client.AuthClient).
type UserProvisioner interface {
	CreateUser(ctx context.Context, req client.CreateUserRequest) (*client.AuthUser, error)
	DeleteUser(ctx context.Context, id uint) error
}

// NewUserProvisioner is a variable so tests can swap the auth client.
var NewUserProvisioner = func() UserProvisioner { return client.NewAuthClient() }

var (
	errAlreadyLinked = apperr.Conflict("ACCOUNT_ALREADY_LINKED", "Akun sudah terhubung ke sekolah")
	errLinkNotFound  = apperr.New(http.StatusNotFound, "ACCOUNT_LINK_NOT_FOUND", "Akun tidak terhubung ke sekolah ini")
)

// AdminListSchools returns a page of schools with account_count.
// status: "active" | "inactive" | "" (all).
func AdminListSchools(ctx context.Context, q dto.PageQuery, status string) ([]dto.AdminSchoolItem, int64, error) {
	db := config.DB.WithContext(ctx)
	base := db.Model(&model.School{})
	if q.Search != "" {
		like := dto.LikePattern(q.Search)
		base = base.Where("(name ILIKE ? OR code ILIKE ? OR npsn ILIKE ?)", like, like, like)
	}
	switch status {
	case "active":
		base = base.Where("is_active = ?", true)
	case "inactive":
		base = base.Where("is_active = ?", false)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var schools []model.School
	if err := base.Session(&gorm.Session{}).Order("created_at DESC").Offset(q.Offset()).Limit(q.Limit).Find(&schools).Error; err != nil {
		return nil, 0, err
	}

	counts := map[uuid.UUID]int64{}
	if len(schools) > 0 {
		ids := make([]uuid.UUID, len(schools))
		for i, s := range schools {
			ids[i] = s.ID
		}
		var rows []struct {
			SchoolID uuid.UUID
			Count    int64
		}
		if err := db.Model(&model.InstitutionAccount{}).
			Select("school_id, COUNT(*) AS count").
			Where("school_id IN ?", ids).
			Group("school_id").Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			counts[r.SchoolID] = r.Count
		}
	}

	items := make([]dto.AdminSchoolItem, len(schools))
	for i, s := range schools {
		items[i] = dto.AdminSchoolItem{School: s, AccountCount: counts[s.ID]}
	}
	return items, total, nil
}

// AdminGetSchool returns one school with account_count.
func AdminGetSchool(ctx context.Context, id uuid.UUID) (*dto.AdminSchoolItem, error) {
	db := config.DB.WithContext(ctx)
	school, err := findSchool(db, id)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := db.Model(&model.InstitutionAccount{}).Where("school_id = ?", id).Count(&count).Error; err != nil {
		return nil, err
	}
	return &dto.AdminSchoolItem{School: *school, AccountCount: count}, nil
}

func findSchool(db *gorm.DB, id uuid.UUID) (*model.School, error) {
	var school model.School
	if err := db.First(&school, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errSchoolNotFound
		}
		return nil, err
	}
	return &school, nil
}

// AdminCreateSchool creates a school and, when req.Account is set, an auth user
// (role institution) linked to it. All-or-nothing: if anything fails after the
// auth user was created, the user is deleted again (compensation) and the DB
// transaction is rolled back.
func AdminCreateSchool(ctx context.Context, req dto.AdminCreateSchoolRequest) (*model.School, error) {
	var school model.School
	var createdUserID uint
	var provisioner UserProvisioner

	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if taken, err := schoolCodeTaken(tx, req.Code, uuid.Nil); err != nil {
			return err
		} else if taken {
			return errSchoolCodeExists
		}

		school = model.School{IsActive: true}
		applySchoolRequest(&school, req.SchoolRequest)
		if err := tx.Create(&school).Error; err != nil {
			return mapSchoolWriteError(err)
		}

		if req.Account == nil {
			return nil
		}

		provisioner = NewUserProvisioner()
		// Detach from client cancellation so we always learn the created user id
		// (needed for compensation); the HTTP client has its own 10s timeout.
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		user, err := provisioner.CreateUser(callCtx, client.CreateUserRequest{
			Email:    req.Account.Email,
			Password: req.Account.Password,
			FullName: req.Account.FullName,
			Phone:    dto.TrimPtr(req.Account.Phone),
			Role:     "institution",
		})
		if err != nil {
			return mapAuthError(err)
		}
		createdUserID = user.ID

		link := model.InstitutionAccount{SchoolID: school.ID, UserID: user.ID}
		if err := tx.Create(&link).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errAlreadyLinked
			}
			return err
		}
		return nil
	})

	if err != nil {
		if createdUserID != 0 && provisioner != nil {
			compensateUser(provisioner, createdUserID)
		}
		return nil, err
	}
	return &school, nil
}

// userDeleter is the part of the auth client needed for compensation.
type userDeleter interface {
	DeleteUser(ctx context.Context, id uint) error
}

// compensateUser deletes an auth user created during a failed operation.
func compensateUser(p userDeleter, userID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.DeleteUser(ctx, userID); err != nil {
		config.Logger.Error("compensation failed: auth user left orphaned",
			zap.Uint("user_id", userID), zap.Error(err))
		return
	}
	config.Logger.Info("compensation: deleted auth user", zap.Uint("user_id", userID))
}

// mapAuthError forwards client errors from the auth service (e.g. email taken)
// and hides everything else behind a 502.
func mapAuthError(err error) error {
	var ae *client.AuthError
	if errors.As(err, &ae) {
		msg := ae.Message
		switch ae.Status {
		case http.StatusConflict:
			if msg == "" {
				msg = "Email sudah terdaftar"
			}
			return apperr.Conflict("EMAIL_EXISTS", msg)
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			if msg == "" {
				msg = "Data akun tidak valid"
			}
			return apperr.BadRequest(msg)
		}
	}
	config.Logger.Error("auth service create user failed", zap.Error(err))
	return apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal membuat akun di layanan autentikasi")
}

func AdminUpdateSchool(ctx context.Context, id uuid.UUID, req dto.SchoolRequest) (*model.School, error) {
	db := config.DB.WithContext(ctx)
	school, err := findSchool(db, id)
	if err != nil {
		return nil, err
	}
	if taken, err := schoolCodeTaken(db, req.Code, school.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, errSchoolCodeExists
	}
	applySchoolRequest(school, req)
	if err := db.Save(school).Error; err != nil {
		return nil, mapSchoolWriteError(err)
	}
	return school, nil
}

func AdminSetSchoolStatus(ctx context.Context, id uuid.UUID, isActive bool) (*model.School, error) {
	db := config.DB.WithContext(ctx)
	school, err := findSchool(db, id)
	if err != nil {
		return nil, err
	}
	if err := db.Model(school).Update("is_active", isActive).Error; err != nil {
		return nil, err
	}
	school.IsActive = isActive
	return school, nil
}

func AdminLinkAccount(ctx context.Context, schoolID uuid.UUID, userID uint) (*model.InstitutionAccount, error) {
	db := config.DB.WithContext(ctx)
	if _, err := findSchool(db, schoolID); err != nil {
		return nil, err
	}
	var count int64
	if err := db.Model(&model.InstitutionAccount{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, errAlreadyLinked
	}
	account := model.InstitutionAccount{SchoolID: schoolID, UserID: userID}
	if err := db.Create(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errAlreadyLinked
		}
		return nil, err
	}
	return &account, nil
}

func AdminUnlinkAccount(ctx context.Context, schoolID uuid.UUID, userID uint) error {
	db := config.DB.WithContext(ctx)
	if _, err := findSchool(db, schoolID); err != nil {
		return err
	}
	res := db.Where("school_id = ? AND user_id = ?", schoolID, userID).Delete(&model.InstitutionAccount{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errLinkNotFound
	}
	return nil
}

func AdminGetLinkedAccounts(ctx context.Context, schoolID uuid.UUID) ([]model.InstitutionAccount, error) {
	db := config.DB.WithContext(ctx)
	if _, err := findSchool(db, schoolID); err != nil {
		return nil, err
	}
	accounts := []model.InstitutionAccount{}
	if err := db.Where("school_id = ?", schoolID).Order("created_at").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

func AdminGetStats(ctx context.Context) (*dto.AdminStats, error) {
	db := config.DB.WithContext(ctx)
	var stats dto.AdminStats
	if err := db.Model(&model.School{}).Count(&stats.TotalSchools).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.School{}).Where("is_active = ?", true).Count(&stats.ActiveSchools).Error; err != nil {
		return nil, err
	}
	stats.InactiveSchools = stats.TotalSchools - stats.ActiveSchools
	if err := db.Model(&model.Employees{}).Where("employee_type = ?", model.EmployeeTypeTeacher).Count(&stats.TotalTeachers).Error; err != nil {
		return nil, err
	}
	return &stats, nil
}
