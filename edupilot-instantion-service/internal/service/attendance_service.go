package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AttendanceService struct {
	db *gorm.DB
}

// NewAttendanceService: a nil db means config.DB, resolved per call (the
// controller creates the service at package init, before ConnectDB runs).
func NewAttendanceService(db *gorm.DB) *AttendanceService {
	return &AttendanceService{db: db}
}

func (s *AttendanceService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

// schoolNow converts now to the school's time zone (API_CONTRACT §9).
func (s *AttendanceService) schoolNow(ctx context.Context, schoolID uuid.UUID, now time.Time) (time.Time, *time.Location) {
	loc := schoolLocation(ctx, s.conn(ctx), schoolID)
	return now.In(loc), loc
}

// resolveDate parses a "YYYY-MM-DD" query value; empty → today in the school tz.
func (s *AttendanceService) resolveDate(ctx context.Context, schoolID uuid.UUID, dateStr string) (time.Time, string, error) {
	now, loc := s.schoolNow(ctx, schoolID, time.Now())
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return now, now.Format(dateLayout), nil
	}
	d, err := time.ParseInLocation(dateLayout, dateStr, loc)
	if err != nil {
		return time.Time{}, "", apperr.BadRequest("Format tanggal harus YYYY-MM-DD")
	}
	return d, dateStr, nil
}

// lockKey serializes writes of one logical attendance row for the rest of the tx
// (works even when a guarded unique index was skipped on a legacy database).
func lockKey(tx *gorm.DB, key string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key).Error
}

var (
	errForbiddenSession = apperr.New(http.StatusForbidden, "FORBIDDEN", "Sesi mengajar ini bukan milik Anda")
	errSessionNotFound  = apperr.NotFound("Sesi mengajar tidak ditemukan")
	errNotCheckedIn     = apperr.New(http.StatusBadRequest, "NOT_CHECKED_IN", "Anda belum melakukan presensi kedatangan hari ini")
	errAlreadyIn        = apperr.Conflict("ALREADY_CHECKED_IN", "Anda sudah melakukan presensi kedatangan hari ini")
	errAlreadyOut       = apperr.Conflict("ALREADY_CHECKED_OUT", "Anda sudah melakukan presensi kepulangan hari ini")
)

// =========================================================================
// Configuration & Shifts
// =========================================================================

func (s *AttendanceService) GetOrCreateConfig(ctx context.Context, schoolID uuid.UUID) (*model.AttendanceConfig, error) {
	var cfg model.AttendanceConfig
	err := s.conn(ctx).Where("school_id = ?", schoolID).First(&cfg).Error
	if err == nil {
		return &cfg, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Create default config (ON CONFLICT: two first requests may race).
	cfg = model.AttendanceConfig{
		SchoolID:              schoolID,
		SchoolLatitude:        -6.200000,
		SchoolLongitude:       106.816666,
		RadiusMeters:          150,
		LocationName:          "Kampus Utama",
		DefaultCheckInTime:    "07:00",
		DefaultCheckOutTime:   "15:00",
		LateToleranceMinutes:  15,
		TeacherScheduleMode:   "first_schedule",
		AllowSelfCheckInClass: true,
		CodeValidMinutes:      30,
		AllowRemoteCheckIn:    true,
	}
	if err := s.conn(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "school_id"}}, DoNothing: true}).
		Create(&cfg).Error; err != nil {
		return nil, err
	}
	var saved model.AttendanceConfig
	if err := s.conn(ctx).Where("school_id = ?", schoolID).First(&saved).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *AttendanceService) UpdateConfig(ctx context.Context, schoolID uuid.UUID, req *dto.AttendanceConfigRequest) (*model.AttendanceConfig, error) {
	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}

	lat, lng := cfg.SchoolLatitude, cfg.SchoolLongitude
	if req.SchoolLatitude != nil {
		lat = *req.SchoolLatitude
	}
	if req.SchoolLongitude != nil {
		lng = *req.SchoolLongitude
	}
	if !ValidCoordinates(lat, lng) {
		return nil, apperr.BadRequest("Koordinat sekolah tidak valid (latitude -90..90, longitude -180..180)")
	}
	cfg.SchoolLatitude, cfg.SchoolLongitude = lat, lng
	if req.RadiusMeters != nil {
		if *req.RadiusMeters < 10 || *req.RadiusMeters > 10000 {
			return nil, apperr.BadRequest("Radius harus antara 10 dan 10000 meter")
		}
		cfg.RadiusMeters = *req.RadiusMeters
	}
	if req.LocationName != nil {
		name := strings.TrimSpace(*req.LocationName)
		if len(name) > 255 {
			return nil, apperr.BadRequest("Nama lokasi maksimal 255 karakter")
		}
		if name != "" {
			cfg.LocationName = name
		}
	}
	if req.DefaultCheckInTime != nil && *req.DefaultCheckInTime != "" {
		if !ValidHHMM(*req.DefaultCheckInTime) {
			return nil, apperr.BadRequest("Jam masuk harus berformat HH:MM")
		}
		cfg.DefaultCheckInTime = *req.DefaultCheckInTime
	}
	if req.DefaultCheckOutTime != nil && *req.DefaultCheckOutTime != "" {
		if !ValidHHMM(*req.DefaultCheckOutTime) {
			return nil, apperr.BadRequest("Jam pulang harus berformat HH:MM")
		}
		cfg.DefaultCheckOutTime = *req.DefaultCheckOutTime
	}
	if cfg.DefaultCheckOutTime <= cfg.DefaultCheckInTime {
		return nil, apperr.BadRequest("Jam pulang harus setelah jam masuk")
	}
	if req.LateToleranceMinutes != nil {
		if *req.LateToleranceMinutes < 0 || *req.LateToleranceMinutes > 240 {
			return nil, apperr.BadRequest("Toleransi keterlambatan harus antara 0 dan 240 menit")
		}
		cfg.LateToleranceMinutes = *req.LateToleranceMinutes
	}
	if req.TeacherScheduleMode != nil && *req.TeacherScheduleMode != "" {
		mode := strings.TrimSpace(*req.TeacherScheduleMode)
		if mode != "first_schedule" && mode != "fixed" {
			return nil, apperr.BadRequest("teacher_schedule_mode harus 'first_schedule' atau 'fixed'")
		}
		cfg.TeacherScheduleMode = mode
	}
	if req.AllowSelfCheckInClass != nil {
		cfg.AllowSelfCheckInClass = *req.AllowSelfCheckInClass
	}
	if req.AllowRemoteCheckIn != nil {
		cfg.AllowRemoteCheckIn = *req.AllowRemoteCheckIn
	}
	if req.CodeValidMinutes != nil {
		if *req.CodeValidMinutes < 1 || *req.CodeValidMinutes > 240 {
			return nil, apperr.BadRequest("Masa berlaku kode harus antara 1 dan 240 menit")
		}
		cfg.CodeValidMinutes = *req.CodeValidMinutes
	}

	if err := s.conn(ctx).Save(cfg).Error; err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *AttendanceService) ListShifts(ctx context.Context, schoolID uuid.UUID) ([]model.AttendanceShift, error) {
	shifts := []model.AttendanceShift{}
	err := s.conn(ctx).
		Where("school_id = ?", schoolID).
		Order("start_time ASC, name ASC").
		Find(&shifts).Error
	return shifts, err
}

// validateShift normalizes and validates a shift request.
func validateShift(req *dto.AttendanceShiftRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.StartTime = strings.TrimSpace(req.StartTime)
	req.EndTime = strings.TrimSpace(req.EndTime)
	req.CheckOutStartTime = strings.TrimSpace(req.CheckOutStartTime)
	if req.Name == "" || len(req.Name) > 100 {
		return apperr.BadRequest("Nama shift wajib diisi (maksimal 100 karakter)")
	}
	if !ValidHHMM(req.StartTime) || !ValidHHMM(req.EndTime) {
		return apperr.BadRequest("Jam mulai dan jam selesai harus berformat HH:MM")
	}
	if req.EndTime <= req.StartTime {
		return apperr.BadRequest("Jam selesai harus setelah jam mulai")
	}
	if req.CheckOutStartTime == "" {
		req.CheckOutStartTime = req.EndTime
	}
	if !ValidHHMM(req.CheckOutStartTime) {
		return apperr.BadRequest("Jam mulai pulang harus berformat HH:MM")
	}
	if req.CheckOutStartTime <= req.StartTime {
		return apperr.BadRequest("Jam mulai pulang harus setelah jam mulai")
	}
	if req.LateToleranceMinutes != nil && (*req.LateToleranceMinutes < 0 || *req.LateToleranceMinutes > 240) {
		return apperr.BadRequest("Toleransi keterlambatan harus antara 0 dan 240 menit")
	}
	return nil
}

// saveShiftTx keeps at most one default shift per school.
func saveShiftTx(tx *gorm.DB, shift *model.AttendanceShift, create bool) error {
	if err := lockKey(tx, "attendance_shifts:"+shift.SchoolID.String()); err != nil {
		return err
	}
	var dup int64
	q := tx.Model(&model.AttendanceShift{}).Where("school_id = ? AND lower(name) = lower(?)", shift.SchoolID, shift.Name)
	if !create {
		q = q.Where("id <> ?", shift.ID)
	}
	if err := q.Count(&dup).Error; err != nil {
		return err
	}
	if dup > 0 {
		return apperr.Conflict("DUPLICATE", "Nama shift sudah digunakan")
	}
	if shift.IsDefault {
		q := tx.Model(&model.AttendanceShift{}).Where("school_id = ? AND is_default = true", shift.SchoolID)
		if !create {
			q = q.Where("id <> ?", shift.ID)
		}
		if err := q.Updates(map[string]interface{}{"is_default": false, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
	}
	if create {
		// GORM skips zero values that have a column default (tolerance 0 → 15), so persist them explicitly.
		tolerance := shift.LateToleranceMinutes
		if err := tx.Create(shift).Error; err != nil {
			return err
		}
		if tolerance != shift.LateToleranceMinutes {
			shift.LateToleranceMinutes = tolerance
			return tx.Model(shift).Update("late_tolerance_minutes", tolerance).Error
		}
		return nil
	}
	return tx.Save(shift).Error
}

func (s *AttendanceService) CreateShift(ctx context.Context, schoolID uuid.UUID, req *dto.AttendanceShiftRequest) (*model.AttendanceShift, error) {
	if err := validateShift(req); err != nil {
		return nil, err
	}
	shift := model.AttendanceShift{
		SchoolID:             schoolID,
		Name:                 req.Name,
		StartTime:            req.StartTime,
		EndTime:              req.EndTime,
		LateToleranceMinutes: 15,
		CheckOutStartTime:    req.CheckOutStartTime,
		IsDefault:            req.IsDefault,
	}
	if req.LateToleranceMinutes != nil {
		shift.LateToleranceMinutes = *req.LateToleranceMinutes
	}

	if err := s.conn(ctx).Transaction(func(tx *gorm.DB) error { return saveShiftTx(tx, &shift, true) }); err != nil {
		return nil, err
	}
	return &shift, nil
}

func (s *AttendanceService) UpdateShift(ctx context.Context, schoolID, shiftID uuid.UUID, req *dto.AttendanceShiftRequest) (*model.AttendanceShift, error) {
	if err := validateShift(req); err != nil {
		return nil, err
	}
	var shift model.AttendanceShift
	if err := s.conn(ctx).Where("id = ? AND school_id = ?", shiftID, schoolID).First(&shift).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Shift tidak ditemukan")
		}
		return nil, err
	}

	shift.Name = req.Name
	shift.StartTime = req.StartTime
	shift.EndTime = req.EndTime
	if req.LateToleranceMinutes != nil {
		shift.LateToleranceMinutes = *req.LateToleranceMinutes
	}
	shift.CheckOutStartTime = req.CheckOutStartTime
	shift.IsDefault = req.IsDefault

	if err := s.conn(ctx).Transaction(func(tx *gorm.DB) error { return saveShiftTx(tx, &shift, false) }); err != nil {
		return nil, err
	}
	return &shift, nil
}

func (s *AttendanceService) DeleteShift(ctx context.Context, schoolID, shiftID uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var used int64
		if err := tx.Model(&model.ClassAttendanceShift{}).Where("school_id = ? AND shift_id = ?", schoolID, shiftID).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Shift masih digunakan oleh %d kelas", used))
		}
		res := tx.Where("id = ? AND school_id = ?", shiftID, schoolID).Delete(&model.AttendanceShift{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("Shift tidak ditemukan")
		}
		return nil
	})
}

func (s *AttendanceService) AssignClassShift(ctx context.Context, schoolID uuid.UUID, req *dto.AssignClassShiftRequest) (*model.ClassAttendanceShift, error) {
	db := s.conn(ctx)
	var n int64
	if err := db.Model(&model.Classes{}).Where("id = ? AND school_id = ?", req.ClassID, schoolID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, apperr.NotFound("Kelas tidak ditemukan")
	}
	if err := db.Model(&model.AttendanceShift{}).Where("id = ? AND school_id = ?", req.ShiftID, schoolID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, apperr.NotFound("Shift tidak ditemukan")
	}

	mapping := model.ClassAttendanceShift{
		SchoolID: schoolID,
		ClassID:  req.ClassID,
		ShiftID:  req.ShiftID,
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "class_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"shift_id", "updated_at"}),
	}).Create(&mapping).Error
	if err != nil {
		return nil, err
	}
	var saved model.ClassAttendanceShift
	if err := db.Where("school_id = ? AND class_id = ?", schoolID, req.ClassID).First(&saved).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func (s *AttendanceService) ListClassShifts(ctx context.Context, schoolID uuid.UUID) ([]model.ClassAttendanceShift, error) {
	list := []model.ClassAttendanceShift{}
	err := s.conn(ctx).
		Preload("Shift").
		Preload("Class").
		Where("school_id = ?", schoolID).
		Find(&list).Error
	return list, err
}

// =========================================================================
// Targets (shift / first schedule)
// =========================================================================

// dailyTarget is the expected arrival/departure for one person on one day.
type dailyTarget struct {
	CheckIn       string
	CheckOut      string
	Tolerance     int
	ShiftID       *uuid.UUID
	FirstSchedule string
}

func configTarget(cfg *model.AttendanceConfig) dailyTarget {
	return dailyTarget{CheckIn: cfg.DefaultCheckInTime, CheckOut: cfg.DefaultCheckOutTime, Tolerance: cfg.LateToleranceMinutes}
}

func shiftTarget(sh *model.AttendanceShift) dailyTarget {
	out := sh.CheckOutStartTime
	if out == "" {
		out = sh.EndTime
	}
	id := sh.ID
	return dailyTarget{CheckIn: sh.StartTime, CheckOut: out, Tolerance: sh.LateToleranceMinutes, ShiftID: &id}
}

func defaultShift(db *gorm.DB, schoolID uuid.UUID) *model.AttendanceShift {
	var sh model.AttendanceShift
	if err := db.Where("school_id = ? AND is_default = true", schoolID).Order("start_time").First(&sh).Error; err != nil {
		return nil
	}
	return &sh
}

// activeScheduleScope returns the active academic year / semester of a school.
func activeScheduleScope(db *gorm.DB, schoolID uuid.UUID) (yearID, semID *uuid.UUID) {
	var y model.AcademicYear
	if err := db.Where("school_id = ? AND is_active = true", schoolID).First(&y).Error; err != nil {
		return nil, nil
	}
	sem, err := activeSemesterOf(db, y.ID)
	if err != nil || sem == nil {
		return &y.ID, nil
	}
	return &y.ID, &sem.ID
}

// todaySchedules is the base query for active schedules on a weekday in the
// active academic year / semester (legacy rows without year/semester included).
func todaySchedules(db *gorm.DB, schoolID uuid.UUID, weekday int) *gorm.DB {
	yearID, semID := activeScheduleScope(db, schoolID)
	q := db.Model(&model.TeacherSchedules{}).
		Where("teacher_schedules.school_id = ? AND teacher_schedules.day_of_week = ?", schoolID, weekday).
		Where("COALESCE(lower(teacher_schedules.status), 'active') NOT IN ?", inactiveScheduleStatuses)
	return applyScheduleScope(q, yearID, semID)
}

// teacherTarget: first_schedule → start of the teacher's first schedule that day
// (fallback default shift / config); fixed → default shift / config.
func (s *AttendanceService) teacherTarget(db *gorm.DB, cfg *model.AttendanceConfig, schoolID, teacherID uuid.UUID, day time.Time) dailyTarget {
	t := configTarget(cfg)
	if sh := defaultShift(db, schoolID); sh != nil {
		t = shiftTarget(sh)
	}
	if cfg.TeacherScheduleMode == "first_schedule" {
		var starts []string
		if err := todaySchedules(db, schoolID, isoWeekday(day)).
			Where("teacher_schedules.employee_id = ?", teacherID).
			Pluck("teacher_schedules.start_time", &starts).Error; err != nil {
			config.Logger.Warn("first schedule lookup failed", zap.Error(err))
		}
		if first := FirstScheduleTarget(starts); first != "" {
			t.FirstSchedule = first
			t.CheckIn = first
			t.Tolerance = cfg.LateToleranceMinutes
		}
	}
	return t
}

// studentTarget: class shift → default shift → config.
func studentTarget(db *gorm.DB, cfg *model.AttendanceConfig, schoolID uuid.UUID, classID *uuid.UUID) dailyTarget {
	if classID != nil {
		var m model.ClassAttendanceShift
		if err := db.Preload("Shift").Where("school_id = ? AND class_id = ?", schoolID, *classID).First(&m).Error; err == nil && m.Shift != nil {
			return shiftTarget(m.Shift)
		}
	}
	if sh := defaultShift(db, schoolID); sh != nil {
		return shiftTarget(sh)
	}
	return configTarget(cfg)
}

// EnsureTeacher checks that an employee id (e.g. ?employee_id= from a manager)
// is a non-deleted teacher of the school.
func (s *AttendanceService) EnsureTeacher(ctx context.Context, schoolID, teacherID uuid.UUID) error {
	var n int64
	if err := teacherScope(s.conn(ctx), schoolID).Where("id = ?", teacherID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("Guru tidak ditemukan")
	}
	return nil
}

// =========================================================================
// Teacher Daily Attendance (Arrival & Departure)
// =========================================================================

func (s *AttendanceService) GetTeacherTodayStatus(ctx context.Context, schoolID, teacherID uuid.UUID, dateStr string) (*dto.TodayTeacherStatusResponse, error) {
	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	date, dateStr, err := s.resolveDate(ctx, schoolID, dateStr)
	if err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	target := s.teacherTarget(db, cfg, schoolID, teacherID, date)

	resp := &dto.TodayTeacherStatusResponse{
		Date:                 dateStr,
		Timezone:             date.Location().String(),
		TargetCheckInTime:    target.CheckIn,
		TargetCheckOutTime:   target.CheckOut,
		FirstScheduleTime:    target.FirstSchedule,
		LateToleranceMinutes: target.Tolerance,
		Config:               cfg,
	}

	var daily model.DailyAttendance
	err = db.Where("school_id = ? AND employee_id = ? AND date = ?", schoolID, teacherID, dateStr).First(&daily).Error
	switch {
	case err == nil:
		resp.DailyAttendance = daily
		resp.IsCheckedIn = daily.CheckInTime != nil
		resp.IsCheckedOut = daily.CheckOutTime != nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		resp.DailyAttendance = nil
	default:
		return nil, err
	}
	return resp, nil
}

func (s *AttendanceService) TeacherCheckIn(ctx context.Context, schoolID, teacherID uuid.UUID, req *dto.CheckInRequest, now time.Time) (*model.DailyAttendance, error) {
	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	now, _ = s.schoolNow(ctx, schoolID, now)
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)

	method := strings.ToLower(strings.TrimSpace(req.Method))
	if method == "" {
		method = "gps"
	}
	notes := strings.TrimSpace(req.Notes)
	if len(notes) > 1000 {
		return nil, apperr.BadRequest("Keterangan maksimal 1000 karakter")
	}

	status := "present"
	var distance *float64
	if remoteStatus, isRemote := remoteMethodStatus[method]; isRemote {
		if !cfg.AllowRemoteCheckIn {
			return nil, apperr.New(http.StatusForbidden, "REMOTE_CHECK_IN_DISABLED", "Presensi PJJ/WFH/Dinas Luar tidak diizinkan oleh sekolah")
		}
		if notes == "" {
			return nil, apperr.BadRequest("Keterangan wajib diisi untuk presensi PJJ/WFH/Dinas Luar")
		}
		status = remoteStatus
		if req.Latitude != nil && req.Longitude != nil {
			if !ValidCoordinates(*req.Latitude, *req.Longitude) {
				return nil, apperr.BadRequest("Koordinat lokasi tidak valid (latitude -90..90, longitude -180..180)")
			}
			d := math.Round(Haversine(cfg.SchoolLatitude, cfg.SchoolLongitude, *req.Latitude, *req.Longitude)*100) / 100
			distance = &d
		}
	} else if method == "gps" {
		geo, err := CheckGeofence(cfg.SchoolLatitude, cfg.SchoolLongitude, cfg.RadiusMeters, req.Latitude, req.Longitude, req.Accuracy)
		if err != nil {
			return nil, err
		}
		distance = &geo.Distance
	} else {
		return nil, apperr.BadRequest("Metode presensi harus salah satu dari: gps, pjj, wfh, dinas_luar")
	}

	db := s.conn(ctx)
	target := s.teacherTarget(db, cfg, schoolID, teacherID, now)
	late, lateMinutes := LateMinutes(now, target.CheckIn, target.Tolerance)
	checkInStatus := "on_time"
	if late {
		checkInStatus = "late"
		if status == "present" {
			status = "late"
		}
	}

	var daily model.DailyAttendance
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "daily:"+schoolID.String()+":t:"+teacherID.String()+":"+dateStr); err != nil {
			return err
		}
		err := tx.Where("school_id = ? AND employee_id = ? AND date = ?", schoolID, teacherID, dateStr).First(&daily).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		if exists && daily.CheckInTime != nil {
			return errAlreadyIn
		}
		if !exists {
			daily = model.DailyAttendance{SchoolID: schoolID, Date: dateStr, UserType: "teacher", EmployeeID: &teacherID}
		}
		daily.Status = status
		daily.ShiftID = target.ShiftID
		daily.CheckInTime = &timeStr
		daily.CheckInStatus = checkInStatus
		daily.LateMinutes = lateMinutes
		daily.CheckInMethod = method
		daily.CheckInLat = req.Latitude
		daily.CheckInLng = req.Longitude
		daily.CheckInDistance = distance
		daily.CheckInNotes = notes
		daily.AttachmentURL = strings.TrimSpace(req.AttachmentURL)
		if exists {
			return tx.Save(&daily).Error
		}
		return tx.Create(&daily).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errAlreadyIn
		}
		return nil, err
	}
	return &daily, nil
}

func (s *AttendanceService) TeacherCheckOut(ctx context.Context, schoolID, teacherID uuid.UUID, req *dto.CheckOutRequest, now time.Time) (*model.DailyAttendance, error) {
	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	now, _ = s.schoolNow(ctx, schoolID, now)
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)
	db := s.conn(ctx)

	var daily model.DailyAttendance
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "daily:"+schoolID.String()+":t:"+teacherID.String()+":"+dateStr); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND employee_id = ? AND date = ?", schoolID, teacherID, dateStr).First(&daily).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errNotCheckedIn
			}
			return err
		}
		if daily.CheckInTime == nil {
			return errNotCheckedIn
		}
		if daily.CheckOutTime != nil {
			return errAlreadyOut
		}

		method := strings.ToLower(strings.TrimSpace(req.Method))
		if method == "" {
			method = "gps"
			if _, remote := remoteMethodStatus[daily.CheckInMethod]; remote {
				method = daily.CheckInMethod
			}
		}
		var distance *float64
		if _, remote := remoteMethodStatus[method]; remote {
			if !cfg.AllowRemoteCheckIn {
				return apperr.New(http.StatusForbidden, "REMOTE_CHECK_IN_DISABLED", "Presensi PJJ/WFH/Dinas Luar tidak diizinkan oleh sekolah")
			}
			if req.Latitude != nil && req.Longitude != nil {
				if !ValidCoordinates(*req.Latitude, *req.Longitude) {
					return apperr.BadRequest("Koordinat lokasi tidak valid (latitude -90..90, longitude -180..180)")
				}
				d := math.Round(Haversine(cfg.SchoolLatitude, cfg.SchoolLongitude, *req.Latitude, *req.Longitude)*100) / 100
				distance = &d
			}
		} else if method == "gps" {
			geo, err := CheckGeofence(cfg.SchoolLatitude, cfg.SchoolLongitude, cfg.RadiusMeters, req.Latitude, req.Longitude, req.Accuracy)
			if err != nil {
				return err
			}
			distance = &geo.Distance
		} else {
			return apperr.BadRequest("Metode presensi harus salah satu dari: gps, pjj, wfh, dinas_luar")
		}

		target := s.teacherTarget(tx, cfg, schoolID, teacherID, now)
		early, earlyMinutes := EarlyMinutes(now, target.CheckOut)
		checkOutStatus := "on_time"
		if early {
			checkOutStatus = "early"
		}

		daily.CheckOutTime = &timeStr
		daily.CheckOutStatus = checkOutStatus
		daily.EarlyMinutes = earlyMinutes
		daily.CheckOutMethod = method
		daily.CheckOutLat = req.Latitude
		daily.CheckOutLng = req.Longitude
		daily.CheckOutDistance = distance
		if notes := strings.TrimSpace(req.Notes); notes != "" {
			if len(notes) > 1000 {
				return apperr.BadRequest("Keterangan maksimal 1000 karakter")
			}
			daily.CheckOutNotes = notes
		}
		return tx.Save(&daily).Error
	})
	if err != nil {
		return nil, err
	}
	return &daily, nil
}

// =========================================================================
// General Daily Attendance & Admin Manual Records
// =========================================================================

func (s *AttendanceService) RecordManualDailyAttendance(ctx context.Context, schoolID uuid.UUID, adminUserID uint, req *dto.ManualDailyAttendanceRequest) (*model.DailyAttendance, error) {
	req.Date = strings.TrimSpace(req.Date)
	req.Status = strings.TrimSpace(req.Status)
	if req.Date == "" {
		return nil, apperr.BadRequest("Tanggal presensi wajib diisi")
	}
	if _, err := time.Parse(dateLayout, req.Date); err != nil {
		return nil, apperr.BadRequest("Format tanggal harus YYYY-MM-DD")
	}
	if req.UserType != "teacher" && req.UserType != "student" {
		return nil, apperr.BadRequest("Tipe pengguna harus 'teacher' atau 'student'")
	}
	if req.UserType == "teacher" && (req.EmployeeID == nil || *req.EmployeeID == uuid.Nil) {
		return nil, apperr.BadRequest("Employee ID wajib diisi untuk presensi guru")
	}
	if req.UserType == "student" && (req.StudentID == nil || *req.StudentID == uuid.Nil) {
		return nil, apperr.BadRequest("Student ID wajib diisi untuk presensi siswa")
	}
	if !dailyStatuses[req.Status] {
		return nil, apperr.BadRequest("Status harus salah satu dari: present, late, sick, permission, business_trip, remote, absent")
	}
	for _, t := range []*string{req.CheckInTime, req.CheckOutTime} {
		if t != nil && *t != "" && !ValidClock(*t) {
			return nil, apperr.BadRequest("Jam presensi harus berformat HH:MM atau HH:MM:SS")
		}
	}
	if req.CheckInTime != nil && *req.CheckInTime == "" {
		req.CheckInTime = nil
	}
	if req.CheckOutTime != nil && *req.CheckOutTime == "" {
		req.CheckOutTime = nil
	}
	if req.LateMinutes < 0 || req.LateMinutes > 1440 || req.EarlyMinutes < 0 || req.EarlyMinutes > 1440 {
		return nil, apperr.BadRequest("Menit terlambat/pulang awal tidak valid")
	}
	if len(req.Notes) > 1000 {
		return nil, apperr.BadRequest("Keterangan maksimal 1000 karakter")
	}

	db := s.conn(ctx)
	var studentUserID *uint
	if req.UserType == "teacher" {
		if err := s.EnsureTeacher(ctx, schoolID, *req.EmployeeID); err != nil {
			return nil, err
		}
		req.StudentID = nil
	} else {
		var st model.Students
		if err := db.Select("id, user_id").Where("id = ? AND school_id = ?", *req.StudentID, schoolID).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, apperr.NotFound("Siswa tidak ditemukan")
			}
			return nil, err
		}
		studentUserID = st.UserID
		req.EmployeeID = nil
		if req.ClassID == nil || *req.ClassID == uuid.Nil {
			if h := activeHistoryOf(db, schoolID, st.ID); h != nil {
				req.ClassID = &h.ClassID
			} else {
				req.ClassID = nil
			}
		}
	}
	if req.ClassID != nil && *req.ClassID != uuid.Nil {
		var n int64
		if err := db.Model(&model.Classes{}).Where("id = ? AND school_id = ?", *req.ClassID, schoolID).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, apperr.NotFound("Kelas tidak ditemukan")
		}
	} else {
		req.ClassID = nil
	}

	var record model.DailyAttendance
	err := db.Transaction(func(tx *gorm.DB) error {
		q := tx.Where("school_id = ? AND date = ?", schoolID, req.Date)
		var key string
		if req.UserType == "teacher" {
			key = "daily:" + schoolID.String() + ":t:" + req.EmployeeID.String() + ":" + req.Date
			q = q.Where("employee_id = ?", *req.EmployeeID)
		} else {
			key = "daily:" + schoolID.String() + ":s:" + req.StudentID.String() + ":" + req.Date
			q = q.Where("student_id = ?", *req.StudentID)
		}
		if err := lockKey(tx, key); err != nil {
			return err
		}
		err := q.First(&record).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		if !exists {
			record = model.DailyAttendance{
				SchoolID:   schoolID,
				Date:       req.Date,
				UserType:   req.UserType,
				EmployeeID: req.EmployeeID,
				StudentID:  req.StudentID,
			}
		}
		record.ClassID = req.ClassID
		record.Status = req.Status
		record.CheckInTime = req.CheckInTime
		record.CheckOutTime = req.CheckOutTime
		record.LateMinutes = req.LateMinutes
		record.EarlyMinutes = req.EarlyMinutes
		record.CheckInMethod = "manual"
		record.CheckInNotes = req.Notes
		record.AttachmentURL = req.AttachmentURL
		record.ApprovedByUserID = &adminUserID
		if exists {
			return tx.Save(&record).Error
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return nil, err
	}

	if studentUserID != nil {
		Notify(ctx, NotificationInput{
			UserID:   *studentUserID,
			SchoolID: &schoolID,
			Type:     model.NotifAttendanceMarked,
			Title:    "Presensi harian diperbarui",
			Body:     fmt.Sprintf("Presensi Anda tanggal %s dicatat sekolah sebagai %s.", req.Date, statusLabel(req.Status)),
			Data:     map[string]any{"date": req.Date, "status": req.Status, "kind": "daily"},
		})
	}
	return &record, nil
}

func (s *AttendanceService) ListDailyAttendance(ctx context.Context, schoolID uuid.UUID, date string, userType string, classID *uuid.UUID, status string) ([]model.DailyAttendance, error) {
	q := s.conn(ctx).
		Preload("Employee").
		Preload("Student", func(db *gorm.DB) *gorm.DB { return db.Omit("photo") }).
		Preload("Class").
		Where("school_id = ?", schoolID)

	if date != "" {
		if _, err := time.Parse(dateLayout, date); err != nil {
			return nil, apperr.BadRequest("Format tanggal harus YYYY-MM-DD")
		}
		q = q.Where("date = ?", date)
	}
	if userType != "" {
		if userType != "teacher" && userType != "student" {
			return nil, apperr.BadRequest("user_type harus 'teacher' atau 'student'")
		}
		q = q.Where("user_type = ?", userType)
	}
	if classID != nil && *classID != uuid.Nil {
		q = q.Where("class_id = ?", classID)
	}
	if status != "" {
		if !dailyStatuses[status] {
			return nil, apperr.BadRequest("Status presensi tidak valid")
		}
		q = q.Where("status = ?", status)
	}

	list := []model.DailyAttendance{}
	err := q.Order("created_at DESC").Limit(2000).Find(&list).Error
	return list, err
}

func (s *AttendanceService) GetDailyAttendanceSummary(ctx context.Context, schoolID uuid.UUID, dateStr string, userType string) (*dto.DailyAttendanceSummaryResponse, error) {
	_, dateStr, err := s.resolveDate(ctx, schoolID, dateStr)
	if err != nil {
		return nil, err
	}
	if userType != "" && userType != "teacher" && userType != "student" {
		return nil, apperr.BadRequest("user_type harus 'teacher' atau 'student'")
	}
	db := s.conn(ctx)

	q := db.Model(&model.DailyAttendance{}).Where("school_id = ? AND date = ?", schoolID, dateStr)
	if userType != "" {
		q = q.Where("user_type = ?", userType)
	}

	var rows []struct {
		Status string
		Count  int
	}
	if err := q.Select("status, COUNT(*) as count").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}

	summary := dto.DailyAttendanceSummaryResponse{Date: dateStr}
	presentTotal := 0
	for _, r := range rows {
		switch r.Status {
		case "present":
			summary.PresentCount = r.Count
			presentTotal += r.Count
		case "late":
			summary.LateCount = r.Count
			presentTotal += r.Count
		case "sick":
			summary.SickCount = r.Count
		case "permission":
			summary.PermissionCount = r.Count
		case "remote":
			summary.RemoteCount = r.Count
			presentTotal += r.Count
		case "business_trip":
			summary.BusinessTripCount = r.Count
			presentTotal += r.Count
		case "absent":
			summary.AbsentCount = r.Count
		}
	}

	// Expected = active teachers and/or active students placed in a class.
	var teachers, students int64
	if userType != "student" {
		if err := teacherScope(db, schoolID).
			Where("COALESCE(lower(status), '') NOT IN ?", inactiveTeacherStatuses).
			Count(&teachers).Error; err != nil {
			return nil, err
		}
	}
	if userType != "teacher" {
		if err := db.Raw(`SELECT COUNT(DISTINCT s.id) FROM students s
			JOIN student_class_histories h ON h.student_id = s.id AND h.school_id = s.school_id AND h.status = 'active'
			WHERE s.school_id = ? AND s.deleted_at IS NULL AND s.status = 'active'`, schoolID).Scan(&students).Error; err != nil {
			return nil, err
		}
	}

	summary.TotalExpected = int(teachers + students)
	if summary.TotalExpected > 0 {
		summary.PresentRate = math.Round((float64(presentTotal)/float64(summary.TotalExpected))*1000) / 10
	}
	return &summary, nil
}

// inactiveTeacherStatuses are employees.status values excluded from "expected".
var inactiveTeacherStatuses = []string{"inactive", "nonaktif", "non-aktif", "tidak aktif", "resign", "resigned", "keluar"}

// =========================================================================
// Teaching Sessions & Classroom Attendance (KBM)
// =========================================================================

// buildMonitorItems turns schedules into monitor rows using three batched
// queries (sessions, present counts, class sizes) instead of N+1.
func (s *AttendanceService) buildMonitorItems(db *gorm.DB, schoolID uuid.UUID, schedules []model.TeacherSchedules, dateStr string) ([]dto.LiveClassMonitorItem, error) {
	items := make([]dto.LiveClassMonitorItem, 0, len(schedules))
	if len(schedules) == 0 {
		return items, nil
	}
	scheduleIDs := make([]uuid.UUID, 0, len(schedules))
	classIDs := make([]uuid.UUID, 0, len(schedules))
	for _, sch := range schedules {
		scheduleIDs = append(scheduleIDs, sch.ID)
		classIDs = append(classIDs, sch.ClassID)
	}

	var sessions []model.ClassTeachingSession
	if err := db.Where("school_id = ? AND date = ? AND schedule_id IN ?", schoolID, dateStr, scheduleIDs).Find(&sessions).Error; err != nil {
		return nil, err
	}
	bySchedule := make(map[uuid.UUID]model.ClassTeachingSession, len(sessions))
	sessionIDs := make([]uuid.UUID, 0, len(sessions))
	for _, sess := range sessions {
		bySchedule[sess.ScheduleID] = sess
		sessionIDs = append(sessionIDs, sess.ID)
	}

	present := map[uuid.UUID]int{}
	if len(sessionIDs) > 0 {
		var rows []struct {
			SessionID uuid.UUID
			Count     int
		}
		if err := db.Model(&model.StudentSessionAttendance{}).
			Select("session_id, COUNT(*) AS count").
			Where("school_id = ? AND status = 'present' AND session_id IN ?", schoolID, sessionIDs).
			Group("session_id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			present[r.SessionID] = r.Count
		}
	}

	totals, err := classMemberCounts(db, schoolID, classIDs)
	if err != nil {
		return nil, err
	}

	for _, sch := range schedules {
		item := dto.LiveClassMonitorItem{
			ScheduleID:    sch.ID,
			ClassID:       sch.ClassID,
			SubjectID:     sch.SubjectID,
			TeacherID:     sch.EmployeeID,
			Room:          sch.Room,
			StartTime:     sch.StartTime,
			EndTime:       sch.EndTime,
			SessionStatus: "not_started",
			StudentsTotal: totals[sch.ClassID],
		}
		if sch.Class != nil {
			item.ClassName = sch.Class.DisplayName()
		}
		if sch.Subject != nil {
			item.SubjectName = sch.Subject.Name
		}
		if sch.Teacher != nil {
			item.TeacherName = sch.Teacher.FullName
		}

		if sess, ok := bySchedule[sch.ID]; ok {
			id := sess.ID
			item.SessionID = &id
			item.SessionStatus = sess.Status
			item.TeacherCheckInTime = sess.TeacherCheckInTime
			item.LateMinutes = sess.LateMinutes
			item.Topic = sess.TeachingTopic
			item.SessionToken = sess.SessionToken
			item.TokenExpiresAt = sess.TokenExpiresAt
			switch sess.TeacherCheckInStatus {
			case "on_time":
				item.TeacherStatusLabel = "Hadir Tepat Waktu"
			case "late":
				item.TeacherStatusLabel = fmt.Sprintf("Terlambat %d m", sess.LateMinutes)
			}
			item.StudentsPresent = present[sess.ID]
		} else {
			item.TeacherStatusLabel = "Belum Masuk"
		}
		items = append(items, item)
	}
	return items, nil
}

// classMemberCounts = active students with an active history row per class.
func classMemberCounts(db *gorm.DB, schoolID uuid.UUID, classIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(classIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ClassID uuid.UUID
		Count   int
	}
	if err := db.Raw(`SELECT h.class_id, COUNT(DISTINCT s.id) AS count
		FROM student_class_histories h
		JOIN students s ON s.id = h.student_id AND s.deleted_at IS NULL AND s.status = 'active'
		WHERE h.school_id = ? AND h.status = 'active' AND h.class_id IN ?
		GROUP BY h.class_id`, schoolID, classIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ClassID] = r.Count
	}
	return out, nil
}

// activeHistoryOf returns the student's active class-history row (nil if none).
func activeHistoryOf(db *gorm.DB, schoolID, studentID uuid.UUID) *model.StudentClassHistory {
	var h model.StudentClassHistory
	if err := db.Where("school_id = ? AND student_id = ? AND status = 'active'", schoolID, studentID).First(&h).Error; err != nil {
		return nil
	}
	return &h
}

func (s *AttendanceService) GetTeacherTodayTeachingSessions(ctx context.Context, schoolID, teacherID uuid.UUID, dateStr string) ([]dto.LiveClassMonitorItem, error) {
	date, dateStr, err := s.resolveDate(ctx, schoolID, dateStr)
	if err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	var schedules []model.TeacherSchedules
	err = todaySchedules(db, schoolID, isoWeekday(date)).
		Preload("Class").
		Preload("Subject").
		Preload("Teacher").
		Where("teacher_schedules.employee_id = ?", teacherID).
		Order("teacher_schedules.start_time ASC").
		Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return s.buildMonitorItems(db, schoolID, schedules, dateStr)
}

// uniqueSessionToken returns a token not used by any in-progress session of the school.
func uniqueSessionToken(db *gorm.DB, schoolID uuid.UUID) (string, error) {
	for i := 0; i < 10; i++ {
		token := generateSessionToken()
		var n int64
		if err := db.Model(&model.ClassTeachingSession{}).
			Where("school_id = ? AND status = 'in_progress' AND session_token = ?", schoolID, token).
			Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return token, nil
		}
	}
	return "", errors.New("could not generate a unique session token")
}

func codeValidity(cfg *model.AttendanceConfig) time.Duration {
	minutes := 30
	if cfg != nil && cfg.CodeValidMinutes > 0 {
		minutes = cfg.CodeValidMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (s *AttendanceService) StartTeachingSession(ctx context.Context, schoolID, teacherID uuid.UUID, req *dto.TeachingSessionStartRequest, now time.Time) (*model.ClassTeachingSession, error) {
	db := s.conn(ctx)
	var sch model.TeacherSchedules
	err := db.Where("id = ? AND school_id = ? AND employee_id = ?", req.ScheduleID, schoolID, teacherID).First(&sch).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Jadwal mengajar tidak ditemukan atau bukan milik Anda")
		}
		return nil, err
	}
	if isInactiveScheduleStatus(sch.Status) {
		return nil, apperr.BadRequest("Jadwal mengajar tidak aktif")
	}
	if len(req.Topic) > 255 || len(req.Notes) > 2000 {
		return nil, apperr.BadRequest("Topik maksimal 255 karakter dan catatan maksimal 2000 karakter")
	}

	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	now, _ = s.schoolNow(ctx, schoolID, now)
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)

	if sch.DayOfWeek != isoWeekday(now) {
		return nil, apperr.New(http.StatusBadRequest, "SCHEDULE_NOT_TODAY", "Jadwal ini bukan untuk hari ini")
	}

	var sess model.ClassTeachingSession
	err = db.Where("school_id = ? AND schedule_id = ? AND date = ?", schoolID, sch.ID, dateStr).First(&sess).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		switch sess.Status {
		case "in_progress":
			return &sess, nil // idempotent: the session is already running
		case "completed", "cancelled":
			return nil, apperr.Conflict("SESSION_COMPLETED", "Sesi mengajar untuk jadwal ini sudah selesai hari ini")
		}
	}

	if !SessionStartAllowed(now, sch.StartTime, sch.EndTime) {
		return nil, apperr.New(http.StatusBadRequest, "OUTSIDE_SESSION_WINDOW",
			fmt.Sprintf("Sesi hanya dapat dimulai antara %d menit sebelum %s hingga %s", sessionStartLeadMinutes, sch.StartTime, sch.EndTime))
	}

	late, lateMinutes := LateMinutes(now, sch.StartTime, cfg.LateToleranceMinutes)
	checkInStatus := "on_time"
	if late {
		checkInStatus = "late"
	}
	expiresAt := now.Add(codeValidity(cfg))
	isNew := sess.ID == uuid.Nil

	for attempt := 0; attempt < 3; attempt++ {
		token, err := uniqueSessionToken(db, schoolID)
		if err != nil {
			return nil, err
		}
		if isNew {
			sess = model.ClassTeachingSession{
				SchoolID:             schoolID,
				ScheduleID:           sch.ID,
				Date:                 dateStr,
				ClassID:              sch.ClassID,
				SubjectID:            sch.SubjectID,
				TeacherID:            teacherID,
				AcademicYearID:       sch.AcademicYearID,
				SemesterID:           sch.SemesterID,
				ScheduledStartTime:   sch.StartTime,
				ScheduledEndTime:     sch.EndTime,
				TeacherCheckInTime:   &timeStr,
				TeacherCheckInStatus: checkInStatus,
				LateMinutes:          lateMinutes,
				TeachingTopic:        strings.TrimSpace(req.Topic),
				TeachingNotes:        strings.TrimSpace(req.Notes),
				Status:               "in_progress",
				SessionToken:         token,
				TokenExpiresAt:       &expiresAt,
				AllowSelfCheckIn:     cfg.AllowSelfCheckInClass,
			}
			err = db.Create(&sess).Error
			if err == nil && !cfg.AllowSelfCheckInClass {
				// default:true column: GORM omits the false zero value on insert.
				sess.AllowSelfCheckIn = false
				err = db.Model(&sess).Update("allow_self_check_in", false).Error
			}
		} else {
			// Legacy "not_started" row: start it.
			sess.TeacherCheckInTime = &timeStr
			sess.TeacherCheckInStatus = checkInStatus
			sess.LateMinutes = lateMinutes
			sess.Status = "in_progress"
			sess.SessionToken = token
			sess.TokenExpiresAt = &expiresAt
			if req.Topic != "" {
				sess.TeachingTopic = strings.TrimSpace(req.Topic)
			}
			if req.Notes != "" {
				sess.TeachingNotes = strings.TrimSpace(req.Notes)
			}
			err = db.Save(&sess).Error
		}
		if err == nil {
			return &sess, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, err
		}
		// Either a concurrent start of the same schedule (return it) or a token collision (retry).
		var existing model.ClassTeachingSession
		if e := db.Where("school_id = ? AND schedule_id = ? AND date = ?", schoolID, sch.ID, dateStr).First(&existing).Error; e == nil && existing.Status == "in_progress" {
			return &existing, nil
		}
	}
	return nil, errors.New("could not start teaching session")
}

// ownSession loads a session of the school and enforces teacher ownership
// (teacherID nil = manager, may access any session).
func (s *AttendanceService) ownSession(db *gorm.DB, schoolID, sessionID uuid.UUID, teacherID *uuid.UUID) (*model.ClassTeachingSession, error) {
	var sess model.ClassTeachingSession
	if err := db.Where("id = ? AND school_id = ?", sessionID, schoolID).First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errSessionNotFound
		}
		return nil, err
	}
	if teacherID != nil && sess.TeacherID != *teacherID {
		return nil, errForbiddenSession
	}
	return &sess, nil
}

func (s *AttendanceService) EndTeachingSession(ctx context.Context, schoolID, teacherID, sessionID uuid.UUID, req *dto.TeachingSessionEndRequest, now time.Time) (*model.ClassTeachingSession, error) {
	db := s.conn(ctx)
	sess, err := s.ownSession(db, schoolID, sessionID, &teacherID)
	if err != nil {
		return nil, err
	}
	if sess.Status != "in_progress" {
		return nil, apperr.Conflict("SESSION_NOT_ACTIVE", "Sesi mengajar tidak sedang berlangsung")
	}
	now, _ = s.schoolNow(ctx, schoolID, now)
	timeStr := now.Format(clockLayout)
	sess.TeacherCheckOutTime = &timeStr
	sess.Status = "completed"
	sess.SessionToken = "" // ending a session closes its code
	sess.TokenExpiresAt = nil
	if req.Topic != "" {
		sess.TeachingTopic = strings.TrimSpace(req.Topic)
	}
	if req.Notes != "" {
		sess.TeachingNotes = strings.TrimSpace(req.Notes)
	}
	if len(sess.TeachingTopic) > 255 || len(sess.TeachingNotes) > 2000 {
		return nil, apperr.BadRequest("Topik maksimal 255 karakter dan catatan maksimal 2000 karakter")
	}

	if err := db.Save(sess).Error; err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *AttendanceService) RefreshSessionToken(ctx context.Context, schoolID, teacherID, sessionID uuid.UUID, now time.Time) (*model.ClassTeachingSession, error) {
	db := s.conn(ctx)
	sess, err := s.ownSession(db, schoolID, sessionID, &teacherID)
	if err != nil {
		return nil, err
	}
	if sess.Status != "in_progress" {
		return nil, apperr.Conflict("SESSION_NOT_ACTIVE", "Sesi mengajar tidak sedang berlangsung")
	}
	cfg, err := s.GetOrCreateConfig(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	now, _ = s.schoolNow(ctx, schoolID, now)

	for attempt := 0; attempt < 3; attempt++ {
		token, err := uniqueSessionToken(db, schoolID)
		if err != nil {
			return nil, err
		}
		sess.SessionToken = token
		expiresAt := now.Add(codeValidity(cfg))
		sess.TokenExpiresAt = &expiresAt
		err = db.Save(sess).Error
		if err == nil {
			return sess, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, err
		}
	}
	return nil, errors.New("could not refresh session token")
}

type TeachingSessionDetail struct {
	Session  model.ClassTeachingSession       `json:"session"`
	Schedule *model.TeacherSchedules          `json:"schedule"`
	Students []StudentSessionAttendanceDetail `json:"students"`
}

type StudentSessionAttendanceDetail struct {
	StudentID     uuid.UUID `json:"student_id"`
	StudentName   string    `json:"student_name"`
	NIS           string    `json:"nis"`
	NISN          string    `json:"nisn"`
	Gender        string    `json:"gender"`
	Status        string    `json:"status"` // present, sick, permission, absent, dispensation
	CheckInMethod string    `json:"check_in_method"`
	CheckInTime   *string   `json:"check_in_time"`
	Notes         string    `json:"notes"`
}

// unscopedPreload includes soft-deleted / archived rows in a Preload.
func unscopedPreload(db *gorm.DB) *gorm.DB { return db.Unscoped() }

// classMembers returns the active students of a class (active history row).
func classMembers(db *gorm.DB, schoolID, classID uuid.UUID) ([]model.Students, error) {
	students := []model.Students{}
	err := db.Table("students s").
		Select("s.id, s.full_name, s.nis, s.nisn, s.gender, s.user_id").
		Joins("JOIN student_class_histories h ON h.student_id = s.id AND h.school_id = s.school_id AND h.status = 'active'").
		Where("s.school_id = ? AND h.class_id = ? AND s.deleted_at IS NULL AND s.status = 'active'", schoolID, classID).
		Order("s.full_name ASC, s.id ASC").
		Find(&students).Error
	return students, err
}

// GetTeachingSessionDetail: teacherID != nil restricts to the teacher's own session (403 otherwise).
func (s *AttendanceService) GetTeachingSessionDetail(ctx context.Context, schoolID, sessionID uuid.UUID, teacherID *uuid.UUID) (*TeachingSessionDetail, error) {
	db := s.conn(ctx)
	if _, err := s.ownSession(db, schoolID, sessionID, teacherID); err != nil {
		return nil, err
	}
	var sess model.ClassTeachingSession
	err := db.
		// History must keep resolving names even when the schedule was archived
		// or the class/subject/teacher soft-deleted afterwards.
		Preload("Class", unscopedPreload).
		Preload("Subject", unscopedPreload).
		Preload("Teacher", unscopedPreload).
		Preload("Schedule", unscopedPreload).
		Where("id = ? AND school_id = ?", sessionID, schoolID).
		First(&sess).Error
	if err != nil {
		return nil, errSessionNotFound
	}

	students, err := classMembers(db, schoolID, sess.ClassID)
	if err != nil {
		return nil, err
	}

	var attendances []model.StudentSessionAttendance
	if err := db.Where("session_id = ? AND school_id = ?", sessionID, schoolID).Find(&attendances).Error; err != nil {
		return nil, err
	}
	attMap := make(map[uuid.UUID]model.StudentSessionAttendance, len(attendances))
	for _, a := range attendances {
		attMap[a.StudentID] = a
	}

	studentDetails := make([]StudentSessionAttendanceDetail, 0, len(students))
	for _, st := range students {
		det := StudentSessionAttendanceDetail{
			StudentID:   st.ID,
			StudentName: st.FullName,
			NIS:         st.NIS,
			NISN:        st.NISN,
			Gender:      st.Gender,
			Status:      "present", // default recommendation
		}
		if a, found := attMap[st.ID]; found {
			det.Status = a.Status
			det.CheckInMethod = a.CheckInMethod
			det.CheckInTime = a.CheckInTime
			det.Notes = a.Notes
		}
		studentDetails = append(studentDetails, det)
	}

	return &TeachingSessionDetail{
		Session:  sess,
		Schedule: sess.Schedule,
		Students: studentDetails,
	}, nil
}

// SaveBatchStudentSessionAttendance upserts one row per student (teacherID != nil
// restricts to the teacher's own session).
func (s *AttendanceService) SaveBatchStudentSessionAttendance(ctx context.Context, schoolID, sessionID uuid.UUID, teacherID *uuid.UUID, userID uint, req *dto.StudentSessionAttendanceBatchRequest) error {
	db := s.conn(ctx)
	sess, err := s.ownSession(db, schoolID, sessionID, teacherID)
	if err != nil {
		return err
	}
	if sess.Status == "cancelled" {
		return apperr.Conflict("SESSION_NOT_ACTIVE", "Sesi mengajar dibatalkan")
	}
	if len(req.Students) > 200 {
		return apperr.BadRequest("Maksimal 200 siswa per penyimpanan")
	}

	members, err := classMembers(db, schoolID, sess.ClassID)
	if err != nil {
		return err
	}
	memberUser := make(map[uuid.UUID]*uint, len(members))
	for _, m := range members {
		memberUser[m.ID] = m.UserID
	}
	for _, item := range req.Students {
		if !sessionStatuses[item.Status] {
			return apperr.BadRequest("Status siswa harus salah satu dari: present, sick, permission, absent, dispensation")
		}
		if _, ok := memberUser[item.StudentID]; !ok {
			return apperr.BadRequest("Siswa tidak terdaftar di kelas sesi ini")
		}
		if len(item.Notes) > 500 {
			return apperr.BadRequest("Catatan siswa maksimal 500 karakter")
		}
	}

	now, _ := s.schoolNow(ctx, schoolID, time.Now())
	nowStr := now.Format(clockLayout)
	type notice struct {
		userID uint
		status string
	}
	var notices []notice

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "session_att:"+sessionID.String()); err != nil {
			return err
		}
		var existing []model.StudentSessionAttendance
		if err := tx.Where("school_id = ? AND session_id = ?", schoolID, sessionID).Find(&existing).Error; err != nil {
			return err
		}
		byStudent := make(map[uuid.UUID]*model.StudentSessionAttendance, len(existing))
		for i := range existing {
			byStudent[existing[i].StudentID] = &existing[i]
		}
		for _, item := range req.Students {
			row, ok := byStudent[item.StudentID]
			changed := !ok || row.Status != item.Status
			if !ok {
				row = &model.StudentSessionAttendance{
					SchoolID:  schoolID,
					SessionID: sessionID,
					StudentID: item.StudentID,
					ClassID:   sess.ClassID,
					Date:      sess.Date,
				}
			}
			// Keep check_in_time / method of already-recorded rows; only a change
			// to present (from non-present / new) stamps a new time.
			if changed {
				row.CheckInMethod = "teacher_manual"
				if item.Status == "present" {
					row.CheckInTime = &nowStr
				}
			}
			row.Status = item.Status
			row.Notes = item.Notes
			row.RecordedByUserID = userID
			if ok {
				if err := tx.Save(row).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Create(row).Error; err != nil {
					return err
				}
				byStudent[item.StudentID] = row
			}
			if changed && item.Status != "present" {
				if uid := memberUser[item.StudentID]; uid != nil {
					notices = append(notices, notice{userID: *uid, status: item.Status})
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(notices) > 0 {
		subject := ""
		var names []string
		db.Model(&model.Subject{}).Where("id = ?", sess.SubjectID).Limit(1).Pluck("name", &names)
		if len(names) > 0 {
			subject = names[0]
		}
		for _, n := range notices {
			Notify(ctx, NotificationInput{
				UserID:   n.userID,
				SchoolID: &schoolID,
				Type:     model.NotifAttendanceMarked,
				Title:    "Presensi kelas dicatat guru",
				Body:     fmt.Sprintf("Guru mencatat Anda %s pada pelajaran %s (%s).", statusLabel(n.status), subject, sess.Date),
				Data:     map[string]any{"date": sess.Date, "status": n.status, "kind": "session", "session_id": sess.ID.String()},
			})
		}
	}
	return nil
}

// statusLabel returns the Indonesian label of an attendance status.
func statusLabel(status string) string {
	switch status {
	case "present":
		return "Hadir"
	case "late":
		return "Terlambat"
	case "sick":
		return "Sakit"
	case "permission":
		return "Izin"
	case "absent":
		return "Alpa"
	case "dispensation":
		return "Dispensasi"
	case "remote":
		return "PJJ/WFH"
	case "business_trip":
		return "Dinas Luar"
	}
	return status
}

// LiveClassMonitor returns real-time overview of all classes and teacher presence for school leaders.
func (s *AttendanceService) GetLiveClassMonitor(ctx context.Context, schoolID uuid.UUID, dateStr string) ([]dto.LiveClassMonitorItem, error) {
	date, dateStr, err := s.resolveDate(ctx, schoolID, dateStr)
	if err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	var schedules []model.TeacherSchedules
	err = todaySchedules(db, schoolID, isoWeekday(date)).
		Preload("Class").
		Preload("Subject").
		Preload("Teacher").
		Order("teacher_schedules.start_time ASC, teacher_schedules.class_id ASC").
		Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return s.buildMonitorItems(db, schoolID, schedules, dateStr)
}

// UnassignClassShift removes a class → shift mapping (the class falls back to the default shift).
func (s *AttendanceService) UnassignClassShift(ctx context.Context, schoolID, classID uuid.UUID) error {
	res := s.conn(ctx).Where("school_id = ? AND class_id = ?", schoolID, classID).Delete(&model.ClassAttendanceShift{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("Kelas belum memiliki shift")
	}
	return nil
}

// UpdateTeachingJournal updates topic/notes of an in-progress or completed
// session without touching check-in data (teacherID nil = manager).
func (s *AttendanceService) UpdateTeachingJournal(ctx context.Context, schoolID, sessionID uuid.UUID, teacherID *uuid.UUID, req *dto.TeachingSessionEndRequest) (*model.ClassTeachingSession, error) {
	db := s.conn(ctx)
	sess, err := s.ownSession(db, schoolID, sessionID, teacherID)
	if err != nil {
		return nil, err
	}
	if sess.Status != "in_progress" && sess.Status != "completed" {
		return nil, apperr.Conflict("SESSION_NOT_ACTIVE", "Jurnal hanya dapat diisi untuk sesi yang berlangsung atau selesai")
	}
	topic, notes := strings.TrimSpace(req.Topic), strings.TrimSpace(req.Notes)
	if len(topic) > 255 || len(notes) > 2000 {
		return nil, apperr.BadRequest("Topik maksimal 255 karakter dan catatan maksimal 2000 karakter")
	}
	if err := db.Model(sess).Updates(map[string]interface{}{"teaching_topic": topic, "teaching_notes": notes, "updated_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	sess.TeachingTopic, sess.TeachingNotes = topic, notes
	return sess, nil
}
