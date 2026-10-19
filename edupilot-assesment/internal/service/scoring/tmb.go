package scoring

import (
	"math"
	"sort"
	"strings"
)

// TMBResult — Tes Minat dan Bakat (PRD §6.5).
type TMBResult struct {
	MinatSum          map[string]float64  `json:"minat_sum"`
	MinatPct          map[string]float64  `json:"minat_pct"`
	BakatCorrect      map[string]int      `json:"bakat_correct"`
	BakatTotal        map[string]int      `json:"bakat_total"`
	BakatPct          map[string]float64  `json:"bakat_pct"`
	BakatCategory     map[string]string   `json:"bakat_category"`
	Rumpun            map[string]float64  `json:"rumpun"`
	RumpunCategory    map[string]string   `json:"rumpun_category"`
	Ranking           []string            `json:"ranking"`
	Recommended       string              `json:"recommended"`
	Alternatives      []string            `json:"alternatives"`
	EqualAlternatives bool                `json:"equal_alternatives"`
	Flags             []string            `json:"flags"`
	RumpunFlags       map[string][]string `json:"rumpun_flags"`
}

func bakatCategory(pct float64, cfg *Config) string {
	switch {
	case pct >= cfg.TMB.BakatHighPct:
		return "Tinggi"
	case pct >= cfg.TMB.BakatMidPct:
		return "Sedang"
	}
	return "Rendah"
}

// RumpunCategory: ≥70 Sangat Sesuai | 55–69 Sesuai | 40–54 Cukup Sesuai | <40 Kurang Sesuai.
func RumpunCategory(score float64, cfg *Config) string {
	switch {
	case score >= cfg.TMB.RumpunVeryPct:
		return "Sangat Sesuai"
	case score >= cfg.TMB.RumpunFitPct:
		return "Sesuai"
	case score >= cfg.TMB.RumpunSomePct:
		return "Cukup Sesuai"
	}
	return "Kurang Sesuai"
}

// ScoreTMB implements PRD §6.5 steps 1–4. Unanswered aptitude items score 0.
func ScoreTMB(items []Item, ans map[string]string, cfg *Config) *TMBResult {
	sum, n, mins, maxs := typeSums(items, ans, SubTMBMinat)
	res := &TMBResult{
		MinatSum: map[string]float64{}, MinatPct: map[string]float64{},
		BakatCorrect: map[string]int{}, BakatTotal: map[string]int{}, BakatPct: map[string]float64{}, BakatCategory: map[string]string{},
		Rumpun: map[string]float64{}, RumpunCategory: map[string]string{},
		Alternatives: []string{}, Flags: []string{}, RumpunFlags: map[string][]string{},
	}
	// Step 1: Minat% = (Sum − 4)/12 × 100 (generalised to the item count / scale).
	for _, d := range tmbDomains {
		res.MinatSum[d] = round2(sum[d])
		if span := float64(n[d] * (maxs[d] - mins[d])); span > 0 {
			res.MinatPct[d] = round2(math.Max(0, math.Min(100, (sum[d]-float64(n[d]*mins[d]))/span*100)))
		} else {
			res.MinatPct[d] = 0
		}
	}
	// Step 2: Bakat% = Benar/6 × 100.
	for _, x := range tmbSubtests {
		res.BakatCorrect[x] = 0
		res.BakatTotal[x] = 0
	}
	for _, it := range items {
		if it.Subtest != SubTMBBakat || !it.Scored {
			continue
		}
		res.BakatTotal[it.Dimension]++
		if v := ans[it.ID]; v != "" && strings.EqualFold(v, it.CorrectKey) {
			res.BakatCorrect[it.Dimension]++
		}
	}
	for _, x := range tmbSubtests {
		if res.BakatTotal[x] > 0 {
			res.BakatPct[x] = round2(float64(res.BakatCorrect[x]) / float64(res.BakatTotal[x]) * 100)
		} else {
			res.BakatPct[x] = 0
		}
		res.BakatCategory[x] = bakatCategory(res.BakatPct[x], cfg)
	}

	// Step 3: rumpun = weighted sum (weights_tmb.json).
	order := cfg.Weights.Order
	allBelow := true
	for _, name := range order {
		spec := cfg.Weights.Rumpun[name]
		var s float64
		for k, w := range spec.Weights {
			switch {
			case strings.HasPrefix(k, "minat_"):
				s += w * res.MinatPct[strings.TrimPrefix(k, "minat_")]
			case strings.HasPrefix(k, "bakat_"):
				s += w * res.BakatPct[strings.TrimPrefix(k, "bakat_")]
			}
		}
		s = round2(s)
		res.Rumpun[name] = s
		res.RumpunCategory[name] = RumpunCategory(s, cfg)
		if s >= cfg.TMB.RumpunSomePct {
			allBelow = false
		}
	}
	idx := map[string]int{}
	for i, name := range order {
		idx[name] = i
	}
	ranking := append([]string{}, order...)
	sort.SliceStable(ranking, func(i, j int) bool {
		a, b := res.Rumpun[ranking[i]], res.Rumpun[ranking[j]]
		if math.Abs(a-b) > eps {
			return a > b
		}
		return idx[ranking[i]] < idx[ranking[j]]
	})
	res.Ranking = ranking

	// Step 4: recommendation rules.
	if len(ranking) == 0 {
		return res
	}
	considered := []string{ranking[0]}
	if len(ranking) > 1 && res.Rumpun[ranking[0]]-res.Rumpun[ranking[1]] < cfg.TMB.EqualAlternativeGap {
		res.EqualAlternatives = true
		considered = append(considered, ranking[1])
		res.Flags = append(res.Flags, FlagEqualAlternatives)
	}
	res.Alternatives = considered
	if allBelow {
		res.Flags = append(res.Flags, FlagProfileUnclear)
	} else {
		res.Recommended = ranking[0]
	}
	// Mismatch flags per top rumpun: Minat% = highest related interest domain,
	// related aptitude category = mean of the related subtests.
	for _, name := range considered {
		spec := cfg.Weights.Rumpun[name]
		minat := 0.0
		for _, d := range spec.FlagMinat {
			minat = math.Max(minat, res.MinatPct[d])
		}
		var bsum float64
		for _, x := range spec.FlagBakat {
			bsum += res.BakatPct[x]
		}
		cat := "Sedang"
		if len(spec.FlagBakat) > 0 {
			cat = bakatCategory(bsum/float64(len(spec.FlagBakat)), cfg)
		}
		var fl []string
		if minat >= cfg.TMB.FlagMinatHighPct && cat == "Rendah" {
			fl = append(fl, FlagSkillBuilding)
		}
		if cat == "Tinggi" && minat < cfg.TMB.FlagMinatLowPct {
			fl = append(fl, FlagExploreInterest)
		}
		if len(fl) > 0 {
			res.RumpunFlags[name] = fl
			for _, f := range fl {
				if !contains(res.Flags, f) {
					res.Flags = append(res.Flags, f)
				}
			}
		}
	}
	return res
}
