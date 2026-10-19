package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config holds all gateway settings, read once at startup.
type Config struct {
	AppEnv               string
	Port                 string
	GatewaySecret        string
	AuthServiceURL       string
	InstantionServiceURL string
	AssessmentServiceURL string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	RabbitMQURL       string
	AuditQueue        string
	AuditDLX          string
	AuditDLQ          string
	RabbitMQPrefetch  int
	RequestLogBuffer  int
	RequestLogWorkers int
}

// IsProduction reports whether APP_ENV=production.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("config: invalid %s=%q, using default %d", key, v, def)
	}
	return def
}

// Load reads .env (if present) and the environment. It fails when required
// settings (GATEWAY_SECRET) are missing.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppEnv:               getenv("APP_ENV", "development"),
		Port:                 getenv("PORT", "80"),
		GatewaySecret:        os.Getenv("GATEWAY_SECRET"),
		AuthServiceURL:       getenv("AUTH_SERVICE_URL", "http://auth-service:8080"),
		InstantionServiceURL: getenv("INSTANTION_SERVICE_URL", "http://instantion-service:8080"),
		AssessmentServiceURL: getenv("ASSESSMENT_SERVICE_URL", "http://edupilot-assesment:8080"),

		DBHost:     getenv("DB_HOST", "localhost"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBUser:     getenv("DB_USER", "postgres"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBName:     getenv("DB_NAME", "edupilot_gateway_db"),

		RabbitMQURL:       getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		AuditQueue:        getenv("AUDIT_LOG_QUEUE", "audit_logs_queue"),
		AuditDLX:          getenv("AUDIT_LOG_DLX", "audit_logs_dlx"),
		AuditDLQ:          getenv("AUDIT_LOG_DLQ", "audit_logs_dlq"),
		RabbitMQPrefetch:  getenvInt("RABBITMQ_PREFETCH", 20),
		RequestLogBuffer:  getenvInt("REQUEST_LOG_BUFFER", 1024),
		RequestLogWorkers: getenvInt("REQUEST_LOG_WORKERS", 4),
	}

	if strings.TrimSpace(cfg.GatewaySecret) == "" {
		return nil, errors.New("GATEWAY_SECRET is required but empty")
	}
	return cfg, nil
}

func (c *Config) dsn(dbName string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta",
		c.DBHost, c.DBUser, c.DBPassword, dbName, c.DBPort)
}

// quoteIdent quotes a PostgreSQL identifier safely.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)}
}

// ensureDatabase connects to the maintenance "postgres" DB and creates the
// target database when it does not exist.
func (c *Config) ensureDatabase() error {
	adminDB, err := gorm.Open(postgres.Open(c.dsn("postgres")), gormConfig())
	if err != nil {
		return fmt.Errorf("connect to postgres maintenance db: %w", err)
	}
	sqlDB, err := adminDB.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	var exists bool
	if err := adminDB.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = ?)", c.DBName).
		Scan(&exists).Error; err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if exists {
		return nil
	}
	if err := adminDB.Exec("CREATE DATABASE " + quoteIdent(c.DBName)).Error; err != nil {
		// Another instance may have created it concurrently (42P04 duplicate_database).
		if strings.Contains(err.Error(), "42P04") || strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("create database: %w", err)
	}
	log.Printf("Database %s created", c.DBName)
	return nil
}

func (c *Config) openDB() (*gorm.DB, error) {
	if err := c.ensureDatabase(); err != nil {
		return nil, err
	}
	db, err := gorm.Open(postgres.Open(c.dsn(c.DBName)), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// ConnectDB connects with exponential backoff until ctx is cancelled or
// maxAttempts is reached.
func (c *Config) ConnectDB(ctx context.Context, maxAttempts int) (*gorm.DB, error) {
	backoff := time.Second
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err := c.openDB()
		if err == nil {
			return db, nil
		}
		lastErr = err
		log.Printf("DB connect attempt %d/%d failed: %v (retrying in %s)", attempt, maxAttempts, err, backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
	return nil, fmt.Errorf("database unavailable after %d attempts: %w", maxAttempts, lastErr)
}
