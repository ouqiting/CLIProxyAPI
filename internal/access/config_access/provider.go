package configaccess

import (
	"context"
	"net/http"
	"strings"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

// Register ensures the config-access provider is available to the access manager.
func Register(cfg *sdkconfig.SDKConfig) {
	if cfg == nil {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	keys := normalizeKeys(cfg.APIKeys)
	if len(keys) == 0 {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	sdkaccess.RegisterProvider(
		sdkaccess.AccessProviderTypeConfigAPIKey,
		newProvider(sdkaccess.DefaultAccessProviderName, keys, cfg.APIKeySettings),
	)
}

type provider struct {
	name string
	keys map[string]keyPolicy
}

type keyPolicy struct {
	disabledModels string
	strategy       string
}

func newProvider(name string, keys []string, settings []sdkconfig.APIKeySettings) *provider {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = sdkaccess.DefaultAccessProviderName
	}
	policies := normalizeAPIKeyPolicies(settings)
	keySet := make(map[string]keyPolicy, len(keys))
	for _, key := range keys {
		keySet[key] = policies[key]
	}
	return &provider{name: providerName, keys: keySet}
}

func (p *provider) Identifier() string {
	if p == nil || p.name == "" {
		return sdkaccess.DefaultAccessProviderName
	}
	return p.name
}

func (p *provider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	if len(p.keys) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	authHeader := r.Header.Get("Authorization")
	authHeaderGoogle := r.Header.Get("X-Goog-Api-Key")
	authHeaderAnthropic := r.Header.Get("X-Api-Key")
	queryKey := ""
	queryAuthToken := ""
	if r.URL != nil {
		queryKey = r.URL.Query().Get("key")
		queryAuthToken = r.URL.Query().Get("auth_token")
	}
	if authHeader == "" && authHeaderGoogle == "" && authHeaderAnthropic == "" && queryKey == "" && queryAuthToken == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}

	apiKey := extractBearerToken(authHeader)

	candidates := []struct {
		value  string
		source string
	}{
		{apiKey, "authorization"},
		{authHeaderGoogle, "x-goog-api-key"},
		{authHeaderAnthropic, "x-api-key"},
		{queryKey, "query-key"},
		{queryAuthToken, "query-auth-token"},
	}

	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		if policy, ok := p.keys[candidate.value]; ok {
			metadata := map[string]string{
				"source": candidate.source,
			}
			if policy.disabledModels != "" {
				metadata["disabled_models"] = policy.disabledModels
			}
			if policy.strategy != "" {
				metadata[cliproxyexecutor.RoutingStrategyMetadataKey] = policy.strategy
			}
			return &sdkaccess.Result{
				Provider:  p.Identifier(),
				Principal: candidate.value,
				Metadata:  metadata,
			}, nil
		}
	}

	return nil, sdkaccess.NewInvalidCredentialError()
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return header
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return header
	}
	return strings.TrimSpace(parts[1])
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if _, exists := seen[trimmedKey]; exists {
			continue
		}
		seen[trimmedKey] = struct{}{}
		normalized = append(normalized, trimmedKey)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeAPIKeyPolicies(settings []sdkconfig.APIKeySettings) map[string]keyPolicy {
	if len(settings) == 0 {
		return nil
	}
	merged := make(map[string]keyPolicy, len(settings))
	for _, setting := range settings {
		apiKey := strings.TrimSpace(setting.APIKey)
		if apiKey == "" {
			continue
		}
		policy := merged[apiKey]
		if models := normalizeModelNames(setting.DisabledModels); len(models) > 0 {
			if policy.disabledModels != "" {
				models = append(strings.Split(policy.disabledModels, ","), models...)
			}
			normalized := normalizeModelNames(models)
			if len(normalized) > 0 {
				policy.disabledModels = strings.Join(normalized, ",")
			}
		}
		if strategy := strings.TrimSpace(setting.Strategy); strategy != "" {
			if normalized, ok := sdkconfig.NormalizeRoutingStrategy(strategy); ok && normalized != "" {
				policy.strategy = normalized
			}
		}
		merged[apiKey] = policy
	}
	return merged
}

func normalizeModelNames(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		trimmed := strings.ToLower(strings.TrimSpace(model))
		trimmed = strings.TrimPrefix(trimmed, "models/")
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}
