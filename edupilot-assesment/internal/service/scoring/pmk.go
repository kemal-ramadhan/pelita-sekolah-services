package scoring

import "math"

// PMKResult — Tes Penelusuran Minat Karier Holland (PRD §8.6).
type PMKResult struct {
	A                 map[string]int     `json:"part_a"`
	B                 map[string]int     `json:"part_b"`
	C                 map[string]int     `json:"part_c"`
	D                 map[string]int     `json:"part_d"`
	Total             map[string]int     `json:"total"`
	Pct               map[string]float64 `json:"percentages"`
	Order             []string           `json:"order"`
	HollandCode       string             `json:"holland_code"`
	CodeDisplay       string             `json:"code_display"`
	TieFlag           bool               `json:"tie_flag"`
	Consistency       string             `json:"consistency"`
	Differentiation   int                `json:"differentiation"`
	DiffCategory      string             `json:"differentiation_category"`
	MinatPct          map[string]float64 `json:"minat_pct"`
	KompetensiPct     map[string]float64 `json:"kompetensi_pct"`
	Gap               map[string]float64 `json:"gap"`
	GapFlags          map[string]string  `json:"gap_flags"`
	ImageOverActivity []string           `json:"image_over_activity"`
	Flags             []string           `json:"flags"`
}

func boolCount(items []Item, ans map[string]string, sub string) (cnt map[string]int, n map[string]int) {
	cnt, n = map[string]int{}, map[string]int{}
	for _, it := range items {
		if !it.Scored || it.Subtest != sub {
			continue
		}
		n[it.Dimension]++
		if ans[it.ID] == "1" {
			cnt[it.Dimension]++
		}
	}
	return
}

// ScorePMK implements PRD §8.6 incl. tie-break A → C → B → D and triangulation.
func ScorePMK(items []Item, ans map[string]string, cfg *Config) *PMKResult {
	a, na := boolCount(items, ans, SubPMKA)
	b, nb := boolCount(items, ans, SubPMKB)
	c, nc := boolCount(items, ans, SubPMKC)
	dsum, nd, dmin, dmax := typeSums(items, ans, SubPMKD)

	res := &PMKResult{
		A: map[string]int{}, B: map[string]int{}, C: map[string]int{}, D: map[string]int{},
		Total: map[string]int{}, Pct: map[string]float64{},
		MinatPct: map[string]float64{}, KompetensiPct: map[string]float64{}, Gap: map[string]float64{},
		GapFlags: map[string]string{}, ImageOverActivity: []string{}, Flags: []string{},
	}
	score := map[string]float64{}
	for _, t := range riasecTypes {
		d := int(math.Round(dsum[t]))
		res.A[t], res.B[t], res.C[t], res.D[t] = a[t], b[t], c[t], d
		tot := a[t] + b[t] + c[t] + d
		res.Total[t] = tot
		score[t] = float64(tot)
		// Pct = (Total − 2)/21 × 100 (generalised).
		lo := nd[t] * dmin[t]
		span := na[t] + nb[t] + nc[t] + nd[t]*(dmax[t]-dmin[t])
		if span > 0 {
			res.Pct[t] = round2(float64(tot-lo) / float64(span) * 100)
		}
		// Triangulation: Pct_Minat = (A+C)/6×100; Pct_Kompetensi = ((B/3) + ((D−2)/12))/2×100.
		var minat, komp float64
		if na[t]+nc[t] > 0 {
			minat = float64(a[t]+c[t]) / float64(na[t]+nc[t]) * 100
		}
		var bPart, dPart float64
		if nb[t] > 0 {
			bPart = float64(b[t]) / float64(nb[t])
		}
		if dr := nd[t] * (dmax[t] - dmin[t]); dr > 0 {
			dPart = float64(d-lo) / float64(dr)
		}
		komp = (bPart + dPart) / 2 * 100
		gap := minat - komp
		res.MinatPct[t], res.KompetensiPct[t], res.Gap[t] = round2(minat), round2(komp), round2(gap)
		switch {
		case gap >= cfg.PMK.GapHigh:
			res.GapFlags[t] = FlagInterestOverSkill
		case gap <= cfg.PMK.GapLow:
			res.GapFlags[t] = FlagSkillOverInterest
		}
		// Job images liked (C high) while the core activities are not (A low).
		if nc[t] > 0 && na[t] > 0 && float64(c[t]) >= float64(nc[t])*2/3 && float64(a[t]) <= float64(na[t])/3 {
			res.ImageOverActivity = append(res.ImageOverActivity, t)
		}
	}
	for _, t := range riasecTypes {
		if f, ok := res.GapFlags[t]; ok && !contains(res.Flags, f) {
			res.Flags = append(res.Flags, f)
		}
	}

	order, ties := rankTypes(riasecTypes, score, func(x, y string) int {
		for _, part := range []map[string]int{res.A, res.C, res.B, res.D} {
			if part[x] != part[y] {
				if part[x] > part[y] {
					return 1
				}
				return -1
			}
		}
		return 0
	})
	code, display, tie := codeWithTies(order, ties, 3)
	res.Order, res.HollandCode, res.CodeDisplay, res.TieFlag = order, code, display, tie
	res.Consistency, _ = ConsistencyLabel(code)

	res.Differentiation = res.Total[order[0]] - res.Total[order[len(order)-1]]
	switch {
	case float64(res.Differentiation) >= cfg.PMK.DiffClear:
		res.DiffCategory = "Jelas"
	case float64(res.Differentiation) >= cfg.PMK.DiffMid:
		res.DiffCategory = "Sedang"
	default:
		res.DiffCategory = "Datar"
	}
	return res
}
