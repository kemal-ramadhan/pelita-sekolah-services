package scoring

import (
	"fmt"
	"strings"
)

// Section of an interpretation (contract §13.6).
type Section struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Items []string `json:"items,omitempty"`
}

// Interpretation (contract §13.6).
type Interpretation struct {
	Summary   string    `json:"summary"`
	Sections  []Section `json:"sections"`
	Careers   []Career  `json:"careers,omitempty"`
	StudyTips []string  `json:"study_tips,omitempty"`
}

// Outcome is the full, staff-level scoring result of one attempt.
type Outcome struct {
	InstrumentCode   string
	HollandCode      string
	VarkCategory     string
	RecommendedTrack string
	Scores           map[string]any
	Indices          map[string]any
	Recommendations  map[string]any
	Flags            []string
	ValidityFlags    []string
	ReportSummary    string
	Interpretation   Interpretation
	StaffNotes       []string
	WelfareFlag      bool
	ConfigVersion    string
}

// Evaluate scores one attempt deterministically (pure function, PRD §15).
func Evaluate(code string, in Input, cfg *Config) (*Outcome, error) {
	if in.Answers == nil {
		in.Answers = map[string]string{}
	}
	var out *Outcome
	switch code {
	case InstrumentRIA:
		out = buildRIA(ScoreRIASEC(in.Items, in.Answers, cfg), cfg)
	case InstrumentTMB:
		out = buildTMB(ScoreTMB(in.Items, in.Answers, cfg), cfg)
	case InstrumentPMK:
		out = buildPMK(ScorePMK(in.Items, in.Answers, cfg), cfg)
	case InstrumentADN:
		out = buildADN(ScoreADN(in.Items, in.Answers, cfg), cfg)
	default:
		return nil, fmt.Errorf("unknown instrument %q", code)
	}
	out.InstrumentCode = code
	out.ConfigVersion = cfg.ConfigVersion()
	for _, f := range ResponseValidity(code, in, cfg) {
		if !contains(out.ValidityFlags, f) {
			out.ValidityFlags = append(out.ValidityFlags, f)
		}
	}
	if notes := validityNotes(out.ValidityFlags); len(notes) > 0 {
		out.Interpretation.Sections = append(out.Interpretation.Sections, Section{Title: "Catatan validitas jawaban", Body: notes[0], Items: notes[1:]})
	}
	if out.Flags == nil {
		out.Flags = []string{}
	}
	if out.StaffNotes == nil {
		out.StaffNotes = []string{}
	}
	return out, nil
}

func validityNotes(flags []string) []string {
	out := []string{}
	for _, f := range flags {
		if t, ok := ValidityText[f]; ok {
			out = append(out, t)
		}
	}
	return out
}

func typeSections(code string) []Section {
	out := []Section{}
	for i := 0; i < len(code); i++ {
		t := code[i : i+1]
		ti, ok := TypeInfos[t]
		if !ok {
			continue
		}
		out = append(out, Section{
			Title: TypeLabel(t),
			Body:  ti.Desc,
			Items: []string{
				"Mapel/ekskul yang relevan: " + ti.Subjects,
				"Contoh studi lanjut & karier: " + ti.Careers,
				"Gaya lingkungan yang cocok: " + ti.Environment,
				"Pengembangan: " + ti.Development,
			},
		})
	}
	return out
}

func themeSection(code string) *Section {
	if len(code) < 2 {
		return nil
	}
	items := []string{}
	pairs := [][2]int{{0, 1}, {0, 2}, {1, 2}}
	for _, p := range pairs {
		if p[1] >= len(code) {
			continue
		}
		a, b := code[p[0]:p[0]+1], code[p[1]:p[1]+1]
		if th := CombinationTheme(a, b); th != "" {
			items = append(items, a+b+": "+th)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &Section{Title: "Tema kombinasi minat", Body: "Kombinasi tipe teratasmu sering dijumpai pada bidang berikut (contoh, bukan batasan):", Items: items}
}

func consistencyText(code, consistency string, flat bool) string {
	switch {
	case flat:
		return tplFlat
	case consistency == "Rendah" && len(code) >= 2:
		return fmt.Sprintf(tplBroad, "mis. "+TypeInfos[code[0:1]].Name+" + "+TypeInfos[code[1:2]].Name)
	case consistency == "Tinggi":
		return tplFocused
	}
	return tplMixed
}

func typeNames(code string) string {
	parts := []string{}
	for i := 0; i < len(code); i++ {
		t := code[i : i+1]
		parts = append(parts, TypeInfos[t].Name+" ("+TypeInfos[t].Nick+")")
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " dan " + parts[len(parts)-1]
}

func careerTitles(cs []Career, n int) string {
	t := []string{}
	for i := 0; i < len(cs) && i < n; i++ {
		t = append(t, cs[i].Title)
	}
	return strings.Join(t, ", ")
}

// ---------- RIA ----------

func buildRIA(r *RIASECResult, cfg *Config) *Outcome {
	flat := contains(r.ValidityFlags, FlagFlatProfile) || r.DiffCategory == "Profil datar"
	careers := FindCareers(r.HollandCode, cfg)
	cons := consistencyText(r.HollandCode, r.Consistency, flat)
	summary := fmt.Sprintf("Kode Holland-mu %s: %s. %s", r.CodeDisplay, typeNames(r.HollandCode), cons)
	secs := typeSections(r.HollandCode)
	if th := themeSection(r.HollandCode); th != nil {
		secs = append(secs, *th)
	}
	secs = append(secs,
		Section{Title: "Konsistensi & diferensiasi", Body: cons, Items: []string{
			"Konsistensi (huruf 1 & 2): " + r.Consistency,
			fmt.Sprintf("Diferensiasi: %s (selisih skor tertinggi–terendah %.0f)", r.DiffCategory, r.Differentiation),
		}},
		Section{Title: "Dimensi minat", Body: dimensionText(r.ThingsPeople, r.DataIdeas)},
	)
	if len(careers) > 0 {
		secs = append(secs, Section{Title: "Bidang yang selaras", Body: "Pekerjaan dan program studi berikut memiliki kecocokan kode (Indeks C) tertinggi dengan kodemu. Daftar ini adalah titik awal eksplorasi, bukan batasan."})
	}
	flags := []string{}
	if r.TieFlag {
		flags = append(flags, FlagTie)
	}
	return &Outcome{
		HollandCode: r.HollandCode,
		Scores:      map[string]any{"raw_scores": r.Raw, "percentages": r.Pct, "levels": r.Levels, "ipsative_scores": r.Ipsative},
		Indices: map[string]any{
			"order": r.Order, "code_display": r.CodeDisplay, "tie_flag": r.TieFlag,
			"consistency": r.Consistency, "consistency_distance": r.ConsistencyDistance,
			"differentiation": r.Differentiation, "differentiation_category": r.DiffCategory,
			"amplitude": r.Amplitude, "things_people": r.ThingsPeople, "data_ideas": r.DataIdeas,
			"ranking_basis": r.RankingBasis, "attention_failed": r.AttentionFailed,
		},
		Recommendations: map[string]any{"careers": careers},
		Flags:           flags,
		ValidityFlags:   append([]string{}, r.ValidityFlags...),
		ReportSummary:   fmt.Sprintf("Kode Holland %s · konsistensi %s · diferensiasi %s", r.CodeDisplay, r.Consistency, r.DiffCategory),
		Interpretation:  Interpretation{Summary: summary, Sections: secs, Careers: careers},
	}
}

func dimensionText(tp, di float64) string {
	a := "seimbang antara benda/alat dan orang"
	if tp > 0 {
		a = "lebih condong ke benda/alat"
	} else if tp < 0 {
		a = "lebih condong ke orang"
	}
	b := "seimbang antara data/prosedur dan ide/konsep"
	if di > 0 {
		b = "lebih condong ke data/prosedur"
	} else if di < 0 {
		b = "lebih condong ke ide/konsep"
	}
	return fmt.Sprintf("Pada dimensi Things–People minatmu %s (%.1f); pada dimensi Data–Ideas %s (%.1f). Dimensi ini menggambarkan arah minat, bukan kemampuan.", a, tp, b, di)
}

// ---------- TMB ----------

func buildTMB(r *TMBResult, cfg *Config) *Outcome {
	var summary string
	secs := []Section{}
	if r.Recommended == "" {
		summary = "Profil minat dan bakatmu belum menunjukkan rumpun yang menonjol. Coba Asesmen Preferensi Karier (RIASEC) dan Tes Penelusuran Minat Karier, lalu diskusikan hasilnya bersama Guru BK."
	} else {
		top := cfg.Weights.Rumpun[r.Recommended]
		summary = fmt.Sprintf("Rumpun yang paling selaras denganmu saat ini adalah %s (skor %.0f, %s).", top.Name, r.Rumpun[r.Recommended], r.RumpunCategory[r.Recommended])
		if r.EqualAlternatives && len(r.Alternatives) > 1 {
			alt := cfg.Weights.Rumpun[r.Alternatives[1]]
			summary += fmt.Sprintf(" Skor %s hampir sama, sehingga keduanya ditampilkan sebagai dua alternatif setara.", alt.Name)
		}
	}
	shown := 3
	for i, name := range r.Ranking {
		if i >= shown {
			break
		}
		spec := cfg.Weights.Rumpun[name]
		cat := r.RumpunCategory[name]
		body := RumpunCategoryText[cat]
		if cat != "Kurang Sesuai" {
			body = spec.Description + " " + body
		}
		items := []string{"Contoh studi lanjut/karier (contoh, bukan batasan): " + strings.Join(spec.Careers, ", ")}
		for _, f := range r.RumpunFlags[name] {
			items = append(items, tmbFlagText(f, spec.Name))
		}
		secs = append(secs, Section{Title: fmt.Sprintf("%s — %s (%.0f)", spec.Name, cat, r.Rumpun[name]), Body: body, Items: items})
	}
	minat := []string{}
	for _, d := range tmbDomains {
		minat = append(minat, fmt.Sprintf("%s: %.0f%%", tmbDomainNames[d], r.MinatPct[d]))
	}
	bakat := []string{}
	for _, x := range tmbSubtests {
		bakat = append(bakat, fmt.Sprintf("%s: %s (%d/%d benar)", tmbSubtestNames[x], r.BakatCategory[x], r.BakatCorrect[x], r.BakatTotal[x]))
	}
	secs = append(secs,
		Section{Title: "Minat bidang", Body: "Persentase ketertarikanmu pada enam bidang.", Items: minat},
		Section{Title: "Bakat (skrining)", Body: "Subtes bakat masih singkat sehingga hanya dipakai sebagai skrining awal.", Items: bakat},
	)
	if contains(r.Flags, FlagProfileUnclear) {
		secs = append(secs, Section{Title: "Langkah selanjutnya", Body: "Lanjutkan dengan Asesmen Preferensi Karier (RIASEC) dan Tes Penelusuran Minat Karier, serta sesi konseling bersama Guru BK."})
	}
	track := r.Recommended
	report := "Profil belum jelas — disarankan tes lanjutan & konseling"
	if track != "" {
		report = fmt.Sprintf("Rekomendasi rumpun: %s (%s)", cfg.Weights.Rumpun[track].Name, r.RumpunCategory[track])
		if r.EqualAlternatives && len(r.Alternatives) > 1 {
			report += " · alternatif setara: " + cfg.Weights.Rumpun[r.Alternatives[1]].Name
		}
	}
	return &Outcome{
		RecommendedTrack: track,
		Scores: map[string]any{
			"minat_scores": r.MinatPct, "minat_sum": r.MinatSum,
			"bakat_scores": r.BakatCorrect, "bakat_total": r.BakatTotal, "bakat_percentages": r.BakatPct, "bakat_categories": r.BakatCategory,
			"rumpun_scores": r.Rumpun, "rumpun_categories": r.RumpunCategory,
		},
		Indices: map[string]any{"ranking": r.Ranking, "equal_alternatives": r.EqualAlternatives, "rumpun_flags": r.RumpunFlags},
		Recommendations: map[string]any{
			"primary": track, "alternatives": r.Alternatives, "equal_alternatives": r.EqualAlternatives,
		},
		Flags:          append([]string{}, r.Flags...),
		ValidityFlags:  []string{},
		ReportSummary:  report,
		Interpretation: Interpretation{Summary: summary, Sections: secs},
	}
}

func tmbFlagText(flag, rumpun string) string {
	switch flag {
	case FlagSkillBuilding:
		return "Minatmu pada rumpun " + rumpun + " tinggi, sementara kemampuan terkait masih dapat dikembangkan. Latihan rutin dan pendampingan belajar dapat membantu."
	case FlagExploreInterest:
		return "Kemampuanmu pada bidang terkait rumpun " + rumpun + " menonjol, namun minatmu belum tinggi. Coba eksplorasi kegiatan di bidang ini untuk mengenal minatmu lebih jauh."
	}
	return ""
}

// ---------- PMK ----------

func buildPMK(r *PMKResult, cfg *Config) *Outcome {
	careers := FindCareers(r.HollandCode, cfg)
	descs := []string{}
	for i := 0; i < len(r.HollandCode); i++ {
		descs = append(descs, TypeInfos[r.HollandCode[i:i+1]].Short)
	}
	summary := fmt.Sprintf("Kode kariermu %s. Artinya kamu cenderung %s", r.CodeDisplay, descs[0])
	if len(descs) == 3 {
		summary += fmt.Sprintf(", didukung sifat %s serta %s", descs[1], descs[2])
	}
	summary += "."
	if len(careers) > 0 {
		summary += " Beberapa bidang yang selaras: " + careerTitles(careers, 3) + "."
	}
	summary += " Kode ini adalah titik awal eksplorasi, bukan batasan."

	flat := r.DiffCategory == "Datar"
	cons := consistencyText(r.HollandCode, r.Consistency, flat)
	secs := typeSections(r.HollandCode)
	if th := themeSection(r.HollandCode); th != nil {
		secs = append(secs, *th)
	}
	secs = append(secs, Section{Title: "Konsistensi & diferensiasi", Body: cons, Items: []string{
		"Konsistensi (huruf 1 & 2): " + r.Consistency,
		fmt.Sprintf("Diferensiasi: %s (selisih total tertinggi–terendah %d)", r.DiffCategory, r.Differentiation),
	}})
	tri := []string{}
	for _, t := range riasecTypes {
		switch r.GapFlags[t] {
		case FlagInterestOverSkill:
			tri = append(tri, TypeInfos[t].Name+": minat lebih tinggi daripada kompetensi saat ini — peluang untuk berlatih dan mengikuti pelatihan.")
		case FlagSkillOverInterest:
			tri = append(tri, TypeInfos[t].Name+": kompetensi tinggi tetapi minat rendah — perhatikan rasa bosan dan eksplorasi motivasimu.")
		}
	}
	for _, t := range r.ImageOverActivity {
		tri = append(tri, TypeInfos[t].Name+": tertarik pada citra pekerjaan, belum pada aktivitas inti — eksplorasi lewat magang/wawancara profesi.")
	}
	if len(tri) > 0 {
		secs = append(secs, Section{Title: "Analisis triangulasi minat–kompetensi", Body: "Perbandingan minat (aktivitas & pekerjaan) dengan kompetensi (kemampuan & estimasi diri):", Items: tri})
	}
	if len(careers) > 0 {
		secs = append(secs, Section{Title: "Occupations Finder", Body: "Pekerjaan/program studi dengan Indeks C tertinggi terhadap kodemu (contoh, bukan batasan)."})
	}
	flags := append([]string{}, r.Flags...)
	if r.TieFlag {
		flags = append(flags, FlagTie)
	}
	return &Outcome{
		HollandCode: r.HollandCode,
		Scores: map[string]any{
			"part_a": r.A, "part_b": r.B, "part_c": r.C, "part_d": r.D, "total_scores": r.Total, "percentages": r.Pct,
			"minat_pct": r.MinatPct, "kompetensi_pct": r.KompetensiPct,
		},
		Indices: map[string]any{
			"order": r.Order, "code_display": r.CodeDisplay, "tie_flag": r.TieFlag, "consistency": r.Consistency,
			"differentiation": r.Differentiation, "differentiation_category": r.DiffCategory,
			"gap": r.Gap, "gap_flags": r.GapFlags, "image_over_activity": r.ImageOverActivity,
		},
		Recommendations: map[string]any{"careers": careers},
		Flags:           flags,
		ValidityFlags:   []string{},
		ReportSummary:   fmt.Sprintf("Kode ringkasan %s · konsistensi %s · diferensiasi %s", r.CodeDisplay, r.Consistency, r.DiffCategory),
		Interpretation:  Interpretation{Summary: summary, Sections: secs, Careers: careers},
	}
}

// ---------- ADN ----------

func buildADN(r *ADNResult, cfg *Config) *Outcome {
	secs := []Section{}
	tips := []string{}
	var styleBody string
	switch r.VarkType {
	case VarkUnclear:
		styleBody = "Preferensi belajarmu belum terlihat jelas. Coba beragam cara belajar dan perhatikan cara mana yang paling membantumu."
		tips = append(tips, VarkInfos["multimodal"].Strategies)
	case VarkUnimodal:
		m := r.VarkDominant[0]
		styleBody = fmt.Sprintf("%s: %s.", r.VarkCategory, strings.ToLower(VarkInfos[m].Interpretation[:1])+VarkInfos[m].Interpretation[1:])
		tips = append(tips, ModalityNames[m]+": "+VarkInfos[m].Strategies)
	default:
		styleBody = fmt.Sprintf("%s: kamu %s.", r.VarkCategory, strings.ToLower(VarkInfos["multimodal"].Interpretation))
		for _, m := range r.VarkDominant {
			tips = append(tips, ModalityNames[m]+": "+VarkInfos[m].Strategies)
		}
		tips = append(tips, "Multimodal: "+VarkInfos["multimodal"].Strategies)
	}
	vark := []string{}
	for _, m := range varkOrder {
		vark = append(vark, fmt.Sprintf("%s: %d", ModalityNames[m], r.Vark[m]))
	}
	secs = append(secs, Section{Title: "Gaya belajar", Body: styleBody + " " + LearningStyleNote, Items: append(vark, "Strategi yang dapat dicoba (bukan batasan): "+strings.Join(tips, " · "))})

	socio := []string{}
	notes := []string{}
	staff := []string{}
	for _, sub := range socioOrder {
		lvl, ok := r.SocioLevels[sub]
		if !ok {
			continue
		}
		socio = append(socio, fmt.Sprintf("%s: %s", SocioNames[sub], lvl))
	}
	if r.SocioLevels[SocioMotivasi] == LevelStrong || r.SocioLevels[SocioEfikasi] == LevelStrong {
		notes = append(notes, tplMotivationStrong)
	}
	if r.SocioLevels[SocioRegulasi] == LevelSupport {
		notes = append(notes, tplRegulationNeeds)
	}
	if contains(r.Flags, FlagAnxietyHigh) {
		notes = append(notes, tplAnxietyHigh)
	}
	if r.SocioLevels[SocioDukungan] == LevelSupport {
		staff = append(staff, tplSocialLowStaff)
	}
	if r.WelfareFlag {
		staff = append(staff, tplWelfareStaff)
	}
	body := "Gambaran kebiasaan belajar dan kondisi belajarmu saat ini."
	if len(notes) > 0 {
		body = strings.Join(notes, " ")
	}
	secs = append(secs, Section{Title: "Kebiasaan belajar & sosio-emosional", Body: body, Items: socio})

	brief := []string{}
	for i := 0; i < len(r.BriefCode); i++ {
		t := r.BriefCode[i : i+1]
		brief = append(brief, fmt.Sprintf("%s (%s): %s", TypeLabel(t), r.BriefLevels[t], TypeInfos[t].Desc))
	}
	secs = append(secs, Section{Title: "Minat karier singkat — " + r.BriefDisplay, Body: tplBriefNote, Items: brief})

	flags := append([]string{}, r.Flags...)
	if r.BriefTie {
		flags = append(flags, FlagTie)
	}
	summary := fmt.Sprintf("Preferensi belajarmu: %s. Minat karier singkat: %s. %s", r.VarkCategory, r.BriefDisplay, LearningStyleNote)
	return &Outcome{
		HollandCode:  r.BriefCode,
		VarkCategory: r.VarkCategory,
		Scores: map[string]any{
			"vark_scores": r.Vark, "socio_emotional_scores": r.Socio, "socio_emotional_levels": r.SocioLevels,
			"brief_career_scores": r.Brief, "brief_levels": r.BriefLevels, "wellbeing_items": r.Welfare,
		},
		Indices: map[string]any{
			"vark_type": r.VarkType, "vark_dominant": r.VarkDominant, "brief_order": r.BriefOrder,
			"brief_code_display": r.BriefDisplay, "tie_flag": r.BriefTie,
		},
		Recommendations: map[string]any{"vark_dominant": r.VarkDominant, "study_tips": tips, "staff_notes": staff},
		Flags:           flags,
		ValidityFlags:   []string{},
		ReportSummary:   fmt.Sprintf("Gaya belajar: %s · minat singkat %s", r.VarkCategory, r.BriefDisplay),
		Interpretation:  Interpretation{Summary: summary, Sections: secs, StudyTips: tips},
		StaffNotes:      staff,
		WelfareFlag:     r.WelfareFlag,
	}
}
