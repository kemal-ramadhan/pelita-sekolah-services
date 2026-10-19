package service

import (
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

// Student field validation (API_CONTRACT §8.6). Shared by the JSON API and the
// .xlsx import so both apply exactly the same rules.

const (
	maxGuardians    = 10
	maxAchievements = 50
	maxScholarships = 50
)

var (
	reDigits10   = regexp.MustCompile(`^[0-9]{10}$`)
	reDigits16   = regexp.MustCompile(`^[0-9]{16}$`)
	rePhone      = regexp.MustCompile(`^\+?[0-9]{8,15}$`)
	reShortNum   = regexp.MustCompile(`^[0-9]{1,5}$`)
	rePostalCode = regexp.MustCompile(`^[0-9]{5}$`)
	reNPSN       = regexp.MustCompile(`^[0-9A-Za-z]{8}$`)
	reNIS        = regexp.MustCompile(`^[0-9A-Za-z./-]{1,30}$`)
)

// fieldErrors collects field-path -> message.
type fieldErrors map[string]string

func (fe fieldErrors) add(field, msg string) {
	if _, ok := fe[field]; !ok {
		fe[field] = msg
	}
}

// err returns a VALIDATION_ERROR (first message, sorted by field) or nil.
func (fe fieldErrors) err() error {
	if len(fe) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fe))
	for k := range fe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return apperr.Validation(fe[keys[0]], map[string]string(fe))
}

// normalizePhone strips spaces, dashes, dots and parentheses.
func normalizePhone(s string) string {
	return strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "").Replace(strings.TrimSpace(s))
}

// checkLen enforces a maximum rune length.
func checkLen(fe fieldErrors, field, v string, max int) {
	if len([]rune(v)) > max {
		fe.add(field, fmt.Sprintf("%s maksimal %d karakter", field, max))
	}
}

// normEnum canonicalises an enum value (value or label, case-insensitive);
// empty stays empty. Unknown values are reported.
func normEnum(fe fieldErrors, field, key string, v *string) {
	*v = strings.TrimSpace(*v)
	if *v == "" {
		return
	}
	if canon, ok := dto.MatchEnum(key, *v); ok {
		*v = canon
		return
	}
	fe.add(field, fmt.Sprintf("%s tidak valid (lihat referensi %s)", field, key))
}

// parseDate validates YYYY-MM-DD.
func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse(dto.DateLayout, s)
	return t, err == nil
}

// checkDate validates an optional YYYY-MM-DD date; notFuture rejects dates after today.
func checkDate(fe fieldErrors, field string, v *string, notFuture bool) {
	*v = strings.TrimSpace(*v)
	if *v == "" {
		return
	}
	t, ok := parseDate(*v)
	if !ok {
		fe.add(field, fmt.Sprintf("%s harus berformat YYYY-MM-DD", field))
		return
	}
	if t.Year() < 1900 {
		fe.add(field, fmt.Sprintf("%s tidak valid", field))
		return
	}
	if notFuture && t.After(time.Now()) {
		fe.add(field, fmt.Sprintf("%s tidak boleh di masa depan", field))
	}
}

func checkIntRange(fe fieldErrors, field string, v *int, min, max int) {
	if v != nil && (*v < min || *v > max) {
		fe.add(field, fmt.Sprintf("%s harus antara %d dan %d", field, min, max))
	}
}

func checkFloatRange(fe fieldErrors, field string, v *float64, min, max float64) {
	if v != nil && (*v < min || *v > max) {
		fe.add(field, fmt.Sprintf("%s harus antara %g dan %g", field, min, max))
	}
}

func checkPattern(fe fieldErrors, field string, v *string, re *regexp.Regexp, msg string) {
	*v = strings.TrimSpace(*v)
	if *v != "" && !re.MatchString(*v) {
		fe.add(field, msg)
	}
}

func checkPhone(fe fieldErrors, field string, v *string) {
	*v = normalizePhone(*v)
	if *v != "" && !rePhone.MatchString(*v) {
		fe.add(field, fmt.Sprintf("%s harus berisi 8-15 digit (boleh diawali +)", field))
	}
}

func checkEmail(fe fieldErrors, field string, v *string) {
	*v = strings.TrimSpace(*v)
	if *v == "" {
		return
	}
	a, err := mail.ParseAddress(*v)
	if err != nil || a.Address != *v || len(*v) > 255 {
		fe.add(field, fmt.Sprintf("%s harus berupa email yang valid", field))
	}
}

func trimAll(ps ...*string) {
	for _, p := range ps {
		*p = strings.TrimSpace(*p)
	}
}

// validateStudentFields normalises and validates the flat student record.
// It does not check uniqueness or referenced ids (that needs the DB).
func validateStudentFields(st *model.Students, fe fieldErrors) {
	trimAll(&st.FullName, &st.Nickname, &st.BirthPlace, &st.BirthCertificateNo, &st.Country, &st.Hobby, &st.Aspiration,
		&st.Street, &st.Hamlet, &st.Village, &st.District, &st.City, &st.Province,
		&st.KpsPkhNumber, &st.KipNumber, &st.KipName, &st.KksNumber, &st.PipReason, &st.BankName, &st.BankAccountNumber, &st.BankAccountName,
		&st.PreviousSchoolName, &st.PreviousSkhunNumber, &st.PreviousDiplomaNumber, &st.ExamParticipantNumber,
		&st.ExitReason, &st.ExitDestination, &st.GraduationCertificateNumber)

	// Required (create and update: the merged record must stay complete).
	if st.FullName == "" {
		fe.add("identity.full_name", "Nama lengkap wajib diisi")
	}
	if strings.TrimSpace(st.Gender) == "" {
		fe.add("identity.gender", "Jenis kelamin wajib diisi")
	}
	if st.BirthPlace == "" {
		fe.add("identity.birth_place", "Tempat lahir wajib diisi")
	}
	if strings.TrimSpace(st.BirthDate) == "" {
		fe.add("identity.birth_date", "Tanggal lahir wajib diisi")
	}
	if strings.TrimSpace(st.Religion) == "" {
		fe.add("identity.religion", "Agama wajib diisi")
	}

	checkLen(fe, "identity.full_name", st.FullName, 255)
	checkLen(fe, "identity.nickname", st.Nickname, 100)
	checkLen(fe, "identity.birth_place", st.BirthPlace, 100)
	checkLen(fe, "identity.birth_certificate_no", st.BirthCertificateNo, 100)
	checkLen(fe, "identity.country", st.Country, 100)
	checkLen(fe, "identity.hobby", st.Hobby, 100)
	checkLen(fe, "identity.aspiration", st.Aspiration, 100)

	st.Gender = strings.ToUpper(strings.TrimSpace(st.Gender))
	normEnum(fe, "identity.gender", dto.RefGenders, &st.Gender)
	checkPattern(fe, "identity.nisn", &st.NISN, reDigits10, "NISN harus 10 digit angka")
	checkPattern(fe, "identity.nis", &st.NIS, reNIS, "NIS maksimal 30 karakter (huruf, angka, . / -)")
	checkPattern(fe, "identity.nik", &st.NIK, reDigits16, "NIK harus 16 digit angka")
	checkPattern(fe, "identity.no_kk", &st.NoKK, reDigits16, "No KK harus 16 digit angka")
	checkDate(fe, "identity.birth_date", &st.BirthDate, true)
	normEnum(fe, "identity.religion", dto.RefReligions, &st.Religion)
	normEnum(fe, "identity.citizenship", dto.RefCitizenships, &st.Citizenship)
	if st.Citizenship == "" {
		st.Citizenship = "WNI"
	}
	if st.Country == "" && st.Citizenship == "WNI" {
		st.Country = "Indonesia"
	}
	normEnum(fe, "identity.special_needs", dto.RefSpecialNeeds, &st.SpecialNeeds)
	if st.SpecialNeeds == "" {
		st.SpecialNeeds = "none"
	}
	normEnum(fe, "identity.blood_type", dto.RefBloodTypes, &st.BloodType)
	checkIntRange(fe, "identity.child_order", st.ChildOrder, 1, 30)
	checkIntRange(fe, "identity.siblings_count", st.SiblingsCount, 0, 30)

	checkPhone(fe, "contact.phone", &st.Phone)
	checkPhone(fe, "contact.mobile_phone", &st.MobilePhone)
	checkEmail(fe, "contact.email", &st.Email)

	checkLen(fe, "address.street", st.Street, 255)
	checkPattern(fe, "address.rt", &st.RT, reShortNum, "RT harus berupa angka (maks 5 digit)")
	checkPattern(fe, "address.rw", &st.RW, reShortNum, "RW harus berupa angka (maks 5 digit)")
	for f, v := range map[string]string{"address.hamlet": st.Hamlet, "address.village": st.Village, "address.district": st.District,
		"address.city": st.City, "address.province": st.Province} {
		checkLen(fe, f, v, 100)
	}
	checkPattern(fe, "address.postal_code", &st.PostalCode, rePostalCode, "Kode pos harus 5 digit angka")
	checkFloatRange(fe, "address.latitude", st.Latitude, -90, 90)
	checkFloatRange(fe, "address.longitude", st.Longitude, -180, 180)
	normEnum(fe, "address.residence_type", dto.RefResidenceTypes, &st.ResidenceType)
	normEnum(fe, "address.transportation", dto.RefTransportations, &st.Transportation)
	checkFloatRange(fe, "address.distance_km", st.DistanceKm, 0, 1000)
	checkIntRange(fe, "address.travel_time_minutes", st.TravelTimeMinutes, 0, 1440)

	checkIntRange(fe, "physical.height_cm", st.HeightCm, 30, 250)
	checkIntRange(fe, "physical.weight_kg", st.WeightKg, 2, 300)
	checkIntRange(fe, "physical.head_circumference_cm", st.HeadCircumferenceCm, 20, 80)

	for f, v := range map[string]string{"welfare.kps_pkh_number": st.KpsPkhNumber, "welfare.kip_number": st.KipNumber,
		"welfare.kks_number": st.KksNumber, "welfare.bank_name": st.BankName, "welfare.bank_account_number": st.BankAccountNumber} {
		checkLen(fe, f, v, 100)
	}
	for f, v := range map[string]string{"welfare.kip_name": st.KipName, "welfare.pip_reason": st.PipReason, "welfare.bank_account_name": st.BankAccountName} {
		checkLen(fe, f, v, 255)
	}

	normEnum(fe, "registration.registration_type", dto.RefRegistrationTypes, &st.RegistrationType)
	if st.RegistrationType == "" {
		st.RegistrationType = "siswa_baru"
	}
	checkDate(fe, "registration.entry_date", &st.EntryDate, false)
	checkLen(fe, "registration.previous_school_name", st.PreviousSchoolName, 255)
	checkPattern(fe, "registration.previous_school_npsn", &st.PreviousSchoolNPSN, reNPSN, "NPSN harus 8 karakter huruf/angka")
	for f, v := range map[string]string{"registration.previous_skhun_number": st.PreviousSkhunNumber,
		"registration.previous_diploma_number": st.PreviousDiplomaNumber, "registration.exam_participant_number": st.ExamParticipantNumber} {
		checkLen(fe, f, v, 100)
	}

	checkDate(fe, "exit.exit_date", &st.ExitDate, false)
	checkLen(fe, "exit.exit_reason", st.ExitReason, 255)
	checkLen(fe, "exit.exit_destination", st.ExitDestination, 255)
	checkLen(fe, "exit.graduation_certificate_number", st.GraduationCertificateNumber, 100)

	st.Status = strings.TrimSpace(st.Status)
	if st.Status == "" {
		st.Status = dto.StudentStatusActive
	} else if !dto.ValidEnum(dto.RefStudentStatuses, st.Status) {
		fe.add("status", "Status siswa tidak valid")
	}
}

func checkYear(fe fieldErrors, field string, v *int, maxAhead int) {
	checkIntRange(fe, field, v, 1900, time.Now().Year()+maxAhead)
}

// normalizeGuardians validates guardians and assigns ids to new items.
func normalizeGuardians(items []dto.GuardianItem, fe fieldErrors) []dto.GuardianItem {
	if len(items) > maxGuardians {
		fe.add("guardians", fmt.Sprintf("Maksimal %d data orang tua/wali", maxGuardians))
		return items
	}
	seen := map[string]bool{}
	for i := range items {
		g := &items[i]
		p := fmt.Sprintf("guardians[%d].", i)
		trimAll(&g.Name, &g.Address, &g.Relationship)
		g.Type = strings.ToLower(strings.TrimSpace(g.Type))
		if !dto.ValidEnum(dto.RefGuardianTypes, g.Type) {
			fe.add(p+"type", "Jenis orang tua/wali harus father, mother, atau guardian")
		} else if g.Type != dto.GuardianGuardian {
			if seen[g.Type] {
				fe.add(p+"type", "Data ayah/ibu hanya boleh satu")
			}
			seen[g.Type] = true
		}
		if g.Name == "" {
			fe.add(p+"name", "Nama orang tua/wali wajib diisi")
		}
		checkLen(fe, p+"name", g.Name, 255)
		checkPattern(fe, p+"nik", &g.NIK, reDigits16, "NIK orang tua/wali harus 16 digit angka")
		checkYear(fe, p+"birth_year", g.BirthYear, 0)
		normEnum(fe, p+"education", dto.RefEducationLevels, &g.Education)
		normEnum(fe, p+"occupation", dto.RefOccupations, &g.Occupation)
		normEnum(fe, p+"income", dto.RefIncomes, &g.Income)
		normEnum(fe, p+"special_needs", dto.RefSpecialNeeds, &g.SpecialNeeds)
		checkPhone(fe, p+"phone", &g.Phone)
		checkEmail(fe, p+"email", &g.Email)
		checkLen(fe, p+"address", g.Address, 500)
		checkLen(fe, p+"relationship", g.Relationship, 100)
		g.ID = ensureItemID(g.ID)
	}
	return items
}

func normalizeAchievements(items []dto.AchievementItem, fe fieldErrors) []dto.AchievementItem {
	if len(items) > maxAchievements {
		fe.add("achievements", fmt.Sprintf("Maksimal %d prestasi", maxAchievements))
		return items
	}
	for i := range items {
		a := &items[i]
		p := fmt.Sprintf("achievements[%d].", i)
		trimAll(&a.Name, &a.Rank, &a.Organizer)
		if a.Name == "" {
			fe.add(p+"name", "Nama prestasi wajib diisi")
		}
		checkLen(fe, p+"name", a.Name, 255)
		normEnum(fe, p+"type", dto.RefAchievementTypes, &a.Type)
		normEnum(fe, p+"level", dto.RefAchievementLevels, &a.Level)
		checkLen(fe, p+"rank", a.Rank, 100)
		checkYear(fe, p+"year", a.Year, 1)
		checkLen(fe, p+"organizer", a.Organizer, 255)
		a.ID = ensureItemID(a.ID)
	}
	return items
}

func normalizeScholarships(items []dto.ScholarshipItem, fe fieldErrors) []dto.ScholarshipItem {
	if len(items) > maxScholarships {
		fe.add("scholarships", fmt.Sprintf("Maksimal %d beasiswa", maxScholarships))
		return items
	}
	for i := range items {
		s := &items[i]
		p := fmt.Sprintf("scholarships[%d].", i)
		trimAll(&s.Name, &s.Provider)
		if s.Name == "" {
			fe.add(p+"name", "Nama beasiswa wajib diisi")
		}
		checkLen(fe, p+"name", s.Name, 255)
		normEnum(fe, p+"type", dto.RefScholarshipTypes, &s.Type)
		checkLen(fe, p+"provider", s.Provider, 255)
		checkYear(fe, p+"start_year", s.StartYear, 10)
		checkYear(fe, p+"end_year", s.EndYear, 10)
		if s.StartYear != nil && s.EndYear != nil && *s.EndYear < *s.StartYear {
			fe.add(p+"end_year", "Tahun selesai tidak boleh sebelum tahun mulai")
		}
		if s.Amount != nil && *s.Amount < 0 {
			fe.add(p+"amount", "Nominal beasiswa tidak boleh negatif")
		}
		s.ID = ensureItemID(s.ID)
	}
	return items
}

// ensureItemID keeps a valid uuid id or generates a new one.
func ensureItemID(id string) string {
	if u, err := uuid.Parse(strings.TrimSpace(id)); err == nil && u != uuid.Nil {
		return u.String()
	}
	return uuid.NewString()
}

// hasJSONItems reports whether a stored JSON array column has at least one item.
func hasJSONItems(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && s != "[]" && s != "null"
}
