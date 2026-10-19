package scoring

import (
	"encoding/json"
	"fmt"
	"sync"

	"edupilot-assesment/internal/data"
)

// Config holds every provisional threshold and weight (PRD §0.2: thresholds
// live in config, not in code). Loaded from internal/data/*.json.
type Config struct {
	Version  string `json:"version"`
	Validity struct {
		MaxMissingRatio   float64 `json:"max_missing_ratio"`
		StraightLineRun   int     `json:"straight_line_run"`
		MinSecondsPerItem float64 `json:"min_seconds_per_item"`
		MinMedianRatio    float64 `json:"min_median_ratio"`
		MinMedianSample   int     `json:"min_median_sample"`
		FlatRangeBelow    float64 `json:"flat_range_below"`
	} `json:"validity"`
	RIA struct {
		LevelHighPct float64 `json:"level_high_pct"`
		LevelMidPct  float64 `json:"level_mid_pct"`
		DiffClear    float64 `json:"diff_clear"`
		DiffMid      float64 `json:"diff_mid"`
	} `json:"ria"`
	PMK struct {
		DiffClear float64 `json:"diff_clear"`
		DiffMid   float64 `json:"diff_mid"`
		GapHigh   float64 `json:"gap_high"`
		GapLow    float64 `json:"gap_low"`
	} `json:"pmk"`
	TMB struct {
		BakatMidPct         float64 `json:"bakat_mid_pct"`
		BakatHighPct        float64 `json:"bakat_high_pct"`
		RumpunVeryPct       float64 `json:"rumpun_very_pct"`
		RumpunFitPct        float64 `json:"rumpun_fit_pct"`
		RumpunSomePct       float64 `json:"rumpun_some_pct"`
		EqualAlternativeGap float64 `json:"equal_alternative_gap"`
		FlagMinatHighPct    float64 `json:"flag_minat_high_pct"`
		FlagMinatLowPct     float64 `json:"flag_minat_low_pct"`
	} `json:"tmb"`
	ADN struct {
		VarkUnclearBelow int     `json:"vark_unclear_below"`
		VarkDominantMin  int     `json:"vark_dominant_min"`
		VarkGapDominant  int     `json:"vark_gap_dominant"`
		VarkClose        int     `json:"vark_close"`
		VarkMultiSpread  int     `json:"vark_multi_spread"`
		SocioStrong      float64 `json:"socio_strong"`
		SocioMid         float64 `json:"socio_mid"`
		AnxietyFlag      int     `json:"anxiety_flag"`
		WelfareFlag      int     `json:"welfare_flag"`
		BriefHigh        int     `json:"brief_high"`
		BriefMid         int     `json:"brief_mid"`
	} `json:"adn"`
	CIndex struct {
		High int `json:"high"`
		Mid  int `json:"mid"`
	} `json:"cindex"`
	Careers struct {
		Min int `json:"min"`
		Max int `json:"max"`
	} `json:"careers"`

	Weights    TMBWeights   `json:"-"`
	Occupation []Occupation `json:"-"`
}

// TMBWeights = internal/data/weights_tmb.json.
type TMBWeights struct {
	Version string                `json:"version"`
	Order   []string              `json:"order"`
	Rumpun  map[string]RumpunSpec `json:"rumpun"`
}

type RumpunSpec struct {
	Name        string             `json:"name"`
	Weights     map[string]float64 `json:"weights"`
	FlagMinat   []string           `json:"flag_minat"`
	FlagBakat   []string           `json:"flag_bakat"`
	Description string             `json:"description"`
	Careers     []string           `json:"careers"`
}

// Occupation = one row of internal/data/occupations.json (PRD §8.7).
type Occupation struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Major string `json:"major"`
	KBJI  string `json:"kbji"`
}

var (
	cfgOnce sync.Once
	cfg     *Config
	cfgErr  error
)

// LoadConfig parses the embedded configuration once.
func LoadConfig() (*Config, error) {
	cfgOnce.Do(func() {
		c := &Config{}
		if cfgErr = unmarshalData("scoring_config.json", c); cfgErr != nil {
			return
		}
		if cfgErr = unmarshalData("weights_tmb.json", &c.Weights); cfgErr != nil {
			return
		}
		if cfgErr = unmarshalData("occupations.json", &c.Occupation); cfgErr != nil {
			return
		}
		cfg = c
	})
	return cfg, cfgErr
}

// MustConfig panics when the embedded config is invalid (startup / tests).
func MustConfig() *Config {
	c, err := LoadConfig()
	if err != nil {
		panic(err)
	}
	return c
}

// ConfigVersion identifies the threshold + weight set stored on each result.
func (c *Config) ConfigVersion() string {
	return c.Version + "+tmb-" + c.Weights.Version
}

func unmarshalData(name string, v any) error {
	b, err := data.Read(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}
