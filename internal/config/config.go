package config

import (
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppConfig AppConfig
	DBConfig  DBConfig
	Storage   StorageConfig
	Log       LogConfig
	SMTP      SMTPConfig
}

type DBConfig struct {
	User            string
	Password        string
	Name            string
	Host            string
	Port            string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthTimeout   time.Duration
	ConnectTimeout  time.Duration
}

type AppConfig struct {
	Port             string
	Env              string
	JWTSecret        string
	JWTAccessExpiry  time.Duration
	JWTRefreshExpiry time.Duration
	BcryptCost       int
	RateLimit        int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
	CORSOrigins      string
	CORSMaxAge       string
	DefaultLimit     int
	MaxLimit         int
	DefaultCurrency  string
	FrontendURL      string
}

type StorageConfig struct {
	APIEndpoint           string
	ConsoleEndpoint       string
	AccessKey             string
	SecretKey             string
	PublicBucket          string
	PrivateBucket         string
	UseSSL                bool
	MaxFileSize           int64
	InitTimeout           time.Duration
	PresignedUploadExpiry time.Duration
	PresignedViewExpiry   time.Duration
}

type LogConfig struct {
	Level  slog.Level
	Format string
}

type SMTPConfig struct {
	Host          string
	Port          int
	Username      string
	Password      string
	FromEmail     string
	FromName      string
	AuthRequired  bool
	UseTLS        bool
	ExpiryMinutes int
}

func (d DBConfig) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   net.JoinHostPort(d.Host, d.Port),
		Path:   d.Name,
	}

	q := u.Query()
	if d.SSLMode != "" {
		q.Set("sslmode", d.SSLMode)
	} else {
		q.Set("sslmode", "disable")
	}
	u.RawQuery = q.Encode()

	return u.String()
}

func LoadConfig() Config {
	return Config{
		AppConfig: AppConfig{
			Port:             getEnvWithFallback([]string{"PORT", "APP_PORT"}, "8080"),
			Env:              getEnv("APP_ENV", "development"),
			JWTSecret:        getEnv("JWT_SECRET", "your-secret-key"),
			JWTAccessExpiry:  getEnvAsDuration("JWT_ACCESS_EXPIRY", 15*time.Minute),
			JWTRefreshExpiry: getEnvAsDuration("JWT_REFRESH_EXPIRY", 168*time.Hour),
			BcryptCost:       getEnvAsInt("BCRYPT_COST", 10),
			RateLimit:        getEnvAsInt("RATE_LIMIT", 60),
			ReadTimeout:      getEnvAsDuration("APP_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:     getEnvAsDuration("APP_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:      getEnvAsDuration("APP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:  getEnvAsDuration("APP_SHUTDOWN_TIMEOUT", 20*time.Second),
			CORSOrigins:      getEnv("CORS_ALLOWED_ORIGINS", "*"),
			CORSMaxAge:       getEnv("CORS_MAX_AGE_SECONDS", "86400"),
			DefaultLimit:     getEnvAsInt("PAGINATION_DEFAULT_LIMIT", 20),
			MaxLimit:         getEnvAsInt("PAGINATION_MAX_LIMIT", 100),
			DefaultCurrency:  getEnv("DEFAULT_CURRENCY", "IDR"),
			FrontendURL:      getEnvWithFallback([]string{"FRONTEND_URL", "APP_FRONTEND_URL"}, "http://localhost:8080"),
		},
		DBConfig: DBConfig{
			User:            getEnv("DB_USER", "postgres"),
			Password:        getEnv("DB_PASSWORD", "postgres"),
			Name:            getEnv("DB_NAME", "bbj_database"),
			Host:            getEnv("DB_HOST", "127.0.0.1"),
			Port:            getEnv("DB_PORT", "5432"),
			SSLMode:         getEnvWithFallback([]string{"DB_SSLMODE", "DB_SSL_MODE"}, "disable"),
			MaxConns:        getEnvAsInt32("DB_MAX_CONNS", 25),
			MinConns:        getEnvAsInt32("DB_MIN_CONNS", 5),
			MaxConnLifetime: getEnvAsDuration("DB_MAX_CONN_LIFETIME", 1*time.Hour),
			MaxConnIdleTime: getEnvAsDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
			HealthTimeout:   getEnvAsDuration("DB_HEALTH_TIMEOUT", 3*time.Second),
			ConnectTimeout:  getEnvAsDuration("DB_CONNECT_TIMEOUT", 15*time.Second),
		},
		Storage: StorageConfig{
			APIEndpoint:           getEnv("RUSTFS_API_ENDPOINT", "http://127.0.0.1:9000"),
			ConsoleEndpoint:       getEnv("RUSTFS_CONSOLE_ENDPOINT", "http://127.0.0.1:9001"),
			AccessKey:             getEnv("RUSTFS_ACCESS_KEY", "rustfsadmin"),
			SecretKey:             getEnv("RUSTFS_SECRET_KEY", "rustfsadmin"),
			PublicBucket:          getEnv("RUSTFS_PUBLIC_BUCKET", "public"),
			PrivateBucket:         getEnv("RUSTFS_PRIVATE_BUCKET", "private"),
			UseSSL:                getEnvAsBool("RUSTFS_USE_SSL", false),
			MaxFileSize:           getEnvAsInt64("RUSTFS_MAX_FILE_SIZE", 26214400),
			InitTimeout:           getEnvAsDuration("RUSTFS_INIT_TIMEOUT", 5*time.Second),
			PresignedUploadExpiry: getEnvAsDuration("RUSTFS_PRESIGNED_UPLOAD_EXPIRY", 15*time.Minute),
			PresignedViewExpiry:   getEnvAsDuration("RUSTFS_PRESIGNED_VIEW_EXPIRY", 5*time.Minute),
		},
		Log: LogConfig{
			Level:  parseLogLevel(getEnv("LOG_LEVEL", "info")),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		SMTP: SMTPConfig{
			Host:          getEnv("SMTP_HOST", "127.0.0.1"),
			Port:          getEnvAsInt("SMTP_PORT", 1025),
			Username:      getEnv("SMTP_USERNAME", ""),
			Password:      getEnv("SMTP_PASSWORD", ""),
			FromEmail:     getEnv("SMTP_FROM_EMAIL", "info@berbagibitesjogja.com"),
			FromName:      getEnv("SMTP_FROM_NAME", "Berbagi Bites Jogja"),
			AuthRequired:  getEnvAsBool("SMTP_AUTH_REQUIRED", false),
			UseTLS:        getEnvAsBool("SMTP_USE_TLS", false),
			ExpiryMinutes: getEnvAsInt("SMTP_EXPIRY_MINUTES", 15),
		},
	}
}

func cleanEnv(val string) string {
	if idx := strings.Index(val, "#"); idx != -1 {
		val = val[:idx]
	}
	return strings.TrimSpace(val)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return cleanEnv(val)
	}
	return defaultVal
}

func getEnvWithFallback(keys []string, defaultVal string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return cleanEnv(val)
		}
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	cleaned := cleanEnv(valStr)
	val, err := strconv.Atoi(cleaned)
	if err != nil {
		slog.Warn("Invalid integer for env var, falling back to default", "key", key, "value", valStr, "default", defaultVal)
		return defaultVal
	}
	return val
}

func getEnvAsInt32(key string, defaultVal int32) int32 {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	cleaned := cleanEnv(valStr)
	val, err := strconv.ParseInt(cleaned, 10, 32)
	if err != nil {
		slog.Warn("Invalid integer for env var, falling back to default", "key", key, "value", valStr, "default", defaultVal)
		return defaultVal
	}
	return int32(val)
}

func getEnvAsInt64(key string, defaultVal int64) int64 {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	cleaned := cleanEnv(valStr)
	val, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil {
		slog.Warn("Invalid integer for env var, falling back to default", "key", key, "value", valStr, "default", defaultVal)
		return defaultVal
	}
	return val
}

func getEnvAsDuration(key string, defaultVal time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	cleaned := cleanEnv(valStr)
	d, err := time.ParseDuration(cleaned)
	if err != nil {
		slog.Warn("Invalid duration for env var, falling back to default", "key", key, "value", valStr, "default", defaultVal)
		return defaultVal
	}
	return d
}

func getEnvAsBool(key string, defaultVal bool) bool {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	cleaned := cleanEnv(valStr)
	val, err := strconv.ParseBool(cleaned)
	if err != nil {
		slog.Warn("Invalid boolean for env var, falling back to default", "key", key, "value", valStr, "default", defaultVal)
		return defaultVal
	}
	return val
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
