package openaicompat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

const disabledAPIKeyRegistryRelativePath = "management/disabled-openai-api-key-entries.json"

type DisabledAPIKeyRegistry struct {
	Version   int                           `json:"version"`
	UpdatedAt time.Time                     `json:"updated_at"`
	Entries   []DisabledAPIKeyRegistryEntry `json:"entries"`
}

type DisabledAPIKeyRegistryEntry struct {
	ProviderName    string    `json:"provider_name"`
	ProviderBaseURL string    `json:"provider_base_url"`
	APIKeyHash      string    `json:"api_key_hash"`
	DisabledAt      time.Time `json:"disabled_at"`
}

func DisabledAPIKeyRegistryPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "webui_data", filepath.FromSlash(disabledAPIKeyRegistryRelativePath)), nil
}

func EmptyDisabledAPIKeyRegistry() *DisabledAPIKeyRegistry {
	return &DisabledAPIKeyRegistry{
		Version: 1,
		Entries: []DisabledAPIKeyRegistryEntry{},
	}
}

func LoadDisabledAPIKeyRegistry() (*DisabledAPIKeyRegistry, error) {
	path, err := DisabledAPIKeyRegistryPath()
	if err != nil {
		return nil, err
	}
	return LoadDisabledAPIKeyRegistryFromPath(path)
}

func LoadDisabledAPIKeyRegistryFromPath(path string) (*DisabledAPIKeyRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyDisabledAPIKeyRegistry(), nil
		}
		return nil, err
	}

	registry := EmptyDisabledAPIKeyRegistry()
	if err := json.Unmarshal(data, registry); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	if registry.Version == 0 {
		registry.Version = 1
	}
	if registry.Entries == nil {
		registry.Entries = []DisabledAPIKeyRegistryEntry{}
	}
	return registry, nil
}

func WriteDisabledAPIKeyRegistry(path string, registry *DisabledAPIKeyRegistry) error {
	if registry == nil {
		registry = EmptyDisabledAPIKeyRegistry()
	}
	registry.Version = 1
	if registry.Entries == nil {
		registry.Entries = []DisabledAPIKeyRegistryEntry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func NormalizeProviderBaseURL(raw string) string {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		return ""
	}

	trimSuffixInsensitive := func(value, suffix string) string {
		if len(value) < len(suffix) {
			return value
		}
		if strings.EqualFold(value[len(value)-len(suffix):], suffix) {
			return value[:len(value)-len(suffix)]
		}
		return value
	}

	baseURL = trimSuffixInsensitive(baseURL, "/v0/management")
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = trimSuffixInsensitive(baseURL, "/v0/management")
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return strings.ToLower(baseURL)
}

func HashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func IsDisabled(registry *DisabledAPIKeyRegistry, providerName, providerBaseURL, apiKey string) bool {
	key := disabledAPIKeyRegistryKey(providerName, providerBaseURL, HashAPIKey(apiKey))
	if key == "" || registry == nil {
		return false
	}
	for i := range registry.Entries {
		if disabledAPIKeyRegistryKey(registry.Entries[i].ProviderName, registry.Entries[i].ProviderBaseURL, registry.Entries[i].APIKeyHash) == key {
			return true
		}
	}
	return false
}

func SetDisabled(registry *DisabledAPIKeyRegistry, providerName, providerBaseURL, apiKey string, disabled bool, now time.Time) {
	if registry == nil {
		return
	}
	entry := DisabledAPIKeyRegistryEntry{
		ProviderName:    strings.TrimSpace(providerName),
		ProviderBaseURL: NormalizeProviderBaseURL(providerBaseURL),
		APIKeyHash:      HashAPIKey(apiKey),
		DisabledAt:      now.UTC(),
	}
	key := disabledAPIKeyRegistryKey(entry.ProviderName, entry.ProviderBaseURL, entry.APIKeyHash)
	if key == "" {
		return
	}

	for i := range registry.Entries {
		if disabledAPIKeyRegistryKey(registry.Entries[i].ProviderName, registry.Entries[i].ProviderBaseURL, registry.Entries[i].APIKeyHash) != key {
			continue
		}
		if disabled {
			registry.Entries[i] = entry
		} else {
			registry.Entries = append(registry.Entries[:i], registry.Entries[i+1:]...)
		}
		registry.UpdatedAt = now.UTC()
		return
	}

	if disabled {
		registry.Entries = append(registry.Entries, entry)
		registry.UpdatedAt = now.UTC()
		return
	}

	registry.UpdatedAt = now.UTC()
}

func PruneDisabledAPIKeyRegistry(registry *DisabledAPIKeyRegistry, cfg *config.Config) bool {
	if registry == nil {
		return false
	}
	valid := validDisabledAPIKeyRegistryKeys(cfg)
	if len(registry.Entries) == 0 {
		if registry.Entries == nil {
			registry.Entries = []DisabledAPIKeyRegistryEntry{}
			return true
		}
		return false
	}

	changed := false
	seen := make(map[string]struct{}, len(registry.Entries))
	filtered := make([]DisabledAPIKeyRegistryEntry, 0, len(registry.Entries))
	for i := range registry.Entries {
		entry := DisabledAPIKeyRegistryEntry{
			ProviderName:    strings.TrimSpace(registry.Entries[i].ProviderName),
			ProviderBaseURL: NormalizeProviderBaseURL(registry.Entries[i].ProviderBaseURL),
			APIKeyHash:      strings.TrimSpace(registry.Entries[i].APIKeyHash),
			DisabledAt:      registry.Entries[i].DisabledAt,
		}
		key := disabledAPIKeyRegistryKey(entry.ProviderName, entry.ProviderBaseURL, entry.APIKeyHash)
		if key == "" {
			changed = true
			continue
		}
		if _, ok := valid[key]; !ok {
			changed = true
			continue
		}
		if _, ok := seen[key]; ok {
			changed = true
			continue
		}
		if entry != registry.Entries[i] {
			changed = true
		}
		seen[key] = struct{}{}
		filtered = append(filtered, entry)
	}
	if changed {
		registry.Entries = filtered
		if registry.Entries == nil {
			registry.Entries = []DisabledAPIKeyRegistryEntry{}
		}
	}
	return changed
}

func ApplyDisabledAPIKeyRegistry(cfg *config.Config, registry *DisabledAPIKeyRegistry) {
	if cfg == nil {
		return
	}
	for i := range cfg.OpenAICompatibility {
		for j := range cfg.OpenAICompatibility[i].APIKeyEntries {
			cfg.OpenAICompatibility[i].APIKeyEntries[j].Disabled = IsDisabled(
				registry,
				cfg.OpenAICompatibility[i].Name,
				cfg.OpenAICompatibility[i].BaseURL,
				cfg.OpenAICompatibility[i].APIKeyEntries[j].APIKey,
			)
		}
	}
}

func disabledAPIKeyRegistryKey(providerName, providerBaseURL, apiKeyHash string) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	providerBaseURL = NormalizeProviderBaseURL(providerBaseURL)
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if providerName == "" || providerBaseURL == "" || apiKeyHash == "" {
		return ""
	}
	return providerName + "\x00" + providerBaseURL + "\x00" + apiKeyHash
}

func validDisabledAPIKeyRegistryKeys(cfg *config.Config) map[string]struct{} {
	if cfg == nil || len(cfg.OpenAICompatibility) == 0 {
		return map[string]struct{}{}
	}
	valid := make(map[string]struct{})
	for i := range cfg.OpenAICompatibility {
		compat := &cfg.OpenAICompatibility[i]
		for j := range compat.APIKeyEntries {
			key := disabledAPIKeyRegistryKey(compat.Name, compat.BaseURL, HashAPIKey(compat.APIKeyEntries[j].APIKey))
			if key == "" {
				continue
			}
			valid[key] = struct{}{}
		}
	}
	return valid
}
