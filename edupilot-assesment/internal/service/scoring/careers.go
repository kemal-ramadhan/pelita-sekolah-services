package scoring

import "sort"

// Career is one Occupations Finder suggestion (PRD §8.7).
type Career struct {
	Title  string `json:"title"`
	Code   string `json:"code"`
	Major  string `json:"major,omitempty"`
	CIndex int    `json:"c_index"`
	Level  string `json:"level"`
}

// FindCareers returns the 5–10 occupations with the highest C index against
// the student's 3-letter code: every occupation with C ≥ mid (max 10), topped
// up to the configured minimum with the next best ones.
func FindCareers(code string, cfg *Config) []Career {
	out := []Career{}
	if len(code) < 3 {
		return out
	}
	all := make([]Career, 0, len(cfg.Occupation))
	for _, o := range cfg.Occupation {
		c := BrownGoreCIndex(code, o.Code)
		all = append(all, Career{Title: o.Title, Code: o.Code, Major: o.Major, CIndex: c, Level: CIndexLabel(c, cfg)})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].CIndex != all[j].CIndex {
			return all[i].CIndex > all[j].CIndex
		}
		return all[i].Title < all[j].Title
	})
	for _, c := range all {
		if len(out) >= cfg.Careers.Max {
			break
		}
		if c.CIndex >= cfg.CIndex.Mid || len(out) < cfg.Careers.Min {
			out = append(out, c)
		}
	}
	return out
}
