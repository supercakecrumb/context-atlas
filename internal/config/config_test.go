package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/context_atlas")
	for _, name := range []string{"PORT", "LOG_LEVEL", "PUBLIC_BASE_URL", "WEB_DIST_DIR", "REFERENCE_DIR", "TELEGRAM_BOT_TOKEN", "ADMIN_TG_IDS", "SESSION_ENC_KEY", "SNAGBOX_URL", "SNAGBOX_INGEST_TOKEN", "REFRESH_INTERVAL", "REFRESH_HOUR_UTC", "REFRESH_MINUTE_UTC"} {
		t.Setenv(name, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.RefreshInterval != 24*time.Hour || cfg.ReferenceDir != "assets/reference" || cfg.AuthEnabled() || cfg.SnagboxEnabled() {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfiguredIntegrations(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/context_atlas")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("ADMIN_TG_IDS", "42")
	t.Setenv("SESSION_ENC_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	t.Setenv("SNAGBOX_URL", "https://snagbox.example/")
	t.Setenv("SNAGBOX_INGEST_TOKEN", "write-only")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AuthEnabled() || !cfg.SnagboxEnabled() || cfg.SnagboxURL != "https://snagbox.example" {
		t.Fatalf("integrations not configured: %+v", cfg)
	}
}

func TestLoadRejectsPartialAuth(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/context_atlas")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("ADMIN_TG_IDS", "")
	t.Setenv("SESSION_ENC_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected partial auth configuration to fail")
	}
}

func TestLoadRejectsMultipleAdmins(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example/context_atlas")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("ADMIN_TG_IDS", "42,43")
	t.Setenv("SESSION_ENC_KEY", strings.Repeat("k", 32))
	if _, err := Load(); err == nil {
		t.Fatal("expected multiple owner IDs to fail")
	}
}
