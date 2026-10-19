package controller

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"
)

func TestStudentResultViewStripsWelfare(t *testing.T) {
	r := &model.AssessmentResult{
		InstrumentCode: "ADN", FlagsJSON: model.StringArray{scoring.FlagWelfare, scoring.FlagAnxietyHigh},
		ScoresJSON:          model.JSONMap{"vark_scores": map[string]any{"V": 17}, "wellbeing_items": map[string]any{"ADN-WEL1": 4}},
		RecommendationsJSON: model.JSONMap{"staff_notes": []string{"x"}, "study_tips": []string{"y"}},
		StaffNotes:          model.StringArray{"catatan staf"},
	}
	v := studentResultView(r)
	b, _ := json.Marshal(v)
	s := string(b)
	for _, bad := range []string{scoring.FlagWelfare, "WEL1", "staff_notes", "catatan staf"} {
		if strings.Contains(s, bad) {
			t.Fatalf("student view leaks %q: %s", bad, s)
		}
	}
	if v["wellbeing_message"] != scoring.WellbeingMessage || v["disclaimer"] != scoring.Disclaimer || !strings.Contains(s, scoring.FlagAnxietyHigh) {
		t.Fatalf("view %s", s)
	}
	staff, _ := json.Marshal(resultView(r))
	if !strings.Contains(string(staff), scoring.FlagWelfare) || !strings.Contains(string(staff), "WEL1") {
		t.Fatal("staff view must keep the flag")
	}
	r.FlagsJSON = model.StringArray{}
	if _, ok := studentResultView(r)["wellbeing_message"]; ok {
		t.Fatal("no welfare flag → no message")
	}
}

func TestEffectiveSubStatusAndState(t *testing.T) {
	now := time.Now()
	s := &model.SchoolAssessmentSubscription{Status: "active", TotalQuota: 2, UsedQuota: 1, ExpiresAt: now.Add(time.Hour)}
	if effectiveSubStatus(s, now) != "active" {
		t.Fatal("active")
	}
	s.UsedQuota = 2
	if effectiveSubStatus(s, now) != "depleted" {
		t.Fatal("depleted")
	}
	s.UsedQuota, s.ExpiresAt = 0, now.Add(-time.Hour)
	if effectiveSubStatus(s, now) != "expired" {
		t.Fatal("expired")
	}
	s.Status = "pending"
	if effectiveSubStatus(s, now) != "pending" {
		t.Fatal("pending")
	}
	ses := &model.AssessmentSession{Status: "published", OpensAt: now.Add(time.Hour), ClosesAt: now.Add(2 * time.Hour)}
	if sessionState(ses, now) != "upcoming" {
		t.Fatal("upcoming")
	}
	ses.OpensAt = now.Add(-time.Hour)
	if sessionState(ses, now) != "open" {
		t.Fatal("open")
	}
	ses.Status = "closed"
	if sessionState(ses, now) != "closed" {
		t.Fatal("closed")
	}
}

func TestNormInstruments(t *testing.T) {
	if c, err := normInstruments([]string{"adn", "RIA", "ria"}); err != nil || strings.Join(c, ",") != "RIA,ADN" {
		t.Fatalf("%v %v", c, err)
	}
	for _, bad := range [][]string{nil, {}, {"ALL"}, {"RIA", "XYZ"}} {
		if _, err := normInstruments(bad); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}

func TestQuestionValidation(t *testing.T) {
	five := []string{"1", "2", "3", "4", "5"}
	ok := []questionReq{
		{InstrumentCode: "RIA", Subtest: "riasec_main", ItemCode: "ria-x1", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 5, ScaleLabels: five, Dimension: "R"},
		{InstrumentCode: "RIA", Subtest: "riasec_main", ItemCode: "RIA-AC9", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 5, ScaleLabels: five, Dimension: ""},
		{InstrumentCode: "TMB", Subtest: "part2_bakat", ItemCode: "TMB-SP9", QuestionText: "x", QuestionType: "multiple_choice", Options: []string{"a", "b", "c"}, CorrectKey: "c", Dimension: "S"},
		{InstrumentCode: "PMK", Subtest: "part_a_activities", ItemCode: "PMK-A-R9", QuestionText: "x", QuestionType: "boolean", Dimension: "R"},
		{InstrumentCode: "ADN", Subtest: "module2_wellbeing", ItemCode: "ADN-MOT9", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 4, ScaleLabels: five[:4], Dimension: "motivasi"},
	}
	for i, r := range ok {
		var q model.AssessmentQuestionBank
		if err := r.toRow(&q); err != nil {
			t.Fatalf("case %d rejected: %v", i, err)
		}
		if i == 1 && (q.Subtest != scoring.SubRIAAttention || q.Scored || q.AttentionExpected == nil || *q.AttentionExpected != 3) {
			t.Fatalf("attention item %+v", q)
		}
		if i == 3 && (q.ScaleMin != 0 || q.ScaleMax != 1 || len(q.ScaleLabels) != 2) {
			t.Fatalf("boolean scale %+v", q)
		}
	}
	bad := []questionReq{
		{InstrumentCode: "XYZ", Subtest: "riasec_main", ItemCode: "a", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 5, ScaleLabels: five, Dimension: "R"},
		{InstrumentCode: "RIA", Subtest: "riasec_main", ItemCode: "a", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 5, ScaleLabels: five, Dimension: "V"},
		{InstrumentCode: "RIA", Subtest: "riasec_main", ItemCode: "a", QuestionText: "x", QuestionType: "likert", ScaleMin: 5, ScaleMax: 1, ScaleLabels: five, Dimension: "R"},
		{InstrumentCode: "RIA", Subtest: "riasec_main", ItemCode: "a", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 5, ScaleLabels: five[:3], Dimension: "R"},
		{InstrumentCode: "TMB", Subtest: "part2_bakat", ItemCode: "a", QuestionText: "x", QuestionType: "multiple_choice", Options: []string{"a"}, CorrectKey: "A", Dimension: "S"},
		{InstrumentCode: "TMB", Subtest: "part2_bakat", ItemCode: "a", QuestionText: "x", QuestionType: "multiple_choice", Options: []string{"a", "b"}, CorrectKey: "C", Dimension: "S"},
		{InstrumentCode: "TMB", Subtest: "part1_minat", ItemCode: "a", QuestionText: "x", QuestionType: "multiple_choice", Options: []string{"a", "b"}, CorrectKey: "A", Dimension: "M"},
		{InstrumentCode: "PMK", Subtest: "part_z", ItemCode: "a", QuestionText: "x", QuestionType: "boolean", Dimension: "R"},
		{InstrumentCode: "ADN", Subtest: "module3_career", ItemCode: "", QuestionText: "x", QuestionType: "likert", ScaleMin: 1, ScaleMax: 4, ScaleLabels: five[:4], Dimension: "R"},
	}
	for i, r := range bad {
		var q model.AssessmentQuestionBank
		if err := r.toRow(&q); err == nil {
			t.Fatalf("bad case %d accepted", i)
		}
	}
}
