package config

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"edupilot-assesment/internal/model"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/streadway/amqp"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	DB          *gorm.DB
	RedisClient *redis.Client
	RabbitConn  *amqp.Connection
	RabbitCh    *amqp.Channel
	Logger      *zap.Logger
)

func LoadEnv() {
	_ = godotenv.Load()
}

func InitLogger() {
	var err error
	if os.Getenv("APP_ENV") == "production" {
		Logger, err = zap.NewProduction()
	} else {
		Logger, err = zap.NewDevelopment()
	}
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}
}

func ConnectDB() {
	host := getenv("DB_HOST", "localhost")
	port := getenv("DB_PORT", "5432")
	user := getenv("DB_USER", "postgres")
	password := getenv("DB_PASSWORD", "postgres")
	dbName := getenv("DB_NAME", "edupilot_assesment_db")

	defaultDSN := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=postgres port=%s sslmode=disable",
		host, user, password, port,
	)

	defaultDB, err := sql.Open("pgx", defaultDSN)
	if err == nil {
		var exists bool
		_ = defaultDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, dbName).Scan(&exists)
		if !exists {
			_, _ = defaultDB.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, dbName))
			log.Printf("Database %s created successfully", dbName)
		}
		_ = defaultDB.Close()
	}

	mainDSN := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta",
		host, user, password, dbName, port,
	)

	db, err := gorm.Open(postgres.Open(mainDSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		log.Fatalf("Failed to connect to assessment DB: %v", err)
	}
	DB = db

	_ = db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`)

	preMigrate(db)
	if err := db.AutoMigrate(
		&model.AssessmentPackage{},
		&model.AssessmentVoucher{},
		&model.SchoolAssessmentSubscription{},
		&model.AssessmentVoucherLog{},
		&model.AssessmentSession{},
		&model.AssessmentSessionTarget{},
		&model.AssessmentAttempt{},
		&model.AssessmentResponse{},
		&model.AssessmentResult{},
		&model.AssessmentQuestionBank{},
		&model.AssessmentConsent{},
		&model.AssessmentWelfareAlert{},
		&model.AssessmentAuditLog{},
	); err != nil {
		log.Fatalf("AutoMigrate failed: %v", err)
	}
	postMigrate(db)

	seedDefaultPackages(db)
	seedDefaultVouchers(db)
	seedQuestionBank(db)
}

// preMigrate relaxes legacy NOT NULL columns that the model no longer writes.
func preMigrate(db *gorm.DB) {
	m := db.Migrator()
	if m.HasTable("assessment_sessions") {
		for _, col := range []string{"instrument_code", "duration_minutes", "counselor_id", "event_id"} {
			if m.HasColumn("assessment_sessions", col) {
				execWarn(db, fmt.Sprintf(`ALTER TABLE assessment_sessions ALTER COLUMN %s DROP NOT NULL`, col))
			}
		}
	}
}

// postMigrate backfills new columns and creates guarded unique indexes (a
// failing index on legacy duplicate rows is logged, never fatal).
func postMigrate(db *gorm.DB) {
	if db.Migrator().HasColumn("assessment_sessions", "instrument_code") {
		execWarn(db, `UPDATE assessment_sessions SET instrument_codes =
			CASE WHEN instrument_code IS NULL OR instrument_code = 'ALL' THEN '["TMB","RIA","PMK","ADN"]'::jsonb
			     ELSE jsonb_build_array(instrument_code) END
			WHERE (instrument_codes IS NULL OR instrument_codes = '[]'::jsonb)`)
	}
	execWarn(db, `UPDATE assessment_sessions SET status = 'published' WHERE status = 'draft'`)
	// Duplicate autosave rows (legacy worker re-created responses) → keep the newest.
	execWarn(db, `DELETE FROM assessment_responses a USING assessment_responses b
		WHERE a.attempt_id = b.attempt_id AND a.item_id = b.item_id AND a.ctid < b.ctid`)
	for _, ddl := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_asm_responses_attempt_item ON assessment_responses (attempt_id, item_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_asm_voucher_logs_voucher_school ON assessment_voucher_logs (voucher_id, school_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_asm_targets_session_student ON assessment_session_targets (session_id, student_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_asm_attempts_in_progress ON assessment_attempts (session_id, student_id, instrument_code) WHERE status = 'in_progress'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_asm_consents ON assessment_consents (session_id, student_id, instrument_code)`,
		`CREATE INDEX IF NOT EXISTS ix_asm_attempts_lookup ON assessment_attempts (session_id, student_id, instrument_code)`,
	} {
		execWarn(db, ddl)
	}
}

func execWarn(db *gorm.DB, sql string) {
	if err := db.Exec(sql).Error; err != nil {
		log.Printf("Warning: migration step failed (%v): %s", err, firstLine(sql))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func ConnectRedis() {
	host := getenv("REDIS_HOST", "localhost")
	port := getenv("REDIS_PORT", "6379")
	password := os.Getenv("REDIS_PASSWORD")
	dbIndex, _ := strconv.Atoi(getenv("REDIS_DB", "0"))

	RedisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       dbIndex,
	})
}

// QueueName is the durable submission queue (ASSESSMENT_QUEUE).
var QueueName = "assessment_submissions_queue"

// ConnectRabbitMQ dials with retries; without RabbitMQ submissions are scored in-process.
func ConnectRabbitMQ() {
	url := getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	QueueName = getenv("ASSESSMENT_QUEUE", QueueName)
	var conn *amqp.Connection
	var err error
	for i := 0; i < 10; i++ {
		if conn, err = amqp.Dial(url); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Printf("Warning: RabbitMQ connection failed; submissions will be scored in-process")
		return
	}
	RabbitConn = conn

	ch, err := conn.Channel()
	if err != nil {
		log.Printf("Warning: Failed to open RabbitMQ channel: %v", err)
		return
	}
	if _, err = ch.QueueDeclare(QueueName, true, false, false, false, nil); err != nil {
		log.Printf("Warning: Failed to declare queue %s: %v", QueueName, err)
		_ = ch.Close()
		return
	}
	RabbitCh = ch
}

// Getenv returns the trimmed env value or def.
func Getenv(key, def string) string { return getenv(key, def) }

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func seedDefaultPackages(db *gorm.DB) {
	var count int64
	db.Model(&model.AssessmentPackage{}).Count(&count)
	if count > 0 {
		return
	}

	packages := []model.AssessmentPackage{
		{
			ID:              uuid.New(),
			Name:            "Bundle Komplit: 4 Asesmen Psikologi & Karier",
			Code:            "BUNDLE-4-ALL",
			Description:     "Paket terlengkap mencakup Tes Minat & Bakat Penjurusan (TMB), RIASEC Karier, Penelusuran Minat Holland (PMK), dan Diagnostik Non-Kognitif (ADN).",
			Price:           1500000,
			StudentQuota:    300,
			InstrumentCodes: model.StringArray{"TMB", "RIA", "PMK", "ADN"},
			IsBundle:        true,
			IsActive:        true,
			BadgeColor:      "indigo",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
		{
			ID:              uuid.New(),
			Name:            "Paket Penjurusan: Tes Minat & Bakat (TMB)",
			Code:            "PKG-TMB",
			Description:     "Tes minat 6 bidang dan bakat penalaran kognitif mini (verbal, numerik, logika, spasial) untuk rekomendasi rumpun studi SMA/SMK.",
			Price:           600000,
			StudentQuota:    150,
			InstrumentCodes: model.StringArray{"TMB"},
			IsBundle:        false,
			IsActive:        true,
			BadgeColor:      "blue",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
		{
			ID:              uuid.New(),
			Name:            "Paket Asesmen Preferensi Karier (RIASEC)",
			Code:            "PKG-RIASEC",
			Description:     "Eksplorasi tipe kepribadian vokasional Holland 6 dimensi (Realistic, Investigative, Artistic, Social, Enterprising, Conventional).",
			Price:           500000,
			StudentQuota:    150,
			InstrumentCodes: model.StringArray{"RIA"},
			IsBundle:        false,
			IsActive:        true,
			BadgeColor:      "emerald",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
		{
			ID:              uuid.New(),
			Name:            "Paket Diagnostik Non-Kognitif & Gaya Belajar (VARK)",
			Code:            "PKG-ADN",
			Description:     "Identifikasi preferensi modalitas belajar Visual, Auditori, Baca-Tulis, Kinestetik, serta kebiasaan belajar dan sosio-emosional.",
			Price:           500000,
			StudentQuota:    150,
			InstrumentCodes: model.StringArray{"ADN"},
			IsBundle:        false,
			IsActive:        true,
			BadgeColor:      "amber",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
	}

	for _, p := range packages {
		_ = db.Create(&p).Error
	}
	log.Println("Default assessment packages seeded successfully")
}

func seedDefaultVouchers(db *gorm.DB) {
	var count int64
	db.Model(&model.AssessmentVoucher{}).Count(&count)
	if count > 0 {
		return
	}

	vouchers := []model.AssessmentVoucher{
		{
			ID:                   uuid.New(),
			Code:                 "BKSUPER2026",
			Name:                 "Voucher Gratis 100% Guru BK Cerdas 2026",
			DiscountType:         "free_claim",
			DiscountValue:        100,
			MaxUsage:             500,
			UsedCount:            0,
			ValidFrom:            time.Now().Add(-24 * time.Hour),
			ValidUntil:           time.Now().Add(365 * 24 * time.Hour),
			ApplicablePackageIDs: model.StringArray{}, // Berlaku untuk semua paket
			IsActive:             true,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		},
		{
			ID:                   uuid.New(),
			Code:                 "MERDEKABELAJAR",
			Name:                 "Voucher Klaim Diagnostik Kurikulum Merdeka",
			DiscountType:         "free_claim",
			DiscountValue:        100,
			MaxUsage:             1000,
			UsedCount:            0,
			ValidFrom:            time.Now().Add(-24 * time.Hour),
			ValidUntil:           time.Now().Add(365 * 24 * time.Hour),
			ApplicablePackageIDs: model.StringArray{},
			IsActive:             true,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		},
	}

	for _, v := range vouchers {
		_ = db.Create(&v).Error
	}
	log.Println("Default assessment vouchers seeded successfully")
}

func newUUID() uuid.UUID { return uuid.New() }
