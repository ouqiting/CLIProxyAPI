package usage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

const (
	requestMetadataContextKey   = "__usage_request_metadata__"
	maxUpstreamErrorSampleBytes = 16 * 1024
	maxErrorSummaryRunes        = 512
)

type requestMetadataState struct {
	mu                 sync.RWMutex
	meta               coreusage.RequestMetadata
	upstreamBodySample []byte
}

func RecordUpstreamResponseMetadata(ginCtx *gin.Context, status int, _ http.Header) {
	state := ensureRequestMetadataState(ginCtx)
	if state == nil {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if status > 0 {
		state.meta.UpstreamStatusCode = status
		if state.meta.ErrorStage == "" && status >= http.StatusBadRequest {
			state.meta.ErrorStage = "upstream"
		}
	}
}

func RecordUpstreamError(ginCtx *gin.Context, err error) {
	state := ensureRequestMetadataState(ginCtx)
	if state == nil || err == nil {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if status := statusFromError(err); status > 0 && state.meta.UpstreamStatusCode == 0 {
		state.meta.UpstreamStatusCode = status
	}
	if state.meta.ErrorStage == "" {
		state.meta.ErrorStage = "upstream"
	}
	if state.meta.ErrorCode == "" {
		state.meta.ErrorCode = errorCodeFromError(err)
	}
	if state.meta.UpstreamErrorMessage == "" {
		state.meta.UpstreamErrorMessage = summarizeErrorText(err.Error())
	}
}

func AppendUpstreamResponseBody(ginCtx *gin.Context, chunk []byte) {
	state := ensureRequestMetadataState(ginCtx)
	if state == nil {
		return
	}

	data := bytes.TrimSpace(chunk)
	if len(data) == 0 {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.meta.UpstreamStatusCode < http.StatusBadRequest && state.meta.ErrorStage == "" {
		return
	}

	if len(state.upstreamBodySample) < maxUpstreamErrorSampleBytes {
		remaining := maxUpstreamErrorSampleBytes - len(state.upstreamBodySample)
		if len(data) > remaining {
			data = data[:remaining]
		}
		state.upstreamBodySample = append(state.upstreamBodySample, data...)
	}

	preview := extractErrorPreview(state.upstreamBodySample)
	if preview.Message != "" {
		state.meta.UpstreamErrorMessage = preview.Message
	}
	if state.meta.ErrorCode == "" && preview.Code != "" {
		state.meta.ErrorCode = preview.Code
	}
	if state.meta.ErrorStage == "" && state.meta.UpstreamStatusCode >= http.StatusBadRequest {
		state.meta.ErrorStage = "upstream"
	}
}

func SnapshotRequestMetadata(ctx context.Context, failed bool, execErr error) coreusage.RequestMetadata {
	meta := coreusage.RequestMetadata{}
	ginCtx := ginContextFromContext(ctx)
	if ginCtx != nil {
		if state := ensureRequestMetadataState(ginCtx); state != nil {
			state.mu.RLock()
			meta = state.meta
			state.mu.RUnlock()
		}
	}

	if requestID := logging.GetRequestID(ctx); requestID != "" {
		meta.RequestID = requestID
	} else if ginCtx != nil {
		meta.RequestID = logging.GetGinRequestID(ginCtx)
	}

	if ginCtx != nil && ginCtx.Request != nil {
		if meta.Method == "" {
			meta.Method = strings.TrimSpace(ginCtx.Request.Method)
		}
		if meta.Path == "" {
			path := strings.TrimSpace(ginCtx.FullPath())
			if path == "" && ginCtx.Request.URL != nil {
				path = strings.TrimSpace(ginCtx.Request.URL.Path)
			}
			meta.Path = path
		}
		if meta.StatusCode == 0 {
			if status := ginCtx.Writer.Status(); status > 0 && (!failed || status >= http.StatusBadRequest) {
				meta.StatusCode = status
			}
		}
	}

	if failed {
		if meta.UpstreamStatusCode == 0 {
			meta.UpstreamStatusCode = statusFromError(execErr)
		}
		if meta.StatusCode == 0 {
			if status := statusFromError(execErr); status > 0 {
				meta.StatusCode = status
			} else if meta.UpstreamStatusCode > 0 {
				meta.StatusCode = meta.UpstreamStatusCode
			} else {
				meta.StatusCode = http.StatusInternalServerError
			}
		}
		if meta.ErrorStage == "" {
			if meta.UpstreamStatusCode > 0 {
				meta.ErrorStage = "upstream"
			} else {
				meta.ErrorStage = "proxy"
			}
		}
		if meta.ErrorCode == "" {
			meta.ErrorCode = errorCodeFromError(execErr)
		}
		if meta.UpstreamErrorMessage == "" && execErr != nil {
			meta.UpstreamErrorMessage = summarizeErrorText(execErr.Error())
		}
		if meta.ErrorMessage == "" {
			meta.ErrorMessage = deriveErrorMessage(meta.StatusCode, meta.UpstreamStatusCode, meta.UpstreamErrorMessage, execErr)
		}
		return normaliseRequestMetadata(meta)
	}

	if meta.StatusCode == 0 {
		meta.StatusCode = http.StatusOK
	}

	return normaliseRequestMetadata(clearErrorMetadata(meta))
}

func normaliseRequestMetadata(meta coreusage.RequestMetadata) coreusage.RequestMetadata {
	meta.RequestID = strings.TrimSpace(meta.RequestID)
	meta.Method = strings.TrimSpace(meta.Method)
	meta.Path = strings.TrimSpace(meta.Path)
	meta.ErrorStage = strings.TrimSpace(meta.ErrorStage)
	meta.ErrorCode = strings.TrimSpace(meta.ErrorCode)
	meta.ErrorMessage = strings.TrimSpace(meta.ErrorMessage)
	meta.UpstreamErrorMessage = strings.TrimSpace(meta.UpstreamErrorMessage)
	if meta.StatusCode > 0 && meta.UpstreamStatusCode > 0 && meta.StatusCode == meta.UpstreamStatusCode {
		meta.UpstreamStatusCode = 0
	}
	return meta
}

func clearErrorMetadata(meta coreusage.RequestMetadata) coreusage.RequestMetadata {
	if meta.StatusCode >= http.StatusBadRequest || meta.UpstreamStatusCode >= http.StatusBadRequest {
		return meta
	}
	meta.ErrorStage = ""
	meta.ErrorCode = ""
	meta.ErrorMessage = ""
	meta.UpstreamErrorMessage = ""
	return meta
}

func ginContextFromContext(ctx context.Context) *gin.Context {
	if ctx == nil {
		return nil
	}
	ginCtx, _ := ctx.Value("gin").(*gin.Context)
	return ginCtx
}

func ensureRequestMetadataState(ginCtx *gin.Context) *requestMetadataState {
	if ginCtx == nil {
		return nil
	}
	if existing, ok := ginCtx.Get(requestMetadataContextKey); ok {
		if state, okState := existing.(*requestMetadataState); okState && state != nil {
			return state
		}
	}
	state := &requestMetadataState{}
	ginCtx.Set(requestMetadataContextKey, state)
	return state
}

func deriveErrorMessage(statusCode, upstreamStatusCode int, upstreamMessage string, execErr error) string {
	if statusCode > 0 && upstreamStatusCode > 0 && statusCode != upstreamStatusCode {
		return fmt.Sprintf("proxy returned %d because upstream returned %d", statusCode, upstreamStatusCode)
	}
	if execErr != nil {
		if text := summarizeErrorText(execErr.Error()); text != "" {
			return text
		}
	}
	if upstreamMessage != "" {
		return upstreamMessage
	}
	if statusCode > 0 {
		return http.StatusText(statusCode)
	}
	return ""
}

func statusFromError(err error) int {
	if err == nil {
		return 0
	}
	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) && statusErr != nil {
		if code := statusErr.StatusCode(); code > 0 {
			return code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil && netErr.Timeout() {
		return http.StatusRequestTimeout
	}
	return 0
}

func errorCodeFromError(err error) string {
	if err == nil {
		return ""
	}

	var authErr *coreauth.Error
	if errors.As(err, &authErr) && authErr != nil && strings.TrimSpace(authErr.Code) != "" {
		return strings.TrimSpace(authErr.Code)
	}

	errText := strings.TrimSpace(err.Error())
	parts := strings.SplitN(errText, ":", 2)
	if len(parts) > 1 {
		candidate := strings.TrimSpace(parts[0])
		if looksLikeMachineCode(candidate) {
			return candidate
		}
	}
	return ""
}

func looksLikeMachineCode(candidate string) bool {
	if candidate == "" {
		return false
	}
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

type errorPreview struct {
	Code    string
	Message string
}

func extractErrorPreview(payload []byte) errorPreview {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return errorPreview{}
	}

	if gjson.ValidBytes(trimmed) {
		message := firstNonEmptyGJSON(trimmed,
			"error.message",
			"message",
			"error.error.message",
			"details.0.message",
		)
		code := firstNonEmptyGJSON(trimmed,
			"error.code",
			"code",
			"error.type",
			"type",
		)
		return errorPreview{
			Code:    strings.TrimSpace(code),
			Message: summarizeErrorText(message),
		}
	}

	return errorPreview{Message: summarizeErrorText(string(trimmed))}
}

func firstNonEmptyGJSON(payload []byte, paths ...string) string {
	for _, path := range paths {
		value := strings.TrimSpace(gjson.GetBytes(payload, path).String())
		if value != "" {
			return value
		}
	}
	return ""
}

func summarizeErrorText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > maxErrorSummaryRunes {
		return strings.TrimSpace(string(runes[:maxErrorSummaryRunes])) + "..."
	}
	return text
}
