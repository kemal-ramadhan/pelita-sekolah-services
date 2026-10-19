package dto

import (
	"encoding/json"
	"testing"
)

func TestValidHHMM(t *testing.T) {
	for _, s := range []string{"00:00", "07:30", "23:59"} {
		if !ValidHHMM(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "7:30", "24:00", "12:60", "12:30:00", "ab:cd"} {
		if ValidHHMM(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestValidDayOfWeek(t *testing.T) {
	for d := -1; d <= 8; d++ {
		if got, want := ValidDayOfWeek(d), d >= 1 && d <= 7; got != want {
			t.Errorf("day %d: got %v", d, got)
		}
	}
}

func TestOptionalUUIDAndFlexBool(t *testing.T) {
	var v struct {
		A OptionalUUID `json:"a"`
		B OptionalUUID `json:"b"`
		C OptionalUUID `json:"c"`
		P FlexBool     `json:"p"`
		Q FlexBool     `json:"q"`
	}
	in := `{"a":"","b":null,"c":"6f1c1d36-4b0e-4b0a-9e3c-1b2b3c4d5e6f","p":"true","q":false}`
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if v.A.Valid || v.B.Valid || !v.C.Valid || !bool(v.P) || bool(v.Q) {
		t.Fatalf("unexpected %+v", v)
	}
	if err := json.Unmarshal([]byte(`{"a":"not-a-uuid"}`), &v); err == nil {
		t.Fatal("expected invalid uuid error")
	}
}

func TestTeacherRequestNormalize(t *testing.T) {
	const sub = "6f1c1d36-4b0e-4b0a-9e3c-1b2b3c4d5e6f"
	const cls = "7f1c1d36-4b0e-4b0a-9e3c-1b2b3c4d5e6f"
	parse := func(s string) TeacherRequest {
		var r TeacherRequest
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	ok := parse(`{"full_name":" Budi ","employee_code":"G1","email":"",
		"subjects":[{"subject_id":"","academic_id":""}],
		"assignments":[{"employee_position_id":"","start_date":"","end_date":""}],
		"schedules":[{"subject_id":"` + sub + `","class_id":"` + cls + `","day_of_week":1,"start_time":"07:00","end_time":"08:30"}]}`)
	if msg := ok.Normalize(); msg != "" {
		t.Fatalf("unexpected error %q", msg)
	}
	if ok.FullName != "Budi" || ok.Email != nil || len(ok.Subjects) != 0 || len(ok.Assignments) != 0 || len(ok.Schedules) != 1 {
		t.Fatalf("normalize result wrong: %+v", ok)
	}

	// academic_id is optional: rows assigned via /subjects/:id/teachers have none.
	noYear := parse(`{"full_name":"a","employee_code":"b","subjects":[{"subject_id":"` + sub + `","academic_id":""}]}`)
	if msg := noYear.Normalize(); msg != "" || len(noYear.Subjects) != 1 {
		t.Fatalf("subject without academic_id should be valid: %q", msg)
	}

	bad := map[string]string{
		"day 8":        `{"full_name":"a","employee_code":"b","schedules":[{"subject_id":"` + sub + `","class_id":"` + cls + `","day_of_week":8,"start_time":"07:00","end_time":"08:00"}]}`,
		"bad time":     `{"full_name":"a","employee_code":"b","schedules":[{"subject_id":"` + sub + `","class_id":"` + cls + `","day_of_week":2,"start_time":"7:00","end_time":"08:00"}]}`,
		"end<start":    `{"full_name":"a","employee_code":"b","schedules":[{"subject_id":"` + sub + `","class_id":"` + cls + `","day_of_week":2,"start_time":"09:00","end_time":"08:00"}]}`,
		"missing cls":  `{"full_name":"a","employee_code":"b","schedules":[{"subject_id":"` + sub + `","day_of_week":2,"start_time":"07:00","end_time":"08:00"}]}`,
		"half subject": `{"full_name":"a","employee_code":"b","subjects":[{"subject_id":"","academic_id":"` + sub + `"}]}`,
		"dup subject":  `{"full_name":"a","employee_code":"b","subjects":[{"subject_id":"` + sub + `"},{"subject_id":"` + sub + `"}]}`,
		"bad date":     `{"full_name":"a","employee_code":"b","join_date":"15-01-2024"}`,
		"bad email":    `{"full_name":"a","employee_code":"b","email":"nope"}`,
		"blank name":   `{"full_name":"  ","employee_code":"b"}`,
		"assign date":  `{"full_name":"a","employee_code":"b","assignments":[{"employee_position_id":"` + sub + `","start_date":"2024-13-01"}]}`,
	}
	for name, s := range bad {
		r := parse(s)
		if msg := r.Normalize(); msg == "" {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestPageQuery(t *testing.T) {
	q := ParsePageQuery("0", "500", "  x ")
	if q.Page != 1 || q.Limit != 100 || q.Search != "x" {
		t.Fatalf("%+v", q)
	}
	m := NewPageMeta(ParsePageQuery("2", "10", ""), 42)
	if m.TotalPages != 5 || m.Page != 2 {
		t.Fatalf("%+v", m)
	}
	if got := LikePattern(`50%_a\`); got != `%50\%\_a\\%` {
		t.Fatalf("got %q", got)
	}
}

func TestValidateScheduleWithPeriod(t *testing.T) {
	var req TeacherRequest
	body := `{"full_name":"A","employee_code":"G1","schedules":[
		{"subject_id":"11111111-1111-1111-1111-111111111111","class_id":"22222222-2222-2222-2222-222222222222","day_of_week":1,"period_id":"33333333-3333-3333-3333-333333333333"},
		{"subject_id":"","class_id":"","period_id":"","day_of_week":0}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if msg := req.Normalize(); msg != "" {
		t.Fatalf("period-based schedule should be valid without times: %s", msg)
	}
	if len(req.Schedules) != 1 || !req.Schedules[0].PeriodID.Valid {
		t.Fatalf("schedules = %+v", req.Schedules)
	}
	noTimes := req.Schedules[0]
	noTimes.PeriodID = OptionalUUID{}
	if ValidateSchedule(noTimes) == "" {
		t.Fatal("schedule without period needs times")
	}
}
