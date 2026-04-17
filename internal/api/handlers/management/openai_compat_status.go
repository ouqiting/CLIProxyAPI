package management

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/openaicompat"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func (h *Handler) openAICompatEntriesWithDisabled() ([]config.OpenAICompatibility, error) {
	if h == nil || h.cfg == nil {
		return nil, nil
	}
	entries := normalizedOpenAICompatibilityEntries(h.cfg.OpenAICompatibility)
	registry, err := openaicompat.LoadDisabledAPIKeyRegistry()
	if err != nil {
		return nil, err
	}
	cfgCopy := &config.Config{OpenAICompatibility: entries}
	openaicompat.ApplyDisabledAPIKeyRegistry(cfgCopy, registry)
	return cfgCopy.OpenAICompatibility, nil
}

func (h *Handler) PatchOpenAICompatStatus(c *gin.Context) {
	var body struct {
		ProviderName    *string `json:"provider_name"`
		ProviderBaseURL *string `json:"provider_base_url"`
		APIKey          *string `json:"api_key"`
		Disabled        *bool   `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	providerName := ""
	providerBaseURL := ""
	apiKey := ""
	if body.ProviderName != nil {
		providerName = strings.TrimSpace(*body.ProviderName)
	}
	if body.ProviderBaseURL != nil {
		providerBaseURL = openaicompat.NormalizeProviderBaseURL(*body.ProviderBaseURL)
	}
	if body.APIKey != nil {
		apiKey = strings.TrimSpace(*body.APIKey)
	}
	if providerName == "" || providerBaseURL == "" || apiKey == "" || body.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid provider_name/provider_base_url/api_key/disabled"})
		return
	}

	_, _, providerFound, entryFound := findOpenAICompatAPIKeyEntry(h.cfg, providerName, providerBaseURL, apiKey)
	if !providerFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if !entryFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "api-key-entry not found"})
		return
	}

	registry, err := openaicompat.LoadDisabledAPIKeyRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load disabled openai api key registry: %v", err)})
		return
	}
	now := time.Now().UTC()
	if openaicompat.PruneDisabledAPIKeyRegistry(registry, h.cfg) {
		registry.UpdatedAt = now
	}
	openaicompat.SetDisabled(registry, providerName, providerBaseURL, apiKey, *body.Disabled, now)
	path, err := openaicompat.DisabledAPIKeyRegistryPath()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resolve disabled openai api key registry path: %v", err)})
		return
	}
	if err := openaicompat.WriteDisabledAPIKeyRegistry(path, registry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write disabled openai api key registry: %v", err)})
		return
	}
	if err := h.applyOpenAICompatAuthDisabledState(providerName, providerBaseURL, apiKey, *body.Disabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update runtime openai compat auth state: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "disabled": *body.Disabled})
}

func findOpenAICompatAPIKeyEntry(cfg *config.Config, providerName, providerBaseURL, apiKey string) (int, int, bool, bool) {
	if cfg == nil {
		return -1, -1, false, false
	}
	providerName = strings.TrimSpace(providerName)
	providerBaseURL = openaicompat.NormalizeProviderBaseURL(providerBaseURL)
	apiKeyHash := openaicompat.HashAPIKey(apiKey)
	for i := range cfg.OpenAICompatibility {
		compat := &cfg.OpenAICompatibility[i]
		if !strings.EqualFold(strings.TrimSpace(compat.Name), providerName) {
			continue
		}
		if openaicompat.NormalizeProviderBaseURL(compat.BaseURL) != providerBaseURL {
			continue
		}
		for j := range compat.APIKeyEntries {
			if openaicompat.HashAPIKey(compat.APIKeyEntries[j].APIKey) == apiKeyHash {
				return i, j, true, true
			}
		}
		return i, -1, true, false
	}
	return -1, -1, false, false
}

func (h *Handler) applyOpenAICompatAuthDisabledState(providerName, providerBaseURL, apiKey string, disabled bool) error {
	if h == nil || h.authManager == nil {
		return nil
	}
	ctx := coreauth.WithSkipPersist(context.Background())
	for _, auth := range h.authManager.List() {
		if !matchOpenAICompatAuthEntry(auth, providerName, providerBaseURL, apiKey) {
			continue
		}
		wantStatus := coreauth.StatusActive
		if disabled {
			wantStatus = coreauth.StatusDisabled
		}
		if auth.Disabled == disabled && auth.Status == wantStatus {
			continue
		}
		updated := auth.Clone()
		updated.Disabled = disabled
		if disabled {
			updated.Status = coreauth.StatusDisabled
		} else {
			updated.Status = coreauth.StatusActive
		}
		if _, err := h.authManager.Update(ctx, updated); err != nil {
			return err
		}
	}
	return nil
}

func matchOpenAICompatAuthEntry(auth *coreauth.Auth, providerName, providerBaseURL, apiKey string) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	compatName := strings.TrimSpace(auth.Attributes["compat_name"])
	if compatName == "" {
		compatName = strings.TrimSpace(auth.Label)
	}
	if compatName == "" || !strings.EqualFold(compatName, strings.TrimSpace(providerName)) {
		return false
	}
	if openaicompat.NormalizeProviderBaseURL(auth.Attributes["base_url"]) != openaicompat.NormalizeProviderBaseURL(providerBaseURL) {
		return false
	}
	return openaicompat.HashAPIKey(auth.Attributes["api_key"]) == openaicompat.HashAPIKey(apiKey)
}
