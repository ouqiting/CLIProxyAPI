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
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestPatchAuthFile_RenameAndUpdateContent_PreserveDisabledState(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	oldName := "old-name.json"
	newName := "new-name.json"
	oldPath := filepath.Join(authDir, oldName)
	if errWrite := os.WriteFile(oldPath, []byte(`{"type":"claude","email":"old@example.com"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write old auth file: %v", errWrite)
	}

	manager := coreauth.NewManager(nil, nil, nil)
	oldID := filepath.ToSlash(oldName)
	record := &coreauth.Auth{
		ID:            oldID,
		FileName:      oldName,
		Provider:      "claude",
		Disabled:      true,
		Status:        coreauth.StatusDisabled,
		StatusMessage: "disabled via management API",
		Attributes: map[string]string{
			"path": oldPath,
		},
		Metadata: map[string]any{
			"type":  "claude",
			"email": "old@example.com",
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	body := `{"oldName":"old-name.json","newName":"new-name.json","content":"{\"type\":\"claude\",\"email\":\"new@example.com\"}"}`
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFile(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	if _, errStat := os.Stat(oldPath); !os.IsNotExist(errStat) {
		t.Fatalf("expected old file removed, stat err: %v", errStat)
	}
	newPath := filepath.Join(authDir, newName)
	newData, errRead := os.ReadFile(newPath)
	if errRead != nil {
		t.Fatalf("failed to read new file: %v", errRead)
	}
	if got := strings.TrimSpace(string(newData)); got != `{"type":"claude","email":"new@example.com"}` {
		t.Fatalf("unexpected new file content: %s", got)
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal(rec.Body.Bytes(), &payload); errUnmarshal != nil {
		t.Fatalf("failed to decode response: %v", errUnmarshal)
	}
	if payload["name"] != newName {
		t.Fatalf("response name=%v, want %q", payload["name"], newName)
	}
	if payload["oldName"] != oldName {
		t.Fatalf("response oldName=%v, want %q", payload["oldName"], oldName)
	}

	newID := filepath.ToSlash(newName)
	updated, ok := manager.GetByID(newID)
	if !ok || updated == nil {
		t.Fatalf("expected new auth record id=%q", newID)
	}
	if !updated.Disabled || updated.Status != coreauth.StatusDisabled {
		t.Fatalf("expected disabled status preserved, got disabled=%v status=%q", updated.Disabled, updated.Status)
	}
}

func TestPatchAuthFile_TargetAlreadyExistsReturns409(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	if errWrite := os.WriteFile(filepath.Join(authDir, "old.json"), []byte(`{"type":"claude"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write old file: %v", errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(authDir, "new.json"), []byte(`{"type":"claude"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write new file: %v", errWrite)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, coreauth.NewManager(nil, nil, nil))

	body := `{"oldName":"old.json","newName":"new.json"}`
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFile(ctx)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

func TestPatchAuthFile_SourceMissingReturns404(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, coreauth.NewManager(nil, nil, nil))

	body := `{"oldName":"missing.json","newName":"new.json"}`
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFile(ctx)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestPatchAuthFile_UpdateContentOnlyWithJSONObject(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	name := "same.json"
	path := filepath.Join(authDir, name)
	if errWrite := os.WriteFile(path, []byte(`{"type":"claude","email":"old@example.com"}`), 0o600); errWrite != nil {
		t.Fatalf("failed to write auth file: %v", errWrite)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, coreauth.NewManager(nil, nil, nil))

	body := `{"oldName":"same.json","json":{"type":"claude","email":"updated@example.com"}}`
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFile(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	updated, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("failed to read updated file: %v", errRead)
	}
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(updated, &payload); errUnmarshal != nil {
		t.Fatalf("invalid updated json: %v", errUnmarshal)
	}
	if payload["email"] != "updated@example.com" {
		t.Fatalf("updated email=%v, want %q", payload["email"], "updated@example.com")
	}
}
