package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	errSchoolExists     = apperr.Conflict("SCHOOL_ALREADY_EXISTS", "Profil sekolah sudah ada untuk akun ini")
	errSchoolCodeExists = apperr.Conflict("SCHOOL_CODE_EXISTS", "Kode sekolah sudah digunakan")
	errSchoolNotFound   = apperr.New(http.StatusNotFound, "SCHOOL_NOT_FOUND", "Sekolah tidak ditemukan")
)

// ResolveTenantSchool finds the school for a user: institution_accounts.user_id
// first, then schools.customer_id (API_CONTRACT.md §6). Returns (nil, nil) when none.
func ResolveTenantSchool(ctx context.Context, userID uint) (*model.School, error) {
	db := config.DB.WithContext(ctx)
	var school model.School
	err := db.Joins("JOIN institution_accounts ia ON ia.school_id = schools.id").
		Where("ia.user_id = ?", userID).
		First(&school).Error
	if err == nil {
		return &school, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	err = db.Where("customer_id = ? AND customer_id <> 0", userID).Order("created_at").First(&school).Error
	if err == nil {
		return &school, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, err
}

// applySchoolRequest copies request fields onto a school (shared by tenant and admin flows).
func applySchoolRequest(school *model.School, req dto.SchoolRequest) {
	school.Code = req.Code
	school.Name = req.Name
	school.NPSN = req.NPSN
	school.NSS = req.NSS
	school.SchoolLevel = req.SchoolLevel
	school.SchoolStatus = req.SchoolStatus
	school.Email = req.Email
	school.Phone = req.Phone
	school.Website = req.Website
	school.Address = req.Address
	school.ProvinceID = req.ProvinceID.Ptr()
	school.CityID = req.CityID.Ptr()
	school.DistrictID = req.DistrictID.Ptr()
	school.PostalCode = req.PostalCode
	if req.Timezone != "" {
		school.Timezone = req.Timezone
	}
	if school.Timezone == "" {
		school.Timezone = "Asia/Jakarta"
	}
	if req.AllowStudentJoin != nil {
		school.AllowStudentJoin = *req.AllowStudentJoin
	} else if school.ID == uuid.Nil {
		school.AllowStudentJoin = true
	}
}

// mapSchoolWriteError converts unique violations into 409s.
func mapSchoolWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errSchoolCodeExists
	}
	return err
}

// CreateSchool creates the tenant's own school (self-registered owner flow).
func CreateSchool(ctx context.Context, userID uint, req dto.SchoolRequest) (*model.School, error) {
	existing, err := ResolveTenantSchool(ctx, userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errSchoolExists
	}

	db := config.DB.WithContext(ctx)
	if taken, err := schoolCodeTaken(db, req.Code, uuid.Nil); err != nil {
		return nil, err
	} else if taken {
		return nil, errSchoolCodeExists
	}

	school := model.School{CustomerID: userID, IsActive: true}
	applySchoolRequest(&school, req)

	if err := db.Create(&school).Error; err != nil {
		// Unique index on customer_id may also fire on a concurrent create.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			if again, _ := ResolveTenantSchool(ctx, userID); again != nil {
				return nil, errSchoolExists
			}
		}
		return nil, mapSchoolWriteError(err)
	}
	return &school, nil
}

// UpdateSchool updates the tenant school.
func UpdateSchool(ctx context.Context, schoolID uuid.UUID, req dto.SchoolRequest) (*model.School, error) {
	db := config.DB.WithContext(ctx)
	var school model.School
	if err := db.First(&school, "id = ?", schoolID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errSchoolNotFound
		}
		return nil, err
	}
	if taken, err := schoolCodeTaken(db, req.Code, school.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, errSchoolCodeExists
	}

	applySchoolRequest(&school, req)
	if err := db.Save(&school).Error; err != nil {
		return nil, mapSchoolWriteError(err)
	}
	return &school, nil
}

// schoolCodeTaken checks the global unique code (including soft-deleted rows,
// since the unique index covers them).
func schoolCodeTaken(db *gorm.DB, code string, exclude uuid.UUID) (bool, error) {
	var count int64
	q := db.Unscoped().Model(&model.School{}).Where("code = ?", code)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ResolveTeacherTenant finds the non-deleted employee linked to a teacher account
// (employees.user_id) and its school (API_CONTRACT §7.1). Returns (nil, nil, nil)
// when the account is not linked to an existing school.
func ResolveTeacherTenant(ctx context.Context, userID uint) (*model.School, *model.Employees, error) {
	db := config.DB.WithContext(ctx)
	var employee model.Employees
	err := db.Where("user_id = ?", userID).First(&employee).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var school model.School
	err = db.First(&school, "id = ?", employee.SchoolID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &school, &employee, nil
}

// MarkTeacherActivated sets employees.activated_at the first time the teacher
// uses the account (single conditional UPDATE, idempotent).
func MarkTeacherActivated(ctx context.Context, employeeID uuid.UUID) error {
	return config.DB.WithContext(ctx).Model(&model.Employees{}).
		Where("id = ? AND activated_at IS NULL", employeeID).
		UpdateColumn("activated_at", time.Now()).Error
}

// UpdateSchoolLogo sets (dataURL != nil) or clears (nil) schools.logo.
func UpdateSchoolLogo(ctx context.Context, schoolID uuid.UUID, dataURL *string) (*model.School, error) {
	db := config.DB.WithContext(ctx)
	res := db.Model(&model.School{}).Where("id = ?", schoolID).
		Updates(map[string]interface{}{"logo": dataURL, "updated_at": time.Now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errSchoolNotFound
	}
	return findSchool(db, schoolID)
}
