package scoring

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

var tc = MustConfig()

func answers(items []Item, f func(it Item) string) map[string]string {
	m := map[string]string{}
	for _, it := range items {
		if v := f(it); v != "" {
			m[it.ID] = v
		}
	}
	return m
}

func hasFlag(fs []string, f string) bool { return contains(fs, f) }

func riaAnswers(byType map[string]int, def int, attention int) (items []Item, ans map[string]string) {
	items = CanonicalItems(InstrumentRIA)
	ans = answers(items, func(it Item) string {
		if it.AttentionExpected != nil {
			return strconv.Itoa(attention)
		}
		if v, ok := byType[it.Dimension]; ok {
			return strconv.Itoa(v)
		}
		return strconv.Itoa(def)
	})
	return
}

func TestCanonicalBank(t *testing.T) {
	want := map[string]int{InstrumentTMB: 48, InstrumentRIA: 50, InstrumentPMK: 66, InstrumentADN: 41}
	seen := map[string]bool{}
	for code, n := range want {
		items := CanonicalItems(code)
		if len(items) != n {
			t.Fatalf("%s: %d items, want %d", code, len(items), n)
		}
		for _, it := range items {
			if seen[it.ID] {
				t.Fatalf("duplicate item id %s", it.ID)
			}
			seen[it.ID] = true
			if !ValidSubtest(code, it.Subtest) || !ValidDimension(it.Subtest, it.Dimension) || ExpectedType(it.Subtest) != it.QuestionType {
				t.Fatalf("%s: invalid metadata %+v", it.ID, it)
			}
		}
	}
	for i := 1; i <= 6; i++ {
		if !seen[fmt.Sprintf("TMB-SP%d", i)] {
			t.Fatalf("missing TMB-SP%d", i)
		}
	}
	for i := 1; i <= 4; i++ {
		if !seen[fmt.Sprintf("TMB-S%d", i)] {
			t.Fatalf("missing Sosial TMB-S%d", i)
		}
	}
	if seen["TMB-S5"] || seen["TMB-S6"] {
		t.Fatal("TMB-S5/S6 must not exist (spatial items are TMB-SP*)")
	}
}

// AC-01: I=5, A=4, S=3, others 1 → Raw I=40, A=32, S=24; IAS; consistency Tinggi; Pct I=100.
func TestAC01_RIASEC(t *testing.T) {
	items, ans := riaAnswers(map[string]int{"I": 5, "A": 4, "S": 3}, 1, 3)
	r := ScoreRIASEC(items, ans, tc)
	if r.Raw["I"] != 40 || r.Raw["A"] != 32 || r.Raw["S"] != 24 || r.Raw["R"] != 8 {
		t.Fatalf("raw = %v", r.Raw)
	}
	if r.HollandCode != "IAS" || r.TieFlag {
		t.Fatalf("code %s tie %v", r.HollandCode, r.TieFlag)
	}
	if r.Consistency != "Tinggi" || r.Pct["I"] != 100 || r.Levels["I"] != "Tinggi" {
		t.Fatalf("cons %s pct %v", r.Consistency, r.Pct)
	}
	if r.Differentiation != 32 || r.DiffCategory != "Jelas" {
		t.Fatalf("diff %v %s", r.Differentiation, r.DiffCategory)
	}
	if len(r.ValidityFlags) != 0 {
		t.Fatalf("unexpected validity flags %v", r.ValidityFlags)
	}
	// Prediger: TP = R + .5(I+C) − .5(A+E) − S = 8 + 24 − 20 − 24 = −12; DI = .866(E+C−I−A) = .866(16−72)
	if r.ThingsPeople != -12 || r.DataIdeas != round2(0.866*(8+8-40-32)) {
		t.Fatalf("prediger %v %v", r.ThingsPeople, r.DataIdeas)
	}
}

// AC-02: all 3 → raw 24, diff 0 → PROFIL_DATAR, tie_flag.
func TestAC02_FlatProfile(t *testing.T) {
	items, ans := riaAnswers(nil, 3, 3)
	r := ScoreRIASEC(items, ans, tc)
	for _, ty := range riasecTypes {
		if r.Raw[ty] != 24 {
			t.Fatalf("raw %v", r.Raw)
		}
	}
	if r.Differentiation != 0 || !hasFlag(r.ValidityFlags, FlagFlatProfile) || !r.TieFlag || r.RankingBasis != "ipsatif" {
		t.Fatalf("diff %v flags %v tie %v basis %s", r.Differentiation, r.ValidityFlags, r.TieFlag, r.RankingBasis)
	}
	if r.HollandCode != "RIA" || r.CodeDisplay != "R/I/A/S" {
		t.Fatalf("code %s display %s", r.HollandCode, r.CodeDisplay)
	}
	out, _ := Evaluate(InstrumentRIA, Input{Items: items, Answers: ans, DurationSec: 600}, tc)
	if !hasFlag(out.Flags, FlagTie) || !hasFlag(out.ValidityFlags, FlagStraightLining) {
		t.Fatalf("flags %v validity %v", out.Flags, out.ValidityFlags)
	}
}

// AC-03: attention checks ≠ 3 on both → RESPON_TIDAK_VALID_POTENSIAL (also when only one is wrong).
func TestAC03_AttentionCheck(t *testing.T) {
	items, ans := riaAnswers(map[string]int{"I": 5, "A": 4}, 2, 1)
	r := ScoreRIASEC(items, ans, tc)
	if !hasFlag(r.ValidityFlags, FlagInvalidResponse) || r.AttentionFailed != 2 {
		t.Fatalf("flags %v failed %d", r.ValidityFlags, r.AttentionFailed)
	}
	ans["RIA-AC1"] = "3"
	r = ScoreRIASEC(items, ans, tc)
	if !hasFlag(r.ValidityFlags, FlagInvalidResponse) || r.AttentionFailed != 1 {
		t.Fatalf("one wrong: flags %v", r.ValidityFlags)
	}
	ans["RIA-AC2"] = "3"
	if r = ScoreRIASEC(items, ans, tc); hasFlag(r.ValidityFlags, FlagInvalidResponse) {
		t.Fatal("both right must not flag")
	}
}

func TestAC04_AC05_CIndex(t *testing.T) {
	if c := BrownGoreCIndex("IAS", "IAS"); c != 18 {
		t.Fatalf("IAS/IAS = %d", c)
	}
	if c := BrownGoreCIndex("RIA", "SEC"); c != 0 {
		t.Fatalf("RIA/SEC = %d", c)
	}
	if c := BrownGoreCIndex("RIA", "RIC"); c != 3*3+2*3+1*(3-HexagonDistance("A", "C")) {
		t.Fatalf("RIA/RIC = %d", c)
	}
	if CIndexLabel(14, tc) != "Tinggi" || CIndexLabel(13, tc) != "Sedang" || CIndexLabel(7, tc) != "Rendah" {
		t.Fatal("C labels")
	}
}

func TestAC06_Consistency(t *testing.T) {
	cases := map[string]string{"RS": "Rendah", "IE": "Rendah", "AC": "Rendah", "RI": "Tinggi", "CR": "Tinggi", "EC": "Tinggi",
		"RA": "Sedang", "IS": "Sedang", "AE": "Sedang", "SC": "Sedang", "ER": "Sedang", "CI": "Sedang"}
	for code, want := range cases {
		if got, _ := ConsistencyLabel(code); got != want {
			t.Errorf("%s: %s want %s", code, got, want)
		}
	}
}

func TestRIASEC_NeighbourTieBreak(t *testing.T) {
	// I and S tied at 30; I's neighbours R+A, S's neighbours A+E. E higher than R → S first.
	items, ans := riaAnswers(map[string]int{"I": 4, "S": 4, "A": 3, "E": 2, "R": 1, "C": 1}, 1, 3)
	ans["RIA-I1"], ans["RIA-S1"] = "2", "2" // I=30, S=30
	r := ScoreRIASEC(items, ans, tc)
	if r.Raw["I"] != r.Raw["S"] || r.HollandCode[:2] != "SI" || r.TieFlag {
		t.Fatalf("raw %v code %s tie %v", r.Raw, r.HollandCode, r.TieFlag)
	}
}

func tmbItems() []Item { return CanonicalItems(InstrumentTMB) }

func tmbAnswers(minat map[string]int, defMinat int, correct map[string]int) map[string]string {
	items := tmbItems()
	cnt := map[string]int{}
	return answers(items, func(it Item) string {
		if it.Subtest == SubTMBMinat {
			if v, ok := minat[it.Dimension]; ok {
				return strconv.Itoa(v)
			}
			return strconv.Itoa(defMinat)
		}
		cnt[it.Dimension]++
		if cnt[it.Dimension] <= correct[it.Dimension] {
			return it.CorrectKey
		}
		wrong := "A"
		if it.CorrectKey == "A" {
			wrong = "B"
		}
		return wrong
	})
}

// AC-07: all interests 4; N=6, L=6, V=0, S=0 → Minat M=100, IPA=100, IPS < IPA.
func TestAC07_TMB(t *testing.T) {
	r := ScoreTMB(tmbItems(), tmbAnswers(nil, 4, map[string]int{"N": 6, "L": 6}), tc)
	if r.MinatPct["M"] != 100 || r.Rumpun["IPA"] != 100 || r.Rumpun["IPS"] >= r.Rumpun["IPA"] || r.Recommended != "IPA" {
		t.Fatalf("minat %v rumpun %v rec %s", r.MinatPct, r.Rumpun, r.Recommended)
	}
	if r.BakatCorrect["V"] != 0 || r.BakatCategory["N"] != "Tinggi" || r.RumpunCategory["IPA"] != "Sangat Sesuai" {
		t.Fatalf("bakat %v cat %v", r.BakatCorrect, r.RumpunCategory)
	}
	// IPS = .3·100 + .3·100 + .15·0 + .15·100 + .1·100 = 85
	if r.Rumpun["IPS"] != 85 || r.Rumpun["Vokasi"] != 75 {
		t.Fatalf("IPS %v Vokasi %v", r.Rumpun["IPS"], r.Rumpun["Vokasi"])
	}
}

// AC-08: Minat M = 80 % (4,4,3,3 → 83 %), N = 1, L = 1 → PENGUATAN_KEMAMPUAN on IPA.
func TestAC08_TMBSkillBuilding(t *testing.T) {
	items := tmbItems()
	ans := tmbAnswers(map[string]int{"M": 1}, 1, map[string]int{"N": 1, "L": 1})
	ans["TMB-M1"], ans["TMB-M2"], ans["TMB-M3"], ans["TMB-M4"] = "4", "4", "3", "3"
	r := ScoreTMB(items, ans, tc)
	if r.Ranking[0] != "IPA" || !hasFlag(r.RumpunFlags["IPA"], FlagSkillBuilding) || !hasFlag(r.Flags, FlagSkillBuilding) {
		t.Fatalf("ranking %v flags %v", r.Ranking, r.RumpunFlags)
	}
}

func TestTMB_ExploreAndUnclearAndEqual(t *testing.T) {
	// High aptitude everywhere, low interest → all rumpun < 40? IPA = .2·100+.2·100 = 40 → not unclear.
	r := ScoreTMB(tmbItems(), tmbAnswers(nil, 1, map[string]int{"V": 6, "N": 6, "L": 6, "S": 6}), tc)
	if !hasFlag(r.Flags, FlagExploreInterest) {
		t.Fatalf("expected EKSPLORASI_MINAT: %v %v", r.Flags, r.Rumpun)
	}
	// Everything low → PROFIL_BELUM_JELAS and no recommendation.
	r = ScoreTMB(tmbItems(), tmbAnswers(nil, 1, map[string]int{"V": 1, "N": 1, "L": 1, "S": 1}), tc)
	if !hasFlag(r.Flags, FlagProfileUnclear) || r.Recommended != "" {
		t.Fatalf("expected PROFIL_BELUM_JELAS: %v %v", r.Flags, r.Rumpun)
	}
	// IPA vs IPS within 5 points → two equal alternatives.
	r = ScoreTMB(tmbItems(), tmbAnswers(map[string]int{"M": 4, "T": 3, "S": 4, "E": 3}, 1, map[string]int{"V": 5, "N": 5, "L": 5}), tc)
	if !r.EqualAlternatives || len(r.Alternatives) != 2 || !hasFlag(r.Flags, FlagEqualAlternatives) {
		t.Fatalf("equal alternatives: %v %v %v", r.Rumpun, r.Alternatives, r.Flags)
	}
	if d := r.Rumpun[r.Alternatives[0]] - r.Rumpun[r.Alternatives[1]]; d >= 5 || d < 0 {
		t.Fatalf("gap %v", d)
	}
	if RumpunCategory(69.99, tc) != "Sesuai" || RumpunCategory(55, tc) != "Sesuai" || RumpunCategory(54.9, tc) != "Cukup Sesuai" || RumpunCategory(39.9, tc) != "Kurang Sesuai" {
		t.Fatal("rumpun categories")
	}
	for c, want := range map[int]string{0: "Rendah", 2: "Rendah", 3: "Sedang", 4: "Sedang", 5: "Tinggi", 6: "Tinggi"} {
		if got := bakatCategory(round2(float64(c)/6*100), tc); got != want {
			t.Errorf("bakat %d/6 = %s want %s", c, got, want)
		}
	}
}

func pmkAnswers(a, b, c map[string]int, d map[string][2]int) (items []Item, ans map[string]string) {
	items = CanonicalItems(InstrumentPMK)
	idx := map[string]int{}
	ans = answers(items, func(it Item) string {
		k := it.Subtest + it.Dimension
		idx[k]++
		n := idx[k]
		switch it.Subtest {
		case SubPMKA:
			return boolStr(n <= a[it.Dimension])
		case SubPMKB:
			return boolStr(n <= b[it.Dimension])
		case SubPMKC:
			return boolStr(n <= c[it.Dimension])
		}
		v := d[it.Dimension]
		if v[0] == 0 {
			v = [2]int{1, 1}
		}
		return strconv.Itoa(v[n-1])
	})
	return
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// AC-09: PMK totals per formula; tie-break A → C → B → D; deterministic over 20 cases.
func TestAC09_PMK(t *testing.T) {
	items, ans := pmkAnswers(map[string]int{"S": 3, "A": 2, "E": 2}, map[string]int{"S": 3, "A": 2, "E": 1}, map[string]int{"S": 3, "E": 2, "A": 1},
		map[string][2]int{"S": {7, 6}, "A": {5, 5}, "E": {5, 4}})
	r := ScorePMK(items, ans, tc)
	if r.Total["S"] != 3+3+3+13 || r.Total["A"] != 2+2+1+10 || r.Total["R"] != 2 || r.Pct["R"] != 0 {
		t.Fatalf("totals %v pct %v", r.Total, r.Pct)
	}
	if r.HollandCode != "SAE" && r.HollandCode != "SEA" {
		t.Fatalf("code %s totals %v", r.HollandCode, r.Total)
	}
	if r.Pct["S"] != round2(float64(22-2)/21*100) {
		t.Fatalf("pct S %v", r.Pct["S"])
	}
	type tcase struct {
		a, b, c map[string]int
		d       map[string][2]int
		want    string
		tie     bool
	}
	cases := []tcase{
		// equal totals 8: A decides
		{map[string]int{"R": 2, "I": 1}, map[string]int{"R": 1, "I": 2}, map[string]int{"R": 1, "I": 1}, map[string][2]int{"R": {2, 1}, "I": {2, 1}}, "RIA", false},
		// A equal, C decides
		{map[string]int{"R": 1, "I": 1}, map[string]int{"R": 2, "I": 1}, map[string]int{"R": 1, "I": 2}, map[string][2]int{"R": {2, 1}, "I": {2, 1}}, "IRA", false},
		// A, C equal, B decides
		{map[string]int{"C": 1, "E": 1}, map[string]int{"C": 2, "E": 1}, map[string]int{"C": 1, "E": 1}, map[string][2]int{"C": {1, 1}, "E": {2, 1}}, "CER", false},
		// A, C, B equal, D decides (D equal only when totals differ → use explicit)
		{map[string]int{"S": 1, "A": 1}, map[string]int{"S": 1, "A": 1}, map[string]int{"S": 1, "A": 1}, map[string][2]int{"S": {3, 3}, "A": {3, 2}}, "SAR", false},
		// complete tie → R-I-A-S-E-C order + tie
		{nil, nil, nil, nil, "RIA", true},
	}
	rng := rand.New(rand.NewSource(42))
	for len(cases) < 20 {
		a, b, c := map[string]int{}, map[string]int{}, map[string]int{}
		d := map[string][2]int{}
		for _, ty := range riasecTypes {
			a[ty], b[ty], c[ty] = rng.Intn(4), rng.Intn(4), rng.Intn(4)
			d[ty] = [2]int{1 + rng.Intn(7), 1 + rng.Intn(7)}
		}
		cases = append(cases, tcase{a: a, b: b, c: c, d: d, want: ""})
	}
	for i, cs := range cases {
		items, ans := pmkAnswers(cs.a, cs.b, cs.c, cs.d)
		r1 := ScorePMK(items, ans, tc)
		// shuffled item order must not change anything
		sh := append([]Item{}, items...)
		rng.Shuffle(len(sh), func(x, y int) { sh[x], sh[y] = sh[y], sh[x] })
		r2 := ScorePMK(sh, ans, tc)
		if r1.HollandCode != r2.HollandCode || r1.TieFlag != r2.TieFlag {
			t.Fatalf("case %d not deterministic: %s vs %s", i, r1.HollandCode, r2.HollandCode)
		}
		if cs.want != "" && (r1.HollandCode != cs.want || (cs.tie && !r1.TieFlag)) {
			t.Fatalf("case %d: code %s tie %v want %s %v (totals %v)", i, r1.HollandCode, r1.TieFlag, cs.want, cs.tie, r1.Total)
		}
		// verify ordering property
		for k := 0; k+1 < 6; k++ {
			x, y := r1.Order[k], r1.Order[k+1]
			key := func(t string) [5]int { return [5]int{r1.Total[t], r1.A[t], r1.C[t], r1.B[t], r1.D[t]} }
			kx, ky := key(x), key(y)
			for j := 0; j < 5; j++ {
				if kx[j] != ky[j] {
					if kx[j] < ky[j] {
						t.Fatalf("case %d: order %v violates tie-break at %s/%s", i, r1.Order, x, y)
					}
					break
				}
			}
		}
	}
}

func TestPMK_GapFlags(t *testing.T) {
	// R: A=3,C=3 (minat 100), B=0, D=1+1 (komp 0) → gap +100; C type: komp high, minat 0.
	items, ans := pmkAnswers(map[string]int{"R": 3}, map[string]int{"C": 3}, map[string]int{"R": 3, "I": 3}, map[string][2]int{"C": {7, 7}})
	r := ScorePMK(items, ans, tc)
	if r.GapFlags["R"] != FlagInterestOverSkill || r.GapFlags["C"] != FlagSkillOverInterest {
		t.Fatalf("gap flags %v gaps %v", r.GapFlags, r.Gap)
	}
	if !hasFlag(r.Flags, FlagInterestOverSkill) || !hasFlag(r.Flags, FlagSkillOverInterest) || !contains(r.ImageOverActivity, "I") {
		t.Fatalf("flags %v image %v", r.Flags, r.ImageOverActivity)
	}
	if r.DiffCategory != "Jelas" {
		t.Fatalf("diff %d %s", r.Differentiation, r.DiffCategory)
	}
}

func vark(v, a, r, k int) map[string]int { return map[string]int{"V": v, "A": a, "R": r, "K": k} }

func TestAC10_AC11_VARK(t *testing.T) {
	cases := []struct {
		s     map[string]int
		typ   string
		label string
	}{
		{vark(17, 12, 10, 9), VarkUnimodal, "Visual dominan"},          // AC-10
		{vark(15, 14, 9, 8), VarkBimodal, "Bimodal Visual + Auditori"}, // AC-11
		{vark(14, 13, 13, 8), VarkTrimodal, "Trimodal Visual + Auditori + Baca-Tulis"},
		{vark(16, 15, 15, 10), VarkMultimodal, "Multimodal"}, // three ≥ 15
		{vark(13, 12, 11, 10), VarkMultimodal, "Multimodal"}, // all within 3
		{vark(9, 8, 7, 5), VarkUnclear, "Belum jelas"},
		{vark(10, 18, 9, 8), VarkUnimodal, "Auditori dominan"},
	}
	for _, c := range cases {
		got := ClassifyVARK(c.s, tc)
		if got.Type != c.typ || got.Label != c.label {
			t.Errorf("%v → %s %q, want %s %q", c.s, got.Type, got.Label, c.typ, c.label)
		}
	}
	// Through the full ADN pipeline with the canonical items: V=17, A=12, R=10, K=9.
	items := CanonicalItems(InstrumentADN)
	vals := map[string][]int{"V": {4, 4, 3, 3, 3}, "A": {3, 3, 2, 2, 2}, "R": {2, 2, 2, 2, 2}, "K": {2, 2, 2, 2, 1}}
	idx := map[string]int{}
	ans := answers(items, func(it Item) string {
		if it.Subtest == SubADNVark {
			idx[it.Dimension]++
			return strconv.Itoa(vals[it.Dimension][idx[it.Dimension]-1])
		}
		return "2"
	})
	r := ScoreADN(items, ans, tc)
	if r.Vark["V"] != 17 || r.VarkCategory != "Visual dominan" {
		t.Fatalf("vark %v %s", r.Vark, r.VarkCategory)
	}
}

func adnAnswers(socio map[string]string, wel string, brief map[string]int) (items []Item, ans map[string]string) {
	items = CanonicalItems(InstrumentADN)
	ans = answers(items, func(it Item) string {
		switch it.Subtest {
		case SubADNSocio:
			if it.Dimension == SocioWelfare {
				return wel
			}
			if v, ok := socio[it.ID]; ok {
				return v
			}
			return "3"
		case SubADNCareer:
			if v, ok := brief[it.Dimension]; ok {
				return strconv.Itoa(v)
			}
			return "1"
		}
		return "3"
	})
	return
}

// AC-12: WEL1 = 4 → FLAG_KESEJAHTERAAN; student view strips it (see controller tests / e2e).
func TestAC12_Welfare(t *testing.T) {
	items, ans := adnAnswers(nil, "4", nil)
	r := ScoreADN(items, ans, tc)
	if !r.WelfareFlag || !hasFlag(r.Flags, FlagWelfare) {
		t.Fatalf("flags %v", r.Flags)
	}
	out, _ := Evaluate(InstrumentADN, Input{Items: items, Answers: ans, DurationSec: 600}, tc)
	if !out.WelfareFlag || len(out.StaffNotes) == 0 {
		t.Fatal("welfare outcome")
	}
	for _, s := range out.Interpretation.Sections {
		if strings.Contains(s.Body+strings.Join(s.Items, " "), "WEL1") {
			t.Fatal("student-visible interpretation must not mention WEL1")
		}
	}
	items, ans = adnAnswers(nil, "2", nil)
	if r = ScoreADN(items, ans, tc); r.WelfareFlag {
		t.Fatal("WEL1 = 2 must not flag")
	}
}

func TestADN_SocioAndBrief(t *testing.T) {
	items, ans := adnAnswers(map[string]string{"ADN-MOT1": "4", "ADN-MOT2": "3", "ADN-REG1": "1", "ADN-REG2": "2", "ADN-EFI1": "2", "ADN-EFI2": "3", "ADN-KEC1": "3", "ADN-SOS1": "1"}, "1",
		map[string]int{"S": 4, "E": 3})
	r := ScoreADN(items, ans, tc)
	want := map[string]string{SocioMotivasi: LevelStrong, SocioRegulasi: LevelSupport, SocioEfikasi: LevelGrowing, SocioKecemasan: LevelSupport, SocioDukungan: LevelSupport}
	for k, v := range want {
		if r.SocioLevels[k] != v {
			t.Errorf("%s = %s want %s (avg %v)", k, r.SocioLevels[k], v, r.Socio[k])
		}
	}
	if r.Socio[SocioRegulasi] != 1.5 || !hasFlag(r.Flags, FlagAnxietyHigh) {
		t.Fatalf("socio %v flags %v", r.Socio, r.Flags)
	}
	if _, ok := r.Socio[SocioWelfare]; ok {
		t.Fatal("WEL1 must not be a subscale")
	}
	if r.BriefCode != "SE" || r.Brief["S"] != 8 || r.BriefLevels["S"] != "Tinggi" || r.BriefLevels["E"] != "Sedang" || r.BriefLevels["R"] != "Rendah" || r.BriefTie {
		t.Fatalf("brief %v %s %v", r.Brief, r.BriefCode, r.BriefLevels)
	}
	// KEC1 = 2 → no anxiety flag
	ans["ADN-KEC1"] = "2"
	if r = ScoreADN(items, ans, tc); hasFlag(r.Flags, FlagAnxietyHigh) {
		t.Fatal("KEC1 = 2 must not flag")
	}
	// brief tie → flagged
	items, ans = adnAnswers(nil, "1", map[string]int{"I": 4, "A": 4})
	if r = ScoreADN(items, ans, tc); r.BriefCode != "IA" || !r.BriefTie || r.BriefDisplay != "I/A" {
		t.Fatalf("I/A tie: %s %v %s", r.BriefCode, r.BriefTie, r.BriefDisplay)
	}
	items, ans = adnAnswers(nil, "1", map[string]int{"I": 4, "A": 3, "C": 3})
	if r = ScoreADN(items, ans, tc); r.BriefCode != "IA" || !r.BriefTie || r.BriefDisplay != "IA/C" {
		t.Fatalf("tie at 2nd: %s %v %s", r.BriefCode, r.BriefTie, r.BriefDisplay)
	}
}

func TestValidityChecks(t *testing.T) {
	items, ans := riaAnswers(map[string]int{"I": 5, "A": 4, "S": 3, "E": 2}, 1, 3)
	if f := ResponseValidity(InstrumentRIA, Input{Items: items, Answers: ans, DurationSec: 50}, tc); !hasFlag(f, FlagTooFast) {
		t.Fatalf("50 s for 50 items must be too fast: %v", f)
	}
	if f := ResponseValidity(InstrumentRIA, Input{Items: items, Answers: ans, DurationSec: 600}, tc); len(f) != 0 {
		t.Fatalf("interleaved answers flagged: %v", f)
	}
	if f := ResponseValidity(InstrumentRIA, Input{Items: items, Answers: ans, DurationSec: 200, MedianDurationSec: 600}, tc); !hasFlag(f, FlagTooFast) {
		t.Fatalf("< 40%% median must be too fast: %v", f)
	}
	// 20 identical in a row → straight-lining; 19 → not.
	seq := make([]Item, 30)
	a := map[string]string{}
	for i := range seq {
		seq[i] = Item{ID: fmt.Sprint(i), Scored: true}
		a[seq[i].ID] = "4"
		if i == 19 {
			a[seq[i].ID] = "2"
		}
	}
	if LongestRun(seq, a) != 19 {
		t.Fatalf("run %d", LongestRun(seq, a))
	}
	a["19"] = "4"
	if !hasFlag(ResponseValidity(InstrumentPMK, Input{Items: seq, Answers: a, DurationSec: 900}, tc), FlagStraightLining) {
		t.Fatal("straight-lining not flagged")
	}
	// Missing: > 10 % of scored items (bakat excluded).
	tmb := tmbItems()
	ta := tmbAnswers(nil, 3, nil)
	for k := range ta {
		if strings.HasPrefix(k, "TMB-V") || strings.HasPrefix(k, "TMB-SP") {
			delete(ta, k) // unanswered aptitude items are not "missing"
		}
	}
	if m := MissingItems(tmb, ta); len(m) != 0 || ScoredCount(tmb) != 24 {
		t.Fatalf("missing %v scored %d", m, ScoredCount(tmb))
	}
	delete(ta, "TMB-M1")
	delete(ta, "TMB-K4")
	delete(ta, "TMB-E2")
	if m := MissingItems(tmb, ta); len(m) != 3 || float64(len(m))/float64(ScoredCount(tmb)) <= tc.Validity.MaxMissingRatio {
		t.Fatalf("missing %v", m)
	}
	ria := CanonicalItems(InstrumentRIA)
	if ScoredCount(ria) != 48 {
		t.Fatalf("RIA scored %d (attention checks are not scored)", ScoredCount(ria))
	}
}

func TestNormalizeAnswer(t *testing.T) {
	lik := Item{ID: "x", QuestionType: TypeLikert, ScaleMin: 1, ScaleMax: 5}
	boo := Item{ID: "b", QuestionType: TypeBoolean, ScaleMin: 0, ScaleMax: 1}
	mc := Item{ID: "m", QuestionType: TypeMultiple, OptionCount: 5}
	ok := []struct {
		it   Item
		in   string
		want string
	}{{lik, `3`, "3"}, {lik, `"5"`, "5"}, {boo, `true`, "1"}, {boo, `false`, "0"}, {boo, `1`, "1"}, {boo, `"0"`, "0"}, {mc, `"c"`, "C"}}
	for _, c := range ok {
		got, err := NormalizeAnswer(c.it, json.RawMessage(c.in))
		if err != nil || got != c.want {
			t.Errorf("%s %s → %q %v", c.it.QuestionType, c.in, got, err)
		}
	}
	bad := []struct {
		it Item
		in string
	}{{lik, `6`}, {lik, `0`}, {lik, `2.5`}, {boo, `2`}, {mc, `"F"`}, {mc, `1`}, {lik, `{}`}}
	for _, c := range bad {
		if _, err := NormalizeAnswer(c.it, json.RawMessage(c.in)); err == nil {
			t.Errorf("%s %s accepted", c.it.QuestionType, c.in)
		}
	}
	if AnswerJSON(boo, "1") != true || AnswerJSON(lik, "4") != 4 || AnswerJSON(mc, "B") != "B" {
		t.Fatal("AnswerJSON")
	}
}

func TestCareersFinder(t *testing.T) {
	cs := FindCareers("IAS", tc)
	if len(cs) < tc.Careers.Min || len(cs) > tc.Careers.Max || cs[0].Code != "IAS" || cs[0].CIndex != 18 {
		t.Fatalf("careers %+v", cs)
	}
	for i := 1; i < len(cs); i++ {
		if cs[i].CIndex > cs[i-1].CIndex {
			t.Fatal("not sorted by C index")
		}
	}
}

// AC-15 / PRD §10.3–10.4: every generated text is free of forbidden words, and
// interpretation is always present. Random answer sets across all instruments.
func TestReportsLanguage(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, code := range Instruments {
		items := CanonicalItems(code)
		for n := 0; n < 300; n++ {
			ans := answers(items, func(it Item) string {
				switch it.QuestionType {
				case TypeBoolean:
					return boolStr(rng.Intn(2) == 1)
				case TypeMultiple:
					return OptionKey(rng.Intn(5))
				}
				// bias some runs to produce flat / extreme profiles
				if n%10 == 0 {
					return strconv.Itoa(it.ScaleMin)
				}
				return strconv.Itoa(it.ScaleMin + rng.Intn(it.ScaleMax-it.ScaleMin+1))
			})
			out, err := Evaluate(code, Input{Items: items, Answers: ans, DurationSec: rng.Intn(1500)}, tc)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(struct {
				I Interpretation
				S string
				N []string
			}{out.Interpretation, out.ReportSummary, out.StaffNotes})
			if w := FindForbidden(string(b)); len(w) > 0 {
				t.Fatalf("%s: forbidden %v in %s", code, w, b)
			}
			if out.Interpretation.Summary == "" || len(out.Interpretation.Sections) == 0 || out.ReportSummary == "" {
				t.Fatalf("%s: empty interpretation", code)
			}
			if code == InstrumentADN && !strings.Contains(string(b), LearningStyleNote) {
				t.Fatal("ADN report must include the learning-style note")
			}
			if (code == InstrumentRIA || code == InstrumentPMK) && (len(out.Interpretation.Careers) < 5 || len(out.HollandCode) != 3) {
				t.Fatalf("%s careers %d code %s", code, len(out.Interpretation.Careers), out.HollandCode)
			}
		}
	}
	if len(FindForbidden(Disclaimer)) != 0 {
		t.Fatal("disclaimer must be exempt")
	}
	if len(FindForbidden("Kamu pasti cocok")) == 0 {
		t.Fatal("filter broken")
	}
}
