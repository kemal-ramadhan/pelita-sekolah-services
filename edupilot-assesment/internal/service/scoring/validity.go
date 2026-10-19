package scoring

// LongestRun returns the longest run of identical consecutive answers in
// presentation order (unanswered items break a run).
func LongestRun(items []Item, ans map[string]string) int {
	best, run, prev := 0, 0, ""
	for _, it := range items {
		v, ok := ans[it.ID]
		if !ok || v == "" {
			run, prev = 0, ""
			continue
		}
		if v == prev {
			run++
		} else {
			run, prev = 1, v
		}
		if run > best {
			best = run
		}
	}
	return best
}

// ResponseValidity applies the instrument-independent checks of PRD §11.1:
// straight-lining (≥ 20 identical in a row) and too fast (< 2 s/item or
// < 40 % of the median duration; RIA/PMK/ADN only).
func ResponseValidity(code string, in Input, cfg *Config) []string {
	flags := []string{}
	if LongestRun(in.Items, in.Answers) >= cfg.Validity.StraightLineRun {
		flags = append(flags, FlagStraightLining)
	}
	if code == InstrumentTMB {
		return flags
	}
	answered := 0
	for _, it := range in.Items {
		if v, ok := in.Answers[it.ID]; ok && v != "" {
			answered++
		}
	}
	if answered > 0 && in.DurationSec >= 0 {
		perItem := float64(in.DurationSec) / float64(answered)
		tooFast := perItem < cfg.Validity.MinSecondsPerItem
		if in.MedianDurationSec > 0 && float64(in.DurationSec) < cfg.Validity.MinMedianRatio*in.MedianDurationSec {
			tooFast = true
		}
		if tooFast {
			flags = append(flags, FlagTooFast)
		}
	}
	return flags
}
