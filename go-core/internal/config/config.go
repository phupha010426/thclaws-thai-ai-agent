// Package config loads environment variables for every Go service in the
// project. Tenant isolation is the default: anything that needs to namespace
// data per LINE userId is required, not optional.
package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	AppName       string `env:"APP_NAME" envDefault:"ThaiAiAgent"`
	NodeEnv       string `env:"NODE_ENV" envDefault:"production"`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`
	Port          int    `env:"PORT" envDefault:"3000"`
	PublicBaseURL string `env:"PUBLIC_BASE_URL" envDefault:"https://thaiaiagent.smlsoft.app"`

	// LINE
	LineChannelAccessToken string `env:"LINE_CHANNEL_ACCESS_TOKEN,required"`
	LineChannelSecret      string `env:"LINE_CHANNEL_SECRET,required"`
	LiffID                 string `env:"LIFF_ID"`
	LiffChannelID          string `env:"LIFF_CHANNEL_ID"`

	// Databases
	PostgresURL string `env:"POSTGRES_URL,required"`
	MongoURL    string `env:"MONGODB_URL,required"`
	RedisURL    string `env:"REDIS_URL,required"`
	RedisPrefix string `env:"REDIS_QUEUE_PREFIX" envDefault:"thaiagent"`

	// MinIO
	MinioEndpoint     string `env:"MINIO_ENDPOINT" envDefault:"minio"`
	MinioPort         int    `env:"MINIO_PORT" envDefault:"9000"`
	MinioUseSSL       bool   `env:"MINIO_USE_SSL" envDefault:"false"`
	MinioAccessKey    string `env:"MINIO_ACCESS_KEY,required"`
	MinioSecretKey    string `env:"MINIO_SECRET_KEY,required"`
	MinioBucket       string `env:"MINIO_BUCKET" envDefault:"thaiaiagent"`
	ImageAccessSecret string `env:"IMAGE_ACCESS_SECRET"`

	// SMLGateway
	GatewayURL              string `env:"THCLAWS_GATEWAY_URL,required"`
	GatewayAPIKey           string `env:"THCLAWS_API_KEY,required"`
	GatewayTimeoutMs        int    `env:"THCLAWS_TIMEOUT_MS" envDefault:"30000"`
	LineReplyTimeoutMs      int    `env:"LINE_REPLY_TIMEOUT_MS" envDefault:"35000"`
	GatewayMaxLatencyMs     int    `env:"SMLGATEWAY_MAX_LATENCY_MS" envDefault:"3000"`
	GatewayPreferProviders  string `env:"SMLGATEWAY_PREFER_PROVIDERS" envDefault:"typhoon,thaillm,groq,cerebras"`
	GatewayExcludeProviders string `env:"SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS" envDefault:"mistral"`

	// Models
	DefaultAgentModel       string `env:"DEFAULT_AGENT_MODEL" envDefault:"sml/auto"`
	FastAgentModel          string `env:"FAST_AGENT_MODEL" envDefault:"sml/fast"`
	GeneralAgentModel       string `env:"GENERAL_AGENT_MODEL" envDefault:"sml/thai"`
	VisionAgentModel        string `env:"VISION_AGENT_MODEL" envDefault:"sml/auto"`
	TextFallbackModels      string `env:"TEXT_FALLBACK_MODELS" envDefault:""`
	VisionFallbackModels    string `env:"VISION_FALLBACK_MODELS" envDefault:""`
	EmbeddingModel          string `env:"EMBEDDING_MODEL" envDefault:"bge-m3"`
	EmbeddingFallbackModels string `env:"EMBEDDING_FALLBACK_MODELS" envDefault:""`

	// NATS
	NatsURL        string `env:"NATS_URL"`
	NatsStreamName string `env:"NATS_STREAM_NAME" envDefault:"thaiagent"`

	// Webhook security
	WebhookReplayWindowSec int    `env:"WEBHOOK_REPLAY_WINDOW_SECONDS" envDefault:"300"`
	WebhookBodyLimit       string `env:"WEBHOOK_BODY_LIMIT" envDefault:"1mb"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() // .env is optional in container deploys
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

func (c *Config) ListString(value string) []string {
	if value == "" {
		return nil
	}
	out := make([]string, 0, 4)
	current := ""
	for _, ch := range value {
		if ch == ',' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
			continue
		}
		current += string(ch)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}
