package usage

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

type testStatusError struct {
	code int
	msg  string
}

func (e testStatusError) Error() string   { return e.msg }
func (e testStatusError) StatusCode() int { return e.code }

func TestRequestStatisticsRecordIncludesLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestSnapshotRequestMetadataCapturesUpstreamErrorSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	logging.SetGinRequestID(ginCtx, "req-123")

	RecordUpstreamResponseMetadata(ginCtx, 429, nil)
	AppendUpstreamResponseBody(ginCtx, []byte(`{"error":{"message":"Rate limit reached","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))

	ctx := logging.WithRequestID(context.WithValue(context.Background(), "gin", ginCtx), "req-123")
	meta := SnapshotRequestMetadata(ctx, true, testStatusError{
		code: 500,
		msg:  "proxy returned 500 because upstream returned 429",
	})

	if meta.RequestID != "req-123" {
		t.Fatalf("request_id = %q, want req-123", meta.RequestID)
	}
	if meta.Method != "POST" {
		t.Fatalf("method = %q, want POST", meta.Method)
	}
	if meta.Path != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", meta.Path)
	}
	if meta.StatusCode != 500 {
		t.Fatalf("status_code = %d, want 500", meta.StatusCode)
	}
	if meta.UpstreamStatusCode != 429 {
		t.Fatalf("upstream_status_code = %d, want 429", meta.UpstreamStatusCode)
	}
	if meta.ErrorStage != "upstream" {
		t.Fatalf("error_stage = %q, want upstream", meta.ErrorStage)
	}
	if meta.ErrorCode != "rate_limit_exceeded" {
		t.Fatalf("error_code = %q, want rate_limit_exceeded", meta.ErrorCode)
	}
	if meta.UpstreamErrorMessage != "Rate limit reached" {
		t.Fatalf("upstream_error_message = %q, want Rate limit reached", meta.UpstreamErrorMessage)
	}
	if meta.ErrorMessage != "proxy returned 500 because upstream returned 429" {
		t.Fatalf("error_message = %q", meta.ErrorMessage)
	}
}

func TestRequestStatisticsRecordIncludesRequestMetadata(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 4, 12, 18, 20, 31, 0, time.UTC),
		Latency:     1832 * time.Millisecond,
		Failed:      true,
		Request: coreusage.RequestMetadata{
			RequestID:            "9c2d1eab",
			Method:               "POST",
			Path:                 "/v1/chat/completions",
			StatusCode:           500,
			UpstreamStatusCode:   429,
			ErrorStage:           "upstream",
			ErrorCode:            "rate_limit_exceeded",
			ErrorMessage:         "proxy returned 500 because upstream returned 429",
			UpstreamErrorMessage: "Rate limit reached for this model",
		},
		Detail: coreusage.Detail{
			InputTokens:  123,
			OutputTokens: 456,
			TotalTokens:  579,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].RequestID != "9c2d1eab" {
		t.Fatalf("request_id = %q, want 9c2d1eab", details[0].RequestID)
	}
	if details[0].StatusCode != 500 {
		t.Fatalf("status_code = %d, want 500", details[0].StatusCode)
	}
	if details[0].UpstreamStatusCode != 429 {
		t.Fatalf("upstream_status_code = %d, want 429", details[0].UpstreamStatusCode)
	}
	if details[0].ErrorMessage != "proxy returned 500 because upstream returned 429" {
		t.Fatalf("error_message = %q", details[0].ErrorMessage)
	}
	if details[0].UpstreamErrorMessage != "Rate limit reached for this model" {
		t.Fatalf("upstream_error_message = %q", details[0].UpstreamErrorMessage)
	}
}

func TestSnapshotRequestMetadataIgnoresSuccessBodyAsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	logging.SetGinRequestID(ginCtx, "req-ok")

	RecordUpstreamResponseMetadata(ginCtx, 200, nil)
	AppendUpstreamResponseBody(ginCtx, []byte(`event: response.created
data: {"type":"response.created","response":{"id":"resp_123","status":"completed","error":null}}`))

	ctx := logging.WithRequestID(context.WithValue(context.Background(), "gin", ginCtx), "req-ok")
	meta := SnapshotRequestMetadata(ctx, false, nil)

	if meta.StatusCode != 200 {
		t.Fatalf("status_code = %d, want 200", meta.StatusCode)
	}
	if meta.UpstreamStatusCode != 0 {
		t.Fatalf("upstream_status_code = %d, want 0", meta.UpstreamStatusCode)
	}
	if meta.ErrorMessage != "" {
		t.Fatalf("error_message = %q, want empty", meta.ErrorMessage)
	}
	if meta.UpstreamErrorMessage != "" {
		t.Fatalf("upstream_error_message = %q, want empty", meta.UpstreamErrorMessage)
	}
}
