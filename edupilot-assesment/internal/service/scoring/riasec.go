package scoring

import "math"

// Validity / result flag names (PRD §6.5, §8.6, §9.3, §11.1).
const (
	FlagInvalidResponse   = "RESPON_TIDAK_VALID_POTENSIAL"
	FlagFlatProfile       = "PROFIL_DATAR"
	FlagStraightLining    = "POLA_JAWABAN_BERUNTUN"
	FlagTooFast           = "WAKTU_TERLALU_CEPAT"
	FlagSkillBuilding     = "PENGUATAN_KEMAMPUAN"
	FlagExploreInterest   = "EKSPLORASI_MINAT"
	FlagProfileUnclear    = "PROFIL_BELUM_JELAS"
	FlagEqualAlternatives = "DUA_ALTERNATIF_SETARA"
	FlagInterestOverSkill = "MINAT_TINGGI_KOMPETENSI_BELUM"
	FlagSkillOverInterest = "KOMPETENSI_TINGGI_MINAT_RENDAH"
	FlagAnxietyHigh       = "KECEMASAN_BELAJAR_TINGGI"
	FlagWelfare           = "FLAG_KESEJAHTERAAN"
	FlagTie               = "TIE_FLAG"
)

// RIASECResult — Asesmen Preferensi Karier (PRD §7.4).
type RIASECResult struct {
	Raw                 map[string]float64 `json:"raw"`
	Pct                 map[string]float64 `json:"percentages"`
	Levels              map[string]string  `json:"levels"`
	Ipsative            map[string]float64 `json:"ipsative"`
	Order               []string           `json:"order"`
	HollandCode         string             `json:"holland_code"`
	CodeDisplay         string             `json:"code_display"`
	TieFlag             bool               `json:"tie_flag"`
	Consistency         string             `json:"consistency"`
	ConsistencyDistance int                `json:"consistency_distance"`
	Differentiation     float64            `json:"differentiation"`
	DiffCategory        string             `json:"differentiation_category"`
	Amplitude           float64            `json:"amplitude"`
	ThingsPeople        float64            `json:"things_people"`
	DataIdeas           float64            `json:"data_ideas"`
	RankingBasis        string             `json:"ranking_basis"`
	AttentionFailed     int                `json:"attention_failed"`
	ValidityFlags       []string           `json:"validity_flags"`
}

// typeSums sums the scored items of each RIASEC type in the given subtests,
// prorating when a few items are unanswered (≤10 % is accepted at submit).
// It returns the prorated sum, the item count and the scale bounds per type.
func typeSums(items []Item, ans map[string]string, subtests ...string) (sum map[string]float64, n map[string]int, min, max map[string]int) {
	sum, n, min, max = map[string]float64{}, map[string]int{}, map[string]int{}, map[string]int{}
	answered := map[string]int{}
	raw := map[string]int{}
	for _, it := range items {
		if !it.Scored || !contains(subtests, it.Subtest) {
			continue
		}
		t := it.Dimension
		n[t]++
		min[t], max[t] = it.ScaleMin, it.ScaleMax
		if v, ok := intAnswer(ans, it.ID); ok {
			raw[t] += v
			answered[t]++
		}
	}
	for t, cnt := range n {
		switch {
		case answered[t] == cnt:
			sum[t] = float64(raw[t])
		case answered[t] > 0:
			sum[t] = float64(raw[t]) * float64(cnt) / float64(answered[t])
		default:
			sum[t] = float64(cnt * min[t])
		}
	}
	return sum, n, min, max
}

// ScoreRIASEC implements PRD §7.4 (+ attention checks / flat profile of §11.1).
func ScoreRIASEC(items []Item, ans map[string]string, cfg *Config) *RIASECResult {
	sum, n, mins, maxs := typeSums(items, ans, SubRIAMain)
	raw := map[string]float64{}
	pct := map[string]float64{}
	levels := map[string]string{}
	var total float64
	for _, t := range riasecTypes {
		raw[t] = sum[t]
		total += raw[t]
		span := float64(n[t] * (maxs[t] - mins[t]))
		if span > 0 {
			pct[t] = (raw[t] - float64(n[t]*mins[t])) / span * 100
		}
		switch {
		case pct[t] >= cfg.RIA.LevelHighPct:
			levels[t] = "Tinggi"
		case pct[t] >= cfg.RIA.LevelMidPct:
			levels[t] = "Sedang"
		default:
			levels[t] = "Rendah"
		}
	}
	mean := total / 6
	ips := map[string]float64{}
	var variance float64
	maxRaw, minRaw := math.Inf(-1), math.Inf(1)
	for _, t := range riasecTypes {
		ips[t] = raw[t] - mean
		variance += (raw[t] - mean) * (raw[t] - mean)
		maxRaw = math.Max(maxRaw, raw[t])
		minRaw = math.Min(minRaw, raw[t])
	}

	res := &RIASECResult{ValidityFlags: []string{}}

	// Attention checks (positions 17 & 41, expected 3): ≥1 wrong → flag.
	for _, it := range items {
		if it.AttentionExpected == nil {
			continue
		}
		if v, ok := intAnswer(ans, it.ID); !ok || v != *it.AttentionExpected {
			res.AttentionFailed++
		}
	}
	if res.AttentionFailed > 0 {
		res.ValidityFlags = append(res.ValidityFlags, FlagInvalidResponse)
	}

	diff := maxRaw - minRaw
	basis := "raw"
	scoreForRank := raw
	if diff < cfg.Validity.FlatRangeBelow {
		// "Semua suka / tidak suka": rank on ipsative scores + PROFIL_DATAR.
		res.ValidityFlags = append(res.ValidityFlags, FlagFlatProfile)
		basis = "ipsatif"
		scoreForRank = ips
	}

	// Tie: higher sum of the two hexagon neighbours wins; still tied →
	// R-I-A-S-E-C order and tie_flag (PRD §7.4).
	neighbours := func(t string) float64 {
		p := riasecPositions[t]
		return raw[riasecTypes[(p+5)%6]] + raw[riasecTypes[(p+1)%6]]
	}
	order, ties := rankTypes(riasecTypes, scoreForRank, func(a, b string) int {
		if d := neighbours(a) - neighbours(b); math.Abs(d) > eps {
			if d > 0 {
				return 1
			}
			return -1
		}
		return 0
	})
	code, display, tie := codeWithTies(order, ties, 3)
	cons, d := ConsistencyLabel(code)

	cat := "Profil datar"
	switch {
	case diff >= cfg.RIA.DiffClear:
		cat = "Jelas"
	case diff >= cfg.RIA.DiffMid:
		cat = "Sedang"
	}

	r, i, a, s, e, c := raw["R"], raw["I"], raw["A"], raw["S"], raw["E"], raw["C"]
	res.Raw = mapRound(raw)
	res.Pct = mapRound(pct)
	res.Levels = levels
	res.Ipsative = mapRound(ips)
	res.Order = order
	res.HollandCode = code
	res.CodeDisplay = display
	res.TieFlag = tie
	res.Consistency = cons
	res.ConsistencyDistance = d
	res.Differentiation = round2(diff)
	res.DiffCategory = cat
	res.Amplitude = round2(math.Sqrt(variance / 6))
	res.ThingsPeople = round2(r + 0.5*(i+c) - 0.5*(a+e) - s)
	res.DataIdeas = round2(0.866 * (e + c - i - a))
	res.RankingBasis = basis
	return res
}
