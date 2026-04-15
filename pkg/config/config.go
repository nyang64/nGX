package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment string
	LogLevel    string
	LogFormat   string

	Database  DatabaseConfig
	Kafka     KafkaConfig
	Redis     RedisConfig
	S3        S3Config
	API       APIConfig
	Auth      AuthConfig
	SMTP      SMTPConfig
	Webhook   WebhookConfig
	OTEL      OTELConfig

	AuthServiceURL    string
	InboxServiceURL   string
	WebhookServiceURL string
	SearchServiceURL  string

	EmbedderURL   string
	EmbedderModel string

	// MailDomain is the default domain for provisioning inboxes (e.g. "mail.yourdomain.com").
	// Required for self-hosted deployments. Set via the MAIL_DOMAIN environment variable.
	MailDomain string
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type KafkaConfig struct {
	Brokers []string
	GroupID string
}

type RedisConfig struct {
	URL string
}

type S3Config struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

type APIConfig struct {
	Host string
	Port int
}

type AuthConfig struct {
	// InternalSecret is the signing key for inter-service communication.
	InternalSecret string
}

type SMTPConfig struct {
	ListenAddr string // e.g. ":25" or ":2525" for local dev
	Hostname   string
	// RelayHost overrides MX lookup for outbound delivery (e.g. "localhost:1025" for Mailhog in dev).
	RelayHost string
	// DKIM signing for outbound email.
	DKIMPrivateKeyPEM string // PEM-encoded RSA or Ed25519 private key
	DKIMSelector      string // DNS selector subdomain
	DKIMDomain        string // Signing domain — must match MAIL_DOMAIN or a verified custom domain
}

type WebhookConfig struct {
	Concurrency int
	MaxRetries  int
	Port        int
	// EncryptionKey is a 64-char hex-encoded 32-byte AES-256 key used to
	// encrypt webhook auth header values at rest.
	EncryptionKey string
}

type OTELConfig struct {
	Endpoint    string
	ServiceName string
}

func Load() *Config {
	cfg := &Config{
		Environment: getEnv("ENVIRONMENT", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "json"),
		Database: DatabaseConfig{
			URL:             getEnv("DATABASE_URL", "postgres://agentmail:agentmail_dev@localhost:5432/agentmail?sslmode=disable"),
			MaxConns:        int32(getEnvInt("DB_MAX_CONNS", 25)),
			MinConns:        int32(getEnvInt("DB_MIN_CONNS", 5)),
			MaxConnLifetime: time.Duration(getEnvInt("DB_MAX_CONN_LIFETIME_SEC", 3600)) * time.Second,
			MaxConnIdleTime: time.Duration(getEnvInt("DB_MAX_CONN_IDLE_SEC", 300)) * time.Second,
		},
		Kafka: KafkaConfig{
			Brokers: strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
			GroupID: getEnv("KAFKA_GROUP_ID", "agentmail"),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", "redis://localhost:6379"),
		},
		S3: S3Config{
			Endpoint:        getEnv("S3_ENDPOINT", "http://localhost:9000"),
			Bucket:          getEnv("S3_BUCKET", "agentmail"),
			Region:          getEnv("S3_REGION", "us-east-1"),
			AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", "minioadmin"),
			SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", "minioadmin"),
			UsePathStyle:    getEnvBool("S3_USE_PATH_STYLE", true),
		},
		API: APIConfig{
			Host: getEnv("API_HOST", "0.0.0.0"),
			Port: getEnvInt("API_PORT", 8080),
		},
		Auth: AuthConfig{
			InternalSecret: getEnv("INTERNAL_SECRET", "dev-internal-secret-change-in-prod"),
		},
		SMTP: SMTPConfig{
			ListenAddr:        getEnv("SMTP_LISTEN_ADDR", ":2525"),
			Hostname:          getEnv("SMTP_HOSTNAME", "localhost"),
			RelayHost:         getEnv("SMTP_RELAY_HOST", ""),
			DKIMPrivateKeyPEM: getEnv("DKIM_PRIVATE_KEY_PEM", ""),
			DKIMSelector:      getEnv("DKIM_SELECTOR", "mail"),
			DKIMDomain:        getEnv("DKIM_DOMAIN", ""),
		},
		Webhook: WebhookConfig{
			Concurrency:   getEnvInt("WEBHOOK_CONCURRENCY", 10),
			MaxRetries:    getEnvInt("WEBHOOK_MAX_RETRIES", 8),
			Port:          getEnvInt("WEBHOOK_PORT", 8083),
			EncryptionKey: getEnv("WEBHOOK_ENCRYPTION_KEY", ""),
		},
		OTEL: OTELConfig{
			Endpoint:    getEnv("OTEL_ENDPOINT", ""),
			ServiceName: getEnv("OTEL_SERVICE_NAME", "agentmail"),
		},
		AuthServiceURL:    getEnv("AUTH_SERVICE_URL", "http://localhost:8081"),
		InboxServiceURL:   getEnv("INBOX_SERVICE_URL", "http://localhost:8082"),
		WebhookServiceURL: getEnv("WEBHOOK_SERVICE_URL", "http://localhost:8083"),
		SearchServiceURL:  getEnv("SEARCH_SERVICE_URL", "http://localhost:8084"),

		EmbedderURL:   getEnv("EMBEDDER_URL", "http://localhost:7997"),
		EmbedderModel: getEnv("EMBEDDER_MODEL", "nomic-embed-text-v1.5"),

		MailDomain: getEnv("MAIL_DOMAIN", ""),
	}
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
