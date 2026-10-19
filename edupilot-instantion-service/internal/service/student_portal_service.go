package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Student accounts, join-school and student "me" (API_CONTRACT §10.3–§10.5).

var (
	errJoinSchoolNotFound   = apperr.New(http.StatusNotFound, "SCHOOL_NOT_FOUND", "Sekolah dengan kode tersebut tidak ditemukan atau tidak menerima pendaftaran siswa")
	errStudentAccountLinked = apperr.Conflict("ALREADY_LINKED", "Akun Anda sudah terhubung dengan data siswa di sekolah")
	errRequestPending       = apperr.Conflict("REQUEST_PENDING", "Anda masih memiliki permintaan bergabung yang menunggu persetujuan")
	errJoinNotFound         = apperr.NotFound("Permintaan bergabung tidak ditemukan")
	errJoinNotPending       = apperr.Conflict("REQUEST_NOT_PENDING", "Permintaan bergabung sudah diproses")
	errStudentLinked        = apperr.Conflict("STUDENT_ALREADY_LINKED", "Data siswa ini sudah terhubung dengan akun lain")
	errStudentNoEmail       = apperr.New(http.StatusBadRequest, "EMAIL_REQUIRED", "Siswa belum memiliki email (contact.email)")
	errStudentActive        = apperr.Conflict("ALREADY_ACTIVE", "Akun siswa sudah aktif")
	errStudentInviteRace    = apperr.Conflict("INVITE_CONFLICT", "Status akun siswa berubah, silakan muat ulang data")
	errStudentInviteLimit   = apperr.New(http.StatusTooManyRequests, "RATE_LIMITED", "Undangan baru saja dikirim, coba lagi dalam 60 detik")
)

// ResolveStudentPrincipal returns the non-deleted student linked to an auth
// user and its (non-deleted) school; both nil when not linked.
func ResolveStudentPrincipal(ctx context.Context, userID uint) (*model.Students, *model.School, error) {
	db := config.DB.WithContext(ctx)
	var st model.Students
	err := db.Omit("photo", "guardians_json", "achievements_json", "scholarships_json").
		Where("user_id = ?", userID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var school model.School
	if err := db.Omit("logo").Where("id = ?", st.SchoolID).First(&school).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &st, nil, nil
		}
		return nil, nil, err
	}
	return &st, &school, nil
}

// MarkStudentActivated sets activated_at on the first authenticated student
// request and notifies account_linked (once).
func MarkStudentActivated(ctx context.Context, st *model.Students, userID uint) error {
	now := time.Now()
	res := config.DB.WithContext(ctx).Model(&model.Students{}).
		Where("id = ? AND user_id = ? AND activated_at IS NULL", st.ID, userID).
		Update("activated_at", now)
	if res.Error != nil {
		return res.Error
	}
	st.ActivatedAt = &now
	if res.RowsAffected > 0 {
		var names []string
		config.DB.WithContext(ctx).Model(&model.School{}).Where("id = ?", st.SchoolID).Limit(1).Pluck("name", &names)
		school := "sekolah"
		if len(names) > 0 {
			school = names[0]
		}
		schoolID := st.SchoolID
		Notify(ctx, NotificationInput{
			UserID: userID, SchoolID: &schoolID, Type: model.NotifAccountLinked,
			Title: "Akun terhubung",
			Body:  fmt.Sprintf("Akun Anda telah terhubung dengan data siswa di %s.", school),
			Data:  map[string]any{"student_id": st.ID.String(), "school_id": schoolID.String()},
		})
	}
	return nil
}

func schoolBrief(s *model.School, withLogo bool) dto.SchoolBrief {
	b := dto.SchoolBrief{ID: s.ID, Name: s.Name, Code: s.Code, Timezone: s.Timezone}
	if withLogo {
		b.Logo = s.Logo
	}
	return b
}

func joinRequestResponse(r *model.SchoolJoinRequest, school dto.SchoolBrief) dto.JoinRequestResponse {
	return dto.JoinRequestResponse{
		ID:           r.ID,
		School:       school,
		Email:        r.Email,
		FullName:     r.FullName,
		NISN:         r.NISN,
		BirthDate:    r.BirthDate,
		Message:      r.Message,
		Status:       r.Status,
		RejectReason: r.RejectReason,
		StudentID:    r.StudentID,
		CreatedAt:    r.CreatedAt,
		ReviewedAt:   r.ReviewedAt,
	}
}

func pendingRequestOf(db *gorm.DB, userID uint) (*model.SchoolJoinRequest, error) {
	var r model.SchoolJoinRequest
	err := db.Preload("School").Where("user_id = ? AND status = ?", userID, model.JoinStatusPending).
		Order("created_at DESC").First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetStudentMe builds GET /instantion/student/me (st/school from StudentContext).
func GetStudentMe(ctx context.Context, userID uint, st *model.Students, school *model.School) (*dto.StudentMeResponse, error) {
	db := config.DB.WithContext(ctx)
	resp := &dto.StudentMeResponse{Status: "no_school"}
	if st != nil && school != nil {
		resp.Status = "active"
		var full model.Students
		if err := db.Where("id = ?", st.ID).First(&full).Error; err != nil {
			return nil, err
		}
		var logo model.School
		if err := db.Select("id, logo").Where("id = ?", school.ID).First(&logo).Error; err == nil {
			school.Logo = logo.Logo
		}
		b := schoolBrief(school, true)
		resp.School = &b
		classes, err := activeClassesFor(db, school.ID, []uuid.UUID{full.ID})
		if err != nil {
			return nil, err
		}
		self := dto.StudentSelf{StudentSummaryResponse: studentSummary(&full, classes[full.ID])}
		if full.Photo != "" {
			p := full.Photo
			self.Photo = &p
		}
		resp.Student = &self
		return resp, nil
	}
	pending, err := pendingRequestOf(db, userID)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		resp.Status = "pending"
		jr := joinRequestResponse(pending, joinSchoolBrief(pending))
		resp.PendingRequest = &jr
	}
	return resp, nil
}

func joinSchoolBrief(r *model.SchoolJoinRequest) dto.SchoolBrief {
	if r.School == nil {
		return dto.SchoolBrief{ID: r.SchoolID}
	}
	return schoolBrief(r.School, true)
}

// CreateJoinRequest: POST /instantion/student/join.
func CreateJoinRequest(ctx context.Context, userID uint, email string, req dto.JoinSchoolRequest) (*dto.JoinRequestResponse, error) {
	req.SchoolCode = strings.TrimSpace(req.SchoolCode)
	req.FullName = strings.TrimSpace(req.FullName)
	req.Message = strings.TrimSpace(req.Message)
	fe := fieldErrors{}
	if req.FullName == "" {
		fe.add("full_name", "Nama lengkap wajib diisi")
	}
	checkPattern(fe, "nisn", &req.NISN, reDigits10, "NISN harus 10 digit angka")
	checkDate(fe, "birth_date", &req.BirthDate, true)
	if err := fe.err(); err != nil {
		return nil, err
	}

	db := config.DB.WithContext(ctx)
	var school model.School
	if err := db.Where("lower(code) = lower(?) AND is_active = true AND allow_student_join = true", req.SchoolCode).
		First(&school).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errJoinSchoolNotFound
		}
		return nil, err
	}

	var r model.SchoolJoinRequest
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockKey(tx, fmt.Sprintf("join_request:user:%d", userID)); err != nil {
			return err
		}
		var linked int64
		if err := tx.Model(&model.Students{}).Where("user_id = ?", userID).Count(&linked).Error; err != nil {
			return err
		}
		if linked > 0 {
			return errStudentAccountLinked
		}
		var pending int64
		if err := tx.Model(&model.SchoolJoinRequest{}).Where("user_id = ? AND status = ?", userID, model.JoinStatusPending).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return errRequestPending
		}
		r = model.SchoolJoinRequest{
			SchoolID:  school.ID,
			UserID:    userID,
			Email:     email,
			FullName:  req.FullName,
			NISN:      req.NISN,
			BirthDate: req.BirthDate,
			Message:   req.Message,
			Status:    model.JoinStatusPending,
		}
		return tx.Create(&r).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errRequestPending
		}
		return nil, err
	}
	resp := joinRequestResponse(&r, schoolBrief(&school, true))
	return &resp, nil
}

// CancelJoinRequest: DELETE /instantion/student/join.
func CancelJoinRequest(ctx context.Context, userID uint) error {
	res := config.DB.WithContext(ctx).Model(&model.SchoolJoinRequest{}).
		Where("user_id = ? AND status = ?", userID, model.JoinStatusPending).
		Updates(map[string]interface{}{"status": model.JoinStatusCancelled, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("Tidak ada permintaan bergabung yang menunggu")
	}
	return nil
}

var joinStatuses = map[string]bool{model.JoinStatusPending: true, model.JoinStatusApproved: true, model.JoinStatusRejected: true, model.JoinStatusCancelled: true}

// ListJoinRequests (school managers): paginated with suggestions.
func ListJoinRequests(ctx context.Context, school *model.School, status string, q dto.PageQuery) ([]dto.JoinRequestWithSuggestions, *dto.PageMeta, error) {
	db := config.DB.WithContext(ctx)
	query := db.Model(&model.SchoolJoinRequest{}).Where("school_id = ?", school.ID)
	if status = strings.TrimSpace(status); status != "" {
		if !joinStatuses[status] {
			return nil, nil, apperr.BadRequest("Status harus salah satu dari: pending, approved, rejected, cancelled")
		}
		query = query.Where("status = ?", status)
	}
	if q.Search != "" {
		like := dto.LikePattern(q.Search)
		query = query.Where("(full_name ILIKE ? OR email ILIKE ? OR nisn ILIKE ?)", like, like, like)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, nil, err
	}
	var rows []model.SchoolJoinRequest
	if err := query.Order("created_at DESC, id DESC").Offset(q.Offset()).Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, nil, err
	}

	suggestions, err := joinSuggestions(db, school.ID, rows)
	if err != nil {
		return nil, nil, err
	}
	// The school's own logo is omitted (data URL repeated per row; the app already has it).
	brief := schoolBrief(school, false)
	out := make([]dto.JoinRequestWithSuggestions, 0, len(rows))
	for i := range rows {
		sug := suggestions[rows[i].ID]
		if sug == nil {
			sug = []dto.StudentSummaryResponse{}
		}
		out = append(out, dto.JoinRequestWithSuggestions{
			JoinRequestResponse: joinRequestResponse(&rows[i], brief),
			Suggestions:         sug,
		})
	}
	pm := dto.NewPageMeta(q, total)
	return out, &pm, nil
}

// joinSuggestions finds unlinked students of the school matching each pending
// request (same NISN, or same normalized name + birth date) in one query.
func joinSuggestions(db *gorm.DB, schoolID uuid.UUID, reqs []model.SchoolJoinRequest) (map[uuid.UUID][]dto.StudentSummaryResponse, error) {
	out := map[uuid.UUID][]dto.StudentSummaryResponse{}
	var nisns, births []string
	for _, r := range reqs {
		if r.Status != model.JoinStatusPending {
			continue
		}
		if r.NISN != "" {
			nisns = append(nisns, r.NISN)
		}
		if r.BirthDate != "" {
			births = append(births, r.BirthDate)
		}
	}
	if len(nisns) == 0 && len(births) == 0 {
		return out, nil
	}
	q := db.Table("students s").Select(summaryColumns).
		Where("s.school_id = ? AND s.deleted_at IS NULL AND s.user_id IS NULL", schoolID)
	switch {
	case len(nisns) > 0 && len(births) > 0:
		q = q.Where("(s.nisn IN ? OR s.birth_date IN ?)", nisns, births)
	case len(nisns) > 0:
		q = q.Where("s.nisn IN ?", nisns)
	default:
		q = q.Where("s.birth_date IN ?", births)
	}
	var candidates []model.Students
	if err := q.Limit(1000).Find(&candidates).Error; err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	classes, err := activeClassesFor(db, schoolID, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range reqs {
		if r.Status != model.JoinStatusPending {
			continue
		}
		for i := range candidates {
			c := &candidates[i]
			if JoinSuggestionMatch(r.NISN, r.FullName, r.BirthDate, c.NISN, c.FullName, c.BirthDate) {
				out[r.ID] = append(out[r.ID], studentSummary(c, classes[c.ID]))
			}
		}
	}
	return out, nil
}

func CountJoinRequests(ctx context.Context, schoolID uuid.UUID, status string) (int64, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = model.JoinStatusPending
	}
	if !joinStatuses[status] {
		return 0, apperr.BadRequest("Status harus salah satu dari: pending, approved, rejected, cancelled")
	}
	var n int64
	err := config.DB.WithContext(ctx).Model(&model.SchoolJoinRequest{}).
		Where("school_id = ? AND status = ?", schoolID, status).Count(&n).Error
	return n, err
}

// lockPendingRequest loads a pending request of the school FOR UPDATE.
func lockPendingRequest(tx *gorm.DB, schoolID, id uuid.UUID) (*model.SchoolJoinRequest, error) {
	var r model.SchoolJoinRequest
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND school_id = ?", id, schoolID).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errJoinNotFound
		}
		return nil, err
	}
	if r.Status != model.JoinStatusPending {
		return nil, errJoinNotPending
	}
	return &r, nil
}

// ApproveJoinRequest links an existing student (student_id) or creates one
// (create) to the requesting account.
func ApproveJoinRequest(ctx context.Context, school *model.School, reviewerID uint, id uuid.UUID, req dto.JoinApproveRequest) (*dto.JoinRequestResponse, error) {
	if req.StudentID.Valid == (req.Create != nil) {
		return nil, apperr.BadRequest("Isi salah satu: student_id (tautkan siswa) atau create (buat siswa baru)")
	}
	db := config.DB.WithContext(ctx)
	var r *model.SchoolJoinRequest
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		if r, err = lockPendingRequest(tx, school.ID, id); err != nil {
			return err
		}
		var linked int64
		if err := tx.Model(&model.Students{}).Where("user_id = ?", r.UserID).Count(&linked).Error; err != nil {
			return err
		}
		if linked > 0 {
			return errStudentAccountLinked
		}

		var studentID uuid.UUID
		if req.StudentID.Valid {
			studentID = req.StudentID.UUID
		} else {
			c := req.Create
			sreq := dto.StudentRequest{
				Identity: dto.StudentIdentity{
					FullName: c.FullName, Gender: c.Gender, BirthPlace: c.BirthPlace,
					BirthDate: c.BirthDate, Religion: c.Religion, NISN: c.NISN,
				},
				Contact: &dto.StudentContact{Email: r.Email},
				ClassID: c.ClassID,
			}
			created, err := NewStudentService(tx).CreateStudent(ctx, school.ID, reviewerID, sreq)
			if err != nil {
				return err
			}
			studentID = created.ID
		}

		var st model.Students
		if err := tx.Omit("photo").Where("id = ? AND school_id = ?", studentID, school.ID).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Data siswa tidak ditemukan")
			}
			return err
		}
		if st.UserID != nil {
			return errStudentLinked
		}
		now := time.Now()
		updates := map[string]interface{}{"user_id": r.UserID, "activated_at": now, "updated_at": now}
		if strings.TrimSpace(st.Email) == "" && r.Email != "" {
			updates["email"] = r.Email
		}
		res := tx.Model(&model.Students{}).Where("id = ? AND user_id IS NULL", st.ID).Updates(updates)
		if res.Error != nil {
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				return errStudentAccountLinked
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errStudentLinked
		}
		r.Status = model.JoinStatusApproved
		r.StudentID = &st.ID
		r.ReviewedByUserID = &reviewerID
		r.ReviewedAt = &now
		return tx.Save(r).Error
	})
	if err != nil {
		return nil, err
	}

	schoolID := school.ID
	Notify(ctx, NotificationInput{
		UserID: r.UserID, SchoolID: &schoolID, Type: model.NotifJoinApproved,
		Title: "Permintaan bergabung disetujui",
		Body:  fmt.Sprintf("Selamat! Anda sudah terdaftar sebagai siswa di %s.", school.Name),
		Data:  map[string]any{"request_id": r.ID.String(), "school_id": schoolID.String(), "student_id": r.StudentID.String()},
	})
	resp := joinRequestResponse(r, schoolBrief(school, false))
	return &resp, nil
}

// RejectJoinRequest marks a pending request rejected and notifies the user.
func RejectJoinRequest(ctx context.Context, school *model.School, reviewerID uint, id uuid.UUID, reason string) (*dto.JoinRequestResponse, error) {
	reason = strings.TrimSpace(reason)
	db := config.DB.WithContext(ctx)
	var r *model.SchoolJoinRequest
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		if r, err = lockPendingRequest(tx, school.ID, id); err != nil {
			return err
		}
		now := time.Now()
		r.Status = model.JoinStatusRejected
		r.RejectReason = reason
		r.ReviewedByUserID = &reviewerID
		r.ReviewedAt = &now
		return tx.Save(r).Error
	})
	if err != nil {
		return nil, err
	}
	body := fmt.Sprintf("Permintaan bergabung ke %s ditolak.", school.Name)
	if reason != "" {
		body += " Alasan: " + reason
	}
	schoolID := school.ID
	Notify(ctx, NotificationInput{
		UserID: r.UserID, SchoolID: &schoolID, Type: model.NotifJoinRejected,
		Title: "Permintaan bergabung ditolak", Body: body,
		Data: map[string]any{"request_id": r.ID.String(), "school_id": schoolID.String(), "reason": reason},
	})
	resp := joinRequestResponse(r, schoolBrief(school, false))
	return &resp, nil
}

// =========================================================================
// Student invite (§10.3) — mirrors InviteTeacher (§7.3)
// =========================================================================

func studentInviteKey(id uuid.UUID) string {
	return fmt.Sprintf("instantion:student-invite:%s", id)
}

// InviteStudent creates (none) or re-sends (invited) the student account invitation.
func InviteStudent(ctx context.Context, schoolID uuid.UUID, schoolName string, id uuid.UUID) (*dto.StudentDetailResponse, string, error) {
	db := config.DB.WithContext(ctx)
	var st model.Students
	if err := db.Omit("photo").Where("id = ? AND school_id = ?", id, schoolID).First(&st).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", apperr.NotFound("Data siswa tidak ditemukan")
		}
		return nil, "", err
	}
	email := strings.TrimSpace(st.Email)
	if email == "" {
		return nil, "", errStudentNoEmail
	}
	if model.ComputeAccountStatus(st.UserID, st.ActivatedAt) == model.AccountStatusActive {
		return nil, "", errStudentActive
	}

	ok, release := acquireRateSlot(ctx, studentInviteKey(id))
	if !ok {
		return nil, "", errStudentInviteLimit
	}
	accounts := NewTeacherAccounts()
	warning, err := sendStudentInvite(ctx, accounts, schoolID, schoolName, &st, email)
	if err != nil {
		release()
		return nil, "", err
	}
	detail, err := NewStudentService(nil).GetStudent(ctx, schoolID, id)
	if err != nil {
		return nil, "", err
	}
	return detail, warning, nil
}

// acquireRateSlot: SETNX cooldown key (fails open on Redis errors).
func acquireRateSlot(ctx context.Context, key string) (bool, func()) {
	noop := func() {}
	if config.RDB == nil {
		return true, noop
	}
	ok, err := config.RDB.SetNX(ctx, key, 1, teacherInviteCooldown).Result()
	if err != nil {
		config.Logger.Warn("invite rate limit check failed, allowing", zap.Error(err))
		return true, noop
	}
	if !ok {
		return false, noop
	}
	return true, func() { config.RDB.Del(context.WithoutCancel(ctx), key) }
}

func sendStudentInvite(ctx context.Context, accounts TeacherAccounts, schoolID uuid.UUID, schoolName string, st *model.Students, email string) (string, error) {
	db := config.DB.WithContext(ctx)
	callCtx, cancel := authCallContext(ctx)
	defer cancel()

	if st.UserID != nil {
		u, err := accounts.ResendInvite(callCtx, *st.UserID, schoolName)
		if err == nil {
			if err := db.Model(&model.Students{}).Where("id = ? AND school_id = ?", st.ID, schoolID).
				Updates(map[string]interface{}{"invited_at": time.Now(), "updated_at": time.Now()}).Error; err != nil {
				return "", err
			}
			return studentInviteWarning(u), nil
		}
		var ae *client.AuthError
		if !errors.As(err, &ae) {
			config.Logger.Error("auth service resend student invite failed", zap.Error(err))
			return "", apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal mengirim ulang undangan")
		}
		switch ae.Status {
		case http.StatusConflict:
			return "", errStudentActive
		case http.StatusNotFound:
			config.Logger.Warn("student auth account missing, re-inviting",
				zap.String("student_id", st.ID.String()), zap.Uint("user_id", *st.UserID))
			if err := db.Model(&model.Students{}).Where("id = ? AND school_id = ? AND user_id = ?", st.ID, schoolID, *st.UserID).
				Updates(map[string]interface{}{"user_id": nil, "invited_at": nil, "activated_at": nil, "updated_at": time.Now()}).Error; err != nil {
				return "", err
			}
			st.UserID = nil
		default:
			config.Logger.Error("auth service resend student invite failed", zap.Error(err))
			return "", apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal mengirim ulang undangan")
		}
	}

	var phone *string
	if p := strings.TrimSpace(st.MobilePhone); p != "" {
		phone = &p
	}
	u, err := accounts.InviteUser(callCtx, client.InviteUserRequest{
		Email: email, FullName: st.FullName, Phone: phone, Role: "student", SchoolName: schoolName,
	})
	if err != nil {
		return "", mapInviteError(err)
	}
	now := time.Now()
	res := db.Model(&model.Students{}).
		Where("id = ? AND school_id = ? AND user_id IS NULL", st.ID, schoolID).
		Updates(map[string]interface{}{"user_id": u.ID, "invited_at": now, "activated_at": nil, "updated_at": now})
	if res.Error != nil || res.RowsAffected == 0 {
		compensateUser(accounts, u.ID)
		if res.Error != nil {
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				return "", errEmailInUse
			}
			return "", res.Error
		}
		return "", errStudentInviteRace
	}
	return studentInviteWarning(u), nil
}

// StudentInviteEmailWarning: account created/re-invited but the email failed.
const StudentInviteEmailWarning = "Akun siswa berhasil dibuat, tetapi email undangan gagal dikirim. Silakan kirim ulang undangan."

func studentInviteWarning(u *client.InvitedUser) string {
	if u != nil && !u.EmailSent {
		return StudentInviteEmailWarning
	}
	return ""
}
