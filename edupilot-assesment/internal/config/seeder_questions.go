package config

import (
	"fmt"
	"log"

	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"gorm.io/gorm"
)

// seedQuestionBank makes the question bank contain every canonical PRD item.
// Idempotent and safe on existing data: missing items are inserted, rows with
// broken scoring metadata (invalid subtest/dimension/type) are repaired, and
// texts edited by an admin are never overwritten.
func seedQuestionBank(db *gorm.DB) {
	migrateTMBSpatialIDs(db)
	inserted, repaired := 0, 0
	for _, code := range scoring.Instruments {
		bank, err := scoring.CanonicalBank(code)
		if err != nil {
			log.Printf("Warning: cannot load canonical bank %s: %v", code, err)
			continue
		}
		for _, bi := range bank {
			var rows []model.AssessmentQuestionBank
			err := db.Where("item_code = ?", bi.ID).Limit(1).Find(&rows).Error
			if err == nil && len(rows) == 0 {
				q := bankRow(bi)
				if err := model.CreateQuestion(db, &q); err != nil {
					log.Printf("Warning: seed question %s failed: %v", bi.ID, err)
					continue
				}
				inserted++
				continue
			}
			if err != nil {
				log.Printf("Warning: seed lookup %s failed: %v", bi.ID, err)
				continue
			}
			row := rows[0]
			if fix := repairMetadata(&row, bi); len(fix) > 0 {
				if err := db.Model(&model.AssessmentQuestionBank{}).Where("id = ?", row.ID).Updates(fix).Error; err != nil {
					log.Printf("Warning: repair question %s failed: %v", bi.ID, err)
					continue
				}
				repaired++
			}
		}
	}
	if inserted > 0 || repaired > 0 {
		log.Printf("Question bank seeded: %d inserted, %d repaired", inserted, repaired)
	}
}

func bankRow(bi scoring.BankItem) model.AssessmentQuestionBank {
	q := model.AssessmentQuestionBank{
		InstrumentCode: bi.Instrument, Subtest: bi.Subtest, ItemCode: bi.ID, QuestionText: bi.Text,
		QuestionType: bi.QuestionType, ScaleMin: bi.ScaleMin, ScaleMax: bi.ScaleMax,
		ScaleLabels: model.StringArray(nonNil(bi.ScaleLabels)), Options: model.StringArray(nonNil(bi.Options)),
		CorrectKey: bi.CorrectKey, Dimension: bi.Dimension, Scored: bi.Scored, AttentionExpected: bi.AttentionExpected,
		Weight: 1, OrderNum: bi.OrderNum, IsActive: true,
	}
	q.ID = newUUID()
	return q
}

// repairMetadata returns the column updates needed so that a canonical item
// is scored correctly (structure only; text/options stay as edited).
func repairMetadata(row *model.AssessmentQuestionBank, bi scoring.BankItem) map[string]any {
	fix := map[string]any{}
	broken := row.InstrumentCode != bi.Instrument ||
		!scoring.ValidSubtest(row.InstrumentCode, row.Subtest) ||
		!scoring.ValidDimension(row.Subtest, row.Dimension) ||
		row.QuestionType != scoring.ExpectedType(row.Subtest) ||
		(bi.AttentionExpected != nil && (row.AttentionExpected == nil || row.Scored)) ||
		(bi.AttentionExpected == nil && row.Subtest == scoring.SubRIAAttention)
	if broken {
		fix["instrument_code"] = bi.Instrument
		fix["subtest"] = bi.Subtest
		fix["dimension"] = bi.Dimension
		fix["question_type"] = bi.QuestionType
		fix["scored"] = bi.Scored
		fix["attention_expected"] = bi.AttentionExpected
	}
	typ := bi.QuestionType
	if typ == scoring.TypeBoolean && (row.ScaleMin != 0 || row.ScaleMax != 1 || len(row.ScaleLabels) != 2) {
		fix["scale_min"], fix["scale_max"] = 0, 1
		fix["scale_labels"] = model.StringArray(bi.ScaleLabels)
	}
	if typ == scoring.TypeMultiple {
		if row.CorrectKey == "" {
			fix["correct_key"] = bi.CorrectKey
		}
		if len(row.Options) < 2 {
			fix["options"] = model.StringArray(bi.Options)
		}
		if row.ScaleMin != 0 || row.ScaleMax != 0 || len(row.ScaleLabels) != 0 {
			fix["scale_min"], fix["scale_max"], fix["scale_labels"] = 0, 0, model.StringArray{}
		}
	}
	if typ == scoring.TypeLikert && (row.ScaleMin != bi.ScaleMin || row.ScaleMax != bi.ScaleMax || len(row.ScaleLabels) != bi.ScaleMax-bi.ScaleMin+1) {
		fix["scale_min"], fix["scale_max"] = bi.ScaleMin, bi.ScaleMax
		fix["scale_labels"] = model.StringArray(bi.ScaleLabels)
	}
	return fix
}

// migrateTMBSpatialIDs renames legacy spatial aptitude rows TMB-S1..S6
// (colliding with the Sosial interest items TMB-S1..S4) to TMB-SP1..SP6
// (contract §13.6). Safe to run repeatedly.
func migrateTMBSpatialIDs(db *gorm.DB) {
	for n := 1; n <= 6; n++ {
		oldCode, newCode := fmt.Sprintf("TMB-S%d", n), fmt.Sprintf("TMB-SP%d", n)
		var legacy []model.AssessmentQuestionBank
		db.Where("item_code = ? AND (subtest = ? OR question_type = ?)", oldCode, scoring.SubTMBBakat, scoring.TypeMultiple).Limit(1).Find(&legacy)
		if len(legacy) == 0 {
			continue // nothing legacy to migrate
		}
		row := legacy[0]
		var exists int64
		db.Model(&model.AssessmentQuestionBank{}).Where("item_code = ?", newCode).Count(&exists)
		if exists == 0 {
			if err := db.Model(&model.AssessmentQuestionBank{}).Where("id = ?", row.ID).
				Updates(map[string]any{"item_code": newCode, "subtest": scoring.SubTMBBakat, "dimension": "S"}).Error; err != nil {
				log.Printf("Warning: rename %s → %s failed: %v", oldCode, newCode, err)
			} else {
				log.Printf("Question bank: renamed spatial item %s → %s", oldCode, newCode)
			}
			continue
		}
		// TMB-SPn already exists: the legacy row is a duplicate. TMB-S1..S4 are
		// re-used for the Sosial interest items (repaired by seedQuestionBank via
		// the canonical metadata); S5/S6 are retired.
		if n <= 4 {
			bank, _ := scoring.CanonicalBank(scoring.InstrumentTMB)
			for _, bi := range bank {
				if bi.ID == oldCode {
					q := bankRow(bi)
					db.Model(&model.AssessmentQuestionBank{}).Where("id = ?", row.ID).Updates(map[string]any{
						"subtest": q.Subtest, "dimension": q.Dimension, "question_type": q.QuestionType, "question_text": q.QuestionText,
						"scale_min": q.ScaleMin, "scale_max": q.ScaleMax, "scale_labels": q.ScaleLabels, "options": model.StringArray{},
						"correct_key": "", "order_num": q.OrderNum, "scored": true,
					})
				}
			}
			continue
		}
		db.Model(&model.AssessmentQuestionBank{}).Where("id = ?", row.ID).
			Updates(map[string]any{"item_code": oldCode + "-LEGACY", "is_active": false})
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
