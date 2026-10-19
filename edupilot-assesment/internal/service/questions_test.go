package service

import (
	"testing"

	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"
)

func bankRows(code string) []model.AssessmentQuestionBank {
	b, _ := scoring.CanonicalBank(code)
	out := make([]model.AssessmentQuestionBank, len(b))
	for i, bi := range b {
		out[i] = model.AssessmentQuestionBank{ItemCode: bi.ID, Subtest: bi.Subtest, Dimension: bi.Dimension, OrderNum: bi.OrderNum}
	}
	return out
}

func TestQuestionOrderRIA(t *testing.T) {
	rows := bankRows(scoring.InstrumentRIA)
	o := QuestionOrder(scoring.InstrumentRIA, rows, 12345)
	if len(o) != 50 || o[16] != "RIA-AC1" || o[40] != "RIA-AC2" {
		t.Fatalf("len %d pos17 %s pos41 %s", len(o), o[16], o[40])
	}
	// interleaved R,I,A,S,E,C in every round (attention checks removed)
	dim := map[string]string{}
	for _, r := range rows {
		dim[r.ItemCode] = r.Dimension
	}
	k := 0
	for _, id := range o {
		if dim[id] == "" {
			continue
		}
		if want := scoring.RIASECTypes()[k%6]; dim[id] != want {
			t.Fatalf("item %d is %s, want %s", k, dim[id], want)
		}
		k++
	}
	if o2 := QuestionOrder(scoring.InstrumentRIA, rows, 12345); o2[0] != o[0] || o2[49] != o[49] {
		t.Fatal("order not deterministic for a seed")
	}
	if o3 := QuestionOrder(scoring.InstrumentRIA, rows, 999); equal(o3, o) {
		t.Fatal("different seeds should shuffle differently")
	}
}

func TestQuestionOrderTMBBakatFixed(t *testing.T) {
	o := QuestionOrder(scoring.InstrumentTMB, bankRows(scoring.InstrumentTMB), 7)
	if len(o) != 48 || o[24] != "TMB-V1" || o[47] != "TMB-SP6" {
		t.Fatalf("bakat order: %v", o[24:])
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
