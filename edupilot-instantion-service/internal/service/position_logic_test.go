package service

import (
	"strings"
	"testing"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

func TestGeneratePositionCode(t *testing.T) {
	cases := []struct {
		name  string
		taken map[string]bool
		want  string
	}{
		{"Kepala Tata Usaha", nil, "KTU"},
		{"Bendahara", nil, "BEND"},
		{"  wali   kelas ", nil, "WK"},
		{"Guru Bimbingan Konseling (BK)", nil, "GBKB"},
		{"Kepala Tata Usaha", map[string]bool{"KTU": true}, "KTU-2"},
		{"Kepala Tata Usaha", map[string]bool{"KTU": true, "KTU-2": true}, "KTU-3"},
		{"---", nil, "JAB"},
		{"Ab", nil, "AB"},
	}
	for _, c := range cases {
		if got := GeneratePositionCode(c.name, c.taken); got != c.want {
			t.Errorf("GeneratePositionCode(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDefaultPositionsSeedList(t *testing.T) {
	if len(defaultPositions) != 20 {
		t.Fatalf("expected 20 default positions, got %d", len(defaultPositions))
	}
	names, codes := map[string]bool{}, map[string]bool{}
	perCat := map[string]int{}
	for _, p := range defaultPositions {
		if p.Name == "" || p.Code == "" || p.Category == "" {
			t.Fatalf("incomplete seed %+v", p)
		}
		if names[strings.ToLower(p.Name)] || codes[p.Code] {
			t.Fatalf("duplicate seed %+v", p)
		}
		names[strings.ToLower(p.Name)], codes[p.Code] = true, true
		if canonicalCategory(p.Category) != p.Category || categoryRank(p.Category) >= 100 {
			t.Fatalf("seed category %q is not a standard category", p.Category)
		}
		perCat[p.Category]++
	}
	want := map[string]int{"Pimpinan": 5, "Pendidik": 4, "Tenaga Kependidikan": 6, "Koordinator": 5}
	for cat, n := range want {
		if perCat[cat] != n {
			t.Errorf("category %s: %d seeds, want %d", cat, perCat[cat], n)
		}
	}
	for _, name := range []string{"Kepala Sekolah", "Wali Kelas", "Guru Bimbingan Konseling (BK)", "Operator Sekolah", "Koordinator Kurikulum"} {
		if !names[strings.ToLower(name)] {
			t.Errorf("missing seed %q", name)
		}
	}
}

func TestSortPositionsAndNormalize(t *testing.T) {
	list := []dto.EmployeePositionResponse{
		{Position: "Zeta", Category: "Lainnya"},
		{Position: "Laboran", Category: "Tenaga Kependidikan"},
		{Position: "Wali Kelas", Category: "Pendidik"},
		{Position: "Custom", Category: "Komite"},
		{Position: "Kepala Sekolah", Category: "Pimpinan"},
		{Position: "Guru Kelas", Category: "Pendidik"},
	}
	sortPositions(list)
	got := make([]string, 0, len(list))
	for _, p := range list {
		got = append(got, p.Position)
	}
	want := "Kepala Sekolah,Guru Kelas,Wali Kelas,Laboran,Custom,Zeta"
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}

	req := dto.EmployeePositionRequest{Position: "  Kepala   Lab ", Category: "pimpinan", PositionCode: " kl "}
	if err := normalizePositionRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.Position != "Kepala Lab" || req.Category != "Pimpinan" || req.PositionCode != "KL" {
		t.Fatalf("normalized = %+v", req)
	}
	if err := normalizePositionRequest(&dto.EmployeePositionRequest{Position: " ", Category: "x"}); err == nil {
		t.Fatal("blank name must fail")
	}
	if r := positionResponse(model.EmployeePosition{Position: "Lama"}, 2); r.Category != PositionCategoryOther || r.AssignmentCount != 2 {
		t.Fatalf("legacy row response = %+v", r)
	}
}

func TestMatchStoredSchedule(t *testing.T) {
	p1, p2 := uuid.New(), uuid.New()
	rows := []model.TeacherSchedules{{SchedulePeriodID: &p1}, {}, {SchedulePeriodID: &p2}}
	if got := matchStoredSchedule(nil, dto.OptionalUUID{}); got != -1 {
		t.Fatalf("empty pool = %d", got)
	}
	if got := matchStoredSchedule(rows, dto.OptionalUUID{}); got != 0 {
		t.Fatalf("no period = %d", got)
	}
	if got := matchStoredSchedule(rows, dto.OptionalUUID{UUID: p2, Valid: true}); got != 2 {
		t.Fatalf("same period = %d", got)
	}
	if got := matchStoredSchedule(rows, dto.OptionalUUID{UUID: uuid.New(), Valid: true}); got != 1 {
		t.Fatalf("fallback = %d", got)
	}
	if got := matchStoredSchedule(rows[:1], dto.OptionalUUID{UUID: uuid.New(), Valid: true}); got != -1 {
		t.Fatalf("other period = %d", got)
	}
}
