package management

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type webUIDataWriteRequest struct {
	Path          string  `json:"path"`
	Content       *string `json:"content"`
	ContentBase64 *string `json:"content_base64"`
}

type webUIDataEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

type webUIDataResponse struct {
	Path          string           `json:"path"`
	Type          string           `json:"type"`
	Content       string           `json:"content,omitempty"`
	ContentBase64 string           `json:"content_base64,omitempty"`
	Size          int64            `json:"size,omitempty"`
	Modified      int64            `json:"modified,omitempty"`
	Entries       []webUIDataEntry `json:"entries,omitempty"`
}

// GetWebUIData reads a file under webui_data or lists a directory.
func (h *Handler) GetWebUIData(c *gin.Context) {
	baseDir, errBase := h.webUIDataDirectory()
	if errBase != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resolve webui_data directory: %v", errBase)})
		return
	}

	fullPath, cleanPath, errResolve := resolveScopedPath(baseDir, c.Query("path"))
	if errResolve != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
		return
	}

	if cleanPath == "" {
		resp, errList := listWebUIDataDirectory(baseDir, fullPath, cleanPath)
		if errList != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list webui_data: %v", errList)})
			return
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	info, errStat := os.Stat(fullPath)
	if errStat != nil {
		if os.IsNotExist(errStat) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read path: %v", errStat)})
		return
	}

	if info.IsDir() {
		resp, errList := listWebUIDataDirectory(baseDir, fullPath, cleanPath)
		if errList != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list webui_data: %v", errList)})
			return
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	data, errRead := os.ReadFile(fullPath)
	if errRead != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read file: %v", errRead)})
		return
	}

	c.JSON(http.StatusOK, webUIDataResponse{
		Path:          cleanPath,
		Type:          "file",
		Content:       string(data),
		ContentBase64: base64.StdEncoding.EncodeToString(data),
		Size:          int64(len(data)),
		Modified:      info.ModTime().Unix(),
	})
}

// PutWebUIData writes a file under webui_data, creating parent directories as needed.
func (h *Handler) PutWebUIData(c *gin.Context) {
	var body webUIDataWriteRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	baseDir, errBase := h.webUIDataDirectory()
	if errBase != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resolve webui_data directory: %v", errBase)})
		return
	}

	fullPath, cleanPath, errResolve := resolveScopedPath(baseDir, body.Path)
	if errResolve != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
		return
	}
	if cleanPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file path"})
		return
	}

	data, errDecode := decodeWebUIDataWriteBody(body)
	if errDecode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errDecode.Error()})
		return
	}

	if errMkdir := os.MkdirAll(filepath.Dir(fullPath), 0o755); errMkdir != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create parent directories: %v", errMkdir)})
		return
	}
	if errWrite := os.WriteFile(fullPath, data, 0o644); errWrite != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write file: %v", errWrite)})
		return
	}

	info, errStat := os.Stat(fullPath)
	if errStat != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to stat file: %v", errStat)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"path":     cleanPath,
		"size":     info.Size(),
		"modified": info.ModTime().Unix(),
	})
}

// DeleteWebUIData removes a file or directory under webui_data.
func (h *Handler) DeleteWebUIData(c *gin.Context) {
	baseDir, errBase := h.webUIDataDirectory()
	if errBase != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resolve webui_data directory: %v", errBase)})
		return
	}

	fullPath, cleanPath, errResolve := resolveScopedPath(baseDir, c.Query("path"))
	if errResolve != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
		return
	}
	if cleanPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing path"})
		return
	}

	info, errStat := os.Stat(fullPath)
	if errStat != nil {
		if os.IsNotExist(errStat) {
			c.JSON(http.StatusNotFound, gin.H{"error": "path not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to stat path: %v", errStat)})
		return
	}

	if info.IsDir() {
		if errRemove := os.RemoveAll(fullPath); errRemove != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete directory: %v", errRemove)})
			return
		}
	} else {
		if errRemove := os.Remove(fullPath); errRemove != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to delete file: %v", errRemove)})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": true,
		"path":    cleanPath,
	})
}

func (h *Handler) webUIDataDirectory() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "webui_data"), nil
}

func decodeWebUIDataWriteBody(body webUIDataWriteRequest) ([]byte, error) {
	switch {
	case body.ContentBase64 != nil:
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(*body.ContentBase64))
		if err != nil {
			return nil, fmt.Errorf("invalid content_base64")
		}
		return decoded, nil
	case body.Content != nil:
		return []byte(*body.Content), nil
	default:
		return []byte{}, nil
	}
}

func resolveScopedPath(baseDir, rawPath string) (string, string, error) {
	baseAbs, errAbs := filepath.Abs(baseDir)
	if errAbs != nil {
		return "", "", errAbs
	}

	cleanPath := strings.TrimSpace(rawPath)
	normalizedRaw := strings.ReplaceAll(cleanPath, "\\", "/")
	if filepath.IsAbs(cleanPath) || strings.HasPrefix(normalizedRaw, "/") {
		return "", "", fmt.Errorf("absolute paths are not allowed")
	}
	cleanPath = normalizedRaw
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = strings.TrimPrefix(cleanPath, "./")
	if cleanPath == "." {
		cleanPath = ""
	}

	target := baseAbs
	if cleanPath != "" {
		target = filepath.Join(baseAbs, filepath.FromSlash(cleanPath))
	}
	targetAbs, errTargetAbs := filepath.Abs(target)
	if errTargetAbs != nil {
		return "", "", errTargetAbs
	}
	rel, errRel := filepath.Rel(baseAbs, targetAbs)
	if errRel != nil {
		return "", "", errRel
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path escapes webui_data")
	}
	if rel == "." {
		rel = ""
	}
	return targetAbs, filepath.ToSlash(rel), nil
}

func listWebUIDataDirectory(baseDir, fullPath, cleanPath string) (webUIDataResponse, error) {
	resp := webUIDataResponse{
		Path: cleanPath,
		Type: "directory",
	}

	if cleanPath == "" {
		if _, errStat := os.Stat(baseDir); errStat != nil {
			if os.IsNotExist(errStat) {
				resp.Entries = []webUIDataEntry{}
				return resp, nil
			}
			return resp, errStat
		}
	}

	entries, errRead := os.ReadDir(fullPath)
	if errRead != nil {
		return resp, errRead
	}

	items := make([]webUIDataEntry, 0, len(entries))
	for _, entry := range entries {
		info, errInfo := entry.Info()
		if errInfo != nil {
			return resp, errInfo
		}
		itemPath := entry.Name()
		if cleanPath != "" {
			itemPath = cleanPath + "/" + entry.Name()
		}
		itemType := "file"
		size := info.Size()
		if entry.IsDir() {
			itemType = "directory"
			size = 0
		}
		items = append(items, webUIDataEntry{
			Name:     entry.Name(),
			Path:     itemPath,
			Type:     itemType,
			Size:     size,
			Modified: info.ModTime().Unix(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "directory"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	resp.Entries = items
	return resp, nil
}
