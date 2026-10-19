package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

func TestTimesOverlap(t *testing.T) {
	cases := []struct {
		a1, a2, b1, b2 string
		want           bool
	}{
		{"07:00", "07:40", "07:40", "08:20", false}, // touching
		{"07:00", "07:40", "07:30", "08:00", true},
		{"07:00", "09:00", "07:30", "08:00", true}, // containment
		{"07:30", "08:00", "07:00", "09:00", true},
		{"07:00", "07:40", "07:00", "07:40", true},
		{"10:00", "10:40", "07:00", "07:40", false},
	}
	for _, c := range cases {
		if got := timesOverlap(c.a1, c.a2, c.b1, c.b2); got != c.want {
			t.Errorf("%s-%s vs %s-%s: got %v", c.a1, c.a2, c.b1, c.b2, got)
		}
	}
}

func TestClassifyConflicts(t *testing.T) {
	teacher, otherTeacher := uuid.New(), uuid.New()
	class, otherClass := uuid.New(), uuid.New()
	slot := scheduleSlot{EmployeeID: teacher, ClassID: class, Room: "Lab 1", DayOfWeek: 1, StartTime: "07:00", EndTime: "07:40"}
	rows := []model.TeacherSchedules{
		{ID: uuid.New(), EmployeeID: teacher, ClassID: otherClass, DayOfWeek: 1, StartTime: "07:20", EndTime: "08:00"},                       // teacher
		{ID: uuid.New(), EmployeeID: otherTeacher, ClassID: class, DayOfWeek: 1, StartTime: "06:50", EndTime: "07:10"},                       // class
		{ID: uuid.New(), EmployeeID: otherTeacher, ClassID: otherClass, DayOfWeek: 1, StartTime: "07:00", EndTime: "07:40", Room: " lab 1 "}, // room
		{ID: uuid.New(), EmployeeID: teacher, ClassID: class, DayOfWeek: 1, StartTime: "07:40", EndTime: "08:20"},                            // touching: none
		{ID: uuid.New(), EmployeeID: teacher, ClassID: class, DayOfWeek: 2, StartTime: "07:00", EndTime: "07:40"},                            // other day
		{ID: uuid.New(), EmployeeID: teacher, ClassID: class, DayOfWeek: 1, StartTime: "07:00", EndTime: "07:40", Status: "inactive"},
	}
	got := classifyConflicts(slot, rows)
	types := []string{}
	for _, c := range got {
		types = append(types, c.Type)
		if c.Description == "" {
			t.Errorf("empty description for %s", c.Type)
		}
	}
	if strings.Join(types, ",") != "teacher,class,room" {
		t.Fatalf("got types %v", types)
	}

	// Same teacher and same class in one row → both conflicts reported.
	both := classifyConflicts(slot, []model.TeacherSchedules{{ID: uuid.New(), EmployeeID: teacher, ClassID: class, DayOfWeek: 1, StartTime: "07:00", EndTime: "07:40"}})
	if len(both) != 2 {
		t.Fatalf("want teacher+class, got %+v", both)
	}
	// No room on the candidate → room never matches.
	slot.Room = ""
	if n := len(classifyConflicts(slot, rows[2:3])); n != 0 {
		t.Fatalf("room conflict without room: %d", n)
	}
}

func TestNormalizePeriod(t *testing.T) {
	ok := dto.SchedulePeriodRequest{Name: " Jam ke-1 ", PeriodOrder: 0, StartTime: "07:00", EndTime: "07:40", DayOfWeek: 0}
	if err := normalizePeriod(&ok); err != nil || ok.Name != "Jam ke-1" {
		t.Fatalf("valid period rejected: %v", err)
	}
	bad := []dto.SchedulePeriodRequest{
		{Name: "", StartTime: "07:00", EndTime: "07:40"},
		{Name: "x", StartTime: "7:00", EndTime: "07:40"},
		{Name: "x", StartTime: "07:40", EndTime: "07:40"},
		{Name: "x", StartTime: "08:00", EndTime: "07:40"},
		{Name: "x", StartTime: "07:00", EndTime: "07:40", DayOfWeek: 8},
		{Name: "x", StartTime: "07:00", EndTime: "07:40", PeriodOrder: -1},
		{Name: "x", StartTime: "24:00", EndTime: "24:30"},
	}
	for i, b := range bad {
		if err := normalizePeriod(&b); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestCheckPeriodClash(t *testing.T) {
	id1 := uuid.New()
	existing := []model.SchedulePeriod{
		{ID: id1, Name: "Jam ke-1", PeriodOrder: 1, StartTime: "07:00", EndTime: "07:40"},
		{ID: uuid.New(), Name: "Jam ke-2", PeriodOrder: 2, StartTime: "07:40", EndTime: "08:20"},
	}
	if err := checkPeriodClash(existing, model.SchedulePeriod{PeriodOrder: 3, StartTime: "08:20", EndTime: "09:00"}); err != nil {
		t.Fatalf("adjacent period rejected: %v", err)
	}
	if err := checkPeriodClash(existing, model.SchedulePeriod{PeriodOrder: 3, StartTime: "08:00", EndTime: "08:40"}); err == nil || !strings.Contains(err.Error(), "PERIOD_OVERLAP") {
		t.Fatalf("overlap not detected: %v", err)
	}
	if err := checkPeriodClash(existing, model.SchedulePeriod{PeriodOrder: 2, StartTime: "09:00", EndTime: "09:40"}); err == nil || !strings.Contains(err.Error(), "DUPLICATE_ORDER") {
		t.Fatalf("duplicate order not detected: %v", err)
	}
	// Updating itself is fine.
	if err := checkPeriodClash(existing, model.SchedulePeriod{ID: id1, PeriodOrder: 1, StartTime: "06:50", EndTime: "07:40"}); err != nil {
		t.Fatalf("self clash: %v", err)
	}
	// Friday-specific period may overlap the every-day ones.
	if err := checkPeriodClash(existing, model.SchedulePeriod{DayOfWeek: 5, PeriodOrder: 1, StartTime: "07:00", EndTime: "07:35"}); err != nil {
		t.Fatalf("day-specific period rejected: %v", err)
	}
}

func TestBuildPeriodPreset(t *testing.T) {
	want := map[string]struct{ jp, teach int }{
		"35_MIN": {35, 8}, "SD": {35, 8}, "MI": {35, 8},
		"40_MIN": {40, 9}, "smp": {40, 9}, "MTs": {40, 9},
		"45_MIN": {45, 10}, "SMA": {45, 10}, "SMK": {45, 10}, "standard_45": {45, 10},
	}
	for preset, w := range want {
		slots, ok := buildPeriodPreset(preset)
		if !ok {
			t.Fatalf("%s: not recognised", preset)
		}
		teach := 0
		for i, s := range slots {
			if !dto.ValidHHMM(s.Start) || !dto.ValidHHMM(s.End) || s.End <= s.Start {
				t.Fatalf("%s slot %d invalid times %s-%s", preset, i, s.Start, s.End)
			}
			if i > 0 && slots[i-1].End != s.Start {
				t.Fatalf("%s: gap/overlap between %d and %d", preset, i-1, i)
			}
			if s.Order != i+1 {
				t.Fatalf("%s: order %d at %d", preset, s.Order, i)
			}
			if !s.IsBreak {
				teach++
				if addMinutes(s.Start, w.jp) != s.End {
					t.Fatalf("%s: JP %s-%s is not %d min", preset, s.Start, s.End, w.jp)
				}
				if s.Name != "Jam ke-"+strconv.Itoa(teach) {
					t.Fatalf("%s: name %q", preset, s.Name)
				}
			}
		}
		if teach != w.teach {
			t.Fatalf("%s: %d teaching periods, want %d", preset, teach, w.teach)
		}
		if !slots[0].IsBreak {
			t.Fatalf("%s: first slot should be pembiasaan", preset)
		}
		// Preset periods must pass the same clash check as manual ones.
		var existing []model.SchedulePeriod
		for _, s := range slots {
			p := model.SchedulePeriod{ID: uuid.New(), Name: s.Name, PeriodOrder: s.Order, StartTime: s.Start, EndTime: s.End}
			if err := checkPeriodClash(existing, p); err != nil {
				t.Fatalf("%s: %v", preset, err)
			}
			existing = append(existing, p)
		}
	}
	if _, ok := buildPeriodPreset("60_MIN"); ok {
		t.Fatal("unknown preset accepted")
	}
}

func TestCountJP(t *testing.T) {
	slots, _ := buildPeriodPreset("40_MIN")
	var periods []model.SchedulePeriod
	for _, s := range slots {
		periods = append(periods, model.SchedulePeriod{Name: s.Name, StartTime: s.Start, EndTime: s.End, IsBreak: s.IsBreak, IsActive: true})
	}
	// Jam 1 = 07:15-07:55, Jam 2 = 07:55-08:35.
	if n := countJP(periods, 1, "07:15", "07:55"); n != 1 {
		t.Fatalf("single JP = %d", n)
	}
	if n := countJP(periods, 1, "07:15", "08:35"); n != 2 {
		t.Fatalf("double JP = %d", n)
	}
	// A span across a break counts only teaching periods.
	if n := countJP(periods, 1, "07:15", "10:50"); n != 5 {
		t.Fatalf("span across break = %d", n)
	}
	// Ad-hoc time (no period) counts as 1.
	if n := countJP(periods, 1, "16:00", "16:30"); n != 1 {
		t.Fatalf("ad hoc = %d", n)
	}
	// Day-specific periods replace the every-day ones on that day.
	periods = append(periods,
		model.SchedulePeriod{DayOfWeek: 5, StartTime: "07:00", EndTime: "07:30", IsActive: true},
		model.SchedulePeriod{DayOfWeek: 5, StartTime: "07:30", EndTime: "08:00", IsActive: true})
	if n := countJP(periods, 5, "07:00", "08:00"); n != 2 {
		t.Fatalf("friday = %d", n)
	}
	if got := len(teachingPeriodsForDay(periods, 1)); got != 9 {
		t.Fatalf("monday teaching periods = %d", got)
	}
}

func TestHoursStatus(t *testing.T) {
	if hoursStatus(2, 2) != "ok" || hoursStatus(1, 2) != "under" || hoursStatus(3, 2) != "over" || hoursStatus(2, 2.5) != "under" {
		t.Fatal("hoursStatus wrong")
	}
}
