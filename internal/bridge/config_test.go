package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveNeverPersistsRelayCredentials(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	cfg := &Config{
		Host:            "127.0.0.1",
		Port:            3001,
		RelayMode:       true,
		UpstreamBaseURL: "https://license.example.com",
		UpstreamAPIKey:  "tok_licence_token_value",
		UpstreamFormat:  FormatAnthropic,
		DefaultModel:    "claude-opus-4-8",
		ModelMap:        map[string]string{},
		EnvPath:         envPath,
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	written := string(data)

	if strings.Contains(written, "tok_licence_token_value") {
		t.Fatalf("the licence token was persisted to .env:\n%s", written)
	}
	if strings.Contains(written, "UPSTREAM_API_KEY=") {
		t.Fatalf("UPSTREAM_API_KEY must not be written in relay mode:\n%s", written)
	}
	if strings.Contains(written, "UPSTREAM_BASE_URL=") {
		t.Fatalf("UPSTREAM_BASE_URL must not be written in relay mode:\n%s", written)
	}
}

func TestSavePersistsDirectCredentials(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	cfg := &Config{
		Host:            "127.0.0.1",
		Port:            3001,
		UpstreamBaseURL: "https://gorouter.app",
		UpstreamAPIKey:  "sk-direct-key",
		UpstreamFormat:  FormatAnthropic,
		ModelMap:        map[string]string{},
		EnvPath:         envPath,
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "UPSTREAM_API_KEY=sk-direct-key") {
		t.Fatalf("a direct install must persist its own key:\n%s", data)
	}
}
