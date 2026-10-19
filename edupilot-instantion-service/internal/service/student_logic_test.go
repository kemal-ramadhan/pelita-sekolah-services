package service

import (
	"errors"
	"testing"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

func TestSuggestPromotedClassName(t *testing.T) {
	g := func(n int, short string) dto.GradeLevelShort { return dto.GradeLevelShort{Number: n, ShortName: short} }
	cases := []struct {
		name      string
		cur, next dto.GradeLevelShort
		want      string
	}{
		{"7A", g(7, "7"), g(8, "8"), "8A"},
		{"7 B", g(7, "7"), g(8, "8"), "8 B"},
		{"VII-A", g(7, "VII"), g(8, "VIII"), "VIII-A"},
		{"VIIA", g(7, "VII"), g(8, "VIII"), "VIIIA"},
		{"X TKJ 1", g(10, "X"), g(11, "XI"), "XI TKJ 1"},
		{"XI IPA 2", g(11, "XI"), g(12, "XII"), "XII IPA 2"},
		{"9C", g(9, "9"), g(10, "10"), "10C"},
		{"1", g(1, "1"), g(2, "2"), "2"},
		{"A1", g(1, "A"), g(2, "B"), "B1"},
		{"10A", g(1, "1"), g(2, "2"), ""}, // "10" is not grade 1
		{"Ibnu Sina", g(7, "7"), g(8, "8"), ""},
		{"XII TKJ", g(10, "X"), g(11, "XI"), ""},
	}
	for _, c := range cases {
		if got := SuggestPromotedClassName(c.name, c.cur, c.next); got != c.want {
			t.Errorf("SuggestPromotedClassName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPickTargetClass(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	targets := []targetClass{
		{ID: a, Name: "7A", Alias: "Ibnu Sina", Grade: 7}, // same grade, same alias: must not be picked
		{ID: b, Name: "8A", Alias: "Al-Farabi", Grade: 8},
		{ID: c, Name: "8B", Alias: "Ibnu Sina", Grade: 8},
		{ID: d, Name: "9A", Grade: 9},
	}
	if got := pickTargetClass(targets, 8, "8A", "Ibnu Sina"); got == nil || got.ID != b {
		t.Fatalf("name match expected 8A, got %+v", got)
	}
	if got := pickTargetClass(targets, 8, "", "Ibnu Sina"); got == nil || got.ID != c {
		t.Fatalf("alias match expected 8B, got %+v", got)
	}
	if got := pickTargetClass(targets, 8, "8Z", ""); got != nil {
		t.Fatalf("ambiguous grade must not suggest, got %+v", got)
	}
	if got := pickTargetClass(targets, 9, "", ""); got == nil || got.ID != d {
		t.Fatalf("single class in grade expected 9A, got %+v", got)
	}
}

func TestNextGradeLevel(t *testing.T) {
	levels := []model.GradeLevel{{Number: 9}, {Number: 7}, {Number: 8}}
	if n := nextGradeLevel(levels, 7); n == nil || n.Number != 8 {
		t.Fatalf("want 8, got %+v", n)
	}
	if n := nextGradeLevel(levels, 9); n != nil {
		t.Fatalf("want nil after final, got %+v", n)
	}
}

func TestGradePresetLevels(t *testing.T) {
	lv, err := GradePresetLevels("SMP", "roman")
	if err != nil || len(lv) != 3 || lv[0].ShortName != "VII" || lv[0].Name != "Kelas VII" || !lv[2].IsFinal || lv[1].IsFinal || lv[0].Number != 7 {
		t.Fatalf("SMP roman: %+v %v", lv, err)
	}
	lv, err = GradePresetLevels("MTs", "")
	if err != nil || len(lv) != 3 || lv[0].Stage != "MTs" || lv[0].ShortName != "7" {
		t.Fatalf("MTs (case kept, arabic default): %+v %v", lv, err)
	}
	if lv, err = GradePresetLevels("mts", "arabic"); err != nil || lv[0].Stage != "MTs" {
		t.Fatalf("mts lower case: %+v %v", lv, err)
	}
	lv, err = GradePresetLevels("SMK_4Y", "roman")
	if err != nil || len(lv) != 4 || lv[3].ShortName != "XIII" || !lv[3].IsFinal || lv[3].Number != 13 {
		t.Fatalf("SMK_4Y: %+v %v", lv, err)
	}
	lv, err = GradePresetLevels("TK", "roman")
	if err != nil || len(lv) != 2 || lv[0].ShortName != "A" || !lv[1].IsFinal {
		t.Fatalf("TK: %+v %v", lv, err)
	}
	lv, err = GradePresetLevels("SD", "arabic")
	if err != nil || len(lv) != 6 || !lv[5].IsFinal || lv[5].Number != 6 {
		t.Fatalf("SD: %+v %v", lv, err)
	}
	if _, err := GradePresetLevels("SMA_LOCAL", "roman"); err != nil {
		t.Fatal(err)
	}
	if _, err := GradePresetLevels("XYZ", "arabic"); err == nil {
		t.Fatal("unknown preset must fail")
	}
	if _, err := GradePresetLevels("SD", "greek"); err == nil {
		t.Fatal("unknown numbering must fail")
	}
}

func TestToRoman(t *testing.T) {
	for n, want := range map[int]string{1: "I", 4: "IV", 7: "VII", 8: "VIII", 9: "IX", 10: "X", 12: "XII", 13: "XIII"} {
		if got := toRoman(n); got != want {
			t.Errorf("toRoman(%d) = %q want %q", n, got, want)
		}
	}
}

func validStudent() model.Students {
	return model.Students{FullName: " Ani ", Gender: "p", BirthPlace: "Depok", BirthDate: "2012-01-02", Religion: "Islam"}
}

func detailsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "VALIDATION_ERROR" {
		t.Fatalf("want VALIDATION_ERROR, got %v", err)
	}
	return ae.Details
}

func TestValidateStudentFields(t *testing.T) {
	st := validStudent()
	fe := fieldErrors{}
	validateStudentFields(&st, fe)
	if len(fe) != 0 {
		t.Fatalf("valid student rejected: %v", fe)
	}
	if st.FullName != "Ani" || st.Gender != "P" || st.Religion != "islam" || st.Citizenship != "WNI" ||
		st.Country != "Indonesia" || st.SpecialNeeds != "none" || st.Status != "active" || st.RegistrationType != "siswa_baru" {
		t.Fatalf("normalisation failed: %+v", st)
	}

	bad := validStudent()
	bad.NISN, bad.NIK, bad.NoKK = "12345", "123", "abcdefghijklmnop"
	bad.Religion = "jedi"
	bad.BirthDate = "2999-01-01"
	bad.Email = "not-an-email"
	bad.Phone = "12ab"
	h := 999
	bad.HeightCm = &h
	bad.Transportation = "teleport"
	bad.Status = "zombie"
	fe = fieldErrors{}
	validateStudentFields(&bad, fe)
	d := detailsOf(t, fe.err())
	for _, k := range []string{"identity.nisn", "identity.nik", "identity.no_kk", "identity.religion", "identity.birth_date",
		"contact.email", "contact.phone", "physical.height_cm", "address.transportation", "status"} {
		if d[k] == "" {
			t.Errorf("expected error for %s, got %v", k, d)
		}
	}

	missing := model.Students{}
	fe = fieldErrors{}
	validateStudentFields(&missing, fe)
	d = detailsOf(t, fe.err())
	for _, k := range []string{"identity.full_name", "identity.gender", "identity.birth_place", "identity.birth_date", "identity.religion"} {
		if d[k] == "" {
			t.Errorf("expected required error for %s", k)
		}
	}

	// Labels are accepted and canonicalised.
	lab := validStudent()
	lab.Transportation = "Sepeda motor"
	lab.SpecialNeeds = "Autis"
	lab.Phone = "0812-3456-7890"
	lab.NISN = "0081234567"
	fe = fieldErrors{}
	validateStudentFields(&lab, fe)
	if len(fe) != 0 || lab.Transportation != "sepeda_motor" || lab.SpecialNeeds != "Q" || lab.Phone != "081234567890" {
		t.Fatalf("label match failed: %v %+v", fe, lab)
	}
}

func TestNormalizeGuardians(t *testing.T) {
	y := 1800
	fe := fieldErrors{}
	g := normalizeGuardians([]dto.GuardianItem{
		{Type: "father", Name: "Budi", NIK: "3201011111110001", Education: "S1", Income: "5m_20m"},
		{Type: "father", Name: "Dup"},
		{Type: "uncle", Name: "", NIK: "12", BirthYear: &y},
	}, fe)
	if g[0].ID == "" || g[0].Education != "s1" {
		t.Fatalf("id/enum normalisation: %+v", g[0])
	}
	for _, k := range []string{"guardians[1].type", "guardians[2].type", "guardians[2].name", "guardians[2].nik", "guardians[2].birth_year"} {
		if fe[k] == "" {
			t.Errorf("expected %s error, got %v", k, fe)
		}
	}
	keep := uuid.NewString()
	fe = fieldErrors{}
	g = normalizeGuardians([]dto.GuardianItem{{ID: keep, Type: "mother", Name: "Siti"}}, fe)
	if len(fe) != 0 || g[0].ID != keep {
		t.Fatalf("existing id must be kept: %+v %v", g, fe)
	}
}

func TestCalculateCompleteness(t *testing.T) {
	if c := CalculateCompleteness(&model.Students{GuardiansJSON: "null"}); c != 0 {
		t.Fatalf("empty student = %d", c)
	}
	full := model.Students{FullName: "a", Gender: "L", NISN: "1", NIS: "1", NIK: "1", NoKK: "1", BirthPlace: "x", BirthDate: "x",
		Religion: "x", Street: "x", Village: "x", District: "x", City: "x", Province: "x", GuardiansJSON: `[{"type":"mother"}]`}
	if c := CalculateCompleteness(&full); c != 100 {
		t.Fatalf("full student = %d", c)
	}
	full.GuardiansJSON = "[]"
	if c := CalculateCompleteness(&full); c >= 100 || c < 90 {
		t.Fatalf("no guardians = %d", c)
	}
}

func TestHistoryStatusFor(t *testing.T) {
	for in, want := range map[string]string{"graduated": "graduated", "transferred_out": "transferred_out",
		"dropped_out": "withdrawn", "deceased": "withdrawn", "inactive": "withdrawn"} {
		if got := historyStatusFor(in); got != want {
			t.Errorf("historyStatusFor(%s) = %s want %s", in, got, want)
		}
	}
}

func TestStudentTemplateHeadersMapToColumns(t *testing.T) {
	keys := map[string]bool{}
	for _, h := range StudentTemplateHeaders {
		k := NormalizeStudentHeader(h)
		if keys[k] {
			t.Fatalf("duplicate normalized header %q", k)
		}
		keys[k] = true
	}
	for _, c := range studentColumns {
		if !keys[c.Key] {
			t.Errorf("column %q not in template", c.Key)
		}
	}
	for _, w := range guardianWho {
		for _, h := range guardianFieldHeader {
			if h == "hubungan" && w.Suffix != "wali" {
				continue
			}
			if !keys[h+" "+w.Suffix] {
				t.Errorf("guardian column %q not in template", h+" "+w.Suffix)
			}
		}
	}
	if !keys["kelas"] || NormalizeStudentHeader("Jenis Kelamin* (L/P)") != "jenis kelamin" {
		t.Fatal("header normalization broken")
	}
}

func TestParseStudentRows(t *testing.T) {
	header := []string{"Nama Lengkap*", "Jenis Kelamin* (L/P)", "NISN", "NIS", "Tanggal Lahir* (YYYY-MM-DD)", "Agama*",
		"Alat Transportasi", "Jarak (km)", "Penerima KIP (Ya/Tidak)", "Nama Ibu", "Tahun Lahir Ibu", "Pendidikan Ibu", "Kelas", "Tinggi (cm)", "Kolom Asing"}
	rows := [][]string{
		header,
		{"Budi", "L", "81234567", "123", "15/05/2010", "Islam", "Sepeda motor", "2,5", "Ya", "Siti", "1980", "SMA sederajat", "7A", "150", "x"},
		nil,
		{"Ani", "P", "", "124", "40313", "kristen", "", "", "tidak", "", "", "", "", "abc"},
	}
	out, err := ParseStudentRows(rows)
	if err != nil || len(out) != 2 {
		t.Fatalf("parse: %v %d", err, len(out))
	}
	b := out[0]
	if b.Row != 2 || b.Student.NISN != "0081234567" || b.Student.NIS != "123" || b.Student.BirthDate != "2010-05-15" ||
		*b.Student.DistanceKm != 2.5 || !b.Student.KipReceiver || b.ClassName != "7A" || *b.Student.HeightCm != 150 {
		t.Fatalf("row 2 parsed wrong: %+v", b.Student)
	}
	if m := b.Guardians[dto.GuardianMother]; m == nil || m.Name != "Siti" || *m.BirthYear != 1980 {
		t.Fatalf("mother guardian: %+v", b.Guardians)
	}
	if !b.Provided["nisn"] || b.Provided["nik"] {
		t.Fatalf("provided columns wrong: %v", b.Provided)
	}
	a := out[1]
	if a.Row != 4 || a.Student.BirthDate != "2010-05-15" || a.Student.KipReceiver || len(a.Errors) != 1 || a.Errors[0].Column != "Tinggi" {
		t.Fatalf("row 4 parsed wrong: %+v errs=%v", a.Student, a.Errors)
	}
	// Validation after parsing canonicalises enum labels.
	st := b.Student
	fe := fieldErrors{}
	validateStudentFields(&st, fe)
	if len(fe) == 0 {
		// birth place is missing in this sheet
		t.Fatal("expected birth_place required error")
	}
	if st.Transportation != "sepeda_motor" || st.Religion != "islam" {
		t.Fatalf("enum canonicalisation: %+v", st)
	}
	if _, err := ParseStudentRows([][]string{{"Nama", "X"}}); err == nil {
		t.Fatal("missing Nama Lengkap header must fail")
	}
}

func TestGuardianErrorColumn(t *testing.T) {
	order := []string{"mother", "father"}
	if c := guardianErrorColumn("guardians[1].nik", order); c != "NIK Ayah" {
		t.Fatalf("got %q", c)
	}
	if c := pathColumn("identity.nisn"); c != "NISN" {
		t.Fatalf("got %q", c)
	}
	if c := pathColumn("identity.birth_date"); c != "Tanggal Lahir" {
		t.Fatalf("got %q", c)
	}
}
