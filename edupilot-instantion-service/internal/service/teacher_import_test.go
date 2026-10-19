package service

import (
	"bytes"
	"errors"
	"testing"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/xuri/excelize/v2"
)

func TestNormalizeImportDate(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"2024-01-15": "2024-01-15",
		"15/01/2024": "2024-01-15",
		"5/1/2024":   "2024-01-05",
		"45306":      "2024-01-15",
	}
	for in, want := range cases {
		got, ok := normalizeImportDate(in)
		if !ok || got != want {
			t.Errorf("%q: got %q ok=%v want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"2024-13-01", "abc", "-5"} {
		if _, ok := normalizeImportDate(in); ok {
			t.Errorf("%q should be invalid", in)
		}
	}
}

func TestParseImportRows(t *testing.T) {
	rows := [][]string{
		{"Full Name*", "employee_code", "email", "phone", "number_type", "number", "employee_status", "join_date"},
		{"Budi", "G001", "budi@school.id", "0812", "NIP", "123", "PNS", "2024-01-15"},
		{},                            // blank row: skipped
		{"", "G002"},                  // missing name
		{"Sari", "G001"},              // duplicate code in file
		{"Andi", "G003", "bad-email"}, // invalid email
		{"Dewi", "G004", "", "", "", "", "", "31/02/2024"}, // invalid date
		{"Eka", "G005"},
	}
	valid, errs, total, err := parseImportRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 {
		t.Errorf("total=%d want 6", total)
	}
	if len(valid) != 2 || valid[0].EmployeeCode != "G001" || valid[1].Row != 8 {
		t.Errorf("valid=%+v", valid)
	}
	wantRows := []int{4, 5, 6, 7}
	if len(errs) != len(wantRows) {
		t.Fatalf("errs=%+v", errs)
	}
	for i, r := range wantRows {
		if errs[i].Row != r {
			t.Errorf("err %d row=%d want %d (%s)", i, errs[i].Row, r, errs[i].Message)
		}
	}
	if valid[0].Email == nil || *valid[0].Email != "budi@school.id" || valid[0].JoinDate != "2024-01-15" {
		t.Errorf("row mapping wrong: %+v", valid[0])
	}
}

func TestParseImportRowsBadHeader(t *testing.T) {
	_, _, _, err := parseImportRows([][]string{{"name", "code"}, {"a", "b"}})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "INVALID_TEMPLATE" {
		t.Fatalf("expected INVALID_TEMPLATE, got %v", err)
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	buf, err := BuildImportTemplate()
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	sheet := f.GetSheetList()[0]
	if err := f.SetSheetRow(sheet, "A2", &[]interface{}{"Budi", "G001", "", "", "", "", "", "2024-01-15"}); err != nil {
		t.Fatal(err)
	}
	out, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readImportFile("guru.XLSX", int64(out.Len()), bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	valid, errs, total, err := parseImportRows(rows)
	if err != nil || total != 1 || len(valid) != 1 || len(errs) != 0 {
		t.Fatalf("valid=%v errs=%v total=%d err=%v", valid, errs, total, err)
	}
}

func TestReadImportFileRejects(t *testing.T) {
	if _, err := readImportFile("a.csv", 10, bytes.NewReader([]byte("x"))); err == nil {
		t.Error("csv must be rejected")
	}
	if _, err := readImportFile("a.xlsx", 10, bytes.NewReader([]byte("not a zip"))); err == nil {
		t.Error("non-zip must be rejected")
	}
	if _, err := readImportFile("a.xlsx", MaxImportFileSize+1, bytes.NewReader(nil)); err == nil {
		t.Error("oversized must be rejected")
	}
}
