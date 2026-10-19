package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/edupilot-auth-service/internal/model"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

const ServiceName = "edupilot-auth-service"

var (
	DB             *gorm.DB
	RDB            *redis.Client
	ctx            = context.Background()
	Logger         *zap.Logger
	TracerProvider *sdktrace.TracerProvider
)

// LoadEnv loads .env when present. In containers the env comes from compose env_file.
func LoadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println(".env file not found, using system environment variables")
	}
}

func IsProduction() bool {
	return os.Getenv("APP_ENV") == "production"
}

func ConnectDB() {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	DB = db

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := db.AutoMigrate(&model.User{}, &model.Customer{}, &model.Ability{}, &model.Role{}, &model.UserRole{}, &model.UserAbility{}); err != nil {
		log.Fatalf("Failed to migrate models: %v", err)
	}
	log.Println("Connected to PostgreSQL and migrated models")
}

func ConnectRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT"),
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
	jaegerURL := os.Getenv("JAEGER_ENDPOINT")
	if jaegerURL == "" {
		jaegerURL = "http://localhost:14268/api/traces"
	}

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
