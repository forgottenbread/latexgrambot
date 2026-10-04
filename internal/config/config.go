// Package config reads the latexgrambot configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting.
type Config struct {
	TelegramBotToken string

	// MongoURI is the shared cluster connection string. MongoDatabase is
	// the bot's database inside it.
	MongoURI      string
	MongoDatabase string

	// S3 hosts rendered inline files. All required values must be set
	// together; otherwise inline mode offers only native rich text.
	S3Endpoint        string
	S3Region          string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Prefix          string
	S3PublicEndpoint  string
	S3PresignTTL      time.Duration

	HealthAddr string

	// RichTextEnabled controls the rich-message replies (native math block).
	// The format needs a recent Bot API server; when disabled the bot falls
	// back to PNG and PDF only.
	RichTextEnabled bool

	// RichDefaultMath controls how the rich format treats ambiguous bare
	// source. It defaults to false so non-math LaTeX is never guessed to be
	// math; formulas need explicit delimiters unless operators opt into the
	// legacy behavior.
	RichDefaultMath bool

	RenderDPI      int
	RenderTimeout  time.Duration
	MaxConcurrency int

	// MaxExpressionLen caps chat-mode expressions. MaxPreambleLen caps the
	// per-user preamble (Telegram messages are at most 4096 characters).
	MaxExpressionLen int
	MaxPreambleLen   int

	PdflatexBin string
	PdftoppmBin string

	// LatexFormat is the preloaded TeX format name (built from the default
	// preamble at image build time). Empty disables it.
	LatexFormat string
}

// Load builds the Config and validates the mandatory keys.
func Load() (*Config, error) {
	cfg := &Config{
		TelegramBotToken:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		MongoURI:          strings.TrimSpace(os.Getenv("MONGODB_URI")),
		MongoDatabase:     getenv("MONGODB_DATABASE", "latexgram"),
		S3Endpoint:        strings.TrimRight(strings.TrimSpace(os.Getenv("S3_ENDPOINT_URL")), "/"),
		S3Region:          getenv("S3_REGION", "us-east-1"),
		S3Bucket:          strings.TrimSpace(os.Getenv("S3_BUCKET")),
		S3AccessKeyID:     strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
		S3SecretAccessKey: strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
		S3Prefix:          strings.Trim(getenv("S3_PREFIX", "content"), "/"),
		S3PublicEndpoint:  strings.TrimRight(strings.TrimSpace(os.Getenv("S3_PUBLIC_ENDPOINT_URL")), "/"),
		HealthAddr:        getenv("HEALTH_ADDR", ":8080"),
		PdflatexBin:       getenv("PDFLATEX_BIN", "pdflatex"),
		PdftoppmBin:       getenv("PDFTOPPM_BIN", "pdftoppm"),
		LatexFormat:       getenv("LATEX_FORMAT", ""),
	}

	var err error
	if cfg.RichTextEnabled, err = getenvBool("RICH_TEXT_ENABLED", true); err != nil {
		return nil, err
	}
	if cfg.RichDefaultMath, err = getenvBool("RICH_DEFAULT_MATH", false); err != nil {
		return nil, err
	}
	if cfg.RenderDPI, err = getenvInt("RENDER_DPI", 600); err != nil {
		return nil, err
	}
	if cfg.MaxConcurrency, err = getenvInt("RENDER_MAX_CONCURRENCY", 2); err != nil {
		return nil, err
	}
	if cfg.MaxExpressionLen, err = getenvInt("MAX_EXPRESSION_LEN", 4000); err != nil {
		return nil, err
	}
	if cfg.MaxPreambleLen, err = getenvInt("MAX_PREAMBLE_LEN", 4000); err != nil {
		return nil, err
	}
	if cfg.RenderTimeout, err = getenvDuration("RENDER_TIMEOUT", 30*time.Second); err != nil {
		return nil, err
	}
	if cfg.S3PresignTTL, err = getenvDuration("S3_PRESIGN_TTL", time.Hour); err != nil {
		return nil, err
	}

	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("missing required environment variable: TELEGRAM_BOT_TOKEN")
	}
	if cfg.MongoURI == "" {
		return nil, fmt.Errorf("missing required environment variable: MONGODB_URI")
	}
	if cfg.RenderDPI < 72 || cfg.RenderDPI > 1200 {
		return nil, fmt.Errorf("RENDER_DPI must be between 72 and 1200")
	}
	if cfg.RenderTimeout <= 0 {
		return nil, fmt.Errorf("RENDER_TIMEOUT must be positive")
	}
	if cfg.MaxConcurrency < 1 {
		return nil, fmt.Errorf("RENDER_MAX_CONCURRENCY must be at least 1")
	}
	if cfg.MaxExpressionLen < 1 || cfg.MaxPreambleLen < 1 {
		return nil, fmt.Errorf("MAX_EXPRESSION_LEN and MAX_PREAMBLE_LEN must be positive")
	}
	s3Set := 0
	for _, value := range []string{
		cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID,
		cfg.S3SecretAccessKey, cfg.S3PublicEndpoint,
	} {
		if value != "" {
			s3Set++
		}
	}
	if s3Set != 0 && s3Set != 5 {
		return nil, fmt.Errorf(
			"incomplete S3 configuration: set S3_ENDPOINT_URL, S3_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and S3_PUBLIC_ENDPOINT_URL together")
	}
	if cfg.S3PresignTTL <= 0 {
		return nil, fmt.Errorf("S3_PRESIGN_TTL must be positive")
	}
	return cfg, nil
}

// S3Enabled reports whether private inline PNG/PDF assets can be stored and
// exposed through short-lived presigned URLs.
func (c *Config) S3Enabled() bool {
	return c.S3Endpoint != "" && c.S3Bucket != "" &&
		c.S3AccessKeyID != "" && c.S3SecretAccessKey != "" &&
		c.S3PublicEndpoint != ""
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func getenvBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func getenvDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}
