package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	errTeacherNotFound  = apperr.New(http.StatusNotFound, "TEACHER_NOT_FOUND", "Guru tidak ditemukan")
	errEmployeeCodeUsed = apperr.Conflict("EMPLOYEE_CODE_EXISTS", "Kode pegawai sudah digunakan di sekolah ini")
)

// teacherScope restricts a query to non-deleted teachers of one school.
func teacherScope(db *gorm.DB, schoolID uuid.UUID) *gorm.DB {
	return db.Model(&model.Employees{}).
		Where("school_id = ? AND employee_type = ?", schoolID, model.EmployeeTypeTeacher)
}

// ListTeachers returns a page of the school's teachers.
func ListTeachers(ctx context.Context, schoolID uuid.UUID, q dto.PageQuery) ([]model.Employees, int64, error) {
	base := teacherScope(config.DB.WithContext(ctx), schoolID)
	if q.Search != "" {
		like := dto.LikePattern(q.Search)
		base = base.Where("(full_name ILIKE ? OR employee_code ILIKE ? OR email ILIKE ?)", like, like, like)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	teachers := []model.Employees{}
	if err := base.Session(&gorm.Session{}).
		Order("full_name ASC, created_at DESC").
		Offset(q.Offset()).Limit(q.Limit).
		Find(&teachers).Error; err != nil {
		return nil, 0, err
	}
	return teachers, total, nil
}

func findTeacher(db *gorm.DB, schoolID, id uuid.UUID) (*model.Employees, error) {
	var t model.Employees
	if err := teacherScope(db, schoolID).Where("id = ?", id).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errTeacherNotFound
		}
		return nil, err
	}
	return &t, nil
}

// GetTeacher returns a teacher with subjects, assignments and schedules.
func GetTeacher(ctx context.Context, schoolID, id uuid.UUID) (*dto.TeacherDetail, error) {
	db := config.DB.WithContext(ctx)
	t, err := findTeacher(db, schoolID, id)
	if err != nil {
		return nil, err
	}
	detail := dto.TeacherDetail{
		Employees:   *t,
		Subjects:    []model.TeacherSubjects{},
		Assignments: []dto.TeacherAssignmentItem{},
		Schedules:   []dto.TeacherScheduleItem{},
	}
	if err := db.Preload("Subject").Where("school_id = ? AND employee_id = ?", schoolID, id).Order("created_at").Find(&detail.Subjects).Error; err != nil {
		return nil, err
	}
	var assignments []model.EmployeeAssignments
	if err := db.Where("school_id = ? AND employee_id = ?", schoolID, id).Order("created_at").Find(&assignments).Error; err != nil {
		return nil, err
	}
	if len(assignments) > 0 {
		posIDs := make([]uuid.UUID, 0, len(assignments))
		for _, a := range assignments {
			posIDs = append(posIDs, a.EmployeePositionID)
		}
		var positions []model.EmployeePosition
		if err := db.Where("school_id = ? AND id IN ?", schoolID, posIDs).Find(&positions).Error; err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]*dto.PositionShort, len(positions))
		for _, p := range positions {
			r := positionResponse(p, 0)
			byID[p.ID] = &dto.PositionShort{ID: p.ID, PositionCode: r.PositionCode, Position: r.Position, Category: r.Category}
		}
		for _, a := range assignments {
			detail.Assignments = append(detail.Assignments, dto.TeacherAssignmentItem{EmployeeAssignments: a, Position: byID[a.EmployeePositionID]})
		}
	}
	var schedules []model.TeacherSchedules
	if err := db.Preload("Subject").Preload("Class").Preload("SchedulePeriod").Where("school_id = ? AND employee_id = ?", schoolID, id).Order("day_of_week, start_time").Find(&schedules).Error; err != nil {
		return nil, err
	}
	for _, sch := range schedules {
		detail.Schedules = append(detail.Schedules, dto.TeacherScheduleItem{TeacherSchedules: sch, PeriodID: sch.SchedulePeriodID})
	}
	return &detail, nil
}

// employeeCodeTaken checks uniqueness of employee_code within a school among
// non-deleted employees (matches the partial unique index).
func employeeCodeTaken(db *gorm.DB, schoolID uuid.UUID, code string, exclude uuid.UUID) (bool, error) {
	var count int64
	q := db.Model(&model.Employees{}).Where("school_id = ? AND employee_code = ?", schoolID, code)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// verifyOwned ensures every id exists (and, for soft-delete tables, is not
// deleted) in table with the given school_id. label names the data for the
// client message ("Mata pelajaran", ...).
func verifyOwned(db *gorm.DB, table string, schoolID uuid.UUID, ids map[uuid.UUID]struct{}, label string, softDelete bool) error {
	if len(ids) == 0 {
		return nil
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var count int64
	q := db.Table(table).Where("school_id = ? AND id IN ?", schoolID, list)
	if softDelete {
		q = q.Where("deleted_at IS NULL")
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(list)) {
		return apperr.New(http.StatusBadRequest, "INVALID_REFERENCE", fmt.Sprintf("%s tidak ditemukan di sekolah ini (mungkin sudah dihapus)", label))
	}
	return nil
}

// verifyReferences checks that all referenced ids belong to the tenant school.
func verifyReferences(db *gorm.DB, schoolID uuid.UUID, req *dto.TeacherRequest) error {
	subjects := map[uuid.UUID]struct{}{}
	academics := map[uuid.UUID]struct{}{}
	classes := map[uuid.UUID]struct{}{}
	positions := map[uuid.UUID]struct{}{}
	for _, s := range req.Subjects {
		subjects[s.SubjectID.UUID] = struct{}{}
		if s.AcademicID.Valid {
			academics[s.AcademicID.UUID] = struct{}{}
		}
	}
	for _, s := range req.Schedules {
		subjects[s.SubjectID.UUID] = struct{}{}
		classes[s.ClassID.UUID] = struct{}{}
	}
	for _, a := range req.Assignments {
		positions[a.EmployeePositionID.UUID] = struct{}{}
	}
	if err := verifyOwned(db, "subjects", schoolID, subjects, "Mata pelajaran", true); err != nil {
		return err
	}
	if err := verifyOwned(db, "academic_years", schoolID, academics, "Tahun ajaran", false); err != nil {
		return err
	}
	if err := verifyOwned(db, "classes", schoolID, classes, "Kelas", true); err != nil {
		return err
	}
	if err := verifyOwned(db, "employee_positions", schoolID, positions, "Jabatan", false); err != nil {
		return err
	}
	return resolveSchedulePeriods(db, schoolID, req.Schedules)
}

// resolveSchedulePeriods checks every schedule's period_id (tenant, not a
// break, active, valid on that day) and takes start/end times from it.
func resolveSchedulePeriods(db *gorm.DB, schoolID uuid.UUID, items []dto.TeacherScheduleReq) error {
	ids := map[uuid.UUID]struct{}{}
	for _, it := range items {
		if it.PeriodID.Valid {
			ids[it.PeriodID.UUID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var periods []model.SchedulePeriod
	if err := db.Where("school_id = ? AND id IN ?", schoolID, list).Find(&periods).Error; err != nil {
		return err
	}
	byID := make(map[uuid.UUID]model.SchedulePeriod, len(periods))
	for _, p := range periods {
		byID[p.ID] = p
	}
	for i := range items {
		it := &items[i]
		if !it.PeriodID.Valid {
			continue
		}
		field := fmt.Sprintf("schedules[%d].period_id", i)
		p, ok := byID[it.PeriodID.UUID]
		var msg string
		switch {
		case !ok:
			return apperr.New(http.StatusBadRequest, "INVALID_REFERENCE", "Jam pelajaran tidak ditemukan di sekolah ini").
				WithDetails(map[string]string{field: "Jam pelajaran tidak ditemukan di sekolah ini"})
		case p.IsBreak:
			msg = fmt.Sprintf("%s adalah jam istirahat, tidak dapat dipakai untuk jadwal mengajar", p.Name)
		case !p.IsActive:
			msg = fmt.Sprintf("%s tidak aktif", p.Name)
		case p.DayOfWeek != 0 && p.DayOfWeek != it.DayOfWeek:
			msg = fmt.Sprintf("%s hanya berlaku pada hari %s", p.Name, dayName(p.DayOfWeek))
		}
		if msg != "" {
			return apperr.Validation(msg, map[string]string{field: msg})
		}
		it.StartTime, it.EndTime = p.StartTime, p.EndTime
	}
	return nil
}

func applyTeacherRequest(e *model.Employees, req *dto.TeacherRequest) {
	e.FullName = req.FullName
	e.EmployeeCode = req.EmployeeCode
	e.Email = req.Email
	e.Phone = req.Phone
	e.NumberType = req.NumberType
	e.Number = req.Number
	e.EmployeeType = model.EmployeeTypeTeacher
	e.EmployeeStatus = req.EmployeeStatus
	e.JoinDate = req.JoinDate
	e.Status = req.Status
	if req.IsCounselor != nil {
		e.IsCounselor = *req.IsCounselor
	}
}

// writeRelations replaces the relation sets that are present (non-nil) in req.
func writeRelations(tx *gorm.DB, schoolID, employeeID uuid.UUID, req *dto.TeacherRequest, replace bool) error {
	if req.Subjects != nil {
		if replace {
			if err := tx.Where("school_id = ? AND employee_id = ?", schoolID, employeeID).Delete(&model.TeacherSubjects{}).Error; err != nil {
				return err
			}
		}
		for _, s := range req.Subjects {
			var acadPtr *uuid.UUID
			if s.AcademicID.Valid {
				acadPtr = &s.AcademicID.UUID
			}
			row := model.TeacherSubjects{
				ID:         uuid.New(),
				SchoolID:   schoolID,
				EmployeeID: employeeID,
				SubjectID:  s.SubjectID.UUID,
				AcademicID: acadPtr,
				IsPrimary:  s.IsPrimary.String(),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
	}

	if req.Assignments != nil {
		if replace {
			if err := tx.Where("school_id = ? AND employee_id = ?", schoolID, employeeID).Delete(&model.EmployeeAssignments{}).Error; err != nil {
				return err
			}
		}
		for _, a := range req.Assignments {
			active := dto.FlexBool(true)
			if a.IsActive != nil {
				active = *a.IsActive
			}
			row := model.EmployeeAssignments{
				ID:                 uuid.New(),
				EmployeeID:         employeeID,
				SchoolID:           schoolID,
				EmployeePositionID: a.EmployeePositionID.UUID,
				StartDate:          a.StartDate,
				EndDate:            a.EndDate,
				IsActive:           active.String(),
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
	}

	if req.Schedules != nil {
		if err := writeTeacherSchedules(tx, schoolID, employeeID, req.Schedules, replace); err != nil {
			return err
		}
	}
	return nil
}

func mapTeacherWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errEmployeeCodeUsed
	}
	return err
}

// CreateTeacher creates a teacher and its relations in one transaction.
// req must already be normalized (dto.TeacherRequest.Normalize). When an email is
// present and send_invite is not false, the teacher's auth account is invited
// inside the flow (after all validation passed); if anything fails afterwards the
// account is deleted again (compensation). The returned string is a non-fatal
// warning (invite email not sent), or "".
func CreateTeacher(ctx context.Context, schoolID uuid.UUID, schoolName string, req *dto.TeacherRequest) (*model.Employees, string, error) {
	var employee model.Employees
	var invited *client.InvitedUser
	var accounts TeacherAccounts

	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if taken, err := employeeCodeTaken(tx, schoolID, req.EmployeeCode, uuid.Nil); err != nil {
			return err
		} else if taken {
			return errEmployeeCodeUsed
		}
		if err := verifyReferences(tx, schoolID, req); err != nil {
			return err
		}

		employee = model.Employees{ID: uuid.New(), SchoolID: schoolID}
		applyTeacherRequest(&employee, req)

		if req.WantsInvite() {
			accounts = NewTeacherAccounts()
			callCtx, cancel := authCallContext(ctx)
			defer cancel()
			u, err := accounts.InviteUser(callCtx, inviteRequest(&employee, schoolName))
			if err != nil {
				return mapInviteError(err)
			}
			invited = u
			now := time.Now()
			employee.UserID = &u.ID
			employee.InvitedAt = &now
		}

		if err := tx.Create(&employee).Error; err != nil {
			return mapTeacherWriteError(err)
		}
		return writeRelations(tx, schoolID, employee.ID, req, false)
	})
	if err != nil {
		if invited != nil && accounts != nil {
			compensateUser(accounts, invited.ID)
		}
		return nil, "", err
	}
	employee.RefreshAccountStatus()
	return &employee, inviteWarning(invited), nil
}

// UpdateTeacher updates a teacher; relation arrays present in req replace existing rows.
func UpdateTeacher(ctx context.Context, schoolID, id uuid.UUID, req *dto.TeacherRequest) (*model.Employees, error) {
	var employee *model.Employees
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		employee, err = findTeacher(tx, schoolID, id)
		if err != nil {
			return err
		}
		if emailChangeLocked(employee, req.Email) {
			return errEmailLocked
		}
		if taken, err := employeeCodeTaken(tx, schoolID, req.EmployeeCode, id); err != nil {
			return err
		} else if taken {
			return errEmployeeCodeUsed
		}
		if err := verifyReferences(tx, schoolID, req); err != nil {
			return err
		}

		applyTeacherRequest(employee, req)
		if err := tx.Select("FullName", "EmployeeCode", "Email", "Phone", "NumberType", "Number",
			"EmployeeType", "EmployeeStatus", "JoinDate", "Status", "IsCounselor", "UpdatedAt").
			Where("school_id = ?", schoolID).
			Updates(employee).Error; err != nil {
			return mapTeacherWriteError(err)
		}
		return writeRelations(tx, schoolID, id, req, true)
	})
	if err != nil {
		return nil, err
	}
	return employee, nil
}

// DeleteTeacher soft-deletes the teacher and removes its schedules. A linked auth
// account is deleted afterwards (best effort, logged).
func DeleteTeacher(ctx context.Context, schoolID, id uuid.UUID) error {
	var teacher *model.Employees
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if teacher, err = findTeacher(tx, schoolID, id); err != nil {
			return err
		}
		if err := removeSchedulesWhere(tx, schoolID, "employee_id = ?", id); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND employee_id = ?", schoolID, id).Delete(&model.TeacherSubjects{}).Error; err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND employee_id = ?", schoolID, id).Delete(&model.EmployeeAssignments{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ClassSubject{}).Where("school_id = ? AND primary_teacher_id = ?", schoolID, id).
			Update("primary_teacher_id", nil).Error; err != nil {
			return err
		}
		return tx.Where("school_id = ? AND id = ?", schoolID, id).Delete(&model.Employees{}).Error
	})
	if err != nil {
		return err
	}
	if teacher.UserID != nil && *teacher.UserID != 0 {
		deleteTeacherAccount(ctx, *teacher.UserID)
	}
	return nil
}

func teacherScheduleKey(subjectID, classID uuid.UUID, day int, start, end string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", subjectID, classID, day, start, end)
}

// writeTeacherSchedules applies the teacher form's schedule list. With replace,
// rows that are resent unchanged are kept as-is (so room, notes, year/semester
// set in the timetable module survive a teacher edit); others are removed. A
// resent row without period_id matches a stored row regardless of its period;
// with period_id it matches the same period (or a stored row without one,
// which then gets the period). New rows get the class's academic year + active
// semester and go through the same teacher/class conflict check as POST
// /schedules; a conflict reports the offending row as details.index.
func writeTeacherSchedules(tx *gorm.DB, schoolID, employeeID uuid.UUID, items []dto.TeacherScheduleReq, replace bool) error {
	if err := lockSchoolSchedules(tx, schoolID); err != nil {
		return err
	}
	pool := map[string][]model.TeacherSchedules{}
	var existingIDs []uuid.UUID
	if replace {
		var existing []model.TeacherSchedules
		if err := tx.Where("school_id = ? AND employee_id = ?", schoolID, employeeID).Order("created_at").Find(&existing).Error; err != nil {
			return err
		}
		for _, e := range existing {
			k := teacherScheduleKey(e.SubjectID, e.ClassID, e.DayOfWeek, e.StartTime, e.EndTime)
			pool[k] = append(pool[k], e)
			existingIDs = append(existingIDs, e.ID)
		}
	}
	keep := map[uuid.UUID]bool{}
	setPeriod := map[uuid.UUID]uuid.UUID{}
	type newRow struct {
		index int
		item  dto.TeacherScheduleReq
	}
	var toCreate []newRow
	for i, it := range items {
		k := teacherScheduleKey(it.SubjectID.UUID, it.ClassID.UUID, it.DayOfWeek, it.StartTime, it.EndTime)
		if j := matchStoredSchedule(pool[k], it.PeriodID); j >= 0 {
			row := pool[k][j]
			keep[row.ID] = true
			if it.PeriodID.Valid && row.SchedulePeriodID == nil {
				setPeriod[row.ID] = it.PeriodID.UUID
			}
			pool[k] = append(pool[k][:j:j], pool[k][j+1:]...)
			continue
		}
		toCreate = append(toCreate, newRow{index: i, item: it})
	}
	var drop []uuid.UUID
	for _, id := range existingIDs {
		if !keep[id] {
			drop = append(drop, id)
		}
	}
	if len(drop) > 0 {
		if _, err := removeSchedules(tx, schoolID, drop); err != nil {
			return err
		}
	}
	for id, periodID := range setPeriod {
		if err := tx.Model(&model.TeacherSchedules{}).Where("school_id = ? AND id = ?", schoolID, id).
			Update("schedule_period_id", periodID).Error; err != nil {
			return err
		}
	}
	if len(toCreate) == 0 {
		return nil
	}

	classIDs := make([]uuid.UUID, 0, len(toCreate))
	for _, nr := range toCreate {
		classIDs = append(classIDs, nr.item.ClassID.UUID)
	}
	var classes []model.Classes
	if err := tx.Where("school_id = ? AND id IN ?", schoolID, classIDs).Find(&classes).Error; err != nil {
		return err
	}
	classYear := map[uuid.UUID]*uuid.UUID{}
	for i := range classes {
		classYear[classes[i].ID] = classes[i].AcademicYearID
	}
	semByYear := map[uuid.UUID]*uuid.UUID{}
	for _, nr := range toCreate {
		it := nr.item
		yearID := classYear[it.ClassID.UUID]
		var semID *uuid.UUID
		if yearID != nil {
			if cached, ok := semByYear[*yearID]; ok {
				semID = cached
			} else {
				sem, err := activeSemesterOf(tx, *yearID)
				if err != nil {
					return err
				}
				if sem != nil {
					semID = &sem.ID
				}
				semByYear[*yearID] = semID
			}
		}
		var periodID *uuid.UUID
		if it.PeriodID.Valid {
			id := it.PeriodID.UUID
			periodID = &id
		}
		row := model.TeacherSchedules{
			ID:               uuid.New(),
			SchoolID:         schoolID,
			EmployeeID:       employeeID,
			SubjectID:        it.SubjectID.UUID,
			ClassID:          it.ClassID.UUID,
			AcademicYearID:   yearID,
			SemesterID:       semID,
			SchedulePeriodID: periodID,
			DayOfWeek:        it.DayOfWeek,
			StartTime:        it.StartTime,
			EndTime:          it.EndTime,
			Status:           scheduleStatusActive,
		}
		conflicts, err := findScheduleConflicts(tx, scheduleSlot{
			SchoolID: schoolID, EmployeeID: employeeID, ClassID: row.ClassID,
			DayOfWeek: row.DayOfWeek, StartTime: row.StartTime, EndTime: row.EndTime,
			AcademicYearID: yearID, SemesterID: semID,
		})
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return teacherScheduleConflictError(conflicts, nr.index)
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// matchStoredSchedule picks the stored row (same subject/class/day/time) a
// resent item keeps: any row when the item has no period, else the row with
// that period, else a row without a period. -1 when none matches.
func matchStoredSchedule(rows []model.TeacherSchedules, period dto.OptionalUUID) int {
	if len(rows) == 0 {
		return -1
	}
	if !period.Valid {
		return 0
	}
	fallback := -1
	for j, r := range rows {
		if r.SchedulePeriodID != nil && *r.SchedulePeriodID == period.UUID {
			return j
		}
		if r.SchedulePeriodID == nil && fallback < 0 {
			fallback = j
		}
	}
	return fallback
}

// teacherScheduleConflictError is scheduleConflictError plus the index of the
// teacher-form row (details.index) so the form can highlight it.
func teacherScheduleConflictError(conflicts []dto.ScheduleConflictItem, index int) error {
	c := conflicts[0]
	return apperr.Conflict("SCHEDULE_CONFLICT", c.Description).WithDetails(map[string]string{
		"type":        c.Type,
		"schedule_id": c.ScheduleID.String(),
		"index":       strconv.Itoa(index),
	})
}
