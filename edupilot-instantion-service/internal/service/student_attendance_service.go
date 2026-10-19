package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Student self-service attendance (API_CONTRACT §10.5). The caller (middleware
// RequireEnrolledStudent) guarantees st is an active student of an active school.

var (
	errInvalidCode  = apperr.New(http.StatusNotFound, "INVALID_CODE", "Kode presensi tidak valid atau sesi pelajaran belum dimulai")
	errCodeExpired  = apperr.New(http.StatusGone, "CODE_EXPIRED", "Kode presensi sudah kedaluwarsa. Mintalah guru untuk memperbarui kode.")
	errNotInClass   = apperr.New(http.StatusForbidden, "NOT_IN_CLASS", "Anda tidak terdaftar di kelas sesi pelajaran ini")
	errSelfCheckOff = apperr.New(http.StatusForbidden, "SELF_CHECK_IN_DISABLED", "Presensi mandiri tidak diizinkan untuk sesi ini")
)

func (s *AttendanceService) StudentToday(ctx context.Context, st *model.Students, school *model.School, now time.Time) (*dto.StudentAttendanceToday, error) {
	cfg, err := s.GetOrCreateConfig(ctx, school.ID)
	if err != nil {
		return nil, err
	}
	loc := LoadSchoolLocation(school.Timezone)
	now = now.In(loc)
	dateStr := now.Format(dateLayout)
	db := s.conn(ctx)

	var classID *uuid.UUID
	if h := activeHistoryOf(db, school.ID, st.ID); h != nil {
		classID = &h.ClassID
	}
	target := studentTarget(db, cfg, school.ID, classID)

	resp := &dto.StudentAttendanceToday{
		Date:     dateStr,
		Now:      now.Format(time.RFC3339),
		Timezone: loc.String(),
		SchoolLocation: dto.SchoolLocation{
			Latitude: cfg.SchoolLatitude, Longitude: cfg.SchoolLongitude,
			RadiusMeters: cfg.RadiusMeters, LocationName: cfg.LocationName,
		},
		TargetCheckInTime:    target.CheckIn,
		TargetCheckOutTime:   target.CheckOut,
		LateToleranceMinutes: target.Tolerance,
		Sessions:             []dto.StudentTodaySession{},
	}

	var daily model.DailyAttendance
	err = db.Where("school_id = ? AND student_id = ? AND date = ?", school.ID, st.ID, dateStr).First(&daily).Error
	switch {
	case err == nil:
		resp.DailyAttendance = daily
		resp.IsCheckedIn = daily.CheckInTime != nil
		resp.IsCheckedOut = daily.CheckOutTime != nil
	case errors.Is(err, gorm.ErrRecordNotFound):
	default:
		return nil, err
	}

	if classID == nil {
		return resp, nil
	}
	var schedules []model.TeacherSchedules
	if err := todaySchedules(db, school.ID, isoWeekday(now)).
		Preload("Subject").Preload("Teacher").
		Where("teacher_schedules.class_id = ?", *classID).
		Order("teacher_schedules.start_time ASC").
		Find(&schedules).Error; err != nil {
		return nil, err
	}
	if len(schedules) == 0 {
		return resp, nil
	}
	ids := make([]uuid.UUID, 0, len(schedules))
	for _, sch := range schedules {
		ids = append(ids, sch.ID)
	}
	var sessions []model.ClassTeachingSession
	if err := db.Where("school_id = ? AND date = ? AND schedule_id IN ?", school.ID, dateStr, ids).Find(&sessions).Error; err != nil {
		return nil, err
	}
	bySchedule := map[uuid.UUID]model.ClassTeachingSession{}
	sessIDs := []uuid.UUID{}
	for _, se := range sessions {
		bySchedule[se.ScheduleID] = se
		sessIDs = append(sessIDs, se.ID)
	}
	mine := map[uuid.UUID]model.StudentSessionAttendance{}
	if len(sessIDs) > 0 {
		var rows []model.StudentSessionAttendance
		if err := db.Where("school_id = ? AND student_id = ? AND session_id IN ?", school.ID, st.ID, sessIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			mine[r.SessionID] = r
		}
	}
	for _, sch := range schedules {
		item := dto.StudentTodaySession{
			ScheduleID: sch.ID, StartTime: sch.StartTime, EndTime: sch.EndTime, Room: sch.Room,
			SessionStatus: "not_started",
		}
		if sch.Subject != nil {
			item.SubjectName = sch.Subject.Name
		}
		if sch.Teacher != nil {
			item.TeacherName = sch.Teacher.FullName
		}
		if se, ok := bySchedule[sch.ID]; ok {
			id := se.ID
			item.SessionID = &id
			item.SessionStatus = se.Status
			if r, ok := mine[se.ID]; ok {
				status := r.Status
				item.MyStatus = &status
				item.MyCheckInTime = r.CheckInTime
			}
		}
		resp.Sessions = append(resp.Sessions, item)
	}
	return resp, nil
}

func (s *AttendanceService) StudentCheckIn(ctx context.Context, st *model.Students, school *model.School, req *dto.StudentGPSRequest, now time.Time) (*model.DailyAttendance, error) {
	cfg, err := s.GetOrCreateConfig(ctx, school.ID)
	if err != nil {
		return nil, err
	}
	geo, err := CheckGeofence(cfg.SchoolLatitude, cfg.SchoolLongitude, cfg.RadiusMeters, req.Latitude, req.Longitude, req.Accuracy)
	if err != nil {
		return nil, err
	}
	now = now.In(LoadSchoolLocation(school.Timezone))
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)
	db := s.conn(ctx)

	var classID *uuid.UUID
	if h := activeHistoryOf(db, school.ID, st.ID); h != nil {
		classID = &h.ClassID
	}
	target := studentTarget(db, cfg, school.ID, classID)
	late, lateMinutes := LateMinutes(now, target.CheckIn, target.Tolerance)
	status, checkInStatus := "present", "on_time"
	if late {
		status, checkInStatus = "late", "late"
	}

	var daily model.DailyAttendance
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "daily:"+school.ID.String()+":s:"+st.ID.String()+":"+dateStr); err != nil {
			return err
		}
		err := tx.Where("school_id = ? AND student_id = ? AND date = ?", school.ID, st.ID, dateStr).First(&daily).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		if exists && daily.CheckInTime != nil {
			return errAlreadyIn
		}
		if !exists {
			sid := st.ID
			daily = model.DailyAttendance{SchoolID: school.ID, Date: dateStr, UserType: "student", StudentID: &sid}
		}
		daily.ClassID = classID
		daily.ShiftID = target.ShiftID
		daily.Status = status
		daily.CheckInTime = &timeStr
		daily.CheckInStatus = checkInStatus
		daily.LateMinutes = lateMinutes
		daily.CheckInMethod = "gps"
		daily.CheckInLat = req.Latitude
		daily.CheckInLng = req.Longitude
		daily.CheckInDistance = &geo.Distance
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
	if late && st.UserID != nil {
		schoolID := school.ID
		Notify(ctx, NotificationInput{
			UserID: *st.UserID, SchoolID: &schoolID, Type: model.NotifLateCheckIn,
			Title: "Anda terlambat",
			Body:  "Presensi kedatangan tercatat terlambat " + strconv.Itoa(lateMinutes) + " menit (target " + target.CheckIn + ").",
			Data:  map[string]any{"date": dateStr, "late_minutes": lateMinutes},
		})
	}
	return &daily, nil
}

func (s *AttendanceService) StudentCheckOut(ctx context.Context, st *model.Students, school *model.School, req *dto.StudentGPSRequest, now time.Time) (*model.DailyAttendance, error) {
	cfg, err := s.GetOrCreateConfig(ctx, school.ID)
	if err != nil {
		return nil, err
	}
	now = now.In(LoadSchoolLocation(school.Timezone))
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)
	db := s.conn(ctx)

	var daily model.DailyAttendance
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "daily:"+school.ID.String()+":s:"+st.ID.String()+":"+dateStr); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND student_id = ? AND date = ?", school.ID, st.ID, dateStr).First(&daily).Error; err != nil {
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
		geo, err := CheckGeofence(cfg.SchoolLatitude, cfg.SchoolLongitude, cfg.RadiusMeters, req.Latitude, req.Longitude, req.Accuracy)
		if err != nil {
			return err
		}
		target := studentTarget(tx, cfg, school.ID, daily.ClassID)
		early, earlyMinutes := EarlyMinutes(now, target.CheckOut)
		daily.CheckOutStatus = "on_time"
		if early {
			daily.CheckOutStatus = "early"
		}
		daily.CheckOutTime = &timeStr
		daily.EarlyMinutes = earlyMinutes
		daily.CheckOutMethod = "gps"
		daily.CheckOutLat = req.Latitude
		daily.CheckOutLng = req.Longitude
		daily.CheckOutDistance = &geo.Distance
		return tx.Save(&daily).Error
	})
	if err != nil {
		return nil, err
	}
	return &daily, nil
}

// StudentClaim marks the student present in an in-progress session via its code.
func (s *AttendanceService) StudentClaim(ctx context.Context, st *model.Students, school *model.School, req *dto.StudentClaimRequest, now time.Time) (*dto.StudentClaimResponse, error) {
	code := NormalizeSessionCode(req.Code)
	if code == "" {
		return nil, apperr.BadRequest("Kode presensi wajib diisi")
	}
	if (req.Latitude == nil) != (req.Longitude == nil) ||
		(req.Latitude != nil && !ValidCoordinates(*req.Latitude, *req.Longitude)) {
		return nil, apperr.BadRequest("Koordinat lokasi tidak valid (latitude -90..90, longitude -180..180)")
	}
	now = now.In(LoadSchoolLocation(school.Timezone))
	dateStr := now.Format(dateLayout)
	timeStr := now.Format(clockLayout)
	db := s.conn(ctx)

	var sess model.ClassTeachingSession
	if err := db.Preload("Subject").Preload("Class").
		Where("school_id = ? AND session_token = ? AND date = ? AND status = 'in_progress'", school.ID, code, dateStr).
		First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errInvalidCode
		}
		return nil, err
	}
	h := activeHistoryOf(db, school.ID, st.ID)
	if h == nil || h.ClassID != sess.ClassID {
		return nil, errNotInClass
	}

	resp := &dto.StudentClaimResponse{SessionID: sess.ID, Status: "present"}
	if sess.Subject != nil {
		resp.SubjectName = sess.Subject.Name
	}
	if sess.Class != nil {
		resp.ClassName = sess.Class.DisplayName()
	}

	var row model.StudentSessionAttendance
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, "session_att:"+sess.ID.String()); err != nil {
			return err
		}
		err := tx.Where("school_id = ? AND session_id = ? AND student_id = ?", school.ID, sess.ID, st.ID).First(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		exists := err == nil
		if exists && row.Status == "present" {
			return nil // idempotent
		}
		if sess.TokenExpiresAt != nil && now.After(*sess.TokenExpiresAt) {
			return errCodeExpired
		}
		if !sess.AllowSelfCheckIn {
			return errSelfCheckOff
		}
		if !exists {
			row = model.StudentSessionAttendance{
				SchoolID: school.ID, SessionID: sess.ID, StudentID: st.ID, ClassID: sess.ClassID, Date: sess.Date,
			}
		}
		row.Status = "present"
		row.CheckInMethod = "unique_code"
		row.CheckInTime = &timeStr
		if st.UserID != nil {
			row.RecordedByUserID = *st.UserID
		}
		if exists {
			return tx.Save(&row).Error
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	resp.CheckInTime = row.CheckInTime
	return resp, nil
}

// StudentHistory: month = "YYYY-MM" (empty = current month in the school tz).
func (s *AttendanceService) StudentHistory(ctx context.Context, st *model.Students, school *model.School, month string) (*dto.StudentAttendanceHistory, error) {
	month = strings.TrimSpace(month)
	if month == "" {
		month = time.Now().In(LoadSchoolLocation(school.Timezone)).Format(monthLayout)
	} else if _, err := time.Parse(monthLayout, month); err != nil {
		return nil, apperr.BadRequest("Format bulan harus YYYY-MM")
	}
	db := s.conn(ctx)
	like := month + "-%"

	daily := []model.DailyAttendance{}
	if err := db.Where("school_id = ? AND student_id = ? AND date LIKE ?", school.ID, st.ID, like).
		Order("date DESC").Find(&daily).Error; err != nil {
		return nil, err
	}
	resp := &dto.StudentAttendanceHistory{Month: month, Daily: daily, Sessions: []dto.StudentHistorySession{}}
	for _, d := range daily {
		switch d.Status {
		case "present", "remote", "business_trip":
			resp.Summary.Present++
		case "late":
			resp.Summary.Late++
		case "sick":
			resp.Summary.Sick++
		case "permission":
			resp.Summary.Permission++
		case "absent":
			resp.Summary.Absent++
		}
	}

	var rows []dto.StudentHistorySession
	if err := db.Raw(`SELECT a.date, COALESCE(sub.name, '') AS subject_name, COALESCE(e.full_name, '') AS teacher_name,
			COALESCE(cts.scheduled_start_time, '') AS start_time, a.status, COALESCE(a.check_in_method, '') AS check_in_method
		FROM student_session_attendances a
		JOIN class_teaching_sessions cts ON cts.id = a.session_id
		LEFT JOIN subjects sub ON sub.id = cts.subject_id
		LEFT JOIN employees e ON e.id = cts.teacher_id
		WHERE a.school_id = ? AND a.student_id = ? AND a.date LIKE ?
		ORDER BY a.date DESC, cts.scheduled_start_time ASC`, school.ID, st.ID, like).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if rows != nil {
		resp.Sessions = rows
	}
	return resp, nil
}
