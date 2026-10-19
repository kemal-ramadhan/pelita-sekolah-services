package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/edupilot-instantion-service/internal/model"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// ServiceName is used for logs, tracing, metrics and health output.
const ServiceName = "edupilot-instantion-service"

var (
	DB             *gorm.DB
	RDB            *redis.Client
	ctx            = context.Background()
	Logger         *zap.Logger
	TracerProvider *sdktrace.TracerProvider
)

var dbNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func LoadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println(".env file not found, using system environment variables")
	}
}

// Getenv returns the env value or a fallback when unset/empty.
func Getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ConnectDB() {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")

	if !dbNamePattern.MatchString(dbName) {
		log.Fatalf("Invalid DB_NAME %q", dbName)
	}

	gormCfg := &gorm.Config{
		TranslateError: true,
		Logger: gormlogger.New(log.New(os.Stdout, "", log.LstdFlags), gormlogger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true, // never log bound values (PII)
			Colorful:                  false,
		}),
	}

	// Step 1: connect to the default DB to make sure the target DB exists.
	defaultDSN := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=postgres port=%s sslmode=disable",
		host, user, password, port,
	)
	defaultDB, err := gorm.Open(postgres.Open(defaultDSN), gormCfg)
	if err != nil {
		log.Fatalf("Failed to connect to default DB: %v", err)
	}

	var exists bool
	if err := defaultDB.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = ?)", dbName).Scan(&exists).Error; err != nil {
		log.Fatalf("Failed to check database existence: %v", err)
	}
	if !exists {
		if err := defaultDB.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)).Error; err != nil {
			log.Fatalf("Failed to create database %s: %v", dbName, err)
		}
		log.Printf("Database %s created successfully", dbName)
	}
	if sqlDefault, err := defaultDB.DB(); err == nil {
		_ = sqlDefault.Close()
	}

	// Step 2: connect to the service DB.
	mainDSN := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		host, user, password, dbName, port,
	)
	db, err := gorm.Open(postgres.Open(mainDSN), gormCfg)
	if err != nil {
		log.Fatalf("Failed to connect to main DB: %v", err)
	}
	DB = db

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
		log.Printf("Failed to create uuid-ossp extension: %v", err)
	}

	if err := preMigrate(db); err != nil {
		log.Fatalf("Pre-migration failed: %v", err)
	}

	if err := db.AutoMigrate(
		&model.School{},
		&model.InstitutionAccount{},
		&model.AcademicYear{},
		&model.GradeLevel{},
		&model.Major{},
		&model.Classes{},
		&model.Contacts{},
		&model.EmployeeAssignments{},
		&model.EmployeeCertifications{},
		&model.EmployeeEducation{},
		&model.EmployeePosition{},
		&model.Employees{},
		&model.Locations{},
		&model.Semester{},
		&model.StudentDocumentType{},
		&model.StudentDocuments{},
		&model.Students{},
		&model.StudentClassHistory{},
		&model.Subject{},
		&model.TeacherProfiles{},
		&model.TeacherSubjects{},
		&model.ClassSubject{},
		&model.SchedulePeriod{},
		&model.TeacherSchedules{},
		&model.TeachingAssesments{},
		&model.AttendanceConfig{},
		&model.AttendanceShift{},
		&model.ClassAttendanceShift{},
		&model.DailyAttendance{},
		&model.ClassTeachingSession{},
		&model.StudentSessionAttendance{},
		&model.Notification{},
		&model.SchoolJoinRequest{},
		&model.CounselingSession{},
		&model.CounselingParticipant{},
		&model.CounselingOutcome{},
		&model.CounselingEvent{},
		&model.CounselingEventAttendance{},
		&model.CounselingDailyJournal{},
	); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	if err := postMigrate(db); err != nil {
		log.Fatalf("Post-migration failed: %v", err)
	}

	log.Println("Connected to PostgreSQL and migrated models successfully")
}

func tableExists(db *gorm.DB, table string) bool {
	return db.Migrator().HasTable(table)
}

// columnInfo reports whether table.column exists, its data type and nullability.
func columnInfo(db *gorm.DB, table, column string) (exists bool, dataType string, nullable bool, err error) {
	var rows []struct {
		DataType   string
		IsNullable string
	}
	err = db.Raw(`SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return false, "", false, err
	}
	return true, rows[0].DataType, rows[0].IsNullable == "YES", nil
}

// dropNotNull relaxes a legacy NOT NULL column (no-op when the column is
// missing or already nullable).
func dropNotNull(db *gorm.DB, table, column string) error {
	exists, _, nullable, err := columnInfo(db, table, column)
	if err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if !exists || nullable {
		return nil
	}
	if err := db.Exec(fmt.Sprintf(`ALTER TABLE %q ALTER COLUMN %q DROP NOT NULL`, table, column)).Error; err != nil {
		return fmt.Errorf("%s.%s drop not null: %w", table, column, err)
	}
	log.Printf("Relaxed legacy NOT NULL on %s.%s", table, column)
	return nil
}

// toBoolean converts a legacy text/int column to boolean (USING), keeping
// true-ish values ("true", "1", "t", "yes", "y", "ya") as true and everything
// else (including NULL) as false. No-op when missing or already boolean.
func toBoolean(db *gorm.DB, table, column string, def bool) error {
	exists, dataType, _, err := columnInfo(db, table, column)
	if err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if !exists || dataType == "boolean" {
		return nil
	}
	stmts := []string{
		fmt.Sprintf(`ALTER TABLE %q ALTER COLUMN %q DROP DEFAULT`, table, column),
		fmt.Sprintf(`ALTER TABLE %q ALTER COLUMN %q TYPE boolean USING (COALESCE(lower(trim(%q::text)), '') IN ('true','1','t','yes','y','ya'))`, table, column, column),
		fmt.Sprintf(`UPDATE %q SET %q = %t WHERE %q IS NULL`, table, column, def, column),
		fmt.Sprintf(`ALTER TABLE %q ALTER COLUMN %q SET DEFAULT %t`, table, column, def),
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, q := range stmts {
			if err := tx.Exec(q).Error; err != nil {
				return fmt.Errorf("%s.%s to boolean: %w", table, column, err)
			}
		}
		log.Printf("Converted legacy %s.%s (%s) to boolean", table, column, dataType)
		return nil
	})
}

// legacyNullable lists legacy columns that are optional now (API_CONTRACT §8).
var legacyNullable = map[string][]string{
	"employees":              {"customer_id"},
	"students":               {"customer_id", "student_code", "nis", "nisn", "join_date", "graduate_date"},
	"classes":                {"semester_id", "class_code", "class_description"},
	"academic_years":         {"academic_code", "year"},
	"semesters":              {"semester_code", "semester_name"},
	"student_documents":      {"file_path", "upload_at", "status", "file_name"},
	"student_document_types": {"description"},
	"subjects":               {"level", "description"},
	"teacher_subjects":       {"academic_id"},
	"employee_positions":     {"position_code", "category"},
}

// legacyBooleans lists columns that may be non-boolean in old databases.
var legacyBooleans = []struct {
	table, column string
	def           bool
}{
	{"student_document_types", "is_required", false},
	{"academic_years", "is_active", false},
	{"semesters", "is_active", false},
	{"classes", "is_active", true},
	{"majors", "is_active", true},
}

// preMigrate adjusts legacy columns that AutoMigrate might not change safely.
// Every step checks that the table/column exists, so it is idempotent and safe
// on both fresh and existing databases.
func preMigrate(db *gorm.DB) error {
	for table, cols := range legacyNullable {
		if !tableExists(db, table) {
			continue
		}
		for _, col := range cols {
			if err := dropNotNull(db, table, col); err != nil {
				return err
			}
		}
	}
	for _, b := range legacyBooleans {
		if !tableExists(db, b.table) {
			continue
		}
		if err := toBoolean(db, b.table, b.column, b.def); err != nil {
			return err
		}
	}
	// Backfill the contract "name" (stored in academic_name) from legacy columns.
	if tableExists(db, "academic_years") {
		hasName, _, _, err := columnInfo(db, "academic_years", "academic_name")
		if err != nil {
			return err
		}
		hasYear, _, _, _ := columnInfo(db, "academic_years", "year")
		hasCode, _, _, _ := columnInfo(db, "academic_years", "academic_code")
		if hasName && hasYear && hasCode {
			if err := db.Exec(`UPDATE academic_years SET academic_name = COALESCE(NULLIF(trim(year), ''), NULLIF(trim(academic_code), ''), academic_name)
				WHERE academic_name IS NULL OR trim(academic_name) = ''`).Error; err != nil {
				return fmt.Errorf("backfill academic_years.academic_name: %w", err)
			}
		}
	}
	return nil
}

// guardedUniqueIndex creates a (partial) unique index unless it already exists.
// When legacy data violates it (dupSQL returns a count > 0) the index is
// skipped with a warning instead of failing startup or rewriting data.
func guardedUniqueIndex(db *gorm.DB, table, name, dupSQL, createSQL string) error {
	if !tableExists(db, table) {
		return nil
	}
	var exists bool
	if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?)`, name).Scan(&exists).Error; err != nil {
		return fmt.Errorf("check index %s: %w", name, err)
	}
	if exists {
		return nil
	}
	var dups int64
	if err := db.Raw(dupSQL).Scan(&dups).Error; err != nil {
		return fmt.Errorf("check duplicates for %s: %w", name, err)
	}
	if dups > 0 {
		log.Printf("WARNING: %d duplicate groups violate %s on %s; skipping index. Resolve manually (the service still enforces it on write).", dups, name, table)
		return nil
	}
	if err := db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("create index %s: %w", name, err)
	}
	return nil
}

// postMigrate creates partial unique indexes that GORM tags cannot express,
// cleaning up conflicting legacy data first where that is safe.
func postMigrate(db *gorm.DB) error {
	// Employees: make legacy duplicate (school_id, employee_code) pairs unique by
	// suffixing all but the oldest row, then add the partial unique index.
	res := db.Exec(`
		UPDATE employees e
		SET employee_code = e.employee_code || '-dup-' || left(e.id::text, 8)
		FROM (
			SELECT id, row_number() OVER (PARTITION BY school_id, employee_code ORDER BY created_at, id) AS rn
			FROM employees
			WHERE deleted_at IS NULL
		) d
		WHERE e.id = d.id AND d.rn > 1`)
	if res.Error != nil {
		return fmt.Errorf("dedupe employee codes: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		log.Printf("Renamed %d duplicate employee_code values (suffix -dup-<id>)", res.RowsAffected)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_employees_school_code_unique
		ON employees (school_id, employee_code) WHERE deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("create employees unique index: %w", err)
	}

	// Employees: one employee per teacher auth account. Never rewrite links
	// automatically; if legacy duplicates exist, log and skip the index.
	var dupUsers int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
			SELECT user_id FROM employees
			WHERE user_id IS NOT NULL
			GROUP BY user_id HAVING COUNT(*) > 1) t`).Scan(&dupUsers).Error; err != nil {
		return fmt.Errorf("check duplicate employee user_id: %w", err)
	}
	if dupUsers > 0 {
		log.Printf("WARNING: %d user_id values are linked to more than one employee; skipping idx_employees_user_id_unique. Resolve manually.", dupUsers)
	} else if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_employees_user_id_unique
		ON employees (user_id) WHERE user_id IS NOT NULL`).Error; err != nil {
		return fmt.Errorf("create employees user_id unique index: %w", err)
	}

	// Schools: one school per self-registered customer. Do not destroy data if
	// legacy duplicates exist; log and skip (the service still enforces it on create).
	var dupCustomers int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
			SELECT customer_id FROM schools
			WHERE customer_id <> 0 AND deleted_at IS NULL
			GROUP BY customer_id HAVING COUNT(*) > 1) t`).Scan(&dupCustomers).Error; err != nil {
		return fmt.Errorf("check duplicate school customers: %w", err)
	}
	if dupCustomers > 0 {
		log.Printf("WARNING: %d customer_id values own more than one school; skipping idx_schools_customer_unique. Resolve manually.", dupCustomers)
	} else if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_schools_customer_unique
		ON schools (customer_id) WHERE customer_id <> 0 AND deleted_at IS NULL`).Error; err != nil {
		return fmt.Errorf("create schools customer unique index: %w", err)
	}

	guarded := []struct{ table, name, dup, create string }{
		{"academic_years", "idx_academic_years_school_active_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM academic_years WHERE is_active = true GROUP BY school_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_academic_years_school_active_unique ON academic_years (school_id) WHERE is_active = true`},
		{"academic_years", "idx_academic_years_school_name_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM academic_years WHERE academic_name IS NOT NULL AND academic_name <> '' GROUP BY school_id, lower(academic_name) HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_academic_years_school_name_unique ON academic_years (school_id, lower(academic_name)) WHERE academic_name IS NOT NULL AND academic_name <> ''`},
		{"grade_levels", "idx_grade_levels_school_number_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM grade_levels GROUP BY school_id, number HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_grade_levels_school_number_unique ON grade_levels (school_id, number)`},
		{"classes", "idx_classes_school_year_name_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM classes WHERE deleted_at IS NULL AND academic_year_id IS NOT NULL GROUP BY school_id, academic_year_id, lower(class_name) HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_classes_school_year_name_unique ON classes (school_id, academic_year_id, lower(class_name)) WHERE deleted_at IS NULL`},
		{"students", "idx_students_school_nisn_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM students WHERE nisn IS NOT NULL AND nisn <> '' AND deleted_at IS NULL GROUP BY school_id, nisn HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_students_school_nisn_unique ON students (school_id, nisn) WHERE nisn IS NOT NULL AND nisn <> '' AND deleted_at IS NULL`},
		{"students", "idx_students_school_nis_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM students WHERE nis IS NOT NULL AND nis <> '' AND deleted_at IS NULL GROUP BY school_id, nis HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_students_school_nis_unique ON students (school_id, nis) WHERE nis IS NOT NULL AND nis <> '' AND deleted_at IS NULL`},
		{"students", "idx_students_school_nik_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM students WHERE nik IS NOT NULL AND nik <> '' AND deleted_at IS NULL GROUP BY school_id, nik HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_students_school_nik_unique ON students (school_id, nik) WHERE nik IS NOT NULL AND nik <> '' AND deleted_at IS NULL`},
		{"student_class_histories", "idx_student_class_history_active_unique",
			`SELECT COUNT(*) FROM (SELECT student_id FROM student_class_histories WHERE status = 'active' GROUP BY student_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_student_class_history_active_unique ON student_class_histories (student_id) WHERE status = 'active'`},
		{"subjects", "idx_subjects_school_code_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM subjects WHERE deleted_at IS NULL GROUP BY school_id, lower(subject_code) HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_subjects_school_code_unique ON subjects (school_id, lower(subject_code)) WHERE deleted_at IS NULL`},
		{"class_subjects", "idx_class_subjects_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM class_subjects WHERE deleted_at IS NULL GROUP BY school_id, class_id, subject_id, academic_year_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_class_subjects_unique ON class_subjects (school_id, class_id, subject_id, academic_year_id) WHERE deleted_at IS NULL`},
		// Attendance (API_CONTRACT §9): guarded so legacy duplicates never block startup.
		{"daily_attendances", "idx_daily_attendance_teacher_date",
			`SELECT COUNT(*) FROM (SELECT 1 FROM daily_attendances WHERE employee_id IS NOT NULL GROUP BY school_id, employee_id, date HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_attendance_teacher_date ON daily_attendances (school_id, employee_id, date) WHERE employee_id IS NOT NULL`},
		{"daily_attendances", "idx_daily_attendance_student_date",
			`SELECT COUNT(*) FROM (SELECT 1 FROM daily_attendances WHERE student_id IS NOT NULL GROUP BY school_id, student_id, date HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_attendance_student_date ON daily_attendances (school_id, student_id, date) WHERE student_id IS NOT NULL`},
		{"class_teaching_sessions", "idx_teaching_session_schedule_date",
			`SELECT COUNT(*) FROM (SELECT 1 FROM class_teaching_sessions GROUP BY school_id, schedule_id, date HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_teaching_session_schedule_date ON class_teaching_sessions (school_id, schedule_id, date)`},
		{"class_teaching_sessions", "idx_teaching_session_active_token",
			`SELECT COUNT(*) FROM (SELECT 1 FROM class_teaching_sessions WHERE status = 'in_progress' AND session_token IS NOT NULL AND session_token <> '' GROUP BY school_id, session_token HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_teaching_session_active_token ON class_teaching_sessions (school_id, session_token) WHERE status = 'in_progress' AND session_token IS NOT NULL AND session_token <> ''`},
		{"student_session_attendances", "idx_student_session_att_unique",
			`SELECT COUNT(*) FROM (SELECT 1 FROM student_session_attendances GROUP BY school_id, session_id, student_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_student_session_att_unique ON student_session_attendances (school_id, session_id, student_id)`},
		// Student accounts & join requests (API_CONTRACT §10.3, §10.4).
		{"students", "idx_students_user_id_unique",
			`SELECT COUNT(*) FROM (SELECT user_id FROM students WHERE user_id IS NOT NULL GROUP BY user_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_students_user_id_unique ON students (user_id) WHERE user_id IS NOT NULL`},
		{"school_join_requests", "idx_school_join_requests_pending_user",
			`SELECT COUNT(*) FROM (SELECT user_id FROM school_join_requests WHERE status = 'pending' GROUP BY user_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_school_join_requests_pending_user ON school_join_requests (user_id) WHERE status = 'pending'`},
		{"subjects", "idx_subjects_school_name_level_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM subjects WHERE deleted_at IS NULL GROUP BY school_id, lower(name), COALESCE(grade_level_id, '00000000-0000-0000-0000-000000000000'::uuid) HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_subjects_school_name_level_unique ON subjects (school_id, lower(name), COALESCE(grade_level_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE deleted_at IS NULL`},
		// Jabatan names are unique per school (the service also enforces it, 409 DUPLICATE_NAME).
		{"employee_positions", "idx_employee_positions_school_name_unique",
			`SELECT COUNT(*) FROM (SELECT school_id FROM employee_positions GROUP BY school_id, lower(position) HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_employee_positions_school_name_unique ON employee_positions (school_id, lower(position))`},
		// Bimbingan Konseling (API_CONTRACT §12): one participant row per student, one check-in per attendee.
		{"counseling_participants", "idx_counseling_participants_unique",
			`SELECT COUNT(*) FROM (SELECT 1 FROM counseling_participants GROUP BY counseling_session_id, student_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_counseling_participants_unique ON counseling_participants (counseling_session_id, student_id)`},
		{"counseling_event_attendances", "idx_counseling_event_att_student",
			`SELECT COUNT(*) FROM (SELECT 1 FROM counseling_event_attendances WHERE student_id IS NOT NULL GROUP BY event_id, student_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_counseling_event_att_student ON counseling_event_attendances (event_id, student_id) WHERE student_id IS NOT NULL`},
		{"counseling_event_attendances", "idx_counseling_event_att_employee",
			`SELECT COUNT(*) FROM (SELECT 1 FROM counseling_event_attendances WHERE employee_id IS NOT NULL GROUP BY event_id, employee_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_counseling_event_att_employee ON counseling_event_attendances (event_id, employee_id) WHERE employee_id IS NOT NULL`},
		{"counseling_event_attendances", "idx_counseling_event_att_institution",
			`SELECT COUNT(*) FROM (SELECT 1 FROM counseling_event_attendances WHERE user_type = 'institution' AND user_id IS NOT NULL GROUP BY event_id, user_id HAVING COUNT(*) > 1) t`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_counseling_event_att_institution ON counseling_event_attendances (event_id, user_id) WHERE user_type = 'institution' AND user_id IS NOT NULL`},
	}
	for _, g := range guarded {
		if err := guardedUniqueIndex(db, g.table, g.name, g.dup, g.create); err != nil {
			return err
		}
	}

	// Lookup indexes for timetable conflict checks / grids (idempotent).
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_teacher_schedules_school_day ON teacher_schedules (school_id, day_of_week, start_time)`,
		`CREATE INDEX IF NOT EXISTS idx_teacher_subjects_school_employee_subject ON teacher_subjects (school_id, employee_id, subject_id)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_attendances_school_date ON daily_attendances (school_id, date)`,
		`CREATE INDEX IF NOT EXISTS idx_teaching_sessions_school_date ON class_teaching_sessions (school_id, date)`,
		`CREATE INDEX IF NOT EXISTS idx_counseling_sessions_school_date ON counseling_sessions (school_id, requested_date)`,
		`CREATE INDEX IF NOT EXISTS idx_counseling_events_school_date ON counseling_events (school_id, event_date)`,
		`CREATE INDEX IF NOT EXISTS idx_counseling_daily_journals_school_date ON counseling_daily_journals (school_id, date)`,
		`CREATE INDEX IF NOT EXISTS idx_counseling_sessions_counselor_status ON counseling_sessions (school_id, counselor_id, status)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create attendance/schedule index: %w", err)
		}
	}

	return nil
}

func ConnectRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_HOST") + ":" + Getenv("REDIS_PORT", "6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	if _, err := RDB.Ping(ctx).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")
}

func RedisDB() *redis.Client { return RDB }
func Ctx() context.Context   { return ctx }

func InitLogger() {
	var err error
	Logger, err = zap.NewProduction()
	if err != nil {
		panic(err)
	}
}

func InitTracer() {
	jaegerURL := Getenv("JAEGER_ENDPOINT", "http://localhost:14268/api/traces")

	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(jaegerURL)))
	if err != nil {
		log.Fatalf("failed to create Jaeger exporter: %v", err)
	}

	TracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(ServiceName),
		)),
	)

	otel.SetTracerProvider(TracerProvider)
	log.Println("OpenTelemetry tracing initialized")
}

func ShutdownTracer(ctx context.Context) {
	if TracerProvider != nil {
		if err := TracerProvider.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}
}
