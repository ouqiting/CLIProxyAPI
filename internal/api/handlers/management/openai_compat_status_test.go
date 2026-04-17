package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/openaicompat"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestPatchOpenAICompatStatusAndGet(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

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

	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	registerOpenAICompatAuthForTest(t, manager, &coreauth.Auth{
		ID:       "vercel-key-1",
		Provider: "vercel",
		Label:    "Vercel",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"compat_name":  "Vercel",
			"provider_key": "vercel",
			"base_url":     "https://ai-gateway.vercel.sh/v1",
			"api_key":      "sk-live-1",
		},
	})
	registerOpenAICompatAuthForTest(t, manager, &coreauth.Auth{
		ID:       "vercel-key-2",
		Provider: "vercel",
		Label:    "Vercel",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"compat_name":  "Vercel",
			"provider_key": "vercel",
			"base_url":     "https://ai-gateway.vercel.sh/v1",
			"api_key":      "sk-live-2",
		},
	})

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "Vercel",
			BaseURL: "https://ai-gateway.vercel.sh/v1",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{
				{APIKey: "sk-live-1"},
				{APIKey: "sk-live-2"},
			},
		}},
	}, manager)

	patchRec := httptest.NewRecorder()
	patchCtx, _ := gin.CreateTestContext(patchRec)
	patchCtx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/openai-compatibility/status", strings.NewReader(`{"provider_name":"Vercel","provider_base_url":"https://AI-GATEWAY.VERCEL.SH/v1/","api_key":"sk-live-1","disabled":true}`))
	patchCtx.Request.Header.Set("Content-Type", "application/json")
	h.PatchOpenAICompatStatus(patchCtx)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200, body=%s", patchRec.Code, patchRec.Body.String())
	}
	if !strings.Contains(patchRec.Body.String(), `"disabled":true`) {
		t.Fatalf("unexpected patch response: %s", patchRec.Body.String())
	}

	registryPath := filepath.Join(projectDir, "webui_data", "management", "disabled-openai-api-key-entries.json")
	registryBytes, errRead := os.ReadFile(registryPath)
	if errRead != nil {
		t.Fatalf("read registry file: %v", errRead)
	}
	if strings.Contains(string(registryBytes), "sk-live-1") {
		t.Fatalf("registry file leaked plaintext api key: %s", string(registryBytes))
	}
	registry, errLoad := openaicompat.LoadDisabledAPIKeyRegistryFromPath(registryPath)
	if errLoad != nil {
		t.Fatalf("load registry: %v", errLoad)
	}
	if len(registry.Entries) != 1 {
		t.Fatalf("registry entries len = %d, want 1", len(registry.Entries))
	}
	if registry.Entries[0].ProviderBaseURL != "https://ai-gateway.vercel.sh/v1" {
		t.Fatalf("provider_base_url = %q, want normalized value", registry.Entries[0].ProviderBaseURL)
	}
	if registry.Entries[0].APIKeyHash != openaicompat.HashAPIKey("sk-live-1") {
		t.Fatalf("api_key_hash = %q, want %q", registry.Entries[0].APIKeyHash, openaicompat.HashAPIKey("sk-live-1"))
	}

	auth1, ok := manager.GetByID("vercel-key-1")
	if !ok || auth1 == nil {
		t.Fatalf("expected auth vercel-key-1")
	}
	if !auth1.Disabled || auth1.Status != coreauth.StatusDisabled {
		t.Fatalf("expected vercel-key-1 disabled, got disabled=%v status=%v", auth1.Disabled, auth1.Status)
	}
	auth2, ok := manager.GetByID("vercel-key-2")
	if !ok || auth2 == nil {
		t.Fatalf("expected auth vercel-key-2")
	}
	if auth2.Disabled {
		t.Fatalf("expected vercel-key-2 to remain enabled")
	}

	getRec := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRec)
	getCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/openai-compatibility", nil)
	h.GetOpenAICompat(getCtx)

	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200, body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp struct {
		Items []config.OpenAICompatibility `json:"openai-compatibility"`
	}
	if errDecode := json.Unmarshal(getRec.Body.Bytes(), &getResp); errDecode != nil {
		t.Fatalf("decode get response: %v", errDecode)
	}
	if len(getResp.Items) != 1 || len(getResp.Items[0].APIKeyEntries) != 2 {
		t.Fatalf("unexpected get response: %+v", getResp.Items)
	}
	if !getResp.Items[0].APIKeyEntries[0].Disabled {
		t.Fatalf("expected first api-key-entry disabled=true")
	}
	if getResp.Items[0].APIKeyEntries[1].Disabled {
		t.Fatalf("expected second api-key-entry disabled=false")
	}
}

func TestPatchOpenAICompatStatusNotFoundSemantics(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h := NewHandlerWithoutConfigFilePath(&config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:    "Vercel",
			BaseURL: "https://ai-gateway.vercel.sh/v1",
			APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
				APIKey: "sk-live-1",
			}},
		}},
	}, nil)

	tests := []struct {
		name       string
		body       string
		statusCode int
		wantError  string
	}{
		{
			name:       "provider not found",
			body:       `{"provider_name":"Missing","provider_base_url":"https://ai-gateway.vercel.sh/v1","api_key":"sk-live-1","disabled":true}`,
			statusCode: http.StatusNotFound,
			wantError:  "provider not found",
		},
		{
			name:       "api key entry not found",
			body:       `{"provider_name":"Vercel","provider_base_url":"https://ai-gateway.vercel.sh/v1","api_key":"sk-missing","disabled":true}`,
			statusCode: http.StatusNotFound,
			wantError:  "api-key-entry not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/openai-compatibility/status", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.PatchOpenAICompatStatus(ctx)

			if rec.Code != tt.statusCode {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.statusCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantError) {
				t.Fatalf("body = %s, want error %q", rec.Body.String(), tt.wantError)
			}
		})
	}
}

func registerOpenAICompatAuthForTest(t *testing.T, manager *coreauth.Manager, auth *coreauth.Auth) {
	t.Helper()
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth %s: %v", auth.ID, err)
	}
}
