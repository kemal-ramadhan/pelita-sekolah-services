package scoring

import (
	"math"
	"sort"
	"strings"
)

// VARK classification types (PRD §9.2).
const (
	VarkUnimodal   = "unimodal"
	VarkBimodal    = "bimodal"
	VarkTrimodal   = "trimodal"
	VarkMultimodal = "multimodal"
	VarkUnclear    = "belum_jelas"
)

// Socio-emotional levels (PRD §9.3).
const (
	LevelStrong  = "Kuat"
	LevelGrowing = "Berkembang"
	LevelSupport = "Perlu dukungan"
)

// ModalityNames maps V/A/R/K to the Indonesian labels used in reports & analytics.
var ModalityNames = map[string]string{"V": "Visual", "A": "Auditori", "R": "Baca-Tulis", "K": "Kinestetik"}

// ADNResult — Asesmen Diagnostik Non-Kognitif (PRD §9.2–9.4).
type ADNResult struct {
	Vark         map[string]int     `json:"vark"`
	VarkType     string             `json:"vark_type"`
	VarkDominant []string           `json:"vark_dominant"`
	VarkCategory string             `json:"vark_category"`
	Socio        map[string]float64 `json:"socio_emotional"`
	SocioLevels  map[string]string  `json:"socio_levels"`
	Welfare      map[string]int     `json:"wellbeing_items"`
	WelfareFlag  bool               `json:"welfare_flag"`
	Brief        map[string]int     `json:"brief_career"`
	BriefLevels  map[string]string  `json:"brief_levels"`
	BriefOrder   []string           `json:"brief_order"`
	BriefCode    string             `json:"brief_code"`
	BriefDisplay string             `json:"brief_code_display"`
	BriefTie     bool               `json:"brief_tie_flag"`
	Flags        []string           `json:"flags"`
}

// VarkClass is the result of ClassifyVARK.
type VarkClass struct {
	Type     string
	Dominant []string
	Label    string
}

// ClassifyVARK follows PRD §9.2 / §19 classifyVARK (provisional rules):
// belum jelas (all < 10) → multimodal when ≥3 modalities ≥ 15 → unimodal
// (top ≥ 15 and ≥ 3 above 2nd) → bimodal (top two ≤ 2 apart, ≥ 3 above 3rd)
// → trimodal (top three ≤ 2 apart, ≥ 3 above 4th) → multimodal.
func ClassifyVARK(s map[string]int, cfg *Config) VarkClass {
	ord := append([]string{}, varkOrder...)
	pos := map[string]int{"V": 0, "A": 1, "R": 2, "K": 3}
	sort.SliceStable(ord, func(i, j int) bool {
		if s[ord[i]] != s[ord[j]] {
			return s[ord[i]] > s[ord[j]]
		}
		return pos[ord[i]] < pos[ord[j]]
	})
	v := []int{s[ord[0]], s[ord[1]], s[ord[2]], s[ord[3]]}
	a := cfg.ADN
	high := 0
	for _, x := range v {
		if x >= a.VarkDominantMin {
			high++
		}
	}
	names := func(keys []string) string {
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = ModalityNames[k]
		}
		return strings.Join(out, " + ")
	}
	switch {
	case v[0] < a.VarkUnclearBelow:
		return VarkClass{Type: VarkUnclear, Dominant: []string{}, Label: "Belum jelas"}
	case high >= 3:
		return VarkClass{Type: VarkMultimodal, Dominant: append([]string{}, ord...), Label: "Multimodal"}
	case v[0] >= a.VarkDominantMin && v[0]-v[1] >= a.VarkGapDominant:
		return VarkClass{Type: VarkUnimodal, Dominant: ord[:1], Label: ModalityNames[ord[0]] + " dominan"}
	case v[0]-v[1] <= a.VarkClose && v[1]-v[2] >= a.VarkGapDominant:
		return VarkClass{Type: VarkBimodal, Dominant: ord[:2], Label: "Bimodal " + names(ord[:2])}
	case v[0]-v[2] <= a.VarkClose && v[2]-v[3] >= a.VarkGapDominant:
		return VarkClass{Type: VarkTrimodal, Dominant: ord[:3], Label: "Trimodal " + names(ord[:3])}
	}
	return VarkClass{Type: VarkMultimodal, Dominant: append([]string{}, ord...), Label: "Multimodal"}
}

// SocioLevel: < 2.0 Perlu dukungan · 2.0–2.9 Berkembang · ≥ 3.0 Kuat. Anxiety
// (KEC1, direction −) is reversed: high score = needs attention.
func SocioLevel(sub string, avg float64, cfg *Config) string {
	if sub == SocioKecemasan {
		switch {
		case avg >= cfg.ADN.SocioStrong:
			return LevelSupport
		case avg >= cfg.ADN.SocioMid:
			return LevelGrowing
		}
		return LevelStrong
	}
	switch {
	case avg >= cfg.ADN.SocioStrong:
		return LevelStrong
	case avg >= cfg.ADN.SocioMid:
		return LevelGrowing
	}
	return LevelSupport
}

// ScoreADN implements PRD §9.2–9.4.
func ScoreADN(items []Item, ans map[string]string, cfg *Config) *ADNResult {
	res := &ADNResult{
		Vark: map[string]int{}, Socio: map[string]float64{}, SocioLevels: map[string]string{},
		Welfare: map[string]int{}, Brief: map[string]int{}, BriefLevels: map[string]string{}, Flags: []string{},
	}
	// Module 1: Skor_m = sum of the 5 items of modality m (5–20).
	vs, _, _, _ := typeSums(items, ans, SubADNVark)
	for _, m := range varkOrder {
		res.Vark[m] = int(math.Round(vs[m]))
	}
	vc := ClassifyVARK(res.Vark, cfg)
	res.VarkType, res.VarkDominant, res.VarkCategory = vc.Type, vc.Dominant, vc.Label

	// Module 2: subscale means (1–4); WEL1 is a screening item outside subscales.
	sums, counts := map[string]int{}, map[string]int{}
	for _, it := range items {
		if it.Subtest != SubADNSocio {
			continue
		}
		v, ok := intAnswer(ans, it.ID)
		if !ok {
			continue
		}
		if it.Dimension == SocioWelfare {
			res.Welfare[it.ID] = v
			if v >= cfg.ADN.WelfareFlag {
				res.WelfareFlag = true
			}
			continue
		}
		if !it.Scored {
			continue
		}
		sums[it.Dimension] += v
		counts[it.Dimension]++
	}
	for _, sub := range socioOrder {
		if counts[sub] == 0 {
			continue
		}
		avg := float64(sums[sub]) / float64(counts[sub])
		res.Socio[sub] = round2(avg)
		res.SocioLevels[sub] = SocioLevel(sub, avg, cfg)
	}
	if avg, ok := res.Socio[SocioKecemasan]; ok && avg >= float64(cfg.ADN.AnxietyFlag) {
		res.Flags = append(res.Flags, FlagAnxietyHigh)
	}
	if res.WelfareFlag {
		res.Flags = append(res.Flags, FlagWelfare)
	}

	// Module 3: Raw_t = 2 items (2–8); brief code = top two, ties flagged.
	bs, _, _, _ := typeSums(items, ans, SubADNCareer)
	score := map[string]float64{}
	for _, t := range riasecTypes {
		res.Brief[t] = int(math.Round(bs[t]))
		score[t] = float64(res.Brief[t])
		switch {
		case res.Brief[t] >= cfg.ADN.BriefHigh:
			res.BriefLevels[t] = "Tinggi"
		case res.Brief[t] >= cfg.ADN.BriefMid:
			res.BriefLevels[t] = "Sedang"
		default:
			res.BriefLevels[t] = "Rendah"
		}
	}
	order, ties := rankTypes(riasecTypes, score, nil)
	res.BriefOrder = order
	res.BriefCode, res.BriefDisplay, res.BriefTie = codeWithTies(order, ties, 2)
	return res
}
