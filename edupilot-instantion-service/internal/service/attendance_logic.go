package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // embedded zone database: school time zones work even without OS tzdata

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Pure attendance helpers (API_CONTRACT §9): school time zone, geofence,
// session tokens and late/early minute computation. Everything here is free of
// DB access except schoolLocation, so it is unit-tested directly.

// DefaultSchoolTimezone is used when a school has no (valid) timezone.
const DefaultSchoolTimezone = "Asia/Jakarta"

const (
	dateLayout      = "2006-01-02"
	monthLayout     = "2006-01"
	clockLayout     = "15:04:05"
	shortClock      = "15:04"
	sessionTokenLen = 6
	// SessionTokenAlphabet has no ambiguous characters (no 0/O, 1/I).
	SessionTokenAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// sessionStartLeadMinutes: a session may be started this many minutes before the schedule start.
	sessionStartLeadMinutes = 15
)

// LoadSchoolLocation resolves an IANA zone name, falling back to Asia/Jakarta
// (and to a fixed UTC+7 if even that is unavailable).
func LoadSchoolLocation(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if loc, err := time.LoadLocation(DefaultSchoolTimezone); err == nil {
		return loc
	}
	return time.FixedZone("WIB", 7*3600)
}

// schoolLocation loads the time zone of a school (single place used by every
// attendance computation).
func schoolLocation(ctx context.Context, db *gorm.DB, schoolID uuid.UUID) *time.Location {
	var tz []string
	db.WithContext(ctx).Model(&model.School{}).Where("id = ?", schoolID).Limit(1).Pluck("timezone", &tz)
	if len(tz) == 0 {
		return LoadSchoolLocation("")
	}
	return LoadSchoolLocation(tz[0])
}

// isoWeekday maps time.Weekday to 1=Monday … 7=Sunday (teacher_schedules.day_of_week).
func isoWeekday(t time.Time) int {
	d := int(t.Weekday())
	if d == 0 {
		return 7
	}
	return d
}

// Haversine calculates the great-circle distance in meters between two coordinates.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000 // meters
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	radLat1 := lat1 * (math.Pi / 180.0)
	radLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(radLat1)*math.Cos(radLat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadius * c
}

// ValidCoordinates reports whether lat/lng are finite and within range.
func ValidCoordinates(lat, lng float64) bool {
	if math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) {
		return false
	}
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

// GeofenceResult is the outcome of a GPS geofence check.
type GeofenceResult struct {
	Distance float64
	Radius   int
}

// CheckGeofence validates coordinates/accuracy and that the point lies inside
// the school radius. Errors: 400 VALIDATION_ERROR (missing/invalid coords),
// 400 LOW_GPS_ACCURACY (accuracy > 2×radius), 400 OUTSIDE_GEOFENCE.
func CheckGeofence(schoolLat, schoolLng float64, radius int, lat, lng, accuracy *float64) (GeofenceResult, error) {
	res := GeofenceResult{Radius: radius}
	if lat == nil || lng == nil {
		return res, apperr.BadRequest("Koordinat lokasi (GPS) wajib dikirim")
	}
	if !ValidCoordinates(*lat, *lng) {
		return res, apperr.BadRequest("Koordinat lokasi tidak valid (latitude -90..90, longitude -180..180)")
	}
	if accuracy != nil {
		if math.IsNaN(*accuracy) || *accuracy < 0 {
			return res, apperr.BadRequest("Akurasi GPS tidak valid")
		}
		if *accuracy > float64(2*radius) {
			return res, apperr.New(http.StatusBadRequest, "LOW_GPS_ACCURACY",
				fmt.Sprintf("Akurasi GPS terlalu rendah (%.0f m). Aktifkan lokasi presisi tinggi lalu coba lagi.", *accuracy))
		}
	}
	res.Distance = math.Round(Haversine(schoolLat, schoolLng, *lat, *lng)*100) / 100
	if res.Distance > float64(radius) {
		e := apperr.New(http.StatusBadRequest, "OUTSIDE_GEOFENCE", fmt.Sprintf(
			"Anda berada di luar radius sekolah (jarak: %.0f meter, batas maksimal: %d meter).", res.Distance, radius))
		e.Data = map[string]any{"distance_m": math.Round(res.Distance), "radius_m": radius}
		return res, e
	}
	return res, nil
}

// generateSessionToken returns a crypto-random 6-char code from SessionTokenAlphabet
// (uniform: rand.Int avoids modulo bias).
func generateSessionToken() string {
	b := make([]byte, sessionTokenLen)
	max := big.NewInt(int64(len(SessionTokenAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		b[i] = SessionTokenAlphabet[n.Int64()]
	}
	return string(b)
}

// NormalizeSessionCode upper-cases/trims a code typed by a student.
func NormalizeSessionCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
var hhmmssPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d)?$`)

// ValidHHMM reports whether s is a zero-padded 24h "HH:MM".
func ValidHHMM(s string) bool { return hhmmPattern.MatchString(s) }

// ValidClock accepts "HH:MM" or "HH:MM:SS".
func ValidClock(s string) bool { return hhmmssPattern.MatchString(s) }

// parseTimeToMinutes converts "HH:MM[:SS]" to minutes from midnight (0 when invalid).
func parseTimeToMinutes(t string) int {
	parts := strings.Split(strings.TrimSpace(t), ":")
	if len(parts) < 2 {
		return 0
	}
	var h, m int
	_, _ = fmt.Sscanf(parts[0], "%d", &h)
	_, _ = fmt.Sscanf(parts[1], "%d", &m)
	return h*60 + m
}

// minutesOfDay returns the minutes since local midnight of t.
func minutesOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

// LateMinutes: late when now (minute precision) > target + tolerance; the
// minutes are counted from the target (API_CONTRACT §9).
func LateMinutes(now time.Time, target string, tolerance int) (late bool, minutes int) {
	cur := minutesOfDay(now)
	t := parseTimeToMinutes(target)
	if cur > t+tolerance {
		return true, cur - t
	}
	return false, 0
}

// EarlyMinutes: early when now is before the check-out start time.
func EarlyMinutes(now time.Time, checkOutStart string) (early bool, minutes int) {
	cur := minutesOfDay(now)
	t := parseTimeToMinutes(checkOutStart)
	if cur < t {
		return true, t - cur
	}
	return false, 0
}

// FirstScheduleTarget returns the earliest "HH:MM" start time (or "" when none).
func FirstScheduleTarget(starts []string) string {
	first := ""
	for _, s := range starts {
		s = strings.TrimSpace(s)
		if !ValidHHMM(s) {
			continue
		}
		if first == "" || s < first {
			first = s
		}
	}
	return first
}

// SessionStartAllowed: a teaching session may start within [start−15 min, end].
func SessionStartAllowed(now time.Time, start, end string) bool {
	cur := minutesOfDay(now)
	return cur >= parseTimeToMinutes(start)-sessionStartLeadMinutes && cur <= parseTimeToMinutes(end)
}

// Remote check-in methods (teachers only) skip the geofence but need notes.
var remoteMethodStatus = map[string]string{
	"pjj":        "remote",
	"wfh":        "remote",
	"dinas_luar": "business_trip",
}

// Allowed values for statuses (API_CONTRACT §9).
var (
	dailyStatuses   = map[string]bool{"present": true, "late": true, "sick": true, "permission": true, "business_trip": true, "remote": true, "absent": true}
	sessionStatuses = map[string]bool{"present": true, "sick": true, "permission": true, "absent": true, "dispensation": true}
)

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// NormalizePersonName lower-cases, strips punctuation and collapses whitespace
// ("  Budi  S. Putra" → "budi s putra"), used for join-request suggestions.
func NormalizePersonName(s string) string {
	return strings.TrimSpace(nonWord.ReplaceAllString(strings.ToLower(s), " "))
}

// JoinSuggestionMatch: a student matches a join request on equal non-empty NISN,
// or on the same normalized name AND the same non-empty birth date.
func JoinSuggestionMatch(reqNISN, reqName, reqBirth, stNISN, stName, stBirth string) bool {
	reqNISN, stNISN = strings.TrimSpace(reqNISN), strings.TrimSpace(stNISN)
	if reqNISN != "" && reqNISN == stNISN {
		return true
	}
	reqBirth, stBirth = strings.TrimSpace(reqBirth), strings.TrimSpace(stBirth)
	if reqBirth == "" || reqBirth != stBirth {
		return false
	}
	n := NormalizePersonName(reqName)
	return n != "" && n == NormalizePersonName(stName)
}
