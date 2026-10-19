package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"edupilot-assesment/internal/apperr"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/middleware"
	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AdminController — platform admin routes (contract §13.3).
type AdminController struct{}

func NewAdminController() *AdminController { return &AdminController{} }

var (
	errDuplicateCode     = apperr.Conflict("DUPLICATE_CODE", "Kode sudah digunakan")
	errInvalidInstrument = apperr.Invalid("INVALID_INSTRUMENT", "instrument_codes harus berisi minimal satu dari TMB, RIA, PMK, ADN")
)

// normInstruments dedupes/uppercases and validates a non-empty subset of the 4 codes.
func normInstruments(codes []string) ([]string, error) {
	out := []string{}
	for _, c := range codes {
		c = strings.ToUpper(strings.TrimSpace(c))
		if !scoring.IsInstrument(c) {
			return nil, errInvalidInstrument
		}
		if !contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, errInvalidInstrument
	}
	// canonical order
	ordered := []string{}
	for _, c := range scoring.Instruments {
		if contains(out, c) {
			ordered = append(ordered, c)
		}
	}
	return ordered, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ---------- Packages ----------

type packageReq struct {
	Name            string   `json:"name"`
	Code            string   `json:"code"`
	Description     string   `json:"description"`
	Price           float64  `json:"price"`
	StudentQuota    int      `json:"student_quota"`
	InstrumentCodes []string `json:"instrument_codes"`
	BadgeColor      string   `json:"badge_color"`
	IsActive        *bool    `json:"is_active"`
}

func (r *packageReq) validate() error {
	r.Name, r.Code = strings.TrimSpace(r.Name), strings.ToUpper(strings.TrimSpace(r.Code))
	switch {
	case r.Name == "" || len(r.Name) > 150:
		return apperr.BadRequest("name wajib diisi (maks. 150 karakter)")
	case r.Code == "" || len(r.Code) > 50:
		return apperr.BadRequest("code wajib diisi (maks. 50 karakter)")
	case r.StudentQuota <= 0:
		return apperr.BadRequest("student_quota harus lebih besar dari 0")
	case r.Price < 0:
		return apperr.BadRequest("price tidak boleh negatif")
	}
	codes, err := normInstruments(r.InstrumentCodes)
	if err != nil {
		return err
	}
	r.InstrumentCodes = codes
	if r.BadgeColor == "" {
		r.BadgeColor = "indigo"
	}
	return nil
}

func codeTaken(db *gorm.DB, table any, code string, exclude uuid.UUID) bool {
	var n int64
	db.Model(table).Where("upper(code) = ? AND id <> ?", strings.ToUpper(code), exclude).Count(&n)
	return n > 0
}

func (ac *AdminController) GetPackages(c *gin.Context) {
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentPackage{})
	if p.Search != "" {
		like := likePattern(p.Search)
		q = q.Where("name ILIKE ? OR code ILIKE ?", like, like)
	}
	switch c.Query("status") {
	case "active":
		q = q.Where("is_active = true")
	case "inactive":
		q = q.Where("is_active = false")
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentPackage{}
	if err := q.Order("created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	listOK(c, rows, p, total)
}

func (ac *AdminController) CreatePackage(c *gin.Context) {
	var req packageReq
	if !bindJSON(c, &req) {
		return
	}
	if err := req.validate(); err != nil {
		respondError(c, err)
		return
	}
	if codeTaken(config.DB, &model.AssessmentPackage{}, req.Code, uuid.Nil) {
		respondError(c, errDuplicateCode)
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	pkg := model.AssessmentPackage{
		ID: uuid.New(), Name: req.Name, Code: req.Code, Description: req.Description, Price: req.Price,
		StudentQuota: req.StudentQuota, InstrumentCodes: req.InstrumentCodes, IsBundle: len(req.InstrumentCodes) > 1,
		IsActive: active, BadgeColor: req.BadgeColor,
	}
	if err := config.DB.Create(&pkg).Error; err != nil {
		if isUniqueViolation(err) {
			err = errDuplicateCode
		}
		respondError(c, err)
		return
	}
	if !active {
		config.DB.Exec(`UPDATE assessment_packages SET is_active = false WHERE id = ?`, pkg.ID)
		pkg.IsActive = false
	}
	dataOK(c, http.StatusCreated, pkg, "Paket asesmen berhasil dibuat")
}

func (ac *AdminController) UpdatePackage(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var req packageReq
	if !bindJSON(c, &req) {
		return
	}
	if err := req.validate(); err != nil {
		respondError(c, err)
		return
	}
	var pkg model.AssessmentPackage
	if err := config.DB.First(&pkg, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	if codeTaken(config.DB, &model.AssessmentPackage{}, req.Code, id) {
		respondError(c, errDuplicateCode)
		return
	}
	pkg.Name, pkg.Code, pkg.Description, pkg.Price = req.Name, req.Code, req.Description, req.Price
	pkg.StudentQuota, pkg.InstrumentCodes, pkg.IsBundle, pkg.BadgeColor = req.StudentQuota, req.InstrumentCodes, len(req.InstrumentCodes) > 1, req.BadgeColor
	if req.IsActive != nil {
		pkg.IsActive = *req.IsActive
	}
	if err := config.DB.Save(&pkg).Error; err != nil {
		if isUniqueViolation(err) {
			err = errDuplicateCode
		}
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, pkg, "Paket berhasil diperbarui")
}

type statusReq struct {
	IsActive *bool `json:"is_active"`
}

// readStatus: { is_active } sets the flag; an empty body toggles it.
func readStatus(c *gin.Context) (*bool, bool) {
	var req statusReq
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !strings.Contains(err.Error(), "EOF") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "is_active harus boolean", "code": "VALIDATION_ERROR"})
			return nil, false
		}
	}
	return req.IsActive, true
}

func (ac *AdminController) TogglePackageStatus(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	set, ok := readStatus(c)
	if !ok {
		return
	}
	var pkg model.AssessmentPackage
	if err := config.DB.First(&pkg, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	pkg.IsActive = !pkg.IsActive
	if set != nil {
		pkg.IsActive = *set
	}
	if err := config.DB.Model(&pkg).Update("is_active", pkg.IsActive).Error; err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Status paket berhasil diubah", "data": pkg, "is_active": pkg.IsActive})
}

// ---------- Vouchers ----------

type voucherReq struct {
	Code                 string     `json:"code"`
	Name                 string     `json:"name"`
	DiscountType         string     `json:"discount_type"`
	DiscountValue        float64    `json:"discount_value"`
	MaxUsage             int        `json:"max_usage"`
	ValidFrom            *time.Time `json:"valid_from"`
	ValidUntil           *time.Time `json:"valid_until"`
	ApplicablePackageIDs []string   `json:"applicable_package_ids"`
}

func (ac *AdminController) GetVouchers(c *gin.Context) {
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentVoucher{})
	if p.Search != "" {
		like := likePattern(p.Search)
		q = q.Where("name ILIKE ? OR code ILIKE ?", like, like)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentVoucher{}
	if err := q.Order("created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	listOK(c, rows, p, total)
}

func (ac *AdminController) CreateVoucher(c *gin.Context) {
	var req voucherReq
	if !bindJSON(c, &req) {
		return
	}
	req.Code, req.Name = strings.ToUpper(strings.TrimSpace(req.Code)), strings.TrimSpace(req.Name)
	from, until := time.Now(), time.Now().Add(90*24*time.Hour)
	if req.ValidFrom != nil {
		from = *req.ValidFrom
	}
	if req.ValidUntil != nil {
		until = *req.ValidUntil
	}
	var verr error
	switch {
	case req.Code == "" || len(req.Code) > 50:
		verr = apperr.BadRequest("code wajib diisi (maks. 50 karakter)")
	case req.Name == "" || len(req.Name) > 150:
		verr = apperr.BadRequest("name wajib diisi (maks. 150 karakter)")
	case req.MaxUsage <= 0:
		verr = apperr.BadRequest("max_usage harus lebih besar dari 0")
	case !until.After(from):
		verr = apperr.BadRequest("valid_until harus setelah valid_from")
	}
	if verr == nil {
		switch req.DiscountType {
		case "free_claim":
		case "percentage":
			if req.DiscountValue < 1 || req.DiscountValue > 100 {
				verr = apperr.BadRequest("discount_value persentase harus 1–100")
			}
		case "fixed_amount":
			if req.DiscountValue <= 0 {
				verr = apperr.BadRequest("discount_value harus lebih besar dari 0")
			}
		default:
			verr = apperr.BadRequest("discount_type harus free_claim, percentage, atau fixed_amount")
		}
	}
	ids := []string{}
	for _, s := range req.ApplicablePackageIDs {
		if verr != nil {
			break
		}
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			verr = apperr.BadRequest("applicable_package_ids berisi id yang tidak valid")
			break
		}
		var n int64
		config.DB.Model(&model.AssessmentPackage{}).Where("id = ?", id).Count(&n)
		if n == 0 {
			verr = apperr.BadRequest("Paket pada applicable_package_ids tidak ditemukan")
			break
		}
		if !contains(ids, id.String()) {
			ids = append(ids, id.String())
		}
	}
	if verr != nil {
		respondError(c, verr)
		return
	}
	if codeTaken(config.DB, &model.AssessmentVoucher{}, req.Code, uuid.Nil) {
		respondError(c, errDuplicateCode)
		return
	}
	v := model.AssessmentVoucher{
		ID: uuid.New(), Code: req.Code, Name: req.Name, DiscountType: req.DiscountType, DiscountValue: req.DiscountValue,
		MaxUsage: req.MaxUsage, ValidFrom: from, ValidUntil: until, ApplicablePackageIDs: ids, IsActive: true,
	}
	if err := config.DB.Create(&v).Error; err != nil {
		if isUniqueViolation(err) {
			err = errDuplicateCode
		}
		respondError(c, err)
		return
	}
	if req.DiscountValue == 0 { // DB default is 100 (GORM skips zero values on INSERT)
		config.DB.Exec(`UPDATE assessment_vouchers SET discount_value = 0 WHERE id = ?`, v.ID)
		v.DiscountValue = 0
	}
	dataOK(c, http.StatusCreated, v, "Kode voucher berhasil dibuat")
}

func (ac *AdminController) ToggleVoucherStatus(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	set, ok := readStatus(c)
	if !ok {
		return
	}
	var v model.AssessmentVoucher
	if err := config.DB.First(&v, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	v.IsActive = !v.IsActive
	if set != nil {
		v.IsActive = *set
	}
	if err := config.DB.Model(&v).Update("is_active", v.IsActive).Error; err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Status voucher berhasil diubah", "data": v, "is_active": v.IsActive})
}

func (ac *AdminController) GetVoucherLogs(c *gin.Context) {
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentVoucherLog{})
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	logs := []model.AssessmentVoucherLog{}
	if err := q.Preload("Voucher").Order("claimed_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&logs).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(logs))
	for _, l := range logs {
		row := gin.H{"id": l.ID, "voucher_id": l.VoucherID, "school_id": l.SchoolID, "subscription_id": l.SubscriptionID, "claimed_at": l.ClaimedAt, "voucher": nil}
		if l.SchoolName != "" {
			row["school_name"] = l.SchoolName
		}
		if l.Voucher != nil {
			row["voucher"] = gin.H{"code": l.Voucher.Code, "name": l.Voucher.Name}
		}
		out = append(out, row)
	}
	listOK(c, out, p, total)
}

// ---------- Subscriptions ----------

func (ac *AdminController) GetSubscriptions(c *gin.Context) {
	p := parsePage(c)
	q := config.DB.Model(&model.SchoolAssessmentSubscription{})
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	if p.Search != "" {
		q = q.Where("school_name ILIKE ?", likePattern(p.Search))
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	subs := []model.SchoolAssessmentSubscription{}
	if err := q.Preload("Package").Preload("Voucher").Order("created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&subs).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(subs))
	for i := range subs {
		out = append(out, subscriptionView(&subs[i]))
	}
	listOK(c, out, p, total)
}

func (ac *AdminController) UpdateSubscriptionStatus(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.Status != model.SubStatusActive && req.Status != model.SubStatusRejected {
		respondError(c, apperr.BadRequest("status harus active atau rejected"))
		return
	}
	uid := middleware.UserID(c)
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var sub model.SchoolAssessmentSubscription
		if err := tx.First(&sub, "id = ?", id).Error; err != nil {
			return err
		}
		now := time.Now()
		upd := map[string]any{"status": req.Status, "reviewed_at": now, "reviewed_by_user_id": uid}
		if req.Status == model.SubStatusActive {
			upd["expires_at"] = now.Add(365 * 24 * time.Hour)
		}
		res := tx.Model(&model.SchoolAssessmentSubscription{}).Where("id = ? AND status = ?", id, model.SubStatusPending).Updates(upd)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.Conflict("INVALID_STATUS", "Hanya langganan berstatus pending yang dapat disetujui/ditolak")
		}
		if req.Status == model.SubStatusRejected && sub.VoucherID != nil {
			tx.Model(&model.AssessmentVoucher{}).Where("id = ? AND used_count > 0", *sub.VoucherID).Update("used_count", gorm.Expr("used_count - 1"))
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	var sub model.SchoolAssessmentSubscription
	config.DB.Preload("Package").Preload("Voucher").First(&sub, "id = ?", id)
	msg := "Langganan disetujui"
	if req.Status == model.SubStatusRejected {
		msg = "Langganan ditolak"
	}
	dataOK(c, http.StatusOK, subscriptionView(&sub), msg)
}

func (ac *AdminController) GetAdminStats(c *gin.Context) {
	db := config.DB
	count := func(m any, where string, args ...any) int64 {
		var n int64
		q := db.Model(m)
		if where != "" {
			q = q.Where(where, args...)
		}
		q.Count(&n)
		return n
	}
	mirrorOK(c, http.StatusOK, map[string]any{
		"total_packages":        count(&model.AssessmentPackage{}, ""),
		"active_packages":       count(&model.AssessmentPackage{}, "is_active = true"),
		"total_vouchers":        count(&model.AssessmentVoucher{}, ""),
		"active_vouchers":       count(&model.AssessmentVoucher{}, "is_active = true"),
		"total_claims":          count(&model.AssessmentVoucherLog{}, ""),
		"total_subscriptions":   count(&model.SchoolAssessmentSubscription{}, ""),
		"pending_subscriptions": count(&model.SchoolAssessmentSubscription{}, "status = ?", model.SubStatusPending),
		"total_attempts":        count(&model.AssessmentAttempt{}, ""),
		"completed_results":     count(&model.AssessmentResult{}, ""),
		"total_questions":       count(&model.AssessmentQuestionBank{}, "is_active = true"),
	})
}

// ---------- Question bank ----------

type questionReq struct {
	InstrumentCode    string   `json:"instrument_code"`
	Subtest           string   `json:"subtest"`
	ItemCode          string   `json:"item_code"`
	QuestionText      string   `json:"question_text"`
	QuestionType      string   `json:"question_type"`
	ScaleMin          int      `json:"scale_min"`
	ScaleMax          int      `json:"scale_max"`
	ScaleLabels       []string `json:"scale_labels"`
	Options           []string `json:"options"`
	CorrectKey        string   `json:"correct_key"`
	Dimension         string   `json:"dimension"`
	AttentionExpected *int     `json:"attention_expected"`
	Weight            float64  `json:"weight"`
	OrderNum          int      `json:"order_num"`
	IsActive          *bool    `json:"is_active"`
}

// toRow validates a question (§13.3) and fills q.
func (r *questionReq) toRow(q *model.AssessmentQuestionBank) error {
	r.InstrumentCode = strings.ToUpper(strings.TrimSpace(r.InstrumentCode))
	r.ItemCode = strings.ToUpper(strings.TrimSpace(r.ItemCode))
	r.Subtest, r.Dimension = strings.TrimSpace(r.Subtest), strings.TrimSpace(r.Dimension)
	r.QuestionText = strings.TrimSpace(r.QuestionText)
	if !scoring.IsInstrument(r.InstrumentCode) {
		return apperr.Invalid("INVALID_INSTRUMENT", "instrument_code harus TMB, RIA, PMK, atau ADN")
	}
	if r.ItemCode == "" || len(r.ItemCode) > 50 {
		return apperr.BadRequest("item_code wajib diisi (maks. 50 karakter)")
	}
	if r.QuestionText == "" {
		return apperr.BadRequest("question_text wajib diisi")
	}
	// RIA attention checks: empty dimension (scored = false, attention_expected required).
	attention := r.InstrumentCode == scoring.InstrumentRIA && (r.Subtest == scoring.SubRIAAttention || (r.Dimension == "" && r.Subtest == scoring.SubRIAMain))
	if attention {
		r.Subtest, r.Dimension = scoring.SubRIAAttention, ""
	}
	if !scoring.ValidSubtest(r.InstrumentCode, r.Subtest) {
		return apperr.BadRequest(fmt.Sprintf("subtest untuk %s harus salah satu dari: %s", r.InstrumentCode, strings.Join(scoring.SubtestsOf(r.InstrumentCode), ", ")))
	}
	if !scoring.ValidDimension(r.Subtest, r.Dimension) {
		return apperr.BadRequest(fmt.Sprintf("dimension untuk subtest %s harus salah satu dari: %s", r.Subtest, strings.Join(scoring.DimensionsOf(r.Subtest), ", ")))
	}
	if want := scoring.ExpectedType(r.Subtest); r.QuestionType != want {
		return apperr.BadRequest(fmt.Sprintf("question_type untuk subtest %s harus %s", r.Subtest, want))
	}
	labels := []string{}
	for _, l := range r.ScaleLabels {
		labels = append(labels, strings.TrimSpace(l))
	}
	opts := []string{}
	for _, o := range r.Options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	correct := ""
	switch r.QuestionType {
	case scoring.TypeLikert:
		if r.ScaleMin >= r.ScaleMax {
			return apperr.BadRequest("scale_min harus lebih kecil dari scale_max")
		}
		if len(labels) != r.ScaleMax-r.ScaleMin+1 {
			return apperr.BadRequest(fmt.Sprintf("scale_labels harus berisi %d label", r.ScaleMax-r.ScaleMin+1))
		}
		opts = []string{}
	case scoring.TypeBoolean:
		r.ScaleMin, r.ScaleMax = 0, 1
		if len(labels) == 0 {
			labels = []string{"Tidak", "Ya"}
		}
		if len(labels) != 2 {
			return apperr.BadRequest("scale_labels untuk boolean harus berisi 2 label")
		}
		opts = []string{}
	case scoring.TypeMultiple:
		if len(opts) < 2 || len(opts) > 5 || len(opts) != len(r.Options) {
			return apperr.BadRequest("options harus berisi 2–5 pilihan yang tidak kosong")
		}
		correct = strings.ToUpper(strings.TrimSpace(r.CorrectKey))
		valid := false
		for i := range opts {
			if scoring.OptionKey(i) == correct {
				valid = true
			}
		}
		if !valid {
			return apperr.BadRequest("correct_key harus salah satu pilihan yang tersedia")
		}
		r.ScaleMin, r.ScaleMax, labels = 0, 0, []string{}
	}
	var expected *int
	scored := true
	if attention {
		v := 3
		if r.AttentionExpected != nil {
			v = *r.AttentionExpected
		}
		if v < r.ScaleMin || v > r.ScaleMax {
			return apperr.BadRequest("attention_expected harus berada dalam rentang skala")
		}
		expected, scored = &v, false
	}
	weight := r.Weight
	if weight <= 0 {
		weight = 1
	}
	q.InstrumentCode, q.Subtest, q.ItemCode, q.QuestionText, q.QuestionType = r.InstrumentCode, r.Subtest, r.ItemCode, r.QuestionText, r.QuestionType
	q.ScaleMin, q.ScaleMax, q.ScaleLabels, q.Options, q.CorrectKey = r.ScaleMin, r.ScaleMax, labels, opts, correct
	q.Dimension, q.Scored, q.AttentionExpected, q.Weight, q.OrderNum = r.Dimension, scored, expected, weight, r.OrderNum
	if r.IsActive != nil {
		q.IsActive = *r.IsActive
	}
	return nil
}

func itemCodeTaken(code string, exclude uuid.UUID) bool {
	var n int64
	config.DB.Model(&model.AssessmentQuestionBank{}).Where("upper(item_code) = ? AND id <> ?", strings.ToUpper(code), exclude).Count(&n)
	return n > 0
}

func (ac *AdminController) GetQuestions(c *gin.Context) {
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentQuestionBank{})
	if code := strings.ToUpper(c.Query("instrument_code")); code != "" && code != "ALL" {
		q = q.Where("instrument_code = ?", code)
	}
	if st := c.Query("subtest"); st != "" {
		q = q.Where("subtest = ?", st)
	}
	switch c.Query("is_active") {
	case "true":
		q = q.Where("is_active = true")
	case "false":
		q = q.Where("is_active = false")
	}
	if p.Search != "" {
		like := likePattern(p.Search)
		q = q.Where("item_code ILIKE ? OR question_text ILIKE ? OR dimension ILIKE ?", like, like, like)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentQuestionBank{}
	if err := q.Order("instrument_code ASC, order_num ASC, item_code ASC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	listOK(c, rows, p, total)
}

func (ac *AdminController) GetQuestion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var q model.AssessmentQuestionBank
	if err := config.DB.First(&q, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, q, "")
}

func (ac *AdminController) CreateQuestion(c *gin.Context) {
	var req questionReq
	if !bindJSON(c, &req) {
		return
	}
	q := model.AssessmentQuestionBank{ID: uuid.New(), IsActive: true}
	if err := req.toRow(&q); err != nil {
		respondError(c, err)
		return
	}
	if itemCodeTaken(q.ItemCode, uuid.Nil) {
		respondError(c, apperr.Conflict("DUPLICATE_CODE", "item_code sudah digunakan"))
		return
	}
	if err := model.CreateQuestion(config.DB, &q); err != nil {
		if isUniqueViolation(err) {
			err = apperr.Conflict("DUPLICATE_CODE", "item_code sudah digunakan")
		}
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusCreated, q, "Butir soal berhasil ditambahkan ke bank soal")
}

func (ac *AdminController) UpdateQuestion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var q model.AssessmentQuestionBank
	if err := config.DB.First(&q, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	var req questionReq
	if !bindJSON(c, &req) {
		return
	}
	if err := req.toRow(&q); err != nil {
		respondError(c, err)
		return
	}
	if itemCodeTaken(q.ItemCode, q.ID) {
		respondError(c, apperr.Conflict("DUPLICATE_CODE", "item_code sudah digunakan"))
		return
	}
	if err := config.DB.Save(&q).Error; err != nil {
		if isUniqueViolation(err) {
			err = apperr.Conflict("DUPLICATE_CODE", "item_code sudah digunakan")
		}
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, q, "Butir soal berhasil diperbarui")
}

// DeleteQuestion: items with responses are deactivated (soft), others removed.
func (ac *AdminController) DeleteQuestion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var q model.AssessmentQuestionBank
	if err := config.DB.First(&q, "id = ?", id).Error; err != nil {
		respondError(c, err)
		return
	}
	var used int64
	config.DB.Model(&model.AssessmentResponse{}).Where("item_id = ?", q.ItemCode).Count(&used)
	if used == 0 {
		var inAttempt int64
		config.DB.Model(&model.AssessmentAttempt{}).Where("question_order @> ?::jsonb", fmt.Sprintf("[%q]", q.ItemCode)).Count(&inAttempt)
		used = inAttempt
	}
	if used > 0 {
		if err := config.DB.Model(&q).Update("is_active", false).Error; err != nil {
			respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Butir soal sudah dipakai, sehingga dinonaktifkan", "soft_deleted": true})
		return
	}
	if err := config.DB.Delete(&model.AssessmentQuestionBank{}, "id = ?", q.ID).Error; err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Butir soal berhasil dihapus dari bank soal", "soft_deleted": false})
}

var _ = errors.New
