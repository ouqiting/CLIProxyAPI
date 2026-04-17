package management

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/tidwall/gjson"
)

const (
	requestLogScannerBufferSize = 16 * 1024 * 1024
	requestLogBodyTextLimit     = 16 * 1024
)

type requestLogDetailResponse struct {
	RequestID          string                    `json:"request_id"`
	Timestamp          time.Time                 `json:"timestamp,omitempty"`
	Method             string                    `json:"method,omitempty"`
	Path               string                    `json:"path,omitempty"`
	Model              string                    `json:"model,omitempty"`
	Source             string                    `json:"source,omitempty"`
	AuthIndex          string                    `json:"auth_index,omitempty"`
	Failed             bool                      `json:"failed"`
	StatusCode         int                       `json:"status_code,omitempty"`
	UpstreamStatusCode int                       `json:"upstream_status_code,omitempty"`
	LatencyMs          int64                     `json:"latency_ms,omitempty"`
	Error              *requestLogErrorDetail    `json:"error,omitempty"`
	Upstream           *requestLogUpstreamDetail `json:"upstream,omitempty"`
}

type requestLogErrorDetail struct {
	Stage           string `json:"stage,omitempty"`
	Type            string `json:"type,omitempty"`
	Code            string `json:"code,omitempty"`
	Message         string `json:"message,omitempty"`
	UpstreamMessage string `json:"upstream_message,omitempty"`
}

type requestLogUpstreamDetail struct {
	RequestID string `json:"request_id,omitempty"`
	BodyText  string `json:"body_text,omitempty"`
	BodyJSON  any    `json:"body_json,omitempty"`
	Truncated bool   `json:"truncated"`
}

type requestLogSection struct {
	Name    string
	Content string
}

type parsedRequestInfo struct {
	Timestamp time.Time
	Method    string
	URL       string
}

type parsedHTTPSection struct {
	Timestamp time.Time
	Status    int
	Headers   map[string][]string
	Body      string
}

type parsedAPIErrorSection struct {
	Status  int
	Message string
}

type usageRequestLookup struct {
	Model  string
	Detail internalusage.RequestDetail
}

func wantsStructuredRequestLog(acceptHeader, format string) bool {
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		return true
	}
	for _, part := range strings.Split(acceptHeader, ",") {
		if strings.Contains(strings.ToLower(strings.TrimSpace(part)), "application/json") {
			return true
		}
	}
	return false
}

func (h *Handler) buildRequestLogDetailResponse(requestID, filePath string) (requestLogDetailResponse, error) {
	resp := requestLogDetailResponse{RequestID: requestID}
	data, errRead := os.ReadFile(filePath)
	if errRead != nil {
		return resp, errRead
	}

	sections, errParse := splitRequestLogSections(data)
	if errParse != nil {
		return resp, errParse
	}

	var (
		requestInfo    parsedRequestInfo
		responseBlock  parsedHTTPSection
		apiResponse    parsedHTTPSection
		apiError       parsedAPIErrorSection
		apiResponseSet bool
		apiErrorSet    bool
	)

	for _, section := range sections {
		switch {
		case section.Name == "REQUEST INFO":
			requestInfo = parseRequestInfoSection(section.Content)
		case section.Name == "RESPONSE":
			responseBlock = parseResponseSection(section.Content)
		case strings.HasPrefix(section.Name, "API RESPONSE"):
			apiResponse = parseAPIResponseSection(section.Content)
			apiResponseSet = true
		case section.Name == "API ERROR RESPONSE":
			apiError = parseAPIErrorSection(section.Content)
			apiErrorSet = true
		}
	}

	resp.Timestamp = requestInfo.Timestamp
	resp.Method = requestInfo.Method
	resp.Path = pathFromRequestURL(requestInfo.URL)
	resp.StatusCode = responseBlock.Status
	if apiErrorSet && apiError.Status > 0 {
		resp.UpstreamStatusCode = apiError.Status
	} else if apiResponseSet && apiResponse.Status >= http.StatusBadRequest {
		resp.UpstreamStatusCode = apiResponse.Status
	}

	if h != nil && h.usageStats != nil {
		if usageLookup, ok := lookupUsageDetailByRequestID(h.usageStats.Snapshot(), requestID); ok {
			resp.Model = usageLookup.Model
			resp.Source = usageLookup.Detail.Source
			resp.AuthIndex = usageLookup.Detail.AuthIndex
			resp.LatencyMs = usageLookup.Detail.LatencyMs
			resp.Failed = usageLookup.Detail.Failed
			if resp.Timestamp.IsZero() {
				resp.Timestamp = usageLookup.Detail.Timestamp
			}
			if resp.Method == "" {
				resp.Method = usageLookup.Detail.Method
			}
			if resp.Path == "" {
				resp.Path = usageLookup.Detail.Path
			}
			if resp.StatusCode == 0 {
				resp.StatusCode = usageLookup.Detail.StatusCode
			}
			if resp.UpstreamStatusCode == 0 {
				resp.UpstreamStatusCode = usageLookup.Detail.UpstreamStatusCode
			}

		}
	}

	if !resp.Failed {
		resp.Failed = resp.StatusCode >= http.StatusBadRequest || resp.UpstreamStatusCode >= http.StatusBadRequest
	}

	if resp.StatusCode > 0 && resp.UpstreamStatusCode > 0 && resp.StatusCode == resp.UpstreamStatusCode {
		resp.UpstreamStatusCode = 0
	}

	if resp.Failed && resp.Error == nil && h != nil && h.usageStats != nil {
		if usageLookup, ok := lookupUsageDetailByRequestID(h.usageStats.Snapshot(), requestID); ok {
			errorType, errorCode, upstreamMessage := deriveBodyErrorDetails(apiResponse.Body)
			if apiError.Message != "" {
				upstreamMessage = apiError.Message
			}
			if usageLookup.Detail.UpstreamErrorMessage != "" {
				upstreamMessage = usageLookup.Detail.UpstreamErrorMessage
			}
			stage := usageLookup.Detail.ErrorStage
			if stage == "" {
				stage = inferErrorStage(resp.StatusCode, resp.UpstreamStatusCode)
			}
			message := usageLookup.Detail.ErrorMessage
			if message == "" {
				message = deriveRequestDetailMessage(resp.StatusCode, resp.UpstreamStatusCode, upstreamMessage)
			}
			code := usageLookup.Detail.ErrorCode
			if code == "" {
				code = errorCode
			}
			if message != "" || upstreamMessage != "" || code != "" || stage != "" || errorType != "" {
				resp.Error = &requestLogErrorDetail{
					Stage:           stage,
					Type:            errorType,
					Code:            code,
					Message:         message,
					UpstreamMessage: upstreamMessage,
				}
			}
		}
	}

	if resp.Failed && resp.Error == nil {
		errorType, errorCode, upstreamMessage := deriveBodyErrorDetails(apiResponse.Body)
		if apiError.Message != "" {
			upstreamMessage = apiError.Message
		}
		stage := inferErrorStage(resp.StatusCode, resp.UpstreamStatusCode)
		message := deriveRequestDetailMessage(resp.StatusCode, resp.UpstreamStatusCode, upstreamMessage)
		if message != "" || upstreamMessage != "" || errorCode != "" || stage != "" || errorType != "" {
			resp.Error = &requestLogErrorDetail{
				Stage:           stage,
				Type:            errorType,
				Code:            errorCode,
				Message:         message,
				UpstreamMessage: upstreamMessage,
			}
		}
	}

	upstreamBody := strings.TrimSpace(apiResponse.Body)
	if upstreamBody == "" && apiError.Message != "" {
		upstreamBody = strings.TrimSpace(apiError.Message)
	}
	if resp.Failed && (upstreamBody != "" || len(apiResponse.Headers) > 0) {
		bodyText, truncated := truncateText(upstreamBody, requestLogBodyTextLimit)
		upstream := &requestLogUpstreamDetail{
			RequestID: firstHeaderValueCI(apiResponse.Headers,
				"x-request-id",
				"request-id",
				"openai-request-id",
				"anthropic-request-id",
			),
			BodyText:  bodyText,
			Truncated: truncated,
		}
		if !truncated {
			if bodyJSON := decodeJSONBody(upstreamBody); bodyJSON != nil {
				upstream.BodyJSON = bodyJSON
			}
		}
		resp.Upstream = upstream
	}

	return resp, nil
}

func splitRequestLogSections(data []byte) ([]requestLogSection, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), requestLogScannerBufferSize)

	sections := make([]requestLogSection, 0, 8)
	currentName := ""
	var content strings.Builder

	flush := func() {
		if currentName == "" {
			return
		}
		sections = append(sections, requestLogSection{
			Name:    currentName,
			Content: strings.TrimRight(content.String(), "\n"),
		})
		content.Reset()
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if name, ok := parseSectionHeader(line); ok {
			flush()
			currentName = name
			continue
		}
		if currentName == "" {
			continue
		}
		content.WriteString(line)
		content.WriteByte('\n')
	}
	if errScan := scanner.Err(); errScan != nil {
		return nil, errScan
	}
	flush()
	return sections, nil
}

func parseSectionHeader(line string) (string, bool) {
	if !strings.HasPrefix(line, "=== ") || !strings.HasSuffix(line, " ===") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ==="))
	if name == "" {
		return "", false
	}
	return name, true
}

func parseRequestInfoSection(content string) parsedRequestInfo {
	info := parsedRequestInfo{}
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := splitHeaderLine(line)
		if !ok {
			continue
		}
		switch key {
		case "Timestamp":
			info.Timestamp = parseRFC3339Timestamp(value)
		case "Method":
			info.Method = strings.TrimSpace(value)
		case "URL":
			info.URL = strings.TrimSpace(value)
		}
	}
	return info
}

func parseResponseSection(content string) parsedHTTPSection {
	return parseFlatHTTPSection(content)
}

func parseAPIResponseSection(content string) parsedHTTPSection {
	section := parsedHTTPSection{Headers: map[string][]string{}}
	lines := strings.Split(content, "\n")
	for idx := 0; idx < len(lines); idx++ {
		line := lines[idx]
		key, value, ok := splitHeaderLine(line)
		if ok && key == "Timestamp" {
			section.Timestamp = parseRFC3339Timestamp(value)
			continue
		}
		if ok && key == "Status" {
			section.Status = parseInt(value)
			continue
		}
		if strings.TrimSpace(line) == "Headers:" {
			headers, next := parseHeadersAfterMarker(lines, idx+1)
			section.Headers = headers
			idx = next
			continue
		}
		if strings.TrimSpace(line) == "Body:" {
			section.Body = strings.TrimSpace(strings.Join(lines[idx+1:], "\n"))
			return section
		}
	}

	flat := parseFlatHTTPSection(content)
	if section.Timestamp.IsZero() {
		section.Timestamp = flat.Timestamp
	}
	if section.Status == 0 {
		section.Status = flat.Status
	}
	if len(section.Headers) == 0 {
		section.Headers = flat.Headers
	}
	if section.Body == "" {
		section.Body = flat.Body
	}
	return section
}

func parseFlatHTTPSection(content string) parsedHTTPSection {
	section := parsedHTTPSection{Headers: map[string][]string{}}
	lines := strings.Split(content, "\n")
	bodyStart := len(lines)
	for idx, line := range lines {
		if strings.TrimSpace(line) == "" {
			bodyStart = idx + 1
			break
		}
		key, value, ok := splitHeaderLine(line)
		if !ok {
			continue
		}
		switch key {
		case "Timestamp":
			section.Timestamp = parseRFC3339Timestamp(value)
		case "Status":
			section.Status = parseInt(value)
		default:
			section.Headers[key] = append(section.Headers[key], strings.TrimSpace(value))
		}
	}
	if bodyStart < len(lines) {
		section.Body = strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	}
	if len(section.Headers) == 0 {
		section.Headers = nil
	}
	return section
}

func parseHeadersAfterMarker(lines []string, start int) (map[string][]string, int) {
	headers := make(map[string][]string)
	idx := start
	for ; idx < len(lines); idx++ {
		line := lines[idx]
		if strings.TrimSpace(line) == "" {
			break
		}
		key, value, ok := splitHeaderLine(line)
		if !ok {
			continue
		}
		headers[key] = append(headers[key], strings.TrimSpace(value))
	}
	if len(headers) == 0 {
		headers = nil
	}
	return headers, idx
}

func parseAPIErrorSection(content string) parsedAPIErrorSection {
	section := parsedAPIErrorSection{}
	lines := strings.Split(content, "\n")
	for idx, line := range lines {
		key, value, ok := splitHeaderLine(line)
		if ok && key == "HTTP Status" {
			section.Status = parseInt(value)
			if idx+1 < len(lines) {
				section.Message = strings.TrimSpace(strings.Join(lines[idx+1:], "\n"))
			}
			return section
		}
	}
	section.Message = strings.TrimSpace(content)
	return section
}

func splitHeaderLine(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(parts[1]), true
}

func parseRFC3339Timestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseInt(value string) int {
	var parsed int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed)
	return parsed
}

func pathFromRequestURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		if idx := strings.IndexByte(raw, '?'); idx >= 0 {
			return raw[:idx]
		}
		return raw
	}
	parsed, errParse := url.Parse(raw)
	if errParse != nil {
		return raw
	}
	if parsed.Path == "" {
		return raw
	}
	return parsed.Path
}

func lookupUsageDetailByRequestID(snapshot internalusage.StatisticsSnapshot, requestID string) (usageRequestLookup, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return usageRequestLookup{}, false
	}
	for _, apiSnapshot := range snapshot.APIs {
		for model, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				if strings.TrimSpace(detail.RequestID) == requestID {
					return usageRequestLookup{
						Model:  model,
						Detail: detail,
					}, true
				}
			}
		}
	}
	return usageRequestLookup{}, false
}

func inferErrorStage(statusCode, upstreamStatusCode int) string {
	switch {
	case upstreamStatusCode >= http.StatusBadRequest:
		return "upstream"
	case statusCode >= http.StatusBadRequest:
		return "proxy"
	default:
		return ""
	}
}

func deriveRequestDetailMessage(statusCode, upstreamStatusCode int, upstreamMessage string) string {
	if statusCode > 0 && upstreamStatusCode > 0 && statusCode != upstreamStatusCode {
		return fmt.Sprintf("proxy returned %d because upstream returned %d", statusCode, upstreamStatusCode)
	}
	if upstreamMessage != "" {
		return upstreamMessage
	}
	if statusCode > 0 {
		return http.StatusText(statusCode)
	}
	return ""
}

func deriveBodyErrorDetails(body string) (string, string, string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", "", ""
	}
	if !gjson.Valid(trimmed) {
		return "", "", trimmed
	}
	typeValue := strings.TrimSpace(firstNonEmptyGJSONText(trimmed,
		"error.type",
		"type",
	))
	codeValue := strings.TrimSpace(firstNonEmptyGJSONText(trimmed,
		"error.code",
		"code",
	))
	messageValue := strings.TrimSpace(firstNonEmptyGJSONText(trimmed,
		"error.message",
		"message",
		"error.error.message",
	))
	return typeValue, codeValue, messageValue
}

func firstNonEmptyGJSONText(payload string, paths ...string) string {
	for _, path := range paths {
		value := strings.TrimSpace(gjson.Get(payload, path).String())
		if value != "" {
			return value
		}
	}
	return ""
}

func decodeJSONBody(body string) any {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return nil
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return payload
}

func truncateText(text string, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes), false
	}
	return string(runes[:limit]) + "...", true
}

func firstHeaderValueCI(headers map[string][]string, keys ...string) string {
	for _, key := range keys {
		for headerKey, values := range headers {
			if !strings.EqualFold(strings.TrimSpace(headerKey), key) || len(values) == 0 {
				continue
			}
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	return ""
}
