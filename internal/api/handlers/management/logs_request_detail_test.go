package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestGetRequestLogByIDReturnsStructuredJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "v1-chat-completions-2026-04-12T182031-9c2d1eab.log")
	logContent := `=== REQUEST INFO ===
Version: test
URL: /v1/chat/completions
Method: POST
Timestamp: 2026-04-12T18:20:31Z

=== HEADERS ===
Content-Type: application/json

=== REQUEST BODY ===
{"model":"gpt-4o"}

=== API REQUEST 1 ===
Timestamp: 2026-04-12T18:20:31Z
Upstream URL: https://api.example.com/v1/chat/completions
HTTP Method: POST

Headers:
Content-Type: application/json

Body:
{"model":"gpt-4o"}

=== API RESPONSE 1 ===
Timestamp: 2026-04-12T18:20:32Z

Status: 429
Headers:
Content-Type: application/json
X-Request-Id: req_abc123

Body:
{"error":{"message":"Rate limit reached","type":"rate_limit_error","code":"rate_limit_exceeded"}}

=== RESPONSE ===
Status: 500
Content-Type: application/json

{"error":{"message":"proxy returned 500 because upstream returned 429"}}`
	if errWrite := os.WriteFile(logPath, []byte(logContent), 0o644); errWrite != nil {
		t.Fatalf("failed to write log file: %v", errWrite)
	}

	stats := internalusage.NewRequestStatistics()
	stats.Record(nil, coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-4o",
		RequestedAt: time.Date(2026, 4, 12, 18, 20, 31, 0, time.UTC),
		Latency:     1832 * time.Millisecond,
		Failed:      true,
		Source:      "xxx",
		AuthIndex:   "1",
		Request: coreusage.RequestMetadata{
			RequestID:            "9c2d1eab",
			Method:               "POST",
			Path:                 "/v1/chat/completions",
			StatusCode:           500,
			UpstreamStatusCode:   429,
			ErrorStage:           "upstream",
			ErrorCode:            "rate_limit_exceeded",
			ErrorMessage:         "proxy returned 500 because upstream returned 429",
			UpstreamErrorMessage: "Rate limit reached",
		},
	})

	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	handler.SetUsageStatistics(stats)
	handler.SetLogDirectory(logDir)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/request-log-by-id/9c2d1eab?format=json", nil)
	req.Header.Set("Accept", "application/json")
	ctx.Request = req
	ctx.Params = gin.Params{{Key: "id", Value: "9c2d1eab"}}

	handler.GetRequestLogByID(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload requestLogDetailResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("failed to decode response: %v", errDecode)
	}

	if payload.RequestID != "9c2d1eab" {
		t.Fatalf("request_id = %q, want 9c2d1eab", payload.RequestID)
	}
	if payload.Model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", payload.Model)
	}
	if payload.Source != "xxx" {
		t.Fatalf("source = %q, want xxx", payload.Source)
	}
	if payload.AuthIndex != "1" {
		t.Fatalf("auth_index = %q, want 1", payload.AuthIndex)
	}
	if payload.StatusCode != 500 {
		t.Fatalf("status_code = %d, want 500", payload.StatusCode)
	}
	if payload.UpstreamStatusCode != 429 {
		t.Fatalf("upstream_status_code = %d, want 429", payload.UpstreamStatusCode)
	}
	if payload.Error == nil {
		t.Fatalf("error = nil, want populated")
	}
	if payload.Error.Code != "rate_limit_exceeded" {
		t.Fatalf("error.code = %q, want rate_limit_exceeded", payload.Error.Code)
	}
	if payload.Error.Type != "rate_limit_error" {
		t.Fatalf("error.type = %q, want rate_limit_error", payload.Error.Type)
	}
	if payload.Error.Message != "proxy returned 500 because upstream returned 429" {
		t.Fatalf("error.message = %q", payload.Error.Message)
	}
	if payload.Error.UpstreamMessage != "Rate limit reached" {
		t.Fatalf("error.upstream_message = %q", payload.Error.UpstreamMessage)
	}
	if payload.Upstream == nil {
		t.Fatalf("upstream = nil, want populated")
	}
	if payload.Upstream.RequestID != "req_abc123" {
		t.Fatalf("upstream.request_id = %q, want req_abc123", payload.Upstream.RequestID)
	}
	if payload.Upstream.Truncated {
		t.Fatalf("upstream.truncated = true, want false")
	}
	if payload.Upstream.BodyText == "" {
		t.Fatalf("upstream.body_text = empty, want JSON payload")
	}
	bodyMap, ok := payload.Upstream.BodyJSON.(map[string]any)
	if !ok {
		t.Fatalf("upstream.body_json type = %T, want map[string]any", payload.Upstream.BodyJSON)
	}
	errorMap, ok := bodyMap["error"].(map[string]any)
	if !ok {
		t.Fatalf("upstream.body_json.error type = %T, want map[string]any", bodyMap["error"])
	}
	if errorMap["code"] != "rate_limit_exceeded" {
		t.Fatalf("upstream.body_json.error.code = %v, want rate_limit_exceeded", errorMap["code"])
	}
}

func TestGetRequestLogByIDStructuredJSONOmitsErrorForSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "v1-responses-2026-04-12T182031-reqok123.log")
	logContent := `=== REQUEST INFO ===
Version: test
URL: /v1/responses
Method: POST
Timestamp: 2026-04-12T18:20:31Z

=== HEADERS ===
Content-Type: application/json

=== REQUEST BODY ===
{"model":"gpt-4o"}

=== API RESPONSE 1 ===
Timestamp: 2026-04-12T18:20:32Z

Status: 200
Headers:
Content-Type: text/event-stream

Body:
event: response.created
data: {"type":"response.created","response":{"id":"resp_123","status":"completed","error":null}}

=== RESPONSE ===
Status: 200
Content-Type: text/event-stream

event: response.created
data: {"type":"response.created","response":{"id":"resp_123","status":"completed","error":null}}`
	if errWrite := os.WriteFile(logPath, []byte(logContent), 0o644); errWrite != nil {
		t.Fatalf("failed to write log file: %v", errWrite)
	}

	stats := internalusage.NewRequestStatistics()
	stats.Record(nil, coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-4o",
		RequestedAt: time.Date(2026, 4, 12, 18, 20, 31, 0, time.UTC),
		Latency:     1200 * time.Millisecond,
		Failed:      false,
		Source:      "xxx",
		AuthIndex:   "1",
		Request: coreusage.RequestMetadata{
			RequestID:  "reqok123",
			Method:     "POST",
			Path:       "/v1/responses",
			StatusCode: 200,
		},
	})

	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	handler.SetUsageStatistics(stats)
	handler.SetLogDirectory(logDir)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/request-log-by-id/reqok123?format=json", nil)
	req.Header.Set("Accept", "application/json")
	ctx.Request = req
	ctx.Params = gin.Params{{Key: "id", Value: "reqok123"}}

	handler.GetRequestLogByID(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload requestLogDetailResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("failed to decode response: %v", errDecode)
	}

	if payload.StatusCode != 200 {
		t.Fatalf("status_code = %d, want 200", payload.StatusCode)
	}
	if payload.UpstreamStatusCode != 0 {
		t.Fatalf("upstream_status_code = %d, want 0", payload.UpstreamStatusCode)
	}
	if payload.Error != nil {
		t.Fatalf("error = %+v, want nil", payload.Error)
	}
	if payload.Upstream != nil {
		t.Fatalf("upstream = %+v, want nil", payload.Upstream)
	}
}
