package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
)

func fp(v float64) *float64 { return &v }

func TestHaversineKnownDistance(t *testing.T) {
	// Monas → Bundaran HI is ~2.0 km.
	d := Haversine(-6.175392, 106.827153, -6.195031, 106.822948)
	if d < 2100 || d > 2300 {
		t.Fatalf("unexpected distance %.0f m", d)
	}
	if Haversine(1, 2, 1, 2) != 0 {
		t.Fatal("same point must be 0")
	}
}

func TestCheckGeofence(t *testing.T) {
	lat, lng := -6.2, 106.816666
	cases := []struct {
		name     string
		la, ln   *float64
		acc      *float64
		wantCode string
	}{
		{"inside", fp(-6.2003), fp(106.8167), nil, ""},
		{"missing", nil, fp(106.8), nil, "VALIDATION_ERROR"},
		{"bad lat", fp(-91), fp(106.8), nil, "VALIDATION_ERROR"},
		{"bad lng", fp(-6.2), fp(181), nil, "VALIDATION_ERROR"},
		{"low accuracy", fp(-6.2), fp(106.816666), fp(301), "LOW_GPS_ACCURACY"},
		{"accuracy ok", fp(-6.2), fp(106.816666), fp(300), ""},
		{"outside", fp(-6.21), fp(106.816666), nil, "OUTSIDE_GEOFENCE"},
	}
	for _, c := range cases {
		res, err := CheckGeofence(lat, lng, 150, c.la, c.ln, c.acc)
		code := ""
		var ae *apperr.Error
		if errors.As(err, &ae) {
			code = ae.Code
		}
		if code != c.wantCode {
			t.Fatalf("%s: code %q, want %q (err %v)", c.name, code, c.wantCode, err)
		}
		if c.wantCode == "OUTSIDE_GEOFENCE" {
			if ae.Data["radius_m"] != 150 || ae.Data["distance_m"].(float64) < 1000 {
				t.Fatalf("details: %v", ae.Data)
			}
		}
		if c.wantCode == "" && res.Distance > 150 {
			t.Fatalf("%s: distance %v", c.name, res.Distance)
		}
	}
}

func TestSessionTokenAlphabet(t *testing.T) {
	if strings.ContainsAny(SessionTokenAlphabet, "0O1I") {
		t.Fatal("alphabet must not contain ambiguous characters")
	}
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		tok := generateSessionToken()
		if len(tok) != 6 {
			t.Fatalf("len %d", len(tok))
		}
		for _, r := range tok {
			if !strings.ContainsRune(SessionTokenAlphabet, r) {
				t.Fatalf("char %q outside alphabet", r)
			}
		}
		seen[tok] = true
	}
	if len(seen) < 1990 {
		t.Fatalf("too many collisions: %d unique", len(seen))
	}
	if NormalizeSessionCode("  ab3k9z ") != "AB3K9Z" {
		t.Fatal("normalize")
	}
}

func TestLateAndEarlyMinutesInSchoolTZ(t *testing.T) {
	jkt := LoadSchoolLocation("Asia/Jakarta")
	// 00:20 UTC = 07:20 WIB.
	now := time.Date(2026, 10, 13, 0, 20, 0, 0, time.UTC).In(jkt)
	if late, m := LateMinutes(now, "07:00", 15); !late || m != 20 {
		t.Fatalf("late=%v m=%d, want late 20 (counted from target)", late, m)
	}
	if late, _ := LateMinutes(now, "07:00", 20); late {
		t.Fatal("exactly target+tolerance is not late")
	}
	if late, _ := LateMinutes(now, "07:10", 15); late {
		t.Fatal("within tolerance")
	}
	// The same instant in UTC would wrongly be 00:20 → not late.
	if late, _ := LateMinutes(now.UTC(), "07:00", 15); late {
		t.Fatal("sanity: UTC clock differs")
	}
	if early, m := EarlyMinutes(now, "15:00"); !early || m != 460 {
		t.Fatalf("early=%v m=%d", early, m)
	}
	if early, _ := EarlyMinutes(time.Date(2026, 10, 13, 15, 0, 0, 0, jkt), "15:00"); early {
		t.Fatal("at check-out start is not early")
	}
	if LoadSchoolLocation("Not/AZone").String() != "Asia/Jakarta" {
		t.Fatal("fallback zone")
	}
	if LoadSchoolLocation("Asia/Makassar").String() != "Asia/Makassar" {
		t.Fatal("WITA zone")
	}
}

func TestFirstScheduleTargetAndWeekday(t *testing.T) {
	if got := FirstScheduleTarget([]string{"09:30", "07:45", "bad", "", "10:00"}); got != "07:45" {
		t.Fatalf("got %q", got)
	}
	if FirstScheduleTarget(nil) != "" {
		t.Fatal("empty")
	}
	jkt := LoadSchoolLocation("Asia/Jakarta")
	// Sunday 20:00 UTC = Monday 03:00 WIB → weekday 1 in school tz.
	sun := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	if isoWeekday(sun) != 7 || isoWeekday(sun.In(jkt)) != 1 {
		t.Fatalf("weekday utc=%d jkt=%d", isoWeekday(sun), isoWeekday(sun.In(jkt)))
	}
}

func TestSessionStartWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 13, h, m, 0, 0, time.UTC) }
	cases := []struct {
		h, m int
		ok   bool
	}{{7, 44, false}, {7, 45, true}, {8, 0, true}, {8, 45, true}, {8, 46, false}}
	for _, c := range cases {
		if SessionStartAllowed(at(c.h, c.m), "08:00", "08:45") != c.ok {
			t.Fatalf("%02d:%02d want %v", c.h, c.m, c.ok)
		}
	}
}

func TestClockValidation(t *testing.T) {
	for _, s := range []string{"07:00", "23:59", "00:00"} {
		if !ValidHHMM(s) {
			t.Fatalf("%s should be valid", s)
		}
	}
	for _, s := range []string{"7:00", "24:00", "07:60", "07:00:00", ""} {
		if ValidHHMM(s) {
			t.Fatalf("%s should be invalid", s)
		}
	}
	if !ValidClock("07:00:30") || ValidClock("07:00:61") {
		t.Fatal("ValidClock")
	}
}

func TestJoinSuggestionMatch(t *testing.T) {
	cases := []struct {
		name                         string
		rN, rName, rB, sN, sName, sB string
		want                         bool
	}{
		{"nisn", "0012345678", "x", "", "0012345678", "y", "", true},
		{"name+birth", "", "  Budi  S. Putra", "2012-05-01", "", "budi s putra", "2012-05-01", true},
		{"name only", "", "Budi", "", "", "Budi", "", false},
		{"birth differs", "", "Budi", "2012-05-01", "", "Budi", "2012-05-02", false},
		{"name differs", "", "Budi", "2012-05-01", "", "Budiman", "2012-05-01", false},
		{"empty nisn no match", "", "A", "2012-01-01", "", "B", "2012-01-01", false},
	}
	for _, c := range cases {
		if got := JoinSuggestionMatch(c.rN, c.rName, c.rB, c.sN, c.sName, c.sB); got != c.want {
			t.Fatalf("%s: got %v", c.name, got)
		}
	}
}
