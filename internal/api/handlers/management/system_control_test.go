package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/managementasset"
)

func TestUpdateWebUIReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.managementHTMLSync = func(_ context.Context, staticDir string, proxyURL string, panelRepository string, opts managementasset.UpdateOptions) managementasset.UpdateResult {
		if !opts.Force {
			t.Fatal("expected force update option")
		}
		_ = staticDir
		_ = proxyURL
		_ = panelRepository
		return managementasset.UpdateResult{
			Success:    true,
			Updated:    true,
			Exists:     true,
			FilePath:   "/tmp/static/management.html",
			Message:    "management asset updated successfully",
			StartedAt:  time.Unix(100, 0).UTC(),
			FinishedAt: time.Unix(101, 0).UTC(),
			DurationMS: 1000,
			Logs: []managementasset.UpdateLogEntry{{
				Time:    time.Unix(100, 0).UTC(),
				Level:   "info",
				Message: "checking latest management release",
			}},
		}
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/webui/update", strings.NewReader(`{"force":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.UpdateWebUI(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp managementasset.UpdateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success || !resp.Updated {
		t.Fatalf("unexpected update result: %+v", resp)
	}
	if len(resp.Logs) != 1 || resp.Logs[0].Message != "checking latest management release" {
		t.Fatalf("unexpected logs: %+v", resp.Logs)
	}
}

func TestUpdateWebUIPropagatesFailureStatus(t *testing.T) {
	t.Parallel()

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.managementHTMLSync = func(_ context.Context, _ string, _ string, _ string, _ managementasset.UpdateOptions) managementasset.UpdateResult {
		return managementasset.UpdateResult{
			Success: false,
			Message: "failed to download management asset",
			Error:   "network unreachable",
		}
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/webui/update", nil)

	h.UpdateWebUI(ctx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestRestartSystemAccepted(t *testing.T) {
	t.Parallel()

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	called := false
	h.restartScheduler = func() error {
		called = true
		return nil
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/system/restart", nil)

	h.RestartSystem(ctx)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if !called {
		t.Fatal("expected restart scheduler to be called")
	}
}

func TestRestartSystemReturnsErrorWhenSchedulingFails(t *testing.T) {
	t.Parallel()

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.restartScheduler = func() error {
		return http.ErrServerClosed
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/system/restart", nil)

	h.RestartSystem(ctx)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
