package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ScheduleService struct {
	db *gorm.DB
}

// NewScheduleService: a nil db means config.DB, resolved per call (the
// controller creates the service at package init, before ConnectDB runs).
func NewScheduleService(db *gorm.DB) *ScheduleService {
	return &ScheduleService{db: db}
}

func (s *ScheduleService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

const (
	maxSchedulePeriods    = 60
	scheduleStatusActive  = "active"
	maxScheduleRoomLen    = 100
	maxScheduleNotesLen   = 1000
	maxScheduleStatusLen  = 50
	maxSchedulePeriodName = 100
)

// inactiveScheduleStatuses never take part in conflict detection / JP counts.
var inactiveScheduleStatuses = []string{"inactive", "cancelled", "canceled"}

func isInactiveScheduleStatus(status string) bool {
	st := strings.ToLower(strings.TrimSpace(status))
	for _, v := range inactiveScheduleStatuses {
		if st == v {
			return true
		}
	}
	return false
}

var dayNames = map[int]string{1: "Senin", 2: "Selasa", 3: "Rabu", 4: "Kamis", 5: "Jumat", 6: "Sabtu", 7: "Minggu"}

func dayName(d int) string {
	if n, ok := dayNames[d]; ok {
		return n
	}
	return fmt.Sprintf("hari ke-%d", d)
}

func invalidRef(msg string) error {
	return apperr.New(http.StatusBadRequest, "INVALID_REFERENCE", msg)
}

// timesOverlap reports whether [aStart,aEnd) and [bStart,bEnd) overlap. Times
// are zero-padded "HH:MM" strings, so lexical order equals time order.
func timesOverlap(aStart, aEnd, bStart, bEnd string) bool {
	return aStart < bEnd && bStart < aEnd
}

// lockSchoolSchedules serializes schedule/period writes of one school for the
// rest of the transaction, so check-then-insert cannot race (double booking).
func lockSchoolSchedules(tx *gorm.DB, schoolID uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "teacher_schedules:"+schoolID.String()).Error
}

// ==================== Schedule Periods ====================

func toPeriodResponse(p *model.SchedulePeriod) dto.SchedulePeriodResponse {
	return dto.SchedulePeriodResponse{
		ID:          p.ID,
		SchoolID:    p.SchoolID,
		Name:        p.Name,
		PeriodOrder: p.PeriodOrder,
		StartTime:   p.StartTime,
		EndTime:     p.EndTime,
		DayOfWeek:   p.DayOfWeek,
		IsBreak:     p.IsBreak,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// normalizePeriod trims and validates a period request (no DB access).
func normalizePeriod(req *dto.SchedulePeriodRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.StartTime = strings.TrimSpace(req.StartTime)
	req.EndTime = strings.TrimSpace(req.EndTime)
	fe := fieldErrors{}
	if req.Name == "" {
		fe.add("name", "Nama jam pelajaran wajib diisi")
	} else if len(req.Name) > maxSchedulePeriodName {
		fe.add("name", fmt.Sprintf("Nama jam pelajaran maksimal %d karakter", maxSchedulePeriodName))
	}
	if !dto.ValidHHMM(req.StartTime) {
		fe.add("start_time", "Waktu mulai harus berformat HH:MM")
	}
	if !dto.ValidHHMM(req.EndTime) {
		fe.add("end_time", "Waktu selesai harus berformat HH:MM")
	}
	if dto.ValidHHMM(req.StartTime) && dto.ValidHHMM(req.EndTime) && req.EndTime <= req.StartTime {
		fe.add("end_time", "Waktu selesai harus setelah waktu mulai")
	}
	if req.PeriodOrder < 0 || req.PeriodOrder > 99 {
		fe.add("period_order", "Urutan jam pelajaran harus antara 0 dan 99")
	}
	if req.DayOfWeek < 0 || req.DayOfWeek > 7 {
		fe.add("day_of_week", "Hari harus 0 (semua hari) atau 1 (Senin) sampai 7 (Minggu)")
	}
	return fe.err()
}

// checkPeriodClash validates a candidate period against the school's other
// periods: periods of the same day_of_week (0 = every day) must not overlap in
// time or share period_order. Day-specific periods (e.g. a shorter Friday) may
// overlap the "every day" ones on purpose.
func checkPeriodClash(existing []model.SchedulePeriod, cand model.SchedulePeriod) error {
	for _, p := range existing {
		if p.ID == cand.ID || p.DayOfWeek != cand.DayOfWeek {
			continue
		}
		if p.PeriodOrder == cand.PeriodOrder {
			return apperr.Conflict("DUPLICATE_ORDER", fmt.Sprintf("Urutan %d sudah dipakai oleh \"%s\"", cand.PeriodOrder, p.Name))
		}
		if timesOverlap(p.StartTime, p.EndTime, cand.StartTime, cand.EndTime) {
			return apperr.Conflict("PERIOD_OVERLAP", fmt.Sprintf("Waktu %s - %s bertabrakan dengan \"%s\" (%s - %s)",
				cand.StartTime, cand.EndTime, p.Name, p.StartTime, p.EndTime))
		}
	}
	return nil
}

func (s *ScheduleService) loadPeriods(db *gorm.DB, schoolID uuid.UUID) ([]model.SchedulePeriod, error) {
	var periods []model.SchedulePeriod
	err := db.Where("school_id = ?", schoolID).
		Order("period_order ASC, day_of_week ASC, start_time ASC").
		Find(&periods).Error
	return periods, err
}

func (s *ScheduleService) ListSchedulePeriods(ctx context.Context, schoolID uuid.UUID) ([]dto.SchedulePeriodResponse, error) {
	periods, err := s.loadPeriods(s.conn(ctx), schoolID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.SchedulePeriodResponse, 0, len(periods))
	for i := range periods {
		result = append(result, toPeriodResponse(&periods[i]))
	}
	return result, nil
}

func (s *ScheduleService) CreateSchedulePeriod(ctx context.Context, schoolID uuid.UUID, req dto.SchedulePeriodRequest) (*dto.SchedulePeriodResponse, error) {
	if err := normalizePeriod(&req); err != nil {
		return nil, err
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	period := model.SchedulePeriod{
		SchoolID:    schoolID,
		Name:        req.Name,
		PeriodOrder: req.PeriodOrder,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		DayOfWeek:   req.DayOfWeek,
		IsBreak:     req.IsBreak,
		IsActive:    isActive,
	}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		existing, err := s.loadPeriods(tx, schoolID)
		if err != nil {
			return err
		}
		if len(existing) >= maxSchedulePeriods {
			return apperr.BadRequest(fmt.Sprintf("Maksimal %d jam pelajaran per sekolah", maxSchedulePeriods))
		}
		if err := checkPeriodClash(existing, period); err != nil {
			return err
		}
		return tx.Create(&period).Error
	})
	if err != nil {
		return nil, err
	}
	resp := toPeriodResponse(&period)
	return &resp, nil
}

func (s *ScheduleService) UpdateSchedulePeriod(ctx context.Context, schoolID, id uuid.UUID, req dto.SchedulePeriodRequest) (*dto.SchedulePeriodResponse, error) {
	if err := normalizePeriod(&req); err != nil {
		return nil, err
	}
	var period model.SchedulePeriod
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&period).Error; err != nil {
			return notFoundOr(err, "Jam pelajaran tidak ditemukan")
		}
		oldStart, oldEnd := period.StartTime, period.EndTime

		var used int64
		if err := tx.Model(&model.TeacherSchedules{}).Where("school_id = ? AND schedule_period_id = ?", schoolID, id).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 && req.IsBreak && !period.IsBreak {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Jam pelajaran dipakai oleh %d jadwal sehingga tidak dapat dijadikan jam istirahat", used))
		}

		period.Name = req.Name
		period.PeriodOrder = req.PeriodOrder
		period.StartTime = req.StartTime
		period.EndTime = req.EndTime
		period.DayOfWeek = req.DayOfWeek
		period.IsBreak = req.IsBreak
		if req.IsActive != nil {
			period.IsActive = *req.IsActive
		}

		existing, err := s.loadPeriods(tx, schoolID)
		if err != nil {
			return err
		}
		if err := checkPeriodClash(existing, period); err != nil {
			return err
		}
		if err := tx.Save(&period).Error; err != nil {
			return err
		}
		// Keep single-period schedules in sync with the new times.
		if used > 0 && (oldStart != period.StartTime || oldEnd != period.EndTime) {
			if err := tx.Model(&model.TeacherSchedules{}).
				Where("school_id = ? AND schedule_period_id = ? AND start_time = ? AND end_time = ?", schoolID, id, oldStart, oldEnd).
				Updates(map[string]interface{}{"start_time": period.StartTime, "end_time": period.EndTime}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	resp := toPeriodResponse(&period)
	return &resp, nil
}

func (s *ScheduleService) DeleteSchedulePeriod(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		var period model.SchedulePeriod
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&period).Error; err != nil {
			return notFoundOr(err, "Jam pelajaran tidak ditemukan")
		}
		var used int64
		if err := tx.Model(&model.TeacherSchedules{}).Where("school_id = ? AND schedule_period_id = ?", schoolID, id).Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Jam pelajaran tidak dapat dihapus karena dipakai oleh %d jadwal pelajaran", used))
		}
		if err := detachArchivedSchedulesFromPeriods(tx, schoolID, []uuid.UUID{period.ID}); err != nil {
			return err
		}
		return tx.Delete(&period).Error
	})
}

// periodSlot is one generated preset slot.
type periodSlot struct {
	Name       string
	Order      int
	Start, End string
	IsBreak    bool
}

// presetMinutes maps a period preset (or education stage) to the JP length.
// Permendikbud: 1 JP = 35 min (SD/MI), 40 min (SMP/MTs), 45 min (SMA/MA/SMK).
func presetMinutes(preset string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(preset)) {
	case "35_MIN", "SD", "MI", "STANDARD_35":
		return 35, true
	case "40_MIN", "SMP", "MTS", "STANDARD_40":
		return 40, true
	case "45_MIN", "SMA", "MA", "SMK", "MAK", "STANDARD_45":
		return 45, true
	}
	return 0, false
}

func addMinutes(hhmm string, minutes int) string {
	var h, m int
	_, _ = fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	t := h*60 + m + minutes
	return fmt.Sprintf("%02d:%02d", (t/60)%24, t%60)
}

// buildPeriodPreset generates a contiguous school day starting 07:00:
// pembiasaan, teaching blocks separated by breaks. Pure (unit tested).
func buildPeriodPreset(preset string) ([]periodSlot, bool) {
	jp, ok := presetMinutes(preset)
	if !ok {
		return nil, false
	}
	type block struct {
		breakName string
		breakMin  int
		teach     int
	}
	var layout []block
	switch jp {
	case 35: // SD/MI: 8 JP
		layout = []block{{"Pembiasaan Pagi", 15, 3}, {"Istirahat", 15, 3}, {"Istirahat Kedua", 15, 2}}
	case 40: // SMP/MTs: 9 JP
		layout = []block{{"Pembiasaan Pagi", 15, 4}, {"Istirahat", 15, 3}, {"Ishoma", 30, 2}}
	default: // SMA/MA/SMK: 10 JP
		layout = []block{{"Pembiasaan Pagi", 15, 4}, {"Istirahat", 15, 3}, {"Ishoma", 30, 3}}
	}
	slots := make([]periodSlot, 0, 16)
	cur, order, n := "07:00", 1, 1
	for _, b := range layout {
		end := addMinutes(cur, b.breakMin)
		slots = append(slots, periodSlot{Name: b.breakName, Order: order, Start: cur, End: end, IsBreak: true})
		cur, order = end, order+1
		for i := 0; i < b.teach; i++ {
			end := addMinutes(cur, jp)
			slots = append(slots, periodSlot{Name: fmt.Sprintf("Jam ke-%d", n), Order: order, Start: cur, End: end})
			cur, order, n = end, order+1, n+1
		}
	}
	return slots, true
}

// ApplyPeriodPresets replaces the school's periods with a preset. Refused
// (409 IN_USE) while schedules still reference the current periods.
func (s *ScheduleService) ApplyPeriodPresets(ctx context.Context, schoolID uuid.UUID, preset string) ([]dto.SchedulePeriodResponse, error) {
	slots, ok := buildPeriodPreset(preset)
	if !ok {
		return nil, apperr.BadRequest("Preset tidak valid. Pilihan: 35_MIN (SD/MI), 40_MIN (SMP/MTs), 45_MIN (SMA/MA/SMK)")
	}
	result := make([]dto.SchedulePeriodResponse, 0, len(slots))
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		var used int64
		if err := tx.Model(&model.TeacherSchedules{}).
			Where("school_id = ? AND schedule_period_id IN (SELECT id FROM schedule_periods WHERE school_id = ?)", schoolID, schoolID).
			Count(&used).Error; err != nil {
			return err
		}
		if used > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Template tidak dapat diterapkan karena %d jadwal pelajaran memakai jam pelajaran saat ini. Hapus jadwal tersebut terlebih dahulu.", used))
		}
		var oldPeriodIDs []uuid.UUID
		if err := tx.Model(&model.SchedulePeriod{}).Where("school_id = ?", schoolID).Pluck("id", &oldPeriodIDs).Error; err != nil {
			return err
		}
		if err := detachArchivedSchedulesFromPeriods(tx, schoolID, oldPeriodIDs); err != nil {
			return err
		}
		if err := tx.Where("school_id = ?", schoolID).Delete(&model.SchedulePeriod{}).Error; err != nil {
			return err
		}
		for _, d := range slots {
			p := model.SchedulePeriod{
				SchoolID:    schoolID,
				Name:        d.Name,
				PeriodOrder: d.Order,
				StartTime:   d.Start,
				EndTime:     d.End,
				IsBreak:     d.IsBreak,
				IsActive:    true,
			}
			if err := tx.Create(&p).Error; err != nil {
				return err
			}
			result = append(result, toPeriodResponse(&p))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// teachingPeriodsForDay returns the active non-break periods valid on day:
// day-specific periods when the day has any, otherwise the "every day" (0) ones.
func teachingPeriodsForDay(periods []model.SchedulePeriod, day int) []model.SchedulePeriod {
	var specific, all []model.SchedulePeriod
	for _, p := range periods {
		if p.IsBreak || !p.IsActive {
			continue
		}
		switch p.DayOfWeek {
		case day:
			specific = append(specific, p)
		case 0:
			all = append(all, p)
		}
	}
	if len(specific) > 0 {
		return specific
	}
	return all
}

// countJP returns how many teaching periods (JP) the slot [start,end) covers on
// day; a slot that covers no defined period counts as 1 JP.
func countJP(periods []model.SchedulePeriod, day int, start, end string) int {
	n := 0
	for _, p := range teachingPeriodsForDay(periods, day) {
		if p.StartTime >= start && p.EndTime <= end {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

// ==================== Year / semester scope ====================

type scheduleScope struct {
	Year     *model.AcademicYear
	Semester *model.Semester
}

func (sc scheduleScope) yearID() *uuid.UUID {
	if sc.Year == nil {
		return nil
	}
	id := sc.Year.ID
	return &id
}

func (sc scheduleScope) semesterID() *uuid.UUID {
	if sc.Semester == nil {
		return nil
	}
	id := sc.Semester.ID
	return &id
}

// findSemester loads a semester of the tenant (optionally of a given year).
func findSemester(db *gorm.DB, schoolID, semID uuid.UUID, yearID *uuid.UUID) (*model.Semester, error) {
	q := db.Where("id = ? AND academic_year_id IN (SELECT id FROM academic_years WHERE school_id = ?)", semID, schoolID)
	if yearID != nil {
		q = q.Where("academic_year_id = ?", *yearID)
	}
	var sem model.Semester
	if err := q.First(&sem).Error; err != nil {
		return nil, err
	}
	return &sem, nil
}

func activeSemesterOf(db *gorm.DB, yearID uuid.UUID) (*model.Semester, error) {
	var sem model.Semester
	err := db.Where("academic_year_id = ? AND is_active = true", yearID).Order("number").First(&sem).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sem, nil
}

// resolveScope picks the academic year (given or the active one) and semester
// (given or the active semester of that year) a list/grid applies to.
func (s *ScheduleService) resolveScope(db *gorm.DB, schoolID uuid.UUID, yearID, semID *uuid.UUID) (scheduleScope, error) {
	var sc scheduleScope
	if yearID != nil {
		var y model.AcademicYear
		if err := db.Where("school_id = ? AND id = ?", schoolID, *yearID).First(&y).Error; err != nil {
			return sc, notFoundOr(err, "Tahun ajaran tidak ditemukan")
		}
		sc.Year = &y
	}
	if semID != nil {
		sem, err := findSemester(db, schoolID, *semID, yearID)
		if err != nil {
			return sc, notFoundOr(err, "Semester tidak ditemukan")
		}
		sc.Semester = sem
		if sc.Year == nil {
			var y model.AcademicYear
			if err := db.Where("school_id = ? AND id = ?", schoolID, sem.AcademicYearID).First(&y).Error; err != nil {
				return sc, notFoundOr(err, "Tahun ajaran tidak ditemukan")
			}
			sc.Year = &y
		}
		return sc, nil
	}
	if sc.Year == nil {
		var y model.AcademicYear
		err := db.Where("school_id = ? AND is_active = true", schoolID).First(&y).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return sc, err
		}
		if err == nil {
			sc.Year = &y
		}
	}
	if sc.Year != nil {
		sem, err := activeSemesterOf(db, sc.Year.ID)
		if err != nil {
			return sc, err
		}
		sc.Semester = sem
	}
	return sc, nil
}

// applyScheduleScope filters schedules to a year/semester. Rows without a
// year/semester (legacy rows from the teacher form) apply to every scope.
func applyScheduleScope(db *gorm.DB, yearID, semID *uuid.UUID) *gorm.DB {
	if yearID != nil {
		db = db.Where("(teacher_schedules.academic_year_id = ? OR teacher_schedules.academic_year_id IS NULL)", *yearID)
	}
	if semID != nil {
		db = db.Where("(teacher_schedules.semester_id = ? OR teacher_schedules.semester_id IS NULL)", *semID)
	}
	return db
}

// ==================== Conflict detection ====================

// scheduleSlot is a candidate time slot checked for double booking.
type scheduleSlot struct {
	SchoolID       uuid.UUID
	EmployeeID     uuid.UUID
	ClassID        uuid.UUID
	Room           string
	DayOfWeek      int
	StartTime      string
	EndTime        string
	AcademicYearID *uuid.UUID
	SemesterID     *uuid.UUID
}

func scheduleLabels(row *model.TeacherSchedules) (cls, sub, teacher string) {
	if row.Class != nil {
		cls = row.Class.DisplayName()
	}
	if row.Subject != nil {
		sub = row.Subject.Name
	}
	if row.Teacher != nil {
		teacher = row.Teacher.FullName
	}
	return
}

// classifyConflicts turns overlapping rows into teacher / class / room
// conflicts for slot (pure; rows must already overlap in day & time).
func classifyConflicts(slot scheduleSlot, rows []model.TeacherSchedules) []dto.ScheduleConflictItem {
	out := make([]dto.ScheduleConflictItem, 0)
	room := strings.ToLower(strings.TrimSpace(slot.Room))
	for i := range rows {
		r := &rows[i]
		if r.DayOfWeek != slot.DayOfWeek || !timesOverlap(r.StartTime, r.EndTime, slot.StartTime, slot.EndTime) || isInactiveScheduleStatus(r.Status) {
			continue
		}
		cls, sub, teacher := scheduleLabels(r)
		item := dto.ScheduleConflictItem{
			ScheduleID:  r.ID,
			DayOfWeek:   r.DayOfWeek,
			StartTime:   r.StartTime,
			EndTime:     r.EndTime,
			ClassName:   cls,
			SubjectName: sub,
			TeacherName: teacher,
			Room:        r.Room,
		}
		day := dayName(r.DayOfWeek)
		if r.EmployeeID == slot.EmployeeID {
			it := item
			it.Type = "teacher"
			it.Description = fmt.Sprintf("Guru %s sudah terjadwal mengajar %s di kelas %s pada %s %s - %s", teacher, sub, cls, day, r.StartTime, r.EndTime)
			out = append(out, it)
		}
		if r.ClassID == slot.ClassID {
			it := item
			it.Type = "class"
			it.Description = fmt.Sprintf("Kelas %s sudah memiliki jadwal %s (%s) pada %s %s - %s", cls, sub, teacher, day, r.StartTime, r.EndTime)
			out = append(out, it)
		}
		if room != "" && strings.ToLower(strings.TrimSpace(r.Room)) == room {
			it := item
			it.Type = "room"
			it.Description = fmt.Sprintf("Ruangan %s sudah digunakan oleh kelas %s pada %s %s - %s", r.Room, cls, day, r.StartTime, r.EndTime)
			out = append(out, it)
		}
	}
	return out
}

// findScheduleConflicts returns the teacher / class / room double bookings of
// slot within the same academic year & semester (one query).
func findScheduleConflicts(db *gorm.DB, slot scheduleSlot, excludeIDs ...uuid.UUID) ([]dto.ScheduleConflictItem, error) {
	q := db.Model(&model.TeacherSchedules{}).
		Preload("Teacher").Preload("Subject").Preload("Class").
		Where("school_id = ? AND day_of_week = ? AND start_time < ? AND end_time > ?",
			slot.SchoolID, slot.DayOfWeek, slot.EndTime, slot.StartTime).
		Where("(status IS NULL OR lower(status) NOT IN ?)", inactiveScheduleStatuses)
	q = applyScheduleScope(q, slot.AcademicYearID, slot.SemesterID)
	if len(excludeIDs) > 0 {
		q = q.Where("id NOT IN ?", excludeIDs)
	}
	room := strings.ToLower(strings.TrimSpace(slot.Room))
	if room != "" {
		q = q.Where("(employee_id = ? OR class_id = ? OR lower(trim(room)) = ?)", slot.EmployeeID, slot.ClassID, room)
	} else {
		q = q.Where("(employee_id = ? OR class_id = ?)", slot.EmployeeID, slot.ClassID)
	}
	var rows []model.TeacherSchedules
	if err := q.Order("start_time ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return classifyConflicts(slot, rows), nil
}

func scheduleConflictError(conflicts []dto.ScheduleConflictItem) error {
	c := conflicts[0]
	return apperr.Conflict("SCHEDULE_CONFLICT", c.Description).WithDetails(map[string]string{
		"type":        c.Type,
		"schedule_id": c.ScheduleID.String(),
	})
}

// ==================== Schedules ====================

type ScheduleQuery struct {
	AcademicYearID *uuid.UUID
	SemesterID     *uuid.UUID
	ClassID        *uuid.UUID
	SubjectID      *uuid.UUID
	TeacherID      *uuid.UUID
	DayOfWeek      int
}

// resolvedSchedule is a validated schedule request ready to be stored.
type resolvedSchedule struct {
	Row          model.TeacherSchedules
	Class        model.Classes
	Subject      model.Subject
	Teacher      model.Employees
	ClassSubject *model.ClassSubject
	Warnings     []string
}

func (r *resolvedSchedule) slot() scheduleSlot {
	return scheduleSlot{
		SchoolID:       r.Row.SchoolID,
		EmployeeID:     r.Row.EmployeeID,
		ClassID:        r.Row.ClassID,
		Room:           r.Row.Room,
		DayOfWeek:      r.Row.DayOfWeek,
		StartTime:      r.Row.StartTime,
		EndTime:        r.Row.EndTime,
		AcademicYearID: r.Row.AcademicYearID,
		SemesterID:     r.Row.SemesterID,
	}
}

// resolveSchedule validates req and every referenced id against the tenant,
// derives the academic year (the class's year) / semester (given or active)
// and collects non-blocking warnings.
func (s *ScheduleService) resolveSchedule(db *gorm.DB, schoolID uuid.UUID, req dto.ScheduleRequest) (*resolvedSchedule, error) {
	req.StartTime = strings.TrimSpace(req.StartTime)
	req.EndTime = strings.TrimSpace(req.EndTime)
	req.Room = strings.TrimSpace(req.Room)
	req.Notes = strings.TrimSpace(req.Notes)
	req.Status = strings.TrimSpace(req.Status)

	fe := fieldErrors{}
	if !dto.ValidDayOfWeek(req.DayOfWeek) {
		fe.add("day_of_week", "Hari harus antara 1 (Senin) dan 7 (Minggu)")
	}
	if len(req.Room) > maxScheduleRoomLen {
		fe.add("room", fmt.Sprintf("Ruangan maksimal %d karakter", maxScheduleRoomLen))
	}
	if len(req.Notes) > maxScheduleNotesLen {
		fe.add("notes", fmt.Sprintf("Catatan maksimal %d karakter", maxScheduleNotesLen))
	}
	if len(req.Status) > maxScheduleStatusLen {
		fe.add("status", "Status tidak valid")
	}

	var period *model.SchedulePeriod
	if req.SchedulePeriodID != nil {
		var p model.SchedulePeriod
		if err := db.Where("school_id = ? AND id = ?", schoolID, *req.SchedulePeriodID).First(&p).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, invalidRef("Jam pelajaran tidak ditemukan di sekolah ini")
			}
			return nil, err
		}
		period = &p
		switch {
		case p.IsBreak:
			fe.add("schedule_period_id", "Jam istirahat tidak dapat dipakai untuk jadwal pelajaran")
		case !p.IsActive:
			fe.add("schedule_period_id", "Jam pelajaran tidak aktif")
		case p.DayOfWeek != 0 && p.DayOfWeek != req.DayOfWeek:
			fe.add("schedule_period_id", fmt.Sprintf("Jam pelajaran ini hanya berlaku pada hari %s", dayName(p.DayOfWeek)))
		}
		if req.StartTime == "" {
			req.StartTime = p.StartTime
		}
		if req.EndTime == "" {
			req.EndTime = p.EndTime
		}
	}
	if !dto.ValidHHMM(req.StartTime) {
		fe.add("start_time", "Waktu mulai harus berformat HH:MM")
	}
	if !dto.ValidHHMM(req.EndTime) {
		fe.add("end_time", "Waktu selesai harus berformat HH:MM")
	}
	if dto.ValidHHMM(req.StartTime) && dto.ValidHHMM(req.EndTime) && req.EndTime <= req.StartTime {
		fe.add("end_time", "Waktu mulai harus lebih awal dari waktu selesai")
	}
	if err := fe.err(); err != nil {
		return nil, err
	}

	res := &resolvedSchedule{Warnings: []string{}}
	if err := db.Where("school_id = ? AND id = ?", schoolID, req.ClassID).First(&res.Class).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, invalidRef("Kelas tidak ditemukan di sekolah ini")
		}
		return nil, err
	}
	if err := db.Where("school_id = ? AND id = ?", schoolID, req.SubjectID).First(&res.Subject).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, invalidRef("Mata pelajaran tidak ditemukan di sekolah ini")
		}
		return nil, err
	}
	if err := db.Where("school_id = ? AND id = ?", schoolID, req.TeacherID).First(&res.Teacher).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, invalidRef("Guru tidak ditemukan di sekolah ini")
		}
		return nil, err
	}
	if !res.Subject.IsActive {
		fe.add("subject_id", "Mata pelajaran tidak aktif")
	}

	// Academic year = the class's year (classes are per year).
	var yearID *uuid.UUID
	switch {
	case res.Class.AcademicYearID != nil:
		if req.AcademicYearID != nil && *req.AcademicYearID != *res.Class.AcademicYearID {
			fe.add("academic_year_id", "Kelas tidak termasuk tahun ajaran yang dipilih")
		}
		yearID = res.Class.AcademicYearID
	case req.AcademicYearID != nil:
		var n int64
		if err := db.Model(&model.AcademicYear{}).Where("school_id = ? AND id = ?", schoolID, *req.AcademicYearID).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, invalidRef("Tahun ajaran tidak ditemukan di sekolah ini")
		}
		yearID = req.AcademicYearID
	default:
		var y model.AcademicYear
		err := db.Where("school_id = ? AND is_active = true", schoolID).First(&y).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			yearID = &y.ID
		}
	}

	var semID *uuid.UUID
	if req.SemesterID != nil {
		sem, err := findSemester(db, schoolID, *req.SemesterID, yearID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, invalidRef("Semester tidak ditemukan pada tahun ajaran kelas ini")
			}
			return nil, err
		}
		semID = &sem.ID
	} else if yearID != nil {
		sem, err := activeSemesterOf(db, *yearID)
		if err != nil {
			return nil, err
		}
		if sem != nil {
			semID = &sem.ID
		}
	}

	// Class subject: given (must match) or looked up by class + subject + year.
	if req.ClassSubjectID != nil {
		var cs model.ClassSubject
		if err := db.Where("school_id = ? AND id = ?", schoolID, *req.ClassSubjectID).First(&cs).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, invalidRef("Mata pelajaran kelas tidak ditemukan di sekolah ini")
			}
			return nil, err
		}
		if cs.ClassID != req.ClassID || cs.SubjectID != req.SubjectID {
			fe.add("class_subject_id", "class_subject_id tidak sesuai dengan kelas dan mata pelajaran")
		}
		res.ClassSubject = &cs
	} else {
		var cs model.ClassSubject
		q := db.Where("school_id = ? AND class_id = ? AND subject_id = ?", schoolID, req.ClassID, req.SubjectID)
		if yearID != nil {
			q = q.Where("academic_year_id = ?", *yearID)
		}
		err := q.First(&cs).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			res.ClassSubject = &cs
		} else {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Mata pelajaran %s belum ditetapkan untuk kelas %s", res.Subject.Name, res.Class.DisplayName()))
		}
	}
	if err := fe.err(); err != nil {
		return nil, err
	}

	var mapped int64
	if err := db.Model(&model.TeacherSubjects{}).
		Where("school_id = ? AND employee_id = ? AND subject_id = ?", schoolID, req.TeacherID, req.SubjectID).
		Count(&mapped).Error; err != nil {
		return nil, err
	}
	if mapped == 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Guru %s belum terdaftar sebagai pengampu %s", res.Teacher.FullName, res.Subject.Name))
	}

	status := req.Status
	if status == "" {
		status = scheduleStatusActive
	}
	var csID *uuid.UUID
	if res.ClassSubject != nil {
		id := res.ClassSubject.ID
		csID = &id
	}
	var periodID *uuid.UUID
	if period != nil {
		id := period.ID
		periodID = &id
	}
	res.Row = model.TeacherSchedules{
		SchoolID:         schoolID,
		EmployeeID:       req.TeacherID,
		SubjectID:        req.SubjectID,
		ClassID:          req.ClassID,
		AcademicYearID:   yearID,
		SemesterID:       semID,
		ClassSubjectID:   csID,
		SchedulePeriodID: periodID,
		DayOfWeek:        req.DayOfWeek,
		StartTime:        req.StartTime,
		EndTime:          req.EndTime,
		Room:             req.Room,
		Status:           status,
		Notes:            req.Notes,
	}
	return res, nil
}

// jpWarnings reports when the class subject's weekly JP would exceed
// class_subjects.hours_per_week with this slot.
func (s *ScheduleService) jpWarnings(db *gorm.DB, res *resolvedSchedule, excludeID *uuid.UUID) ([]string, error) {
	if res.ClassSubject == nil || res.ClassSubject.HoursPerWeek <= 0 || isInactiveScheduleStatus(res.Row.Status) {
		return nil, nil
	}
	periods, err := s.loadPeriods(db, res.Row.SchoolID)
	if err != nil {
		return nil, err
	}
	q := db.Where("school_id = ? AND class_id = ? AND subject_id = ?", res.Row.SchoolID, res.Row.ClassID, res.Row.SubjectID).
		Where("(status IS NULL OR lower(status) NOT IN ?)", inactiveScheduleStatuses)
	q = applyScheduleScope(q, res.Row.AcademicYearID, res.Row.SemesterID)
	if excludeID != nil {
		q = q.Where("id <> ?", *excludeID)
	}
	var rows []model.TeacherSchedules
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	total := countJP(periods, res.Row.DayOfWeek, res.Row.StartTime, res.Row.EndTime)
	for _, r := range rows {
		total += countJP(periods, r.DayOfWeek, r.StartTime, r.EndTime)
	}
	if float64(total) > res.ClassSubject.HoursPerWeek {
		return []string{fmt.Sprintf("Jadwal %s di kelas %s menjadi %d JP/minggu, melebihi alokasi %g JP",
			res.Subject.Name, res.Class.DisplayName(), total, res.ClassSubject.HoursPerWeek)}, nil
	}
	return nil, nil
}

func (s *ScheduleService) ListSchedules(ctx context.Context, schoolID uuid.UUID, q ScheduleQuery) ([]dto.ScheduleResponse, error) {
	db := s.conn(ctx)
	sc, err := s.resolveScope(db, schoolID, q.AcademicYearID, q.SemesterID)
	if err != nil {
		return nil, err
	}
	q.AcademicYearID, q.SemesterID = sc.yearID(), sc.semesterID()
	rows, err := s.listScheduleRows(db, schoolID, q)
	if err != nil {
		return nil, err
	}
	result := make([]dto.ScheduleResponse, 0, len(rows))
	for i := range rows {
		result = append(result, *toScheduleResponse(&rows[i]))
	}
	return result, nil
}

// listScheduleRows lists schedules for an already resolved scope.
func (s *ScheduleService) listScheduleRows(db *gorm.DB, schoolID uuid.UUID, q ScheduleQuery) ([]model.TeacherSchedules, error) {
	tx := db.Preload("Teacher").Preload("Subject").Preload("Class").Preload("SchedulePeriod").
		Where("school_id = ?", schoolID)
	tx = applyScheduleScope(tx, q.AcademicYearID, q.SemesterID)
	if q.ClassID != nil {
		tx = tx.Where("class_id = ?", *q.ClassID)
	}
	if q.SubjectID != nil {
		tx = tx.Where("subject_id = ?", *q.SubjectID)
	}
	if q.TeacherID != nil {
		tx = tx.Where("employee_id = ?", *q.TeacherID)
	}
	if q.DayOfWeek > 0 {
		tx = tx.Where("day_of_week = ?", q.DayOfWeek)
	}
	var rows []model.TeacherSchedules
	err := tx.Order("day_of_week ASC, start_time ASC, created_at ASC").Find(&rows).Error
	return rows, err
}

// CheckScheduleConflict validates the candidate like create/update would and
// reports teacher / class / room double bookings plus warnings.
func (s *ScheduleService) CheckScheduleConflict(ctx context.Context, schoolID uuid.UUID, req dto.ScheduleRequest, excludeID *uuid.UUID) (*dto.ScheduleConflictCheckResponse, error) {
	db := s.conn(ctx)
	res, err := s.resolveSchedule(db, schoolID, req)
	if err != nil {
		return nil, err
	}
	var exclude []uuid.UUID
	if excludeID != nil {
		exclude = append(exclude, *excludeID)
	}
	conflicts, err := findScheduleConflicts(db, res.slot(), exclude...)
	if err != nil {
		return nil, err
	}
	jp, err := s.jpWarnings(db, res, excludeID)
	if err != nil {
		return nil, err
	}
	return &dto.ScheduleConflictCheckResponse{
		HasConflict: len(conflicts) > 0,
		Conflicts:   conflicts,
		Warnings:    append(res.Warnings, jp...),
	}, nil
}

func (s *ScheduleService) reloadSchedule(db *gorm.DB, schoolID, id uuid.UUID) (*dto.ScheduleResponse, error) {
	var sch model.TeacherSchedules
	if err := db.Preload("Teacher").Preload("Subject").Preload("Class").Preload("SchedulePeriod").
		Where("school_id = ? AND id = ?", schoolID, id).First(&sch).Error; err != nil {
		return nil, err
	}
	return toScheduleResponse(&sch), nil
}

// CreateSchedule stores a slot after validation and conflict detection, both
// under a per-school advisory lock. Returns non-blocking warnings.
func (s *ScheduleService) CreateSchedule(ctx context.Context, schoolID uuid.UUID, req dto.ScheduleRequest) (*dto.ScheduleResponse, []string, error) {
	var id uuid.UUID
	warnings := []string{}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		res, err := s.resolveSchedule(tx, schoolID, req)
		if err != nil {
			return err
		}
		if !isInactiveScheduleStatus(res.Row.Status) {
			conflicts, err := findScheduleConflicts(tx, res.slot())
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return scheduleConflictError(conflicts)
			}
		}
		jp, err := s.jpWarnings(tx, res, nil)
		if err != nil {
			return err
		}
		warnings = append(append(warnings, res.Warnings...), jp...)
		if err := tx.Create(&res.Row).Error; err != nil {
			return err
		}
		id = res.Row.ID
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.reloadSchedule(s.conn(ctx), schoolID, id)
	return resp, warnings, err
}

func (s *ScheduleService) UpdateSchedule(ctx context.Context, schoolID, id uuid.UUID, req dto.ScheduleRequest) (*dto.ScheduleResponse, []string, error) {
	warnings := []string{}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		var sch model.TeacherSchedules
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&sch).Error; err != nil {
			return notFoundOr(err, "Jadwal pelajaran tidak ditemukan")
		}
		if req.SemesterID == nil && req.ClassID == sch.ClassID {
			req.SemesterID = sch.SemesterID // same class: keep the stored semester
		}
		if req.Status == "" {
			req.Status = sch.Status
		}
		res, err := s.resolveSchedule(tx, schoolID, req)
		if err != nil {
			return err
		}
		if !isInactiveScheduleStatus(res.Row.Status) {
			conflicts, err := findScheduleConflicts(tx, res.slot(), id)
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				return scheduleConflictError(conflicts)
			}
		}
		jp, err := s.jpWarnings(tx, res, &id)
		if err != nil {
			return err
		}
		warnings = append(append(warnings, res.Warnings...), jp...)
		res.Row.ID = sch.ID
		res.Row.CreatedAt = sch.CreatedAt
		return tx.Save(&res.Row).Error
	})
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.reloadSchedule(s.conn(ctx), schoolID, id)
	return resp, warnings, err
}

// DeleteSchedule removes a timetable slot. A slot that already has teaching
// sessions (attendance history) is archived instead of hard-deleted, so the
// history survives and the FK class_teaching_sessions.schedule_id stays valid.
func (s *ScheduleService) DeleteSchedule(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSchoolSchedules(tx, schoolID); err != nil {
			return err
		}
		var sch model.TeacherSchedules
		if err := tx.Select("id").Where("school_id = ? AND id = ?", schoolID, id).First(&sch).Error; err != nil {
			return notFoundOr(err, "Jadwal pelajaran tidak ditemukan")
		}
		_, err := removeSchedules(tx, schoolID, []uuid.UUID{sch.ID})
		return err
	})
}

// removeSchedules deletes the given schedules of a school: rows referenced by
// teaching sessions are archived (soft delete), the rest are hard-deleted.
// Returns the number of archived rows. Every code path that deletes schedules
// (schedule/teacher/class delete, teacher form replacement) goes through here.
func removeSchedules(tx *gorm.DB, schoolID uuid.UUID, ids []uuid.UUID) (archived int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var referenced []uuid.UUID
	if err := tx.Model(&model.ClassTeachingSession{}).
		Where("school_id = ? AND schedule_id IN ?", schoolID, ids).
		Distinct().Pluck("schedule_id", &referenced).Error; err != nil {
		return 0, err
	}
	isRef := make(map[uuid.UUID]bool, len(referenced))
	for _, id := range referenced {
		isRef[id] = true
	}
	hard := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !isRef[id] {
			hard = append(hard, id)
		}
	}
	if len(referenced) > 0 {
		res := tx.Where("school_id = ? AND id IN ?", schoolID, referenced).Delete(&model.TeacherSchedules{})
		if res.Error != nil {
			return 0, res.Error
		}
		archived = res.RowsAffected
	}
	if len(hard) > 0 {
		if err := tx.Unscoped().Where("school_id = ? AND id IN ?", schoolID, hard).Delete(&model.TeacherSchedules{}).Error; err != nil {
			return 0, err
		}
	}
	return archived, nil
}

// removeSchedulesWhere removes (see removeSchedules) the school's active
// schedules matching cond.
func removeSchedulesWhere(tx *gorm.DB, schoolID uuid.UUID, cond string, args ...interface{}) error {
	var ids []uuid.UUID
	if err := tx.Model(&model.TeacherSchedules{}).Where("school_id = ?", schoolID).
		Where(cond, args...).Pluck("id", &ids).Error; err != nil {
		return err
	}
	_, err := removeSchedules(tx, schoolID, ids)
	return err
}

// detachArchivedSchedulesFromPeriods clears schedule_period_id on archived
// schedules so the periods can be deleted (archived rows keep their own
// start/end times, which is all the history needs).
func detachArchivedSchedulesFromPeriods(tx *gorm.DB, schoolID uuid.UUID, periodIDs []uuid.UUID) error {
	if len(periodIDs) == 0 {
		return nil
	}
	return tx.Unscoped().Model(&model.TeacherSchedules{}).
		Where("school_id = ? AND schedule_period_id IN ? AND deleted_at IS NOT NULL", schoolID, periodIDs).
		UpdateColumn("schedule_period_id", nil).Error
}

// GetTimetableGrid builds the weekly grid (Senin–Sabtu, plus Minggu when used)
// for a class, subject or teacher in the given (default: active) year/semester.
func (s *ScheduleService) GetTimetableGrid(ctx context.Context, schoolID uuid.UUID, q ScheduleQuery) (*dto.TimetableGridResponse, error) {
	db := s.conn(ctx)
	sc, err := s.resolveScope(db, schoolID, q.AcademicYearID, q.SemesterID)
	if err != nil {
		return nil, err
	}
	q.AcademicYearID, q.SemesterID = sc.yearID(), sc.semesterID()

	grid := &dto.TimetableGridResponse{
		AcademicYearID: q.AcademicYearID,
		SemesterID:     q.SemesterID,
		ClassID:        q.ClassID,
		SubjectID:      q.SubjectID,
		TeacherID:      q.TeacherID,
	}
	if sc.Year != nil {
		grid.AcademicYearName = sc.Year.Name
	}
	if sc.Semester != nil {
		grid.SemesterName = sc.Semester.Name
	}
	if q.ClassID != nil {
		var c model.Classes
		if err := db.Where("school_id = ? AND id = ?", schoolID, *q.ClassID).First(&c).Error; err != nil {
			return nil, notFoundOr(err, "Kelas tidak ditemukan")
		}
		grid.ClassName = c.DisplayName()
	}
	if q.SubjectID != nil {
		var sub model.Subject
		if err := db.Where("school_id = ? AND id = ?", schoolID, *q.SubjectID).First(&sub).Error; err != nil {
			return nil, notFoundOr(err, "Mata pelajaran tidak ditemukan")
		}
		grid.SubjectName = sub.Name
	}
	if q.TeacherID != nil {
		var emp model.Employees
		if err := db.Where("school_id = ? AND id = ?", schoolID, *q.TeacherID).First(&emp).Error; err != nil {
			return nil, notFoundOr(err, "Guru tidak ditemukan")
		}
		grid.TeacherName = emp.FullName
	}

	rows, err := s.listScheduleRows(db, schoolID, q)
	if err != nil {
		return nil, err
	}
	periods, err := s.loadPeriods(db, schoolID)
	if err != nil {
		return nil, err
	}
	grid.Periods = make([]dto.SchedulePeriodResponse, 0, len(periods))
	for i := range periods {
		grid.Periods = append(grid.Periods, toPeriodResponse(&periods[i]))
	}

	dayMap := make(map[int][]dto.ScheduleResponse)
	for i := range rows {
		dayMap[rows[i].DayOfWeek] = append(dayMap[rows[i].DayOfWeek], *toScheduleResponse(&rows[i]))
	}
	lastDay := 6
	if len(dayMap[7]) > 0 {
		lastDay = 7
	}
	grid.Days = make([]dto.TimetableDay, 0, lastDay)
	for d := 1; d <= lastDay; d++ {
		items := dayMap[d]
		if items == nil {
			items = []dto.ScheduleResponse{}
		}
		grid.Days = append(grid.Days, dto.TimetableDay{DayOfWeek: d, DayName: dayNames[d], Schedules: items})
	}

	if q.ClassID != nil {
		summary, err := s.classHoursSummary(db, schoolID, *q.ClassID, q.AcademicYearID, rows, periods)
		if err != nil {
			return nil, err
		}
		grid.HoursSummary = summary
	}
	return grid, nil
}

// classHoursSummary compares class_subjects.hours_per_week with scheduled JP.
func (s *ScheduleService) classHoursSummary(db *gorm.DB, schoolID, classID uuid.UUID, yearID *uuid.UUID, rows []model.TeacherSchedules, periods []model.SchedulePeriod) ([]dto.SubjectHoursItem, error) {
	q := db.Preload("Subject").Where("school_id = ? AND class_id = ?", schoolID, classID)
	if yearID != nil {
		q = q.Where("academic_year_id = ?", *yearID)
	}
	var css []model.ClassSubject
	if err := q.Find(&css).Error; err != nil {
		return nil, err
	}
	jp := map[uuid.UUID]int{}
	for _, r := range rows {
		if isInactiveScheduleStatus(r.Status) {
			continue
		}
		jp[r.SubjectID] += countJP(periods, r.DayOfWeek, r.StartTime, r.EndTime)
	}
	out := make([]dto.SubjectHoursItem, 0, len(css))
	for _, cs := range css {
		name := ""
		if cs.Subject != nil {
			name = cs.Subject.Name
		}
		out = append(out, dto.SubjectHoursItem{
			ClassSubjectID: cs.ID,
			SubjectID:      cs.SubjectID,
			SubjectName:    name,
			HoursPerWeek:   cs.HoursPerWeek,
			ScheduledJP:    jp[cs.SubjectID],
			Status:         hoursStatus(jp[cs.SubjectID], cs.HoursPerWeek),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SubjectName < out[j].SubjectName })
	return out, nil
}

func hoursStatus(scheduled int, planned float64) string {
	switch {
	case float64(scheduled) > planned:
		return "over"
	case float64(scheduled) < planned:
		return "under"
	}
	return "ok"
}

func toScheduleResponse(sch *model.TeacherSchedules) *dto.ScheduleResponse {
	var tShort *dto.HomeroomShort
	if sch.Teacher != nil {
		tShort = &dto.HomeroomShort{EmployeeID: sch.Teacher.ID, FullName: sch.Teacher.FullName}
	}
	var subResp *dto.SubjectResponse
	if sch.Subject != nil {
		subResp = subjectToResponse(sch.Subject)
	}
	var clsShort *dto.ClassSimpleShort
	if sch.Class != nil {
		clsShort = &dto.ClassSimpleShort{ID: sch.Class.ID, Name: sch.Class.Name, DisplayName: sch.Class.DisplayName()}
	}
	var pResp *dto.SchedulePeriodResponse
	if sch.SchedulePeriod != nil {
		p := toPeriodResponse(sch.SchedulePeriod)
		pResp = &p
	}
	return &dto.ScheduleResponse{
		ID:               sch.ID,
		SchoolID:         sch.SchoolID,
		ClassID:          sch.ClassID,
		SubjectID:        sch.SubjectID,
		TeacherID:        sch.EmployeeID,
		AcademicYearID:   sch.AcademicYearID,
		SemesterID:       sch.SemesterID,
		ClassSubjectID:   sch.ClassSubjectID,
		SchedulePeriodID: sch.SchedulePeriodID,
		DayOfWeek:        sch.DayOfWeek,
		StartTime:        sch.StartTime,
		EndTime:          sch.EndTime,
		Room:             sch.Room,
		Status:           sch.Status,
		Notes:            sch.Notes,
		CreatedAt:        sch.CreatedAt,
		UpdatedAt:        sch.UpdatedAt,
		Teacher:          tShort,
		Subject:          subResp,
		Class:            clsShort,
		Period:           pResp,
	}
}
