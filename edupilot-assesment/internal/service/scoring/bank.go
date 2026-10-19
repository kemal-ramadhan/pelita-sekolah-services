package scoring

import "fmt"

// BankItem is a canonical question-bank row parsed from internal/data/<instrument>.json.
type BankItem struct {
	Item
	Instrument  string
	Text        string
	Options     []string
	ScaleLabels []string
	OrderNum    int
}

// InstrumentInfo is the static description of an instrument (onboarding).
type InstrumentInfo struct {
	Code            string        `json:"code"`
	Name            string        `json:"name"`
	Version         string        `json:"version"`
	Description     string        `json:"description"`
	EstimatedMinute int           `json:"estimated_minutes"`
	Sections        []SectionInfo `json:"sections"`
}

// SectionInfo describes one section of an attempt (contract §13.5).
type SectionInfo struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	Instruction      string   `json:"instruction"`
	ScaleLabels      []string `json:"scale_labels,omitempty"`
	TimeLimitSeconds int      `json:"time_limit_seconds,omitempty"`
}

type scaleJSON struct {
	Min    int      `json:"min"`
	Max    int      `json:"max"`
	Labels []string `json:"labels"`
}

type rawItem struct {
	ID       string   `json:"id"`
	Type     *string  `json:"type"`
	Domain   string   `json:"domain"`
	Subtest  string   `json:"subtest"`
	Modality string   `json:"modality"`
	Subscale string   `json:"subscale"`
	Text     string   `json:"text"`
	Options  []string `json:"options"`
	Key      string   `json:"key"`
	Scored   *bool    `json:"scored"`
	Expected *int     `json:"expected"`
}

type rawPart struct {
	Title       string    `json:"title"`
	Instruction string    `json:"instruction"`
	Scale       scaleJSON `json:"scale"`
	Items       []rawItem `json:"items"`
}

type rawInstrument struct {
	Code         string             `json:"code"`
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Instructions string             `json:"instructions"`
	Duration     int                `json:"duration_minutes"`
	Scale        scaleJSON          `json:"scale"`
	Items        []rawItem          `json:"items"`
	Part1Scale   scaleJSON          `json:"part1_scale"`
	Part1Items   []rawItem          `json:"part1_items"`
	Part2Items   []rawItem          `json:"part2_items"`
	Parts        map[string]rawPart `json:"parts"`
	Module1      rawPart            `json:"module1_gaya_belajar"`
	Module2      rawPart            `json:"module2_sosio_emosional"`
	Module3      rawPart            `json:"module3_minat_karier"`
}

var instrumentFiles = map[string]string{
	InstrumentTMB: "tmb.json", InstrumentRIA: "riasec.json", InstrumentPMK: "pmk.json", InstrumentADN: "adn.json",
}

// Boolean answer labels (stored with scale 0..1).
var pmkBoolLabels = map[string][]string{
	SubPMKA: {"Tidak", "Suka"},
	SubPMKB: {"Belum", "Bisa"},
	SubPMKC: {"Tidak", "Tertarik"},
}

func loadRaw(code string) (*rawInstrument, error) {
	f, ok := instrumentFiles[code]
	if !ok {
		return nil, fmt.Errorf("unknown instrument %q", code)
	}
	var r rawInstrument
	if err := unmarshalData(f, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CanonicalBank returns the PRD question bank of an instrument.
func CanonicalBank(code string) ([]BankItem, error) {
	r, err := loadRaw(code)
	if err != nil {
		return nil, err
	}
	out := []BankItem{}
	add := func(sub, dim, qtype string, sc scaleJSON, it rawItem, order int) {
		bi := BankItem{
			Item: Item{ID: it.ID, Subtest: sub, Dimension: dim, QuestionType: qtype, Scored: true,
				ScaleMin: sc.Min, ScaleMax: sc.Max},
			Instrument: code, Text: it.Text, ScaleLabels: sc.Labels, OrderNum: order,
		}
		if qtype == TypeMultiple {
			bi.Options, bi.CorrectKey, bi.OptionCount = it.Options, it.Key, len(it.Options)
			bi.ScaleMin, bi.ScaleMax, bi.ScaleLabels = 0, 0, []string{}
		}
		if qtype == TypeBoolean {
			bi.ScaleMin, bi.ScaleMax, bi.ScaleLabels = 0, 1, pmkBoolLabels[sub]
		}
		if it.Scored != nil && !*it.Scored {
			bi.Scored = false
		}
		if it.Expected != nil {
			v := *it.Expected
			bi.AttentionExpected = &v
			bi.Scored = false
			bi.Subtest = SubRIAAttention
			bi.Dimension = ""
		}
		out = append(out, bi)
	}
	switch code {
	case InstrumentTMB:
		for i, it := range r.Part1Items {
			add(SubTMBMinat, it.Domain, TypeLikert, r.Part1Scale, it, i+1)
		}
		for i, it := range r.Part2Items {
			add(SubTMBBakat, it.Subtest, TypeMultiple, scaleJSON{}, it, 100+i+1)
		}
	case InstrumentRIA:
		for i, it := range r.Items {
			dim := ""
			if it.Type != nil {
				dim = *it.Type
			}
			add(SubRIAMain, dim, TypeLikert, r.Scale, it, i+1)
		}
	case InstrumentPMK:
		for pi, key := range []string{"part_a", "part_b", "part_c", "part_d"} {
			p := r.Parts[key]
			sub := []string{SubPMKA, SubPMKB, SubPMKC, SubPMKD}[pi]
			qt := TypeBoolean
			if sub == SubPMKD {
				qt = TypeLikert
			}
			sc := p.Scale
			if sub == SubPMKD && len(sc.Labels) == 0 {
				sc.Labels = []string{"1 – Paling rendah", "2", "3", "4 – Rata-rata", "5", "6", "7 – Paling tinggi"}
			}
			for i, it := range p.Items {
				dim := ""
				if it.Type != nil {
					dim = *it.Type
				}
				add(sub, dim, qt, sc, it, (pi+1)*50+i+1)
			}
		}
	case InstrumentADN:
		for i, it := range r.Module1.Items {
			add(SubADNVark, it.Modality, TypeLikert, r.Module1.Scale, it, i+1)
		}
		for i, it := range r.Module2.Items {
			add(SubADNSocio, it.Subscale, TypeLikert, r.Module2.Scale, it, 50+i+1)
		}
		for i, it := range r.Module3.Items {
			dim := ""
			if it.Type != nil {
				dim = *it.Type
			}
			add(SubADNCareer, dim, TypeLikert, r.Module3.Scale, it, 100+i+1)
		}
	}
	return out, nil
}

// CanonicalItems = CanonicalBank without the display fields.
func CanonicalItems(code string) []Item {
	b, err := CanonicalBank(code)
	if err != nil {
		return nil
	}
	out := make([]Item, len(b))
	for i := range b {
		out[i] = b[i].Item
	}
	return out
}

// Info returns the static instrument description and sections.
func Info(code string) (*InstrumentInfo, error) {
	r, err := loadRaw(code)
	if err != nil {
		return nil, err
	}
	info := &InstrumentInfo{Code: code, Name: r.Name, Version: r.Version, Description: r.Instructions, EstimatedMinute: r.Duration}
	switch code {
	case InstrumentTMB:
		info.Sections = []SectionInfo{
			{Key: SubTMBMinat, Title: "Bagian 1 – Minat Bidang", Instruction: "Seberapa tertarik kamu mempelajari/melakukan hal berikut?", ScaleLabels: r.Part1Scale.Labels},
			{Key: SubTMBBakat, Title: "Bagian 2 – Bakat (Aptitude mini)", Instruction: "Pilih satu jawaban yang paling tepat. Waktu pengerjaan 20 menit, tanpa pengurangan nilai untuk jawaban salah.", TimeLimitSeconds: 1200},
		}
	case InstrumentRIA:
		info.Sections = []SectionInfo{{Key: SubRIAMain, Title: "Preferensi Aktivitas", Instruction: r.Instructions, ScaleLabels: r.Scale.Labels}}
	case InstrumentPMK:
		for i, key := range []string{"part_a", "part_b", "part_c", "part_d"} {
			p := r.Parts[key]
			sub := []string{SubPMKA, SubPMKB, SubPMKC, SubPMKD}[i]
			labels := pmkBoolLabels[sub]
			if sub == SubPMKD {
				labels = []string{"1 – Paling rendah", "2", "3", "4 – Rata-rata", "5", "6", "7 – Paling tinggi"}
			}
			info.Sections = append(info.Sections, SectionInfo{Key: sub, Title: p.Title, Instruction: p.Instruction, ScaleLabels: labels})
		}
	case InstrumentADN:
		info.Sections = []SectionInfo{
			{Key: SubADNVark, Title: r.Module1.Title, Instruction: "Seberapa sering pernyataan berikut menggambarkan dirimu?", ScaleLabels: r.Module1.Scale.Labels},
			{Key: SubADNSocio, Title: r.Module2.Title, Instruction: "Seberapa setuju kamu dengan pernyataan berikut?", ScaleLabels: r.Module2.Scale.Labels},
			{Key: SubADNCareer, Title: r.Module3.Title, Instruction: "Seberapa tertarik kamu dengan hal berikut?", ScaleLabels: r.Module3.Scale.Labels},
		}
	}
	return info, nil
}
