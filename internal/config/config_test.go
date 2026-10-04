package config

import (
	"strings"
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("MONGODB_URI", "mongodb://example:27017")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RenderDPI != 600 {
		t.Errorf("RenderDPI = %d, want 600", cfg.RenderDPI)
	}
	if cfg.RenderTimeout != 30*time.Second {
		t.Errorf("RenderTimeout = %s, want 30s", cfg.RenderTimeout)
	}
	if cfg.MaxConcurrency != 2 {
		t.Errorf("MaxConcurrency = %d, want 2", cfg.MaxConcurrency)
	}
	if cfg.MaxExpressionLen != 4000 || cfg.MaxPreambleLen != 4000 {
		t.Errorf("unexpected length caps: %d/%d", cfg.MaxExpressionLen, cfg.MaxPreambleLen)
	}
	if cfg.HealthAddr != ":8080" {
		t.Errorf("HealthAddr = %q, want :8080", cfg.HealthAddr)
	}
	if cfg.MongoDatabase != "latexgram" {
		t.Errorf("MongoDatabase = %q, want latexgram", cfg.MongoDatabase)
	}
	if !cfg.RichTextEnabled {
		t.Error("RichTextEnabled = false, want true by default")
	}
	if !cfg.RichDefaultMath {
		t.Error("RichDefaultMath = false, want true by default")
	}
	if cfg.S3PresignTTL != time.Hour {
		t.Errorf("S3PresignTTL = %s, want 1h", cfg.S3PresignTTL)
	}
	if cfg.S3Enabled() {
		t.Error("S3Enabled = true without S3 configuration")
	}
}

func TestLoadS3(t *testing.T) {
	setRequired(t)
	t.Setenv("S3_ENDPOINT_URL", "http://minio:9000/")
	t.Setenv("S3_BUCKET", "latexgrambot")
	t.Setenv("AWS_ACCESS_KEY_ID", "access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("S3_PUBLIC_ENDPOINT_URL", "https://s3.example/")
	t.Setenv("S3_PRESIGN_TTL", "45m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.S3Enabled() || cfg.S3Endpoint != "http://minio:9000" || cfg.S3PublicEndpoint != "https://s3.example" {
		t.Fatalf("unexpected S3 config: %+v", cfg)
	}
	if cfg.S3PresignTTL != 45*time.Minute {
		t.Errorf("S3PresignTTL = %s", cfg.S3PresignTTL)
	}
}

func TestLoadRejectsIncompleteS3(t *testing.T) {
	setRequired(t)
	t.Setenv("S3_BUCKET", "latexgrambot")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "incomplete S3 configuration") {
		t.Fatalf("expected incomplete S3 error, got %v", err)
	}
}

func TestLoadRichDefaultMathToggle(t *testing.T) {
	setRequired(t)
	t.Setenv("RICH_DEFAULT_MATH", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RichDefaultMath {
		t.Error("RichDefaultMath = true, want false")
	}

	t.Setenv("RICH_DEFAULT_MATH", "banana")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RICH_DEFAULT_MATH") {
		t.Fatalf("expected RICH_DEFAULT_MATH validation error, got %v", err)
	}
}

func TestLoadRichTextToggle(t *testing.T) {
	setRequired(t)
	t.Setenv("RICH_TEXT_ENABLED", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RichTextEnabled {
		t.Error("RichTextEnabled = true, want false")
	}

	t.Setenv("RICH_TEXT_ENABLED", "banana")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RICH_TEXT_ENABLED") {
		t.Fatalf("expected RICH_TEXT_ENABLED validation error, got %v", err)
	}
}

func TestLoadMissingToken(t *testing.T) {
	t.Setenv("MONGODB_URI", "mongodb://example:27017")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestLoadMissingMongoURI(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "MONGODB_URI") {
		t.Fatalf("expected missing MONGODB_URI error, got %v", err)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	setRequired(t)
	t.Setenv("RENDER_DPI", "10")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RENDER_DPI") {
		t.Fatalf("expected RENDER_DPI validation error, got %v", err)
	}

	t.Setenv("RENDER_DPI", "300")
	t.Setenv("RENDER_TIMEOUT", "banana")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RENDER_TIMEOUT") {
		t.Fatalf("expected RENDER_TIMEOUT validation error, got %v", err)
	}
}
