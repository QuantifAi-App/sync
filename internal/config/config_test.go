package config

import (
	"os"
	"path/filepath"
	"testing"
)

// clearAllEnvVars unsets all known env vars to avoid interference between tests.
func clearAllEnvVars(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AI_OPS_API_URL", "AI_OPS_API_KEY", "AI_OPS_SYNC_ENABLED",
		"AI_OPS_BATCH_SIZE", "AI_OPS_FLUSH_INTERVAL", "AI_OPS_HEALTH_PORT",
		"AI_OPS_LOG_LEVEL", "AI_OPS_AUTO_UPDATE", "AI_OPS_UPDATE_CHANNEL",
		"AI_OPS_WATCH_DIR", "AI_OPS_STATE_FILE", "AI_OPS_LOG_FILE",
		"QUANTIFAI_API_URL", "QUANTIFAI_API_KEY", "QUANTIFAI_SYNC_ENABLED",
		"QUANTIFAI_BATCH_SIZE", "QUANTIFAI_FLUSH_INTERVAL", "QUANTIFAI_HEALTH_PORT",
		"QUANTIFAI_LOG_LEVEL", "QUANTIFAI_AUTO_UPDATE", "QUANTIFAI_UPDATE_CHANNEL",
		"QUANTIFAI_WATCH_DIR", "QUANTIFAI_STATE_FILE", "QUANTIFAI_LOG_FILE",
	} {
		t.Setenv(key, "")
	}
}

// TestLayeredConfigResolution verifies that config values follow the
// precedence: env var > user config > system config > defaults.
func TestLayeredConfigResolution(t *testing.T) {
	// Create temp directories for config files
	tmpDir := t.TempDir()

	systemPath := filepath.Join(tmpDir, "system.toml")
	userPath := filepath.Join(tmpDir, "user.toml")

	// System config: lowest file-layer priority
	os.WriteFile(systemPath, []byte(`api_url = "https://system.example.com"
batch_size = 3000
log_level = "debug"
`), 0600)

	// User config: overrides system
	os.WriteFile(userPath, []byte(`api_url = "https://user.example.com"
batch_size = 4000
`), 0600)

	// Env var: highest priority -- override api_url
	clearAllEnvVars(t)
	t.Setenv("AI_OPS_API_URL", "https://env.example.com")

	cfg, err := Load(userPath, systemPath)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// api_url: env wins over user config
	if cfg.APIURL != "https://env.example.com" {
		t.Errorf("APIURL: got %q, want %q", cfg.APIURL, "https://env.example.com")
	}

	// batch_size: user config wins over system config (no env set)
	if cfg.BatchSize != 4000 {
		t.Errorf("BatchSize: got %d, want %d", cfg.BatchSize, 4000)
	}

	// log_level: system config wins over default (user config did not set it)
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "debug")
	}
}

// TestEnvVarOverrides verifies that individual env vars override config.
func TestEnvVarOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(`api_url = "https://file.example.com"
api_key = "file-key"
sync_enabled = false
`), 0600)

	clearAllEnvVars(t)
	t.Setenv("AI_OPS_API_URL", "https://env.example.com")
	t.Setenv("AI_OPS_API_KEY", "env-key")
	t.Setenv("AI_OPS_SYNC_ENABLED", "true")

	cfg, err := Load(configPath, filepath.Join(tmpDir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.APIURL != "https://env.example.com" {
		t.Errorf("APIURL: got %q, want %q", cfg.APIURL, "https://env.example.com")
	}
	if cfg.APIKey != "env-key" {
		t.Errorf("APIKey: got %q, want %q", cfg.APIKey, "env-key")
	}
	if !cfg.SyncEnabled {
		t.Error("SyncEnabled: got false, want true")
	}
}

// TestConfigDefaults verifies that default values match the spec.
func TestConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	emptyConfig := filepath.Join(tmpDir, "empty.toml")
	os.WriteFile(emptyConfig, []byte(""), 0600)

	clearAllEnvVars(t)

	cfg, err := Load(emptyConfig, filepath.Join(tmpDir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.BatchSize != 5000 {
		t.Errorf("BatchSize: got %d, want %d", cfg.BatchSize, 5000)
	}
	if cfg.FlushInterval != 60 {
		t.Errorf("FlushInterval: got %d, want %d", cfg.FlushInterval, 60)
	}
	if cfg.HealthPort != 19876 {
		t.Errorf("HealthPort: got %d, want %d", cfg.HealthPort, 19876)
	}
	if cfg.AutoUpdate {
		t.Error("AutoUpdate: got true, want false")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if !cfg.SyncEnabled {
		t.Error("SyncEnabled: got false, want true")
	}
	if cfg.UpdateChannel != "stable" {
		t.Errorf("UpdateChannel: got %q, want %q", cfg.UpdateChannel, "stable")
	}
}

// TestConfigValidationRejectsMissingAPIURL verifies that Validate returns
// an error when api_url is empty.
func TestConfigValidationRejectsMissingAPIURL(t *testing.T) {
	cfg := Config{} // api_url is zero value (empty string)
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing api_url, got nil")
	}

	cfg.APIURL = "https://example.com"
	err = Validate(cfg)
	if err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase B: QUANTIFAI_* env vars take precedence over AI_OPS_*
// ---------------------------------------------------------------------------

func TestQuantifaiEnvVarPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	emptyConfig := filepath.Join(tmpDir, "empty.toml")
	os.WriteFile(emptyConfig, []byte(""), 0600)

	clearAllEnvVars(t)

	// Set both old and new env vars -- new should win
	t.Setenv("AI_OPS_API_URL", "https://old.example.com")
	t.Setenv("QUANTIFAI_API_URL", "https://new.example.com")

	t.Setenv("AI_OPS_BATCH_SIZE", "1000")
	t.Setenv("QUANTIFAI_BATCH_SIZE", "2000")

	cfg, err := Load(emptyConfig, filepath.Join(tmpDir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.APIURL != "https://new.example.com" {
		t.Errorf("APIURL: got %q, want new env var value", cfg.APIURL)
	}
	if cfg.BatchSize != 2000 {
		t.Errorf("BatchSize: got %d, want 2000 (new env var)", cfg.BatchSize)
	}
}

func TestLegacyEnvVarStillWorks(t *testing.T) {
	tmpDir := t.TempDir()
	emptyConfig := filepath.Join(tmpDir, "empty.toml")
	os.WriteFile(emptyConfig, []byte(""), 0600)

	clearAllEnvVars(t)

	// Only legacy env var set -- should still work
	t.Setenv("AI_OPS_API_URL", "https://legacy.example.com")
	t.Setenv("AI_OPS_API_KEY", "legacy-key")

	cfg, err := Load(emptyConfig, filepath.Join(tmpDir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.APIURL != "https://legacy.example.com" {
		t.Errorf("APIURL: got %q, want legacy env var value", cfg.APIURL)
	}
	if cfg.APIKey != "legacy-key" {
		t.Errorf("APIKey: got %q, want legacy-key", cfg.APIKey)
	}
}

func TestEnvOrLegacy(t *testing.T) {
	t.Setenv("QUANTIFAI_TEST_KEY", "new-value")
	t.Setenv("AI_OPS_TEST_KEY", "old-value")

	// New key takes precedence
	if got := envOrLegacy("QUANTIFAI_TEST_KEY", "AI_OPS_TEST_KEY"); got != "new-value" {
		t.Errorf("envOrLegacy: got %q, want new-value", got)
	}

	// Clear new key -- legacy should work
	t.Setenv("QUANTIFAI_TEST_KEY", "")
	if got := envOrLegacy("QUANTIFAI_TEST_KEY", "AI_OPS_TEST_KEY"); got != "old-value" {
		t.Errorf("envOrLegacy: got %q, want old-value", got)
	}

	// Clear both -- should return empty
	t.Setenv("AI_OPS_TEST_KEY", "")
	if got := envOrLegacy("QUANTIFAI_TEST_KEY", "AI_OPS_TEST_KEY"); got != "" {
		t.Errorf("envOrLegacy: got %q, want empty", got)
	}
}
