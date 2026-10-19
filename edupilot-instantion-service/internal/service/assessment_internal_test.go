package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPrincipalLookup(t *testing.T) {
	cases := []struct{ role, cr, want string }{
		{"customer", "students", PrincipalStudent},
		{"customer", "", PrincipalManager},
		{"customer", "guru", PrincipalManager},
		{"institution", "", PrincipalManager},
		{"institution", "students", PrincipalManager},
		{"teacher", "", PrincipalTeacher},
		{"admin", "", PrincipalNone},
		{"super_admin", "", PrincipalNone},
		{"", "", PrincipalNone},
	}
	for _, tc := range cases {
		if got := PrincipalLookup(tc.role, tc.cr); got != tc.want {
			t.Errorf("PrincipalLookup(%q,%q) = %q want %q", tc.role, tc.cr, got, tc.want)
		}
	}
}

func TestNotifyRecipients(t *testing.T) {
	got := NotifyRecipients([]uint{5, 0, 3, 5}, []uint{3, 9})
	if !reflect.DeepEqual(got, []uint{3, 5, 9}) {
		t.Errorf("got %v", got)
	}
	if got := NotifyRecipients(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestAssessmentNotifyValidate(t *testing.T) {
	ok := AssessmentNotifyRequest{SchoolID: uuid.New(), Type: " assessment_welfare_alert ", Title: "Perlu tindak lanjut", Body: "Ada siswa yang perlu didampingi."}
	if err := ok.Validate(); err != nil || ok.Type != "assessment_welfare_alert" {
		t.Fatalf("valid request rejected: %v", err)
	}
	bad := []AssessmentNotifyRequest{
		{Type: "x", Title: "t", Body: "b"},
		{SchoolID: uuid.New(), Title: "t", Body: "b"},
		{SchoolID: uuid.New(), Type: strings.Repeat("x", 51), Title: "t", Body: "b"},
		{SchoolID: uuid.New(), Type: "x", Body: "b"},
		{SchoolID: uuid.New(), Type: "x", Title: strings.Repeat("t", 256), Body: "b"},
		{SchoolID: uuid.New(), Type: "x", Title: "t", Body: "  "},
	}
	for i, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestNonNilUUIDs(t *testing.T) {
	a := uuid.New()
	if got := nonNilUUIDs([]uuid.UUID{uuid.Nil, a}); len(got) != 1 || got[0] != a {
		t.Errorf("got %v", got)
	}
}
