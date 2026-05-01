package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSanitizeAPIKeySettings_MigratesLegacyAndNormalizes(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{
			APIKeyModels: []APIKeyModelRule{
				{
					APIKey:         "sk-123",
					DisabledModels: []string{" GPT-4O "},
				},
			},
			APIKeySettings: []APIKeySettings{
				{
					APIKey:         "sk-123",
					DisabledModels: []string{"claude-3-7-sonnet"},
					DisableLogging: true,
					Strategy:       "fillfirst",
					Note:           "  team A  ",
				},
			},
		},
	}

	cfg.SanitizeAPIKeySettings()

	if len(cfg.APIKeySettings) != 1 {
		t.Fatalf("len(APIKeySettings) = %d, want 1", len(cfg.APIKeySettings))
	}
	got := cfg.APIKeySettings[0]
	if got.APIKey != "sk-123" {
		t.Fatalf("APIKey = %q, want %q", got.APIKey, "sk-123")
	}
	if len(got.DisabledModels) != 2 || got.DisabledModels[0] != "claude-3-7-sonnet" || got.DisabledModels[1] != "gpt-4o" {
		t.Fatalf("DisabledModels = %#v, want [claude-3-7-sonnet gpt-4o]", got.DisabledModels)
	}
	if !got.DisableLogging {
		t.Fatalf("DisableLogging = %v, want true", got.DisableLogging)
	}
	if got.Strategy != "fill-first" {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, "fill-first")
	}
	if got.Note != "team A" {
		t.Fatalf("Note = %q, want %q", got.Note, "team A")
	}
	if cfg.APIKeyModels != nil {
		t.Fatalf("APIKeyModels = %#v, want nil after migration", cfg.APIKeyModels)
	}
}

func TestSaveConfigPreserveComments_RemovesLegacyAPIKeyModels(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	data := []byte("api-keys:\n  - sk-123\napi-key-models:\n  - api-key: sk-123\n    disabled-models:\n      - gpt-4o\n")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &Config{
		SDKConfig: SDKConfig{
			APIKeys: []string{"sk-123"},
			APIKeySettings: []APIKeySettings{
				{
					APIKey:         "sk-123",
					DisabledModels: []string{"gpt-4o"},
					DisableLogging: true,
					Strategy:       "round-robin",
				},
			},
		},
	}
	cfg.SanitizeAPIKeySettings()

	if err := SaveConfigPreserveComments(configPath, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments() error = %v", err)
	}

	rendered, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(rendered), "api-key-models:") {
		t.Fatalf("saved config still contains legacy api-key-models:\n%s", string(rendered))
	}
	if !strings.Contains(string(rendered), "api-key-settings:") {
		t.Fatalf("saved config missing api-key-settings:\n%s", string(rendered))
	}
	if !strings.Contains(string(rendered), "disable-logging: true") {
		t.Fatalf("saved config missing disable-logging:\n%s", string(rendered))
	}
}

func TestAPIKeySettingsUnmarshalYAML_AcceptsDisableLoggingAliases(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "kebab-case",
			data: "api-key: sk-123\ndisable-logging: true\n",
		},
		{
			name: "camelCase",
			data: "api-key: sk-123\ndisableLogging: true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got APIKeySettings
			if err := yaml.Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			if got.APIKey != "sk-123" {
				t.Fatalf("APIKey = %q, want %q", got.APIKey, "sk-123")
			}
			if !got.DisableLogging {
				t.Fatalf("DisableLogging = %v, want true", got.DisableLogging)
			}
		})
	}
}

func TestAPIKeySettingsUnmarshalJSON_AcceptsDisableLoggingAliases(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "kebab-case",
			data: `{"api-key":"sk-123","disable-logging":true}`,
		},
		{
			name: "camelCase",
			data: `{"api-key":"sk-123","disableLogging":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got APIKeySettings
			if err := json.Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got.APIKey != "sk-123" {
				t.Fatalf("APIKey = %q, want %q", got.APIKey, "sk-123")
			}
			if !got.DisableLogging {
				t.Fatalf("DisableLogging = %v, want true", got.DisableLogging)
			}
		})
	}
}

func TestConfigJSONMarshal_IncludesDisableLogging(t *testing.T) {
	cfg := &Config{
		SDKConfig: SDKConfig{
			APIKeySettings: []APIKeySettings{
				{
					APIKey:         "sk-123",
					DisableLogging: true,
				},
			},
		},
	}

	rendered, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(rendered), `"api-key-settings":[{"api-key":"sk-123","disable-logging":true}]`) {
		t.Fatalf("json output missing disable-logging:\n%s", string(rendered))
	}
}
