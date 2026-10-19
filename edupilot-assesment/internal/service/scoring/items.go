package scoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Instrument codes (PRD §4).
const (
	InstrumentTMB = "TMB"
	InstrumentRIA = "RIA"
	InstrumentPMK = "PMK"
	InstrumentADN = "ADN"
)

// Instruments in canonical order.
var Instruments = []string{InstrumentTMB, InstrumentRIA, InstrumentPMK, InstrumentADN}

// IsInstrument reports whether code is one of the four instruments.
func IsInstrument(code string) bool {
	for _, c := range Instruments {
		if c == code {
			return true
		}
	}
	return false
}

// Question types.
const (
	TypeLikert   = "likert"
	TypeMultiple = "multiple_choice"
	TypeBoolean  = "boolean"
)

// Subtests (question bank `subtest`). Scoring routes every response by these
// item metadata, never by positions inside the item id.
const (
	SubTMBMinat     = "part1_minat"
	SubTMBBakat     = "part2_bakat"
	SubRIAMain      = "riasec_main"
	SubRIAAttention = "attention_check"
	SubPMKA         = "part_a_activities"
	SubPMKB         = "part_b_competencies"
	SubPMKC         = "part_c_occupations"
	SubPMKD         = "part_d_self_estimates"
	SubADNVark      = "module1_vark"
	SubADNSocio     = "module2_wellbeing"
	SubADNCareer    = "module3_career"
)

// ADN module 2 subscales (dimension values).
const (
	SocioMotivasi   = "motivasi"
	SocioEfikasi    = "efikasi_diri"
	SocioRegulasi   = "regulasi_belajar"
	SocioKecemasan  = "kecemasan_belajar"
	SocioDukungan   = "dukungan_sosial"
	SocioWelfare    = "waspada_kesejahteraan"
	TMBDomainsOrder = "MTSEBK"
)

var (
	riasecTypes  = []string{"R", "I", "A", "S", "E", "C"}
	varkOrder    = []string{"V", "A", "R", "K"}
	tmbDomains   = []string{"M", "T", "S", "E", "B", "K"}
	tmbSubtests  = []string{"V", "N", "L", "S"}
	socioOrder   = []string{SocioMotivasi, SocioEfikasi, SocioRegulasi, SocioKecemasan, SocioDukungan}
	instrumentSU = map[string][]string{
		InstrumentTMB: {SubTMBMinat, SubTMBBakat},
		InstrumentRIA: {SubRIAMain, SubRIAAttention},
		InstrumentPMK: {SubPMKA, SubPMKB, SubPMKC, SubPMKD},
		InstrumentADN: {SubADNVark, SubADNSocio, SubADNCareer},
	}
	subtestDims = map[string][]string{
		SubTMBMinat:     tmbDomains,
		SubTMBBakat:     tmbSubtests,
		SubRIAMain:      riasecTypes,
		SubRIAAttention: {""},
		SubPMKA:         riasecTypes,
		SubPMKB:         riasecTypes,
		SubPMKC:         riasecTypes,
		SubPMKD:         riasecTypes,
		SubADNVark:      varkOrder,
		SubADNSocio:     append(append([]string{}, socioOrder...), SocioWelfare),
		SubADNCareer:    riasecTypes,
	}
	subtestType = map[string]string{
		SubTMBMinat:     TypeLikert,
		SubTMBBakat:     TypeMultiple,
		SubRIAMain:      TypeLikert,
		SubRIAAttention: TypeLikert,
		SubPMKA:         TypeBoolean,
		SubPMKB:         TypeBoolean,
		SubPMKC:         TypeBoolean,
		SubPMKD:         TypeLikert,
		SubADNVark:      TypeLikert,
		SubADNSocio:     TypeLikert,
		SubADNCareer:    TypeLikert,
	}
)

// RIASECTypes returns R,I,A,S,E,C.
func RIASECTypes() []string { return append([]string{}, riasecTypes...) }

// SubtestsOf returns the valid subtests of an instrument.
func SubtestsOf(instrument string) []string { return append([]string{}, instrumentSU[instrument]...) }

// ValidSubtest / ValidDimension / ExpectedType back the admin question-bank validation (§13.3).
func ValidSubtest(instrument, subtest string) bool {
	return contains(instrumentSU[instrument], subtest)
}
func ValidDimension(subtest, dim string) bool { return contains(subtestDims[subtest], dim) }
func DimensionsOf(subtest string) []string    { return append([]string{}, subtestDims[subtest]...) }
func ExpectedType(subtest string) string      { return subtestType[subtest] }

// Item is the scoring view of one question-bank row.
type Item struct {
	ID                string
	Subtest           string
	Dimension         string
	QuestionType      string
	CorrectKey        string
	Scored            bool
	AttentionExpected *int
	ScaleMin          int
	ScaleMax          int
	OptionCount       int
}

// Input is everything the pure scoring functions need for one attempt.
type Input struct {
	Items             []Item            // presentation order of the attempt
	Answers           map[string]string // normalised values (see NormalizeAnswer)
	DurationSec       int
	MedianDurationSec float64 // median of completed attempts (0 = unknown / too few)
}

// optionKeys are the multiple-choice keys in display order.
var optionKeys = []string{"A", "B", "C", "D", "E"}

// OptionKey returns the key for option index i (A..E).
func OptionKey(i int) string {
	if i >= 0 && i < len(optionKeys) {
		return optionKeys[i]
	}
	return ""
}

// ErrInvalidAnswer is returned by NormalizeAnswer for out-of-range values.
var ErrInvalidAnswer = errors.New("invalid answer")

// NormalizeAnswer converts a JSON value to the stored string form:
// likert → integer within [min,max]; boolean → "1"/"0"; multiple choice → "A".."E".
func NormalizeAnswer(it Item, raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", ErrInvalidAnswer
	}
	switch it.QuestionType {
	case TypeLikert:
		n, ok := toInt(v)
		if !ok || n < it.ScaleMin || n > it.ScaleMax {
			return "", fmt.Errorf("%w: nilai %s harus %d–%d", ErrInvalidAnswer, it.ID, it.ScaleMin, it.ScaleMax)
		}
		return strconv.Itoa(n), nil
	case TypeBoolean:
		switch t := v.(type) {
		case bool:
			if t {
				return "1", nil
			}
			return "0", nil
		case float64:
			if t == 1 {
				return "1", nil
			}
			if t == 0 {
				return "0", nil
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "1", "true", "ya", "yes":
				return "1", nil
			case "0", "false", "tidak", "no":
				return "0", nil
			}
		}
		return "", fmt.Errorf("%w: nilai %s harus true/false", ErrInvalidAnswer, it.ID)
	case TypeMultiple:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("%w: jawaban %s harus huruf pilihan", ErrInvalidAnswer, it.ID)
		}
		s = strings.ToUpper(strings.TrimSpace(s))
		n := it.OptionCount
		if n <= 0 || n > len(optionKeys) {
			n = len(optionKeys)
		}
		if !contains(optionKeys[:n], s) {
			return "", fmt.Errorf("%w: pilihan %s tidak tersedia", ErrInvalidAnswer, it.ID)
		}
		return s, nil
	}
	return "", ErrInvalidAnswer
}

// AnswerJSON is the inverse of NormalizeAnswer for the AttemptState payload.
func AnswerJSON(it Item, stored string) any {
	switch it.QuestionType {
	case TypeLikert:
		if n, err := strconv.Atoi(stored); err == nil {
			return n
		}
	case TypeBoolean:
		return stored == "1"
	}
	return stored
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		if t != math.Trunc(t) {
			return 0, false
		}
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	}
	return 0, false
}

func intAnswer(ans map[string]string, id string) (int, bool) {
	s, ok := ans[id]
	if !ok || s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// MissingItems lists scored items without an answer. Aptitude items
// (TMB part 2, timed) count as wrong when unanswered, so they are excluded.
func MissingItems(items []Item, ans map[string]string) []string {
	out := []string{}
	for _, it := range items {
		if !it.Scored || it.Subtest == SubTMBBakat {
			continue
		}
		if v, ok := ans[it.ID]; !ok || v == "" {
			out = append(out, it.ID)
		}
	}
	return out
}

// ScoredCount counts items considered by MissingItems.
func ScoredCount(items []Item) int {
	n := 0
	for _, it := range items {
		if it.Scored && it.Subtest != SubTMBBakat {
			n++
		}
	}
	return n
}

// ---------- hexagon helpers ----------

var riasecPositions = map[string]int{"R": 0, "I": 1, "A": 2, "S": 3, "E": 4, "C": 5}

// HexagonDistance d = min(|p1−p2|, 6−|p1−p2|) on R-I-A-S-E-C (PRD §7.4). Unknown letters → 3.
func HexagonDistance(t1, t2 string) int {
	p1, ok1 := riasecPositions[t1]
	p2, ok2 := riasecPositions[t2]
	if !ok1 || !ok2 {
		return 3
	}
	d := p1 - p2
	if d < 0 {
		d = -d
	}
	if 6-d < d {
		return 6 - d
	}
	return d
}

// ConsistencyLabel maps the hexagon distance of letters 1 and 2.
func ConsistencyLabel(code string) (string, int) {
	if len(code) < 2 {
		return "", 0
	}
	d := HexagonDistance(code[0:1], code[1:2])
	switch d {
	case 0, 1:
		return "Tinggi", d
	case 2:
		return "Sedang", d
	}
	return "Rendah", d
}

// BrownGoreCIndex C = 3·x1 + 2·x2 + 1·x3, x = 3 − d (0..18, PRD §7.4).
func BrownGoreCIndex(person, target string) int {
	if len(person) < 3 || len(target) < 3 {
		return 0
	}
	w := []int{3, 2, 1}
	s := 0
	for i := 0; i < 3; i++ {
		s += w[i] * (3 - HexagonDistance(person[i:i+1], target[i:i+1]))
	}
	return s
}

// CIndexLabel: ≥14 Tinggi · 8–13 Sedang · <8 Rendah.
func CIndexLabel(c int, cfg *Config) string {
	switch {
	case c >= cfg.CIndex.High:
		return "Tinggi"
	case c >= cfg.CIndex.Mid:
		return "Sedang"
	}
	return "Rendah"
}

const eps = 1e-9

// rankTypes orders types by score desc, then by tiebreak (>0 = a first),
// then by the given canonical order. ties[i] is true when order[i] and
// order[i+1] remain tied after the tiebreak (resolved only by canonical order).
func rankTypes(types []string, score map[string]float64, tiebreak func(a, b string) int) (order []string, ties []bool) {
	order = append([]string{}, types...)
	idx := map[string]int{}
	for i, t := range types {
		idx[t] = i
	}
	cmp := func(a, b string) int {
		if d := score[a] - score[b]; math.Abs(d) > eps {
			if d > 0 {
				return 1
			}
			return -1
		}
		if tiebreak != nil {
			return tiebreak(a, b)
		}
		return 0
	}
	sort.SliceStable(order, func(i, j int) bool {
		if c := cmp(order[i], order[j]); c != 0 {
			return c > 0
		}
		return idx[order[i]] < idx[order[j]]
	})
	ties = make([]bool, len(order))
	for i := 0; i+1 < len(order); i++ {
		ties[i] = cmp(order[i], order[i+1]) == 0
	}
	return order, ties
}

// codeWithTies builds the n-letter code and its display form ("I/A" for an
// unresolved tie, PRD §7.4). tie is true when a tie touches the first n letters.
func codeWithTies(order []string, ties []bool, n int) (code, display string, tie bool) {
	var b strings.Builder
	for i := 0; i < n && i < len(order); i++ {
		code += order[i]
		b.WriteString(order[i])
		if i < len(ties) && ties[i] {
			tie = true
			b.WriteString("/")
			if i == n-1 && i+1 < len(order) {
				b.WriteString(order[i+1])
			}
		}
	}
	return code, b.String(), tie
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func mapRound(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = round2(v)
	}
	return out
}
