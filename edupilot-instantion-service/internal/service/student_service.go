package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StudentService struct {
	db *gorm.DB
}

func NewStudentService(db *gorm.DB) *StudentService {
	return &StudentService{db: db}
}

// conn resolves the DB lazily: controllers construct services at package init,
// before config.ConnectDB has run.
func (s *StudentService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

type StudentListQuery struct {
	Search         string
	Status         string
	Gender         string
	AcademicYearID *uuid.UUID
	GradeLevelID   *uuid.UUID
	ClassID        *uuid.UUID
	Unassigned     bool
	Page           int
	Limit          int
	Sort           string
}

// studentSorts maps accepted ?sort= values to ORDER BY clauses (unknown → name).
var studentSorts = map[string]string{
	"":              "s.full_name ASC, s.id ASC",
	"name":          "s.full_name ASC, s.id ASC",
	"full_name":     "s.full_name ASC, s.id ASC",
	"name_asc":      "s.full_name ASC, s.id ASC",
	"-name":         "s.full_name DESC, s.id ASC",
	"-full_name":    "s.full_name DESC, s.id ASC",
	"name_desc":     "s.full_name DESC, s.id ASC",
	"newest":        "s.created_at DESC, s.id ASC",
	"-created_at":   "s.created_at DESC, s.id ASC",
	"created_desc":  "s.created_at DESC, s.id ASC",
	"oldest":        "s.created_at ASC, s.id ASC",
	"created_at":    "s.created_at ASC, s.id ASC",
	"created_asc":   "s.created_at ASC, s.id ASC",
	"nisn":          "NULLIF(s.nisn, '') ASC NULLS LAST, s.full_name ASC",
	"-nisn":         "NULLIF(s.nisn, '') DESC NULLS LAST, s.full_name ASC",
	"nis":           "NULLIF(s.nis, '') ASC NULLS LAST, s.full_name ASC",
	"-nis":          "NULLIF(s.nis, '') DESC NULLS LAST, s.full_name ASC",
	"birth_date":    "s.birth_date ASC, s.full_name ASC",
	"-birth_date":   "s.birth_date DESC, s.full_name ASC",
	"updated_desc":  "s.updated_at DESC, s.id ASC",
	"-updated_at":   "s.updated_at DESC, s.id ASC",
	"gender":        "s.gender ASC, s.full_name ASC",
	"status":        "s.status ASC, s.full_name ASC",
	"name_asc_nisn": "s.full_name ASC, s.nisn ASC",
}

// summaryColumns are the columns needed for StudentSummary + completeness
// (never the photo data URL).
const summaryColumns = "s.id, s.full_name, s.nickname, s.gender, s.nisn, s.nis, s.nik, s.no_kk, s.birth_place, s.birth_date, " +
	"s.religion, s.status, s.street, s.village, s.district, s.city, s.province, s.guardians_json, s.created_at, s.updated_at, " +
	"s.user_id, s.invited_at, s.activated_at"

var (
	studentColsOnce sync.Once
	studentColsAll  string
)

// studentColumnsNoPhoto lists every students column except photo (for export).
func studentColumnsNoPhoto(db *gorm.DB) string {
	studentColsOnce.Do(func() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(&model.Students{}); err != nil {
			studentColsAll = "s.*"
			return
		}
		var cols []string
		for _, n := range stmt.Schema.DBNames {
			if n != "photo" {
				cols = append(cols, "s."+n)
			}
		}
		studentColsAll = strings.Join(cols, ", ")
	})
	return studentColsAll
}

// CalculateCompleteness evaluates a student record against critical Dapodik fields (0-100%).
func CalculateCompleteness(st *model.Students) int {
	fields := []string{
		st.FullName, st.Gender, st.NISN, st.NIS, st.NIK, st.NoKK, st.BirthPlace, st.BirthDate, st.Religion,
		st.Street, st.Village, st.District, st.City, st.Province,
	}
	total := len(fields) + 1 // + guardians
	filled := 0
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			filled++
		}
	}
	if hasJSONItems(st.GuardiansJSON) {
		filled++
	}
	return filled * 100 / total
}

func (s *StudentService) activeYearID(ctx context.Context, schoolID uuid.UUID) *uuid.UUID {
	var ids []uuid.UUID
	s.conn(ctx).Model(&model.AcademicYear{}).Where("school_id = ? AND is_active = true", schoolID).Limit(1).Pluck("id", &ids)
	if len(ids) == 0 {
		return nil
	}
	return &ids[0]
}

// filteredStudents builds the tenant-scoped, filtered students query shared by
// the list, the class roster and the export.
func (s *StudentService) filteredStudents(ctx context.Context, schoolID uuid.UUID, q StudentListQuery) (*gorm.DB, error) {
	query := s.conn(ctx).Table("students s").Where("s.school_id = ? AND s.deleted_at IS NULL", schoolID)

	if st := strings.TrimSpace(q.Status); st != "" {
		if !dto.ValidEnum(dto.RefStudentStatuses, st) {
			return nil, apperr.Validation("Status tidak valid", map[string]string{"status": "Status tidak valid"})
		}
		query = query.Where("s.status = ?", st)
	}
	if g := strings.ToUpper(strings.TrimSpace(q.Gender)); g != "" {
		if !dto.ValidEnum(dto.RefGenders, g) {
			return nil, apperr.Validation("Jenis kelamin tidak valid", map[string]string{"gender": "Jenis kelamin harus L atau P"})
		}
		query = query.Where("s.gender = ?", g)
	}
	if term := strings.TrimSpace(q.Search); term != "" {
		if len(term) > 100 {
			term = term[:100]
		}
		like := dto.LikePattern(term)
		query = query.Where("(s.full_name ILIKE ? OR s.nickname ILIKE ? OR s.nisn ILIKE ? OR s.nis ILIKE ? OR s.nik ILIKE ?)",
			like, like, like, like, like)
	}

	switch {
	case q.Unassigned:
		yearID := q.AcademicYearID
		if yearID == nil {
			yearID = s.activeYearID(ctx, schoolID)
		}
		if yearID != nil {
			query = query.Where(`NOT EXISTS (SELECT 1 FROM student_class_histories h
				WHERE h.student_id = s.id AND h.school_id = ? AND h.status = 'active' AND h.academic_year_id = ?)`, schoolID, *yearID)
		} else {
			query = query.Where(`NOT EXISTS (SELECT 1 FROM student_class_histories h
				WHERE h.student_id = s.id AND h.school_id = ? AND h.status = 'active')`, schoolID)
		}
	case q.ClassID != nil || q.GradeLevelID != nil || q.AcademicYearID != nil:
		cond := "EXISTS (SELECT 1 FROM student_class_histories h WHERE h.student_id = s.id AND h.school_id = ? AND h.status = 'active'"
		args := []interface{}{schoolID}
		if q.ClassID != nil {
			cond += " AND h.class_id = ?"
			args = append(args, *q.ClassID)
		}
		if q.GradeLevelID != nil {
			cond += " AND h.grade_level_id = ?"
			args = append(args, *q.GradeLevelID)
		}
		if q.AcademicYearID != nil {
			cond += " AND h.academic_year_id = ?"
			args = append(args, *q.AcademicYearID)
		}
		query = query.Where(cond+")", args...)
	}
	return query, nil
}

func studentOrder(sort string) string {
	if o, ok := studentSorts[strings.TrimSpace(sort)]; ok {
		return o
	}
	return studentSorts[""]
}

func (s *StudentService) ListStudents(ctx context.Context, schoolID uuid.UUID, q StudentListQuery) ([]dto.StudentSummaryResponse, *dto.PageMeta, error) {
	query, err := s.filteredStudents(ctx, schoolID, q)
	if err != nil {
		return nil, nil, err
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, nil, err
	}

	pq := dto.PageQuery{Page: q.Page, Limit: q.Limit}
	if pq.Page < 1 {
		pq.Page = dto.DefaultPage
	}
	if pq.Limit < 1 {
		pq.Limit = dto.DefaultLimit
	}
	if pq.Limit > dto.MaxLimit {
		pq.Limit = dto.MaxLimit
	}

	var students []model.Students
	if err := query.Select(summaryColumns).Order(studentOrder(q.Sort)).
		Offset(pq.Offset()).Limit(pq.Limit).Find(&students).Error; err != nil {
		return nil, nil, err
	}

	ids := make([]uuid.UUID, 0, len(students))
	for _, st := range students {
		ids = append(ids, st.ID)
	}
	classes, err := activeClassesFor(s.conn(ctx), schoolID, ids)
	if err != nil {
		return nil, nil, err
	}

	result := make([]dto.StudentSummaryResponse, 0, len(students))
	for i := range students {
		st := &students[i]
		result = append(result, studentSummary(st, classes[st.ID]))
	}

	pm := dto.NewPageMeta(pq, total)
	return result, &pm, nil
}

// ListClassStudents is the class roster (§8.5): members with an active history
// in the class, paginated StudentSummary.
func (s *StudentService) ListClassStudents(ctx context.Context, schoolID, classID uuid.UUID, q StudentListQuery) ([]dto.StudentSummaryResponse, *dto.PageMeta, error) {
	var n int64
	if err := s.conn(ctx).Model(&model.Classes{}).Where("school_id = ? AND id = ?", schoolID, classID).Count(&n).Error; err != nil {
		return nil, nil, err
	}
	if n == 0 {
		return nil, nil, apperr.NotFound("Kelas tidak ditemukan")
	}
	q.ClassID = &classID
	q.GradeLevelID, q.AcademicYearID, q.Unassigned = nil, nil, false
	return s.ListStudents(ctx, schoolID, q)
}

// studentSummary builds the StudentSummary JSON (§8.6 + §10.3 account fields).
func studentSummary(st *model.Students, class *dto.CurrentClassShort) dto.StudentSummaryResponse {
	return dto.StudentSummaryResponse{
		ID:            st.ID,
		FullName:      st.FullName,
		Nickname:      st.Nickname,
		Gender:        st.Gender,
		NISN:          st.NISN,
		NIS:           st.NIS,
		BirthPlace:    st.BirthPlace,
		BirthDate:     st.BirthDate,
		Status:        st.Status,
		CurrentClass:  class,
		Completeness:  CalculateCompleteness(st),
		UserID:        st.UserID,
		AccountStatus: model.ComputeAccountStatus(st.UserID, st.ActivatedAt),
	}
}

// activeClassesFor returns the current (active history) class of each student
// in one query.
func activeClassesFor(db *gorm.DB, schoolID uuid.UUID, studentIDs []uuid.UUID) (map[uuid.UUID]*dto.CurrentClassShort, error) {
	out := make(map[uuid.UUID]*dto.CurrentClassShort, len(studentIDs))
	if len(studentIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		StudentID        uuid.UUID
		ClassID          uuid.UUID
		ClassName        string
		Alias            string
		GradeLevelName   string
		AcademicYearName string
	}
	err := db.Raw(`
		SELECT sch.student_id, c.id AS class_id, c.class_name, COALESCE(c.alias, '') AS alias,
		       COALESCE(gl.name, '') AS grade_level_name,
		       COALESCE(ay.academic_name, '') AS academic_year_name
		FROM student_class_histories sch
		JOIN classes c ON c.id = sch.class_id
		LEFT JOIN grade_levels gl ON gl.id = sch.grade_level_id
		LEFT JOIN academic_years ay ON ay.id = sch.academic_year_id
		WHERE sch.school_id = ? AND sch.status = 'active' AND sch.student_id IN ?`, schoolID, studentIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.StudentID] = &dto.CurrentClassShort{
			ClassID:          r.ClassID,
			DisplayName:      model.ClassDisplayName(r.ClassName, r.Alias),
			GradeLevelName:   r.GradeLevelName,
			AcademicYearName: r.AcademicYearName,
		}
	}
	return out, nil
}

func (s *StudentService) GetStudent(ctx context.Context, schoolID, id uuid.UUID) (*dto.StudentDetailResponse, error) {
	var st model.Students
	if err := s.conn(ctx).Where("school_id = ? AND id = ?", schoolID, id).First(&st).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Data siswa tidak ditemukan")
		}
		return nil, err
	}
	return s.buildStudentDetailResponse(ctx, schoolID, &st)
}

func (s *StudentService) buildStudentDetailResponse(ctx context.Context, schoolID uuid.UUID, st *model.Students) (*dto.StudentDetailResponse, error) {
	guardians := []dto.GuardianItem{}
	if hasJSONItems(st.GuardiansJSON) {
		_ = json.Unmarshal([]byte(st.GuardiansJSON), &guardians)
	}
	achievements := []dto.AchievementItem{}
	if hasJSONItems(st.AchievementsJSON) {
		_ = json.Unmarshal([]byte(st.AchievementsJSON), &achievements)
	}
	scholarships := []dto.ScholarshipItem{}
	if hasJSONItems(st.ScholarshipsJSON) {
		_ = json.Unmarshal([]byte(st.ScholarshipsJSON), &scholarships)
	}

	classes, err := activeClassesFor(s.conn(ctx), schoolID, []uuid.UUID{st.ID})
	if err != nil {
		return nil, err
	}

	var photo *string
	if st.Photo != "" {
		p := st.Photo
		photo = &p
	}

	return &dto.StudentDetailResponse{
		UserID:        st.UserID,
		AccountStatus: model.ComputeAccountStatus(st.UserID, st.ActivatedAt),
		InvitedAt:     st.InvitedAt,
		ActivatedAt:   st.ActivatedAt,
		ID:            st.ID,
		SchoolID:      st.SchoolID,
		Status:        st.Status,
		Completeness:  CalculateCompleteness(st),
		Identity: dto.StudentIdentity{
			FullName:           st.FullName,
			Nickname:           st.Nickname,
			Gender:             st.Gender,
			NISN:               st.NISN,
			NIS:                st.NIS,
			NIK:                st.NIK,
			NoKK:               st.NoKK,
			BirthPlace:         st.BirthPlace,
			BirthDate:          st.BirthDate,
			BirthCertificateNo: st.BirthCertificateNo,
			Religion:           st.Religion,
			Citizenship:        st.Citizenship,
			Country:            st.Country,
			SpecialNeeds:       st.SpecialNeeds,
			BloodType:          st.BloodType,
			Photo:              photo,
			ChildOrder:         st.ChildOrder,
			SiblingsCount:      st.SiblingsCount,
			Hobby:              st.Hobby,
			Aspiration:         st.Aspiration,
		},
		Contact: dto.StudentContact{
			Phone:       st.Phone,
			MobilePhone: st.MobilePhone,
			Email:       st.Email,
		},
		Address: dto.StudentAddress{
			Street:            st.Street,
			RT:                st.RT,
			RW:                st.RW,
			Hamlet:            st.Hamlet,
			Village:           st.Village,
			District:          st.District,
			City:              st.City,
			Province:          st.Province,
			PostalCode:        st.PostalCode,
			Latitude:          st.Latitude,
			Longitude:         st.Longitude,
			ResidenceType:     st.ResidenceType,
			Transportation:    st.Transportation,
			DistanceKm:        st.DistanceKm,
			TravelTimeMinutes: st.TravelTimeMinutes,
		},
		Physical: dto.StudentPhysical{
			HeightCm:            st.HeightCm,
			WeightKg:            st.WeightKg,
			HeadCircumferenceCm: st.HeadCircumferenceCm,
		},
		Welfare: dto.StudentWelfare{
			KpsPkhReceiver:    st.KpsPkhReceiver,
			KpsPkhNumber:      st.KpsPkhNumber,
			KipReceiver:       st.KipReceiver,
			KipNumber:         st.KipNumber,
			KipName:           st.KipName,
			KksNumber:         st.KksNumber,
			PipEligible:       st.PipEligible,
			PipReason:         st.PipReason,
			BankName:          st.BankName,
			BankAccountNumber: st.BankAccountNumber,
			BankAccountName:   st.BankAccountName,
		},
		Registration: dto.StudentRegistration{
			RegistrationType:      st.RegistrationType,
			EntryDate:             st.EntryDate,
			EntryGradeLevelID:     optUUID(st.EntryGradeLevelID),
			PreviousSchoolName:    st.PreviousSchoolName,
			PreviousSchoolNPSN:    st.PreviousSchoolNPSN,
			PreviousSkhunNumber:   st.PreviousSkhunNumber,
			PreviousDiplomaNumber: st.PreviousDiplomaNumber,
			ExamParticipantNumber: st.ExamParticipantNumber,
		},
		Exit: dto.StudentExit{
			ExitDate:                    st.ExitDate,
			ExitReason:                  st.ExitReason,
			ExitDestination:             st.ExitDestination,
			GraduationCertificateNumber: st.GraduationCertificateNumber,
		},
		Guardians:    guardians,
		Achievements: achievements,
		Scholarships: scholarships,
		CurrentClass: classes[st.ID],
		CreatedAt:    st.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    st.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// applyStudentGroups copies the optional groups of req onto st (groups that are
// nil are left unchanged).
func applyStudentGroups(st *model.Students, req *dto.StudentRequest) {
	if req.Contact != nil {
		st.Phone = req.Contact.Phone
		st.MobilePhone = req.Contact.MobilePhone
		st.Email = req.Contact.Email
	}
	if req.Address != nil {
		st.Street = req.Address.Street
		st.RT = req.Address.RT
		st.RW = req.Address.RW
		st.Hamlet = req.Address.Hamlet
		st.Village = req.Address.Village
		st.District = req.Address.District
		st.City = req.Address.City
		st.Province = req.Address.Province
		st.PostalCode = req.Address.PostalCode
		st.Latitude = req.Address.Latitude
		st.Longitude = req.Address.Longitude
		st.ResidenceType = req.Address.ResidenceType
		st.Transportation = req.Address.Transportation
		st.DistanceKm = req.Address.DistanceKm
		st.TravelTimeMinutes = req.Address.TravelTimeMinutes
	}
	if req.Physical != nil {
		st.HeightCm = req.Physical.HeightCm
		st.WeightKg = req.Physical.WeightKg
		st.HeadCircumferenceCm = req.Physical.HeadCircumferenceCm
	}
	if req.Welfare != nil {
		st.KpsPkhReceiver = req.Welfare.KpsPkhReceiver
		st.KpsPkhNumber = req.Welfare.KpsPkhNumber
		st.KipReceiver = req.Welfare.KipReceiver
		st.KipNumber = req.Welfare.KipNumber
		st.KipName = req.Welfare.KipName
		st.KksNumber = req.Welfare.KksNumber
		st.PipEligible = req.Welfare.PipEligible
		st.PipReason = req.Welfare.PipReason
		st.BankName = req.Welfare.BankName
		st.BankAccountNumber = req.Welfare.BankAccountNumber
		st.BankAccountName = req.Welfare.BankAccountName
	}
	if req.Registration != nil {
		st.RegistrationType = req.Registration.RegistrationType
		st.EntryDate = req.Registration.EntryDate
		st.EntryGradeLevelID = req.Registration.EntryGradeLevelID.Ptr()
		st.PreviousSchoolName = req.Registration.PreviousSchoolName
		st.PreviousSchoolNPSN = req.Registration.PreviousSchoolNPSN
		st.PreviousSkhunNumber = req.Registration.PreviousSkhunNumber
		st.PreviousDiplomaNumber = req.Registration.PreviousDiplomaNumber
		st.ExamParticipantNumber = req.Registration.ExamParticipantNumber
	}
	if req.Exit != nil {
		st.ExitDate = req.Exit.ExitDate
		st.ExitReason = req.Exit.ExitReason
		st.ExitDestination = req.Exit.ExitDestination
		st.GraduationCertificateNumber = req.Exit.GraduationCertificateNumber
	}
}

// validateStudentRequest normalises/validates st plus the child arrays of req
// (only those present), storing them as JSON on st.
func validateStudentRequest(st *model.Students, req *dto.StudentRequest, isCreate bool) error {
	fe := fieldErrors{}
	validateStudentFields(st, fe)
	if req.Guardians != nil || isCreate {
		g := normalizeGuardians(req.Guardians, fe)
		if g == nil {
			g = []dto.GuardianItem{}
		}
		b, _ := json.Marshal(g)
		st.GuardiansJSON = string(b)
	}
	if req.Achievements != nil || isCreate {
		a := normalizeAchievements(req.Achievements, fe)
		if a == nil {
			a = []dto.AchievementItem{}
		}
		b, _ := json.Marshal(a)
		st.AchievementsJSON = string(b)
	}
	if req.Scholarships != nil || isCreate {
		sc := normalizeScholarships(req.Scholarships, fe)
		if sc == nil {
			sc = []dto.ScholarshipItem{}
		}
		b, _ := json.Marshal(sc)
		st.ScholarshipsJSON = string(b)
	}
	return fe.err()
}

// checkStudentRefs verifies uniqueness (NISN/NIS/NIK per school) and that the
// entry grade level belongs to the tenant.
func checkStudentRefs(db *gorm.DB, schoolID uuid.UUID, st *model.Students) error {
	for _, u := range []struct{ col, val, code, msg string }{
		{"nisn", st.NISN, "NISN_EXISTS", "NISN sudah digunakan oleh siswa lain"},
		{"nis", st.NIS, "NIS_EXISTS", "NIS sudah digunakan oleh siswa lain"},
		{"nik", st.NIK, "NIK_EXISTS", "NIK sudah digunakan oleh siswa lain"},
	} {
		if u.val == "" {
			continue
		}
		q := db.Model(&model.Students{}).Where("school_id = ? AND "+u.col+" = ?", schoolID, u.val)
		if st.ID != uuid.Nil {
			q = q.Where("id <> ?", st.ID)
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict(u.code, u.msg).WithDetails(map[string]string{"identity." + u.col: u.msg})
		}
	}
	if st.EntryGradeLevelID != nil {
		if *st.EntryGradeLevelID == uuid.Nil {
			st.EntryGradeLevelID = nil
		} else {
			var n int64
			if err := db.Model(&model.GradeLevel{}).Where("school_id = ? AND id = ?", schoolID, *st.EntryGradeLevelID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return apperr.Validation("Tingkat masuk tidak ditemukan", map[string]string{"registration.entry_grade_level_id": "Tingkat masuk tidak ditemukan"})
			}
		}
	}
	return nil
}

// duplicateStudentErr maps a unique-index race to the contract 409.
func duplicateStudentErr(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return apperr.Conflict("DUPLICATE", "NISN, NIS, atau NIK sudah digunakan oleh siswa lain")
	}
	return err
}

func (s *StudentService) CreateStudent(ctx context.Context, schoolID uuid.UUID, userID uint, req dto.StudentRequest) (*dto.StudentDetailResponse, error) {
	st := model.Students{
		SchoolID:           schoolID,
		Status:             req.Status,
		FullName:           req.Identity.FullName,
		Nickname:           req.Identity.Nickname,
		Gender:             req.Identity.Gender,
		NISN:               req.Identity.NISN,
		NIS:                req.Identity.NIS,
		NIK:                req.Identity.NIK,
		NoKK:               req.Identity.NoKK,
		BirthPlace:         req.Identity.BirthPlace,
		BirthDate:          req.Identity.BirthDate,
		BirthCertificateNo: req.Identity.BirthCertificateNo,
		Religion:           req.Identity.Religion,
		Citizenship:        req.Identity.Citizenship,
		Country:            req.Identity.Country,
		SpecialNeeds:       req.Identity.SpecialNeeds,
		BloodType:          req.Identity.BloodType,
		ChildOrder:         req.Identity.ChildOrder,
		SiblingsCount:      req.Identity.SiblingsCount,
		Hobby:              req.Identity.Hobby,
		Aspiration:         req.Identity.Aspiration,
	}
	applyStudentGroups(&st, &req)
	if err := validateStudentRequest(&st, &req, true); err != nil {
		return nil, err
	}

	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkStudentRefs(tx, schoolID, &st); err != nil {
			return err
		}
		if err := tx.Create(&st).Error; err != nil {
			return duplicateStudentErr(err)
		}

		// Immediate placement into a class of the active academic year.
		if req.ClassID.Valid {
			cls, err := lockClass(tx, schoolID, req.ClassID.UUID)
			if err != nil {
				return apperr.Validation("Kelas tidak ditemukan", map[string]string{"class_id": "Kelas tidak ditemukan"})
			}
			var active int64
			if err := tx.Model(&model.AcademicYear{}).
				Where("school_id = ? AND id = ? AND is_active = true", schoolID, cls.AcademicYearID).Count(&active).Error; err != nil {
				return err
			}
			if active == 0 {
				return apperr.Validation("Kelas harus berada pada tahun ajaran aktif",
					map[string]string{"class_id": "Kelas harus berada pada tahun ajaran aktif"})
			}
			if st.Status != dto.StudentStatusActive {
				return apperr.Validation("Hanya siswa aktif yang dapat ditempatkan di kelas", map[string]string{"class_id": "Siswa tidak aktif"})
			}
			if err := placeInClassTx(tx, schoolID, st.ID, cls, "", "", userID, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.GetStudent(ctx, schoolID, st.ID)
}

func (s *StudentService) UpdateStudent(ctx context.Context, schoolID, id uuid.UUID, req dto.StudentRequest) (*dto.StudentDetailResponse, error) {
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var st model.Students
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Data siswa tidak ditemukan")
			}
			return err
		}
		oldStatus := st.Status

		// Identity is always sent as a whole group: optional identifiers are
		// replaced (so they can be cleared); required fields keep their value
		// when omitted.
		st.NISN = req.Identity.NISN
		st.NIS = req.Identity.NIS
		st.NIK = req.Identity.NIK
		if strings.TrimSpace(req.Identity.FullName) != "" {
			st.FullName = req.Identity.FullName
		}
		st.Nickname = req.Identity.Nickname
		if req.Identity.Gender != "" {
			st.Gender = req.Identity.Gender
		}
		st.NoKK = req.Identity.NoKK
		if strings.TrimSpace(req.Identity.BirthPlace) != "" {
			st.BirthPlace = req.Identity.BirthPlace
		}
		if req.Identity.BirthDate != "" {
			st.BirthDate = req.Identity.BirthDate
		}
		st.BirthCertificateNo = req.Identity.BirthCertificateNo
		if req.Identity.Religion != "" {
			st.Religion = req.Identity.Religion
		}
		if req.Identity.Citizenship != "" {
			st.Citizenship = req.Identity.Citizenship
		}
		if req.Identity.Country != "" {
			st.Country = req.Identity.Country
		}
		if req.Identity.SpecialNeeds != "" {
			st.SpecialNeeds = req.Identity.SpecialNeeds
		}
		st.BloodType = req.Identity.BloodType
		st.ChildOrder = req.Identity.ChildOrder
		st.SiblingsCount = req.Identity.SiblingsCount
		st.Hobby = req.Identity.Hobby
		st.Aspiration = req.Identity.Aspiration
		applyStudentGroups(&st, &req)
		if req.Status != "" {
			st.Status = req.Status
		}

		if err := validateStudentRequest(&st, &req, false); err != nil {
			return err
		}
		if err := checkStudentRefs(tx, schoolID, &st); err != nil {
			return err
		}
		if err := tx.Omit("photo").Save(&st).Error; err != nil {
			return duplicateStudentErr(err)
		}
		// A status change via PUT follows the same history rules as PATCH /status.
		if st.Status != oldStatus && st.Status != dto.StudentStatusActive {
			end := st.ExitDate
			if end == "" {
				end = today()
			}
			if err := closeActiveHistoryTx(tx, schoolID, st.ID, historyStatusFor(st.Status), end); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetStudent(ctx, schoolID, id)
}

// historyStatusFor maps a leaving student status to the class-history status.
func historyStatusFor(studentStatus string) string {
	switch studentStatus {
	case dto.StudentStatusGraduated:
		return dto.HistoryGraduated
	case dto.StudentStatusTransferredOut:
		return dto.HistoryTransferredOut
	default:
		return dto.HistoryWithdrawn
	}
}

func (s *StudentService) UpdateStudentStatus(ctx context.Context, schoolID, id uuid.UUID, req dto.StudentStatusRequest) error {
	status := strings.TrimSpace(req.Status)
	fe := fieldErrors{}
	if !dto.ValidEnum(dto.RefStudentStatuses, status) {
		fe.add("status", "Status siswa tidak valid")
	}
	exitDate := strings.TrimSpace(req.ExitDate)
	checkDate(fe, "exit_date", &exitDate, false)
	checkLen(fe, "exit_reason", req.ExitReason, 255)
	checkLen(fe, "exit_destination", req.ExitDestination, 255)
	if err := fe.err(); err != nil {
		return err
	}

	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var st model.Students
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&st).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Data siswa tidak ditemukan")
			}
			return err
		}
		updates := map[string]interface{}{"status": status}
		if status == dto.StudentStatusActive {
			// Returning to active: clear the exit data.
			updates["exit_date"], updates["exit_reason"], updates["exit_destination"] = "", "", ""
		} else {
			if exitDate == "" {
				exitDate = today()
			}
			updates["exit_date"] = exitDate
			updates["exit_reason"] = strings.TrimSpace(req.ExitReason)
			updates["exit_destination"] = strings.TrimSpace(req.ExitDestination)
		}
		if err := tx.Model(&st).Updates(updates).Error; err != nil {
			return err
		}
		if status != dto.StudentStatusActive {
			return closeActiveHistoryTx(tx, schoolID, st.ID, historyStatusFor(status), exitDate)
		}
		return nil
	})
}

func (s *StudentService) UpdatePhoto(ctx context.Context, schoolID, id uuid.UUID, photoDataURL string) error {
	res := s.conn(ctx).Model(&model.Students{}).
		Where("school_id = ? AND id = ?", schoolID, id).
		Update("photo", photoDataURL)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("Data siswa tidak ditemukan")
	}
	return nil
}

func (s *StudentService) DeletePhoto(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.UpdatePhoto(ctx, schoolID, id, "")
}

// DeleteStudent soft-deletes the student and closes the active class history
// (withdrawn) so class counts and capacity stay correct.
func (s *StudentService) DeleteStudent(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		// Unlink the student account (§10.3): the auth user is kept, it just loses the school.
		if err := tx.Model(&model.Students{}).Where("school_id = ? AND id = ?", schoolID, id).
			Updates(map[string]interface{}{"user_id": nil, "invited_at": nil, "activated_at": nil}).Error; err != nil {
			return err
		}
		res := tx.Where("school_id = ? AND id = ?", schoolID, id).Delete(&model.Students{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("Data siswa tidak ditemukan")
		}
		return closeActiveHistoryTx(tx, schoolID, id, dto.HistoryWithdrawn, today())
	})
}

func (s *StudentService) GetStats(ctx context.Context, schoolID uuid.UUID) (*dto.StudentStatsResponse, error) {
	db := s.conn(ctx)
	var counts struct {
		Total  int
		Active int
		Male   int
		Female int
	}
	if err := db.Raw(`
		SELECT COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE status = 'active') AS active,
		       COUNT(*) FILTER (WHERE status = 'active' AND gender = 'L') AS male,
		       COUNT(*) FILTER (WHERE status = 'active' AND gender = 'P') AS female
		FROM students WHERE school_id = ? AND deleted_at IS NULL`, schoolID).Scan(&counts).Error; err != nil {
		return nil, err
	}

	activeYear := uuid.Nil
	if y := s.activeYearID(ctx, schoolID); y != nil {
		activeYear = *y
	}

	// Active students without an active placement in the active academic year.
	var unassigned int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM students s
		WHERE s.school_id = ? AND s.status = 'active' AND s.deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM student_class_histories h
		                  WHERE h.student_id = s.id AND h.school_id = ? AND h.status = 'active' AND h.academic_year_id = ?)`,
		schoolID, schoolID, activeYear).Scan(&unassigned).Error; err != nil {
		return nil, err
	}

	// Incomplete students (completeness < 80), only the needed columns.
	var students []model.Students
	if err := db.Table("students s").Select(summaryColumns).
		Where("s.school_id = ? AND s.status = 'active' AND s.deleted_at IS NULL", schoolID).
		Find(&students).Error; err != nil {
		return nil, err
	}
	incomplete := 0
	for i := range students {
		if CalculateCompleteness(&students[i]) < 80 {
			incomplete++
		}
	}

	// Per grade level, active students placed in the active academic year.
	var gradeRows []dto.StudentStatsGradeLevel
	if err := db.Raw(`
		SELECT gl.id AS grade_level_id, gl.name,
		       COUNT(s.id) FILTER (WHERE s.gender = 'L') AS male,
		       COUNT(s.id) FILTER (WHERE s.gender = 'P') AS female,
		       COUNT(s.id) AS total
		FROM grade_levels gl
		LEFT JOIN student_class_histories h ON h.grade_level_id = gl.id AND h.school_id = gl.school_id
		     AND h.status = 'active' AND h.academic_year_id = ?
		LEFT JOIN students s ON s.id = h.student_id AND s.deleted_at IS NULL AND s.status = 'active'
		WHERE gl.school_id = ?
		GROUP BY gl.id, gl.name, gl.sort_order, gl.number
		ORDER BY gl.sort_order ASC, gl.number ASC`, activeYear, schoolID).Scan(&gradeRows).Error; err != nil {
		return nil, err
	}
	if gradeRows == nil {
		gradeRows = []dto.StudentStatsGradeLevel{}
	}

	return &dto.StudentStatsResponse{
		Total:        counts.Total,
		Active:       counts.Active,
		Male:         counts.Male,
		Female:       counts.Female,
		ByGradeLevel: gradeRows,
		Unassigned:   int(unassigned),
		Incomplete:   incomplete,
	}, nil
}

func optUUID(id *uuid.UUID) dto.OptionalUUID {
	if id == nil || *id == uuid.Nil {
		return dto.OptionalUUID{}
	}
	return dto.OptionalUUID{UUID: *id, Valid: true}
}

func today() string { return time.Now().Format(dto.DateLayout) }
