package management

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestWebUIDataWriteReadListDelete(t *testing.T) {
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

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	writePayload := `{"path":"state/session.json","content":"{\"theme\":\"light\"}"}`
	writeRec := httptest.NewRecorder()
	writeCtx, _ := gin.CreateTestContext(writeRec)
	writeCtx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/webui-data", strings.NewReader(writePayload))
	writeCtx.Request.Header.Set("Content-Type", "application/json")
	h.PutWebUIData(writeCtx)

	if writeRec.Code != http.StatusOK {
		t.Fatalf("write status = %d, want 200, body=%s", writeRec.Code, writeRec.Body.String())
	}

	writtenPath := filepath.Join(projectDir, "webui_data", "state", "session.json")
	data, errRead := os.ReadFile(writtenPath)
	if errRead != nil {
		t.Fatalf("failed to read written file: %v", errRead)
	}
	if string(data) != `{"theme":"light"}` {
		t.Fatalf("written content = %q, want %q", string(data), `{"theme":"light"}`)
	}

	readRec := httptest.NewRecorder()
	readCtx, _ := gin.CreateTestContext(readRec)
	readCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/webui-data?path="+url.QueryEscape("state/session.json"), nil)
	h.GetWebUIData(readCtx)

	if readRec.Code != http.StatusOK {
		t.Fatalf("read status = %d, want 200, body=%s", readRec.Code, readRec.Body.String())
	}

	var readResp webUIDataResponse
	if errDecode := json.Unmarshal(readRec.Body.Bytes(), &readResp); errDecode != nil {
		t.Fatalf("failed to decode read response: %v", errDecode)
	}
	if readResp.Type != "file" {
		t.Fatalf("read type = %q, want file", readResp.Type)
	}
	if readResp.Content != `{"theme":"light"}` {
		t.Fatalf("read content = %q, want %q", readResp.Content, `{"theme":"light"}`)
	}
	if readResp.ContentBase64 != base64.StdEncoding.EncodeToString([]byte(`{"theme":"light"}`)) {
		t.Fatalf("unexpected content_base64: %q", readResp.ContentBase64)
	}

	listRec := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRec)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/webui-data?path="+url.QueryEscape("state"), nil)
	h.GetWebUIData(listCtx)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body=%s", listRec.Code, listRec.Body.String())
	}

	var listResp webUIDataResponse
	if errDecode := json.Unmarshal(listRec.Body.Bytes(), &listResp); errDecode != nil {
		t.Fatalf("failed to decode list response: %v", errDecode)
	}
	if listResp.Type != "directory" {
		t.Fatalf("list type = %q, want directory", listResp.Type)
	}
	if len(listResp.Entries) != 1 || listResp.Entries[0].Path != "state/session.json" {
		t.Fatalf("unexpected list entries: %+v", listResp.Entries)
	}

	deleteRec := httptest.NewRecorder()
	deleteCtx, _ := gin.CreateTestContext(deleteRec)
	deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/webui-data?path="+url.QueryEscape("state/session.json"), nil)
	h.DeleteWebUIData(deleteCtx)

	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200, body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, errStat := os.Stat(writtenPath); !os.IsNotExist(errStat) {
		t.Fatalf("file still exists after delete")
	}
}

func TestWebUIDataRejectsTraversal(t *testing.T) {
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

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	for _, candidate := range []string{"../secret.txt", `..\secret.txt`, "/etc/passwd"} {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/webui-data?path="+url.QueryEscape(candidate), nil)
		h.GetWebUIData(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q status = %d, want 400", candidate, rec.Code)
		}
	}
}

func TestWebUIDataRootListingReturnsEmptyWhenMissing(t *testing.T) {
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

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/webui-data", nil)
	h.GetWebUIData(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var resp webUIDataResponse
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("failed to decode response: %v", errDecode)
	}
	if resp.Type != "directory" {
		t.Fatalf("type = %q, want directory", resp.Type)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("entries len = %d, want 0", len(resp.Entries))
	}
}
