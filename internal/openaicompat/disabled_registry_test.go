package openaicompat

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestNormalizeProviderBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://ai-gateway.vercel.sh/v1/":   "https://ai-gateway.vercel.sh/v1",
		"AI-GATEWAY.VERCEL.SH/V1":            "http://ai-gateway.vercel.sh/v1",
		"https://example.com/v0/management":  "https://example.com",
		"HTTPS://EXAMPLE.COM/V0/MANAGEMENT/": "https://example.com",
		"  https://Example.com/Some/Path/  ": "https://example.com/some/path",
		"":                                   "",
	}
	for input, want := range tests {
		if got := NormalizeProviderBaseURL(input); got != want {
			t.Fatalf("NormalizeProviderBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadDisabledAPIKeyRegistryFromPath_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disabled.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":[`), 0o644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}
	if _, err := LoadDisabledAPIKeyRegistryFromPath(path); err == nil {
		t.Fatalf("expected invalid json error")
	}
}

func TestPruneDisabledAPIKeyRegistry(t *testing.T) {
	registry := &DisabledAPIKeyRegistry{
		Version: 1,
		Entries: []DisabledAPIKeyRegistryEntry{
			{
				ProviderName:    "Vercel",
				ProviderBaseURL: "https://ai-gateway.vercel.sh/v1/",
				APIKeyHash:      HashAPIKey("sk-live"),
				DisabledAt:      time.Now().UTC(),
			},
			{
				ProviderName:    "Ghost",
				ProviderBaseURL: "https://ghost.example.com/v1",
				APIKeyHash:      HashAPIKey("sk-gone"),
				DisabledAt:      time.Now().UTC(),
			},
		},
	}
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "Vercel",
			BaseURL: "https://ai-gateway.vercel.sh/v1",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
				APIKey: "sk-live",
			}},
		}},
	}
	changed := PruneDisabledAPIKeyRegistry(registry, cfg)
	if !changed {
		t.Fatalf("expected prune change")
	}
	if len(registry.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(registry.Entries))
	}
	if registry.Entries[0].ProviderBaseURL != "https://ai-gateway.vercel.sh/v1" {
		t.Fatalf("provider_base_url = %q", registry.Entries[0].ProviderBaseURL)
	}
	if !IsDisabled(registry, "Vercel", "https://ai-gateway.vercel.sh/v1", "sk-live") {
		t.Fatalf("expected entry to remain disabled")
	}
	if IsDisabled(registry, "Ghost", "https://ghost.example.com/v1", "sk-gone") {
		t.Fatalf("expected stale entry to be pruned")
	}
}
