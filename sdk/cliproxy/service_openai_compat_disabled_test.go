package cliproxy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/openaicompat"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestServiceApplyCoreAuthAddOrUpdate_AppliesOpenAICompatDisabledRegistry(t *testing.T) {
	projectDir := t.TempDir()
	originalWD, errGetwd := os.Getwd()
	if errGetwd != nil {
		t.Fatalf("failed to get working directory: %v", errGetwd)
	}
	if errChdir := os.Chdir(projectDir); errChdir != nil {
		t.Fatalf("failed to chdir to temp project dir: %v", errChdir)
	}
	defer func() {
		if errChdirBack := os.Chdir(originalWD); errChdirBack != nil {
			t.Fatalf("failed to restore working directory: %v", errChdirBack)
		}
	}()

	registryPath, errPath := openaicompat.DisabledAPIKeyRegistryPath()
	if errPath != nil {
		t.Fatalf("resolve registry path: %v", errPath)
	}
	registry := &openaicompat.DisabledAPIKeyRegistry{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Entries: []openaicompat.DisabledAPIKeyRegistryEntry{{
			ProviderName:    "Vercel",
			ProviderBaseURL: "https://ai-gateway.vercel.sh/v1",
			APIKeyHash:      openaicompat.HashAPIKey("sk-disabled"),
			DisabledAt:      time.Now().UTC(),
		}},
	}
	if err := openaicompat.WriteDisabledAPIKeyRegistry(registryPath, registry); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}

	service.applyCoreAuthAddOrUpdate(context.Background(), &coreauth.Auth{
		ID:       "disabled-auth",
		Provider: "vercel",
		Label:    "Vercel",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"compat_name":  "Vercel",
			"provider_key": "vercel",
			"base_url":     "https://ai-gateway.vercel.sh/v1",
			"api_key":      "sk-disabled",
		},
	})
	service.applyCoreAuthAddOrUpdate(context.Background(), &coreauth.Auth{
		ID:       "enabled-auth",
		Provider: "vercel",
		Label:    "Vercel",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"compat_name":  "Vercel",
			"provider_key": "vercel",
			"base_url":     "https://ai-gateway.vercel.sh/v1",
			"api_key":      "sk-enabled",
		},
	})

	disabledAuth, ok := service.coreManager.GetByID("disabled-auth")
	if !ok || disabledAuth == nil {
		t.Fatalf("expected disabled auth in manager")
	}
	if !disabledAuth.Disabled || disabledAuth.Status != coreauth.StatusDisabled {
		t.Fatalf("expected disabled auth to be disabled, got disabled=%v status=%v", disabledAuth.Disabled, disabledAuth.Status)
	}
	enabledAuth, ok := service.coreManager.GetByID("enabled-auth")
	if !ok || enabledAuth == nil {
		t.Fatalf("expected enabled auth in manager")
	}
	if enabledAuth.Disabled {
		t.Fatalf("expected enabled auth to remain active")
	}
}
