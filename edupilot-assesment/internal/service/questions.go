// Package service holds the assessment business logic shared by controllers and the worker.
package service

import (
	"math/rand"
	"sort"
	"time"

	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"gorm.io/gorm"
)

// ToItem converts a question-bank row to the scoring view.
func ToItem(q *model.AssessmentQuestionBank) scoring.Item {
	return scoring.Item{
		ID: q.ItemCode, Subtest: q.Subtest, Dimension: q.Dimension, QuestionType: q.QuestionType,
		CorrectKey: q.CorrectKey, Scored: q.Scored, AttentionExpected: q.AttentionExpected,
		ScaleMin: q.ScaleMin, ScaleMax: q.ScaleMax, OptionCount: len(q.Options),
	}
}

// ActiveBank returns the active question-bank rows of an instrument.
func ActiveBank(db *gorm.DB, code string) ([]model.AssessmentQuestionBank, error) {
	rows := []model.AssessmentQuestionBank{}
	err := db.Where("instrument_code = ? AND is_active = true", code).Order("order_num ASC, item_code ASC").Find(&rows).Error
	return rows, err
}

// CountActive returns the number of active items per instrument.
func CountActive(db *gorm.DB) map[string]int {
	var rows []struct {
		InstrumentCode string
		N              int
	}
	db.Model(&model.AssessmentQuestionBank{}).Select("instrument_code, COUNT(*) AS n").Where("is_active = true").Group("instrument_code").Scan(&rows)
	out := map[string]int{}
	for _, r := range rows {
		out[r.InstrumentCode] = r.N
	}
	return out
}

// RowsByCodes loads rows (active or not) for the codes of an attempt, keyed by item code.
func RowsByCodes(db *gorm.DB, codes []string) (map[string]*model.AssessmentQuestionBank, error) {
	out := map[string]*model.AssessmentQuestionBank{}
	if len(codes) == 0 {
		return out, nil
	}
	rows := []model.AssessmentQuestionBank{}
	if err := db.Where("item_code IN ?", codes).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ItemCode] = &rows[i]
	}
	return out, nil
}

// QuestionOrder is deterministic for a seed (PRD FR-06): RIA types interleaved
// R,I,A,S,E,C with attention checks at positions 17 and 41; TMB aptitude keeps
// its fixed order; other sections are shuffled within the section.
func QuestionOrder(code string, rows []model.AssessmentQuestionBank, seed int64) []string {
	rng := rand.New(rand.NewSource(seed))
	bySub := map[string][]model.AssessmentQuestionBank{}
	for _, r := range rows {
		bySub[r.Subtest] = append(bySub[r.Subtest], r)
	}
	ids := func(rs []model.AssessmentQuestionBank) []string {
		out := make([]string, len(rs))
		for i := range rs {
			out[i] = rs[i].ItemCode
		}
		return out
	}
	shuffled := func(rs []model.AssessmentQuestionBank) []string {
		out := ids(rs)
		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	order := []string{}
	switch code {
	case scoring.InstrumentRIA:
		byType := map[string][]string{}
		for _, r := range bySub[scoring.SubRIAMain] {
			byType[r.Dimension] = append(byType[r.Dimension], r.ItemCode)
		}
		types := scoring.RIASECTypes()
		for _, t := range types {
			l := byType[t]
			rng.Shuffle(len(l), func(i, j int) { l[i], l[j] = l[j], l[i] })
		}
		for round := 0; ; round++ {
			added := false
			for _, t := range types {
				if round < len(byType[t]) {
					order = append(order, byType[t][round])
					added = true
				}
			}
			if !added {
				break
			}
		}
		att := ids(bySub[scoring.SubRIAAttention])
		for i, id := range att {
			pos := len(order)
			switch i {
			case 0:
				pos = 16 // position 17
			case 1:
				pos = 40 // position 41
			}
			if pos > len(order) {
				pos = len(order)
			}
			order = append(order[:pos], append([]string{id}, order[pos:]...)...)
		}
	default:
		for _, sub := range scoring.SubtestsOf(code) {
			if sub == scoring.SubTMBBakat {
				order = append(order, ids(bySub[sub])...) // fixed order (order_num)
				continue
			}
			order = append(order, shuffled(bySub[sub])...)
		}
	}
	// Rows with an unexpected subtest are appended so nothing is silently dropped.
	seen := map[string]bool{}
	for _, id := range order {
		seen[id] = true
	}
	rest := []string{}
	for _, r := range rows {
		if !seen[r.ItemCode] {
			rest = append(rest, r.ItemCode)
		}
	}
	sort.Strings(rest)
	return append(order, rest...)
}

// ---------- AttemptState (contract §13.5) ----------

type OptionView struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type QuestionView struct {
	ID          string       `json:"id"`
	Section     string       `json:"section"`
	Text        string       `json:"text"`
	Type        string       `json:"type"`
	ScaleMin    *int         `json:"scale_min,omitempty"`
	ScaleMax    *int         `json:"scale_max,omitempty"`
	ScaleLabels []string     `json:"scale_labels,omitempty"`
	Options     []OptionView `json:"options,omitempty"`
}

type SectionView struct {
	scoring.SectionInfo
	StartedAt *time.Time `json:"started_at,omitempty"`
}

type AttemptView struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	InstrumentCode string    `json:"instrument_code"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	Seed           int64     `json:"seed"`
}

type AttemptState struct {
	Attempt    AttemptView    `json:"attempt"`
	Sections   []SectionView  `json:"sections"`
	Questions  []QuestionView `json:"questions"`
	Answers    map[string]any `json:"answers"`
	ServerTime time.Time      `json:"server_time"`
}

// SectionStart returns when a section was started (nil if never).
func SectionStart(a *model.AssessmentAttempt, key string) *time.Time {
	if a.SectionStarts == nil {
		return nil
	}
	s, ok := a.SectionStarts[key].(string)
	if !ok {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

// BuildAttemptState renders the attempt for the student (never sends answer keys).
func BuildAttemptState(db *gorm.DB, a *model.AssessmentAttempt) (*AttemptState, error) {
	rows, err := RowsByCodes(db, a.QuestionOrder)
	if err != nil {
		return nil, err
	}
	var resp []model.AssessmentResponse
	if err := db.Where("attempt_id = ?", a.ID).Find(&resp).Error; err != nil {
		return nil, err
	}
	st := &AttemptState{
		Attempt: AttemptView{ID: a.ID.String(), SessionID: a.SessionID.String(), InstrumentCode: a.InstrumentCode,
			Status: a.Status, StartedAt: a.StartedAt, Seed: a.Seed},
		Sections: []SectionView{}, Questions: []QuestionView{}, Answers: map[string]any{}, ServerTime: time.Now(),
	}
	present := map[string]bool{}
	for _, id := range a.QuestionOrder {
		q, ok := rows[id]
		if !ok {
			continue
		}
		sec := q.Subtest
		if sec == scoring.SubRIAAttention {
			sec = scoring.SubRIAMain // attention checks are shown inline (positions 17 & 41)
		}
		present[sec] = true
		qv := QuestionView{ID: q.ItemCode, Section: sec, Text: q.QuestionText, Type: q.QuestionType}
		switch q.QuestionType {
		case scoring.TypeMultiple:
			for i, o := range q.Options {
				qv.Options = append(qv.Options, OptionView{Key: scoring.OptionKey(i), Text: o})
			}
		default:
			mn, mx := q.ScaleMin, q.ScaleMax
			qv.ScaleMin, qv.ScaleMax, qv.ScaleLabels = &mn, &mx, []string(q.ScaleLabels)
		}
		st.Questions = append(st.Questions, qv)
	}
	if info, err := scoring.Info(a.InstrumentCode); err == nil {
		for _, s := range info.Sections {
			if present[s.Key] {
				st.Sections = append(st.Sections, SectionView{SectionInfo: s, StartedAt: SectionStart(a, s.Key)})
			}
		}
	}
	for _, r := range resp {
		if q, ok := rows[r.ItemID]; ok {
			st.Answers[r.ItemID] = scoring.AnswerJSON(ToItem(q), r.Value)
		}
	}
	return st, nil
}
