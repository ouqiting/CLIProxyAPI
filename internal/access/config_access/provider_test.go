package configaccess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestProviderAuthenticate_IncludesDisabledModelsMetadata(t *testing.T) {
	provider := newProvider("config", []string{"sk-1234", "sk-5678"}, []sdkconfig.APIKeySettings{
		{
			APIKey:         "sk-5678",
			DisabledModels: []string{" gpt-4o ", "claude-3-7-sonnet", "GPT-4O"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-5678")

	result, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected auth result")
	}
	if got := result.Metadata["disabled_models"]; got != "gpt-4o,claude-3-7-sonnet" && got != "claude-3-7-sonnet,gpt-4o" {
		t.Fatalf("unexpected disabled_models metadata: %q", got)
	}
}

func TestProviderAuthenticate_OmitsDisabledModelsWhenNotConfigured(t *testing.T) {
	provider := newProvider("config", []string{"sk-1234"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-1234")

	result, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected auth result")
	}
	if _, exists := result.Metadata["disabled_models"]; exists {
		t.Fatal("did not expect disabled_models metadata")
	}
}

func TestProviderAuthenticate_IncludesRoutingStrategyMetadata(t *testing.T) {
	provider := newProvider("config", []string{"sk-1234"}, []sdkconfig.APIKeySettings{
		{
			APIKey:   "sk-1234",
			Strategy: "fillfirst",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-1234")

	result, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected auth result")
	}
	if got := result.Metadata[cliproxyexecutor.RoutingStrategyMetadataKey]; got != "fill-first" {
		t.Fatalf("unexpected routing strategy metadata: %q", got)
	}
}
