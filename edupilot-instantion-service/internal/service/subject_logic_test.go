package service

import (
	"strings"
	"testing"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/google/uuid"
)

func TestSubjectPresets(t *testing.T) {
	for _, preset := range []string{"SD", "MI", "SMP", "MTS", "MTs", "SMA", "MA", "SMK", "mak"} {
		defs, ok := subjectPresetFor(preset)
		if !ok || len(defs) < 8 {
			t.Fatalf("%s: preset missing (%d)", preset, len(defs))
		}
		codes, names := map[string]bool{}, map[string]bool{}
		hasMulok := false
		for _, d := range defs {
			if d.Code == "" || d.Name == "" || d.Short == "" {
				t.Fatalf("%s: incomplete %+v", preset, d)
			}
			if !dto.ValidEnum(dto.RefSubjectTypes, d.Type) {
				t.Fatalf("%s: invalid type %q", preset, d.Type)
			}
			if codes[strings.ToLower(d.Code)] || names[strings.ToLower(d.Name)] {
				t.Fatalf("%s: duplicate %s / %s", preset, d.Code, d.Name)
			}
			codes[strings.ToLower(d.Code)], names[strings.ToLower(d.Name)] = true, true
			if d.Type == dto.SubjectTypeMuatanLokal {
				hasMulok = true
			}
		}
		if !hasMulok {
			t.Errorf("%s: no muatan lokal", preset)
		}
		madrasah := map[string]bool{"MI": true, "MTS": true, "MA": true}[strings.ToUpper(preset)]
		if madrasah != codes["qh"] || madrasah == codes["pai"] {
			t.Errorf("%s: madrasah=%v but QH=%v PAI=%v", preset, madrasah, codes["qh"], codes["pai"])
		}
		for _, must := range []string{"ppkn", "bin", "pjok"} {
			if !codes[must] {
				t.Errorf("%s: missing %s", preset, must)
			}
		}
	}
	sd, _ := subjectPresetFor("SD")
	smk, _ := subjectPresetFor("SMK")
	if !hasCode(sd, "IPAS") || hasCode(sd, "IPA") {
		t.Error("SD must use IPAS (Kurikulum Merdeka)")
	}
	if !hasCode(smk, "PKL") || !hasCode(smk, "PROD") || !hasCode(smk, "KK") {
		t.Error("SMK must contain PKL, Dasar-dasar PK and Konsentrasi Keahlian")
	}
	if _, ok := subjectPresetFor("TK"); ok {
		t.Error("TK preset should not exist")
	}
}

func hasCode(defs []subjectPreset, code string) bool {
	for _, d := range defs {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestNormalizeSubject(t *testing.T) {
	ok := dto.SubjectRequest{SubjectCode: " MAT ", Name: " Matematika ", SubjectType: "wajib_umum"}
	if err := normalizeSubject(&ok); err != nil || ok.SubjectCode != "MAT" || ok.Name != "Matematika" {
		t.Fatalf("valid subject rejected: %v %+v", err, ok)
	}
	for i, b := range []dto.SubjectRequest{
		{SubjectCode: "", Name: "x"},
		{SubjectCode: "x", Name: "  "},
		{SubjectCode: "x", Name: "y", SubjectType: "agama_islam"},
		{SubjectCode: strings.Repeat("x", 51), Name: "y"},
	} {
		if err := normalizeSubject(&b); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestUniqueIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got, err := uniqueIDs([]uuid.UUID{a, b, a, uuid.Nil}, "x")
	if err != nil || len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := uniqueIDs(make([]uuid.UUID, maxSubjectAssignIDs+1), "x"); err == nil {
		t.Fatal("limit not enforced")
	}
	if h, err := normalizeHours(0); err != nil || h != 2 {
		t.Fatal("default JP should be 2")
	}
	if _, err := normalizeHours(-1); err == nil {
		t.Fatal("negative JP accepted")
	}
	if _, err := normalizeHours(maxHoursPerWeek + 1); err == nil {
		t.Fatal("JP above max accepted")
	}
}
