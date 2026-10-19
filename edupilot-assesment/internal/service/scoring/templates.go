package scoring

import "strings"

// Curated report templates (PRD §6.6, §7.5, §8.7, §9.2–9.4, §10.3–10.4).
// Language is positive and strength-based; ForbiddenWords is enforced by tests.

// Disclaimer is mandatory on every report (PRD §10.4).
const Disclaimer = "Hasil ini adalah gambaran minat, kecenderungan, dan preferensi belajar pada saat pengisian, bukan diagnosis, bukan vonis kemampuan, dan bukan satu-satunya dasar memilih jurusan/karier. Diskusikan hasil bersama Guru BK, orang tua, dan pertimbangkan nilai, cita-cita, serta kondisi pribadi."

// WellbeingMessage replaces any welfare flag in the student view (PRD §9.3).
const WellbeingMessage = "Terima kasih sudah menjawab dengan jujur. Guru BK siap mengobrol bila kamu mau."

// LearningStyleNote must appear on every ADN report (PRD §9.2).
const LearningStyleNote = "Gaya belajar adalah kecenderungan, bukan batas kemampuan. Cobalah berbagai cara belajar, terutama untuk materi yang sulit."

// ForbiddenWords (PRD §10.3). The disclaimer itself is exempt.
var ForbiddenWords = []string{"bodoh", "tidak cocok sama sekali", "pasti", "harus", "diagnosis", "gangguan"}

// FindForbidden returns the forbidden words contained in text (case-insensitive).
func FindForbidden(text string) []string {
	t := strings.ToLower(strings.ReplaceAll(text, Disclaimer, ""))
	out := []string{}
	for _, w := range ForbiddenWords {
		if strings.Contains(t, w) {
			out = append(out, w)
		}
	}
	return out
}

// TypeInfo = PRD §7.5 row.
type TypeInfo struct {
	Letter, Name, Nick, Short, Desc, Subjects, Careers, Environment, Development string
}

var TypeInfos = map[string]TypeInfo{
	"R": {"R", "Realistic", "Pelaksana", "praktis dan senang bekerja dengan alat, mesin, tanaman, atau hewan",
		"Praktis, suka bekerja dengan alat, mesin, tanaman/hewan, dan aktivitas fisik; lebih nyaman dengan tindakan konkret.",
		"Prakarya, Fisika terapan, PJOK, pertanian, bengkel",
		"Teknik (sipil/mesin/elektro), Pertanian, Kehutanan, Otomotif, Pilot, Teknisi",
		"Lingkungan kerja lapangan/bengkel, tugas terukur", "Latih komunikasi & kerja tim"},
	"I": {"I", "Investigative", "Pemikir", "analitis, ingin tahu, dan senang menyelidiki",
		"Analitis, ingin tahu, suka menyelidiki, memecahkan masalah abstrak, berbasis bukti.",
		"Matematika, IPA, Informatika, olimpiade/KIR",
		"Kedokteran, MIPA, Data Science, Farmasi, Peneliti",
		"Ruang kerja yang memberi waktu berpikir mendalam", "Latih presentasi & kolaborasi"},
	"A": {"A", "Artistic", "Pencipta", "imajinatif, orisinal, dan ekspresif",
		"Imajinatif, orisinal, ekspresif; menghargai kebebasan dan estetika.",
		"Seni budaya, Bahasa, Sastra, media, teater",
		"DKV, Arsitektur, Film, Musik, Penulis, Desain Produk",
		"Lingkungan fleksibel, minim aturan kaku", "Latih manajemen waktu & portofolio"},
	"S": {"S", "Social", "Penolong", "hangat, empatik, dan senang membantu orang",
		"Hangat, empatik, suka mengajar, membantu, menjembatani orang.",
		"Bimbingan teman sebaya, PMR, OSIS sosial, Bahasa",
		"Pendidikan, Psikologi, Keperawatan, Pekerjaan Sosial, Kesehatan Masyarakat",
		"Kerja tim, layanan kepada orang", "Latih batasan diri & manajemen emosi"},
	"E": {"E", "Enterprising", "Penggerak", "persuasif, berani mengambil risiko, dan senang memimpin",
		"Persuasif, ambisius, berani mengambil risiko, suka memimpin dan mencapai target.",
		"OSIS, debat, kewirausahaan, ekonomi",
		"Manajemen, Hukum, Bisnis, Pemasaran, Politik/Pemerintahan",
		"Lingkungan kompetitif, ada peluang memimpin", "Latih mendengar & etika kepemimpinan"},
	"C": {"C", "Conventional", "Pengatur", "teliti, rapi, dan andal mengelola data",
		"Teliti, rapi, patuh prosedur, andal dalam data & administrasi.",
		"Akuntansi, Informatika (data), tata kelola",
		"Akuntansi, Perbankan, Administrasi, Statistika, Audit, Perpajakan",
		"Lingkungan terstruktur, aturan jelas", "Latih inisiatif & fleksibilitas"},
}

// CombinationThemes = PRD §7.5 two-letter themes (order-independent, keyed in R-I-A-S-E-C order).
var CombinationThemes = map[string]string{
	"RI": "Teknik–sains terapan, riset lapangan",
	"RA": "Kriya, arsitektur lapangan, desain industri",
	"RS": "Fisioterapi, olahraga, paramedis",
	"RE": "Manajer proyek konstruksi, bisnis agro",
	"RC": "Teknisi presisi, logistik, quality control",
	"IA": "Penelitian kreatif, UX research, arsitektur sains",
	"IS": "Kedokteran, psikologi klinis, pendidikan sains",
	"IE": "Teknopreneur, konsultan, hukum paten",
	"IC": "Analisis data, statistika, aktuaria",
	"AS": "Pendidikan seni, terapi seni, konselor kreatif",
	"AE": "Periklanan, media, kewirausahaan kreatif",
	"AC": "Penerbitan, kurasi, desain informasi",
	"SE": "Guru-pemimpin, SDM, politik layanan publik",
	"SC": "Administrasi kesehatan/pendidikan, konselor sekolah",
	"EC": "Keuangan, perbankan, manajemen operasional",
}

// CombinationTheme returns the theme for two letters in any order.
func CombinationTheme(a, b string) string {
	if riasecPositions[a] > riasecPositions[b] {
		a, b = b, a
	}
	return CombinationThemes[a+b]
}

// TypeLabel → "I — Investigative (Pemikir)".
func TypeLabel(t string) string {
	ti := TypeInfos[t]
	return t + " — " + ti.Name + " (" + ti.Nick + ")"
}

// Consistency / differentiation templates (PRD §7.5).
const (
	tplFocused = "Minatmu terfokus dan saling berkaitan; kamu dapat mulai mencari program studi dengan profil serupa."
	tplBroad   = "Minatmu luas dan bervariasi (%s). Ini bukan masalah; carilah karier yang menggabungkan keduanya atau eksplorasi lewat magang/proyek."
	tplFlat    = "Pilihanmu belum condong ke satu bidang. Coba kegiatan baru (ekskul, relawan, kursus singkat), lalu ulangi tes 6 bulan lagi."
	tplMixed   = "Minatmu menunjukkan beberapa arah yang saling melengkapi. Gunakan kode ini sebagai titik awal untuk menjelajahi program studi dan kegiatan yang menggabungkan tipe-tipe teratasmu."
)

// TMB category templates (PRD §6.6; Sesuai / Kurang Sesuai written in the same voice).
var RumpunCategoryText = map[string]string{
	"Sangat Sesuai": "Profilmu sangat selaras dengan rumpun ini. Pertimbangkan sebagai pilihan utama, lalu cocokkan dengan nilai, cita-cita, dan diskusi bersama Guru BK.",
	"Sesuai":        "Profilmu selaras dengan rumpun ini. Rumpun ini layak dipertimbangkan bersama nilai rapor, cita-cita, dan masukan Guru BK.",
	"Cukup Sesuai":  "Ada beberapa kecocokan, namun bukan yang paling menonjol; eksplorasi lebih lanjut disarankan.",
	"Kurang Sesuai": "Rumpun ini belum menonjol pada profilmu saat ini. Minat dan kemampuan dapat terus berkembang seiring pengalaman belajar.",
}

var tmbDomainNames = map[string]string{"M": "MIPA", "T": "Teknologi & Rekayasa", "S": "Sosial-Hukum-Pemerintahan", "E": "Ekonomi-Bisnis", "B": "Bahasa-Sastra-Budaya", "K": "Seni-Desain-Kreatif"}
var tmbSubtestNames = map[string]string{"V": "Verbal", "N": "Numerik", "L": "Penalaran Logis", "S": "Spasial"}

// VARK interpretation & strategies (PRD §9.2 table).
type VarkInfo struct{ Interpretation, Strategies string }

var VarkInfos = map[string]VarkInfo{
	"V":          {"Cenderung terbantu representasi visual", "Peta pikiran, diagram alur, kode warna, infografis, video"},
	"A":          {"Cenderung terbantu penjelasan lisan", "Diskusi kelompok, mengajar teman, merekam & mendengar ringkasan, mnemonik lisan"},
	"R":          {"Cenderung terbantu teks", "Ringkasan, glosarium sendiri, menulis ulang, latihan soal tertulis"},
	"K":          {"Cenderung terbantu praktik", "Praktikum, proyek, role-play, simulasi, jeda gerak antar sesi belajar"},
	"multimodal": {"Fleksibel memakai banyak cara", "Gabungkan beberapa strategi; ubah bentuk materi (teks→diagram→penjelasan lisan)"},
}

// ADN socio-emotional templates (PRD §9.3).
const (
	tplMotivationStrong = "Kamu punya dorongan dan keyakinan diri yang baik. Pertahankan dengan menantang diri lewat target kecil bertahap."
	tplRegulationNeeds  = "Mungkin membantu: jadwal mingguan sederhana, teknik Pomodoro, dan evaluasi diri 5 menit setelah belajar."
	tplAnxietyHigh      = "Rasa cemas sebelum ujian umum dialami. Latihan soal bertahap dan teknik pernapasan dapat membantu; Guru BK bisa mendampingi."
	tplSocialLowStaff   = "Guru/Wali kelas disarankan memfasilitasi kegiatan kelompok yang ramah bagi siswa ini."
	tplBriefNote        = "Tes singkat — disarankan melanjutkan ke Asesmen Preferensi Karier (RIASEC) atau Tes Penelusuran Minat Karier untuk gambaran lebih lengkap."
	tplWelfareStaff     = "Butir waspada kesejahteraan (ADN-WEL1) bernilai ≥ 3. Guru BK disarankan menghubungi siswa dalam 1×24 jam kerja. Butir ini hanya alat skrining dan tidak menetapkan kondisi psikologis apa pun."
)

var SocioNames = map[string]string{
	SocioMotivasi: "Motivasi", SocioEfikasi: "Efikasi diri", SocioRegulasi: "Regulasi belajar",
	SocioKecemasan: "Kecemasan belajar", SocioDukungan: "Dukungan sosial",
}

// Validity notes shown with the report.
var ValidityText = map[string]string{
	FlagInvalidResponse: "Beberapa jawaban pemeriksaan perhatian tidak sesuai petunjuk, sehingga hasil ini mungkin belum menggambarkan dirimu. Kamu dapat meninjau ulang bersama Guru BK atau mengerjakan ulang tes.",
	FlagFlatProfile:     "Skor semua tipe hampir sama, sehingga urutan tipe dihitung dengan skor ipsatif (relatif terhadap rata-ratamu sendiri).",
	FlagStraightLining:  "Banyak jawaban beruntun yang sama. Coba tinjau kembali apakah setiap pernyataan sudah dibaca dengan saksama.",
	FlagTooFast:         "Tes diselesaikan sangat cepat. Hasil sebaiknya ditinjau bersama Guru BK.",
}
