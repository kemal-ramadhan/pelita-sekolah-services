package dto

import "strings"

// Option is one enum entry served by GET /instantion/school/references (API_CONTRACT §8.1).
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Reference keys. These enum lists are the single source of truth: every enum
// field of the academic/student API is validated against them, and the import
// template's "Referensi" sheet is generated from them.
const (
	RefReligions         = "religions"
	RefGenders           = "genders"
	RefBloodTypes        = "blood_types"
	RefCitizenships      = "citizenships"
	RefSpecialNeeds      = "special_needs"
	RefResidenceTypes    = "residence_types"
	RefTransportations   = "transportations"
	RefEducationLevels   = "education_levels"
	RefOccupations       = "occupations"
	RefIncomes           = "incomes"
	RefRegistrationTypes = "registration_types"
	RefStudentStatuses   = "student_statuses"
	RefHistoryStatuses   = "history_statuses"
	RefEducationStages   = "education_stages"
	RefCurricula         = "curricula"
	RefGuardianTypes     = "guardian_types"
	RefAchievementTypes  = "achievement_types"
	RefAchievementLevels = "achievement_levels"
	RefScholarshipTypes  = "scholarship_types"
	RefGradePresets      = "grade_presets"
	RefNumberings        = "numberings"
	RefPromotionActions  = "promotion_actions"
	// Subjects & schedules (not part of the student import "Referensi" sheet).
	RefSubjectTypes   = "subject_types"
	RefSubjectPresets = "subject_presets"
	RefPeriodPresets  = "period_presets"
	// RefDocumentTypes is per school (rows of student_document_types), added by the service.
	RefDocumentTypes = "document_types"
)

// Student statuses (student_statuses).
const (
	StudentStatusActive         = "active"
	StudentStatusGraduated      = "graduated"
	StudentStatusTransferredOut = "transferred_out"
	StudentStatusDroppedOut     = "dropped_out"
	StudentStatusDeceased       = "deceased"
	StudentStatusInactive       = "inactive"
)

// Class history statuses (history_statuses).
const (
	HistoryActive         = "active"
	HistoryMoved          = "moved"
	HistoryPromoted       = "promoted"
	HistoryRetained       = "retained"
	HistoryGraduated      = "graduated"
	HistoryTransferredOut = "transferred_out"
	HistoryWithdrawn      = "withdrawn"
)

// Guardian types.
const (
	GuardianFather   = "father"
	GuardianMother   = "mother"
	GuardianGuardian = "guardian"
)

var references = map[string][]Option{
	RefReligions: {
		{"islam", "Islam"}, {"kristen", "Kristen"}, {"katolik", "Katolik"}, {"hindu", "Hindu"},
		{"buddha", "Buddha"}, {"konghucu", "Konghucu"}, {"kepercayaan", "Kepercayaan kepada Tuhan YME"},
	},
	RefGenders:    {{"L", "Laki-laki"}, {"P", "Perempuan"}},
	RefBloodTypes: {{"A", "A"}, {"B", "B"}, {"AB", "AB"}, {"O", "O"}, {"unknown", "Tidak diketahui"}},
	RefCitizenships: {
		{"WNI", "Warga Negara Indonesia"}, {"WNA", "Warga Negara Asing"},
	},
	RefSpecialNeeds: {
		{"none", "Tidak ada"}, {"A", "A - Tunanetra"}, {"B", "B - Tunarungu"}, {"C", "C - Tunagrahita ringan"},
		{"C1", "C1 - Tunagrahita sedang"}, {"D", "D - Tunadaksa ringan"}, {"D1", "D1 - Tunadaksa sedang"},
		{"E", "E - Tunalaras"}, {"F", "F - Tunawicara"}, {"H", "H - Hiperaktif"}, {"I", "I - Cerdas istimewa"},
		{"J", "J - Bakat istimewa"}, {"K", "K - Kesulitan belajar"}, {"N", "N - Narkoba"}, {"O", "O - Indigo"},
		{"P", "P - Down syndrome"}, {"Q", "Q - Autis"},
	},
	RefResidenceTypes: {
		{"orang_tua", "Bersama orang tua"}, {"wali", "Wali"}, {"kos", "Kos"}, {"asrama", "Asrama"},
		{"panti_asuhan", "Panti asuhan"}, {"pesantren", "Pesantren"}, {"lainnya", "Lainnya"},
	},
	RefTransportations: {
		{"jalan_kaki", "Jalan kaki"}, {"sepeda", "Sepeda"}, {"sepeda_motor", "Sepeda motor"},
		{"mobil_pribadi", "Mobil pribadi"}, {"antar_jemput_sekolah", "Antar jemput sekolah"},
		{"angkutan_umum", "Angkutan umum"}, {"ojek", "Ojek"}, {"perahu", "Perahu penyeberangan"},
		{"kereta_api", "Kereta api"}, {"lainnya", "Lainnya"},
	},
	RefEducationLevels: {
		{"tidak_sekolah", "Tidak sekolah"}, {"putus_sd", "Putus SD"}, {"sd", "SD sederajat"},
		{"smp", "SMP sederajat"}, {"sma", "SMA sederajat"}, {"d1", "D1"}, {"d2", "D2"}, {"d3", "D3"},
		{"d4", "D4"}, {"s1", "S1"}, {"s2", "S2"}, {"s3", "S3"}, {"profesi", "Profesi"},
	},
	RefOccupations: {
		{"tidak_bekerja", "Tidak bekerja"}, {"nelayan", "Nelayan"}, {"petani", "Petani"}, {"peternak", "Peternak"},
		{"pns", "PNS"}, {"tni_polri", "TNI/Polri"}, {"guru_dosen", "Guru/Dosen"}, {"pegawai_swasta", "Karyawan swasta"},
		{"wiraswasta", "Wiraswasta"}, {"pedagang", "Pedagang"}, {"buruh", "Buruh"}, {"pensiunan", "Pensiunan"},
		{"tenaga_kesehatan", "Tenaga kesehatan"}, {"ibu_rumah_tangga", "Ibu rumah tangga"},
		{"sudah_meninggal", "Sudah meninggal"}, {"lainnya", "Lainnya"},
	},
	RefIncomes: {
		{"tidak_berpenghasilan", "Tidak berpenghasilan"}, {"lt_500k", "Kurang dari Rp 500.000"},
		{"500k_999k", "Rp 500.000 - Rp 999.999"}, {"1m_1_99m", "Rp 1.000.000 - Rp 1.999.999"},
		{"2m_4_99m", "Rp 2.000.000 - Rp 4.999.999"}, {"5m_20m", "Rp 5.000.000 - Rp 20.000.000"},
		{"gt_20m", "Lebih dari Rp 20.000.000"},
	},
	RefRegistrationTypes: {
		{"siswa_baru", "Siswa baru"}, {"pindahan", "Pindahan"}, {"kembali_bersekolah", "Kembali bersekolah"},
	},
	RefStudentStatuses: {
		{StudentStatusActive, "Aktif"}, {StudentStatusGraduated, "Lulus"}, {StudentStatusTransferredOut, "Mutasi keluar"},
		{StudentStatusDroppedOut, "Putus sekolah"}, {StudentStatusDeceased, "Meninggal dunia"}, {StudentStatusInactive, "Tidak aktif"},
	},
	RefHistoryStatuses: {
		{HistoryActive, "Aktif"}, {HistoryMoved, "Pindah kelas"}, {HistoryPromoted, "Naik kelas"},
		{HistoryRetained, "Tinggal kelas"}, {HistoryGraduated, "Lulus"}, {HistoryTransferredOut, "Mutasi keluar"},
		{HistoryWithdrawn, "Keluar dari kelas"},
	},
	RefEducationStages: {
		{"TK", "TK"}, {"SD", "SD"}, {"MI", "MI"}, {"SMP", "SMP"}, {"MTs", "MTs"}, {"SMA", "SMA"},
		{"MA", "MA"}, {"SMK", "SMK"}, {"MAK", "MAK"}, {"SLB", "SLB"}, {"Lainnya", "Lainnya"},
	},
	RefCurricula: {
		{"merdeka", "Kurikulum Merdeka"}, {"k13", "Kurikulum 2013"}, {"ktsp", "KTSP"}, {"lainnya", "Lainnya"},
	},
	RefGuardianTypes: {{GuardianFather, "Ayah"}, {GuardianMother, "Ibu"}, {GuardianGuardian, "Wali"}},
	RefAchievementTypes: {
		{"akademik", "Akademik"}, {"non_akademik", "Non akademik"},
	},
	RefAchievementLevels: {
		{"sekolah", "Sekolah"}, {"kecamatan", "Kecamatan"}, {"kabupaten", "Kabupaten/Kota"},
		{"provinsi", "Provinsi"}, {"nasional", "Nasional"}, {"internasional", "Internasional"},
	},
	RefScholarshipTypes: {
		{"anak_berprestasi", "Anak berprestasi"}, {"anak_miskin", "Anak miskin"}, {"pendidikan", "Pendidikan"},
		{"unggulan", "Unggulan"}, {"lainnya", "Lainnya"},
	},
	RefGradePresets: {
		{"SD", "SD (Kelas 1-6)"}, {"MI", "MI (Kelas 1-6)"}, {"SMP", "SMP (Kelas 7-9)"}, {"MTs", "MTs (Kelas 7-9)"},
		{"SMP_LOCAL", "SMP (Kelas 1-3)"}, {"SMA", "SMA (Kelas 10-12)"}, {"MA", "MA (Kelas 10-12)"},
		{"SMA_LOCAL", "SMA (Kelas 1-3)"}, {"SMK", "SMK (Kelas 10-12)"}, {"SMK_4Y", "SMK 4 tahun (Kelas 10-13)"},
		{"SMK_LOCAL", "SMK (Kelas 1-3)"}, {"TK", "TK (Kelompok A-B)"},
	},
	RefNumberings:       {{"arabic", "Angka (7, 8, 9)"}, {"roman", "Romawi (VII, VIII, IX)"}},
	RefPromotionActions: {{"promote", "Naik kelas"}, {"retain", "Tinggal kelas"}, {"graduate", "Lulus"}},
	RefSubjectTypes: {
		{SubjectTypeWajibUmum, "Wajib Umum"}, {SubjectTypePeminatan, "Peminatan / Pilihan"},
		{SubjectTypeKejuruan, "Kejuruan"}, {SubjectTypeMuatanLokal, "Muatan Lokal"},
		{SubjectTypeEkstrakurikuler, "Ekstrakurikuler"},
	},
	RefSubjectPresets: {
		{"SD", "SD (Kurikulum Merdeka)"}, {"MI", "MI (Kurikulum Merdeka + PAI Madrasah)"},
		{"SMP", "SMP (Kurikulum Merdeka)"}, {"MTS", "MTs (Kurikulum Merdeka + PAI Madrasah)"},
		{"SMA", "SMA (Kurikulum Merdeka)"}, {"MA", "MA (Kurikulum Merdeka + PAI Madrasah)"},
		{"SMK", "SMK (Kurikulum Merdeka)"},
	},
	RefPeriodPresets: {
		{"35_MIN", "35 menit / JP (SD/MI)"}, {"40_MIN", "40 menit / JP (SMP/MTs)"}, {"45_MIN", "45 menit / JP (SMA/MA/SMK)"},
	},
}

// Subject types (subject_types).
const (
	SubjectTypeWajibUmum       = "wajib_umum"
	SubjectTypePeminatan       = "peminatan"
	SubjectTypeKejuruan        = "kejuruan"
	SubjectTypeMuatanLokal     = "muatan_lokal"
	SubjectTypeEkstrakurikuler = "ekstrakurikuler"
)

// referenceOrder fixes the key order of the "Referensi" sheet.
var referenceOrder = []string{
	RefGenders, RefReligions, RefCitizenships, RefSpecialNeeds, RefBloodTypes, RefResidenceTypes,
	RefTransportations, RefEducationLevels, RefOccupations, RefIncomes, RefRegistrationTypes,
	RefStudentStatuses, RefHistoryStatuses, RefEducationStages, RefCurricula, RefGuardianTypes,
	RefAchievementTypes, RefAchievementLevels, RefScholarshipTypes, RefGradePresets, RefNumberings,
	RefPromotionActions,
}

// ReferenceKeys returns the static reference keys in display order.
func ReferenceKeys() []string { return append([]string(nil), referenceOrder...) }

// References returns a copy of all static enum lists.
func References() map[string][]Option {
	out := make(map[string][]Option, len(references)+1)
	for k, v := range references {
		out[k] = append([]Option(nil), v...)
	}
	return out
}

// RefOptions returns the options of one key (nil if unknown).
func RefOptions(key string) []Option { return references[key] }

// ValidEnum reports whether value is an exact value of reference key.
func ValidEnum(key, value string) bool {
	for _, o := range references[key] {
		if o.Value == value {
			return true
		}
	}
	return false
}

// EnumLabel returns the label for a value ("" when unknown).
func EnumLabel(key, value string) string {
	for _, o := range references[key] {
		if o.Value == value {
			return o.Label
		}
	}
	return ""
}

// MatchEnum resolves user input (value or label, case-insensitive; for labels of
// the form "X - Name" also just "Name") to the canonical value.
func MatchEnum(key, input string) (string, bool) {
	in := strings.ToLower(strings.Join(strings.Fields(input), " "))
	if in == "" {
		return "", false
	}
	for _, o := range references[key] {
		if strings.ToLower(o.Value) == in || strings.ToLower(o.Label) == in {
			return o.Value, true
		}
		if i := strings.Index(o.Label, " - "); i > 0 && strings.ToLower(o.Label[i+3:]) == in {
			return o.Value, true
		}
	}
	// Underscore/space tolerant value match ("sepeda motor" -> sepeda_motor).
	alt := strings.ReplaceAll(in, " ", "_")
	for _, o := range references[key] {
		if strings.ToLower(o.Value) == alt {
			return o.Value, true
		}
	}
	return "", false
}
