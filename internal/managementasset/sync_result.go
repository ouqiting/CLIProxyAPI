package managementasset

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// UpdateOptions controls manual management asset sync behavior.
type UpdateOptions struct {
	Force bool
}

// UpdateLogEntry represents one structured sync log line for callers.
type UpdateLogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// UpdateResult is the structured outcome of a management asset sync attempt.
type UpdateResult struct {
	Success    bool             `json:"success"`
	Updated    bool             `json:"updated"`
	Exists     bool             `json:"exists"`
	FilePath   string           `json:"file_path,omitempty"`
	Message    string           `json:"message"`
	Error      string           `json:"error,omitempty"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	DurationMS int64            `json:"duration_ms"`
	Logs       []UpdateLogEntry `json:"logs,omitempty"`
}

// SyncLatestManagementHTML checks the latest management asset and returns a structured result.
func SyncLatestManagementHTML(ctx context.Context, staticDir string, proxyURL string, panelRepository string, opts UpdateOptions) UpdateResult {
	if ctx == nil {
		ctx = context.Background()
	}

	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		result := UpdateResult{
			Message:   "management asset sync skipped: empty static directory",
			Error:     "empty static directory",
			StartedAt: time.Now().UTC(),
		}
		appendUpdateLog(&result, "debug", result.Message)
		finalizeUpdateResult(&result, "")
		return result
	}

	localPath := filepath.Join(staticDir, managementAssetName)
	value, _, _ := sfGroup.Do(localPath, func() (interface{}, error) {
		result := UpdateResult{
			FilePath:  localPath,
			StartedAt: time.Now().UTC(),
		}

		if opts.Force {
			appendUpdateLog(&result, "info", "forcing management asset sync; bypassing throttle")
		}

		lastUpdateCheckMu.Lock()
		now := time.Now()
		timeSinceLastAttempt := now.Sub(lastUpdateCheckTime)
		if !opts.Force && !lastUpdateCheckTime.IsZero() && timeSinceLastAttempt < managementSyncMinInterval {
			lastUpdateCheckMu.Unlock()
			appendUpdateLog(
				&result,
				"debug",
				fmt.Sprintf(
					"management asset sync skipped by throttle: last attempt %v ago (interval %v)",
					timeSinceLastAttempt.Round(time.Second),
					managementSyncMinInterval,
				),
			)
			result.Message = "management asset sync skipped by throttle"
			finalizeUpdateResult(&result, localPath)
			if result.Exists {
				result.Success = true
			}
			return result, nil
		}
		lastUpdateCheckTime = now
		lastUpdateCheckMu.Unlock()

		localFileMissing := false
		if _, errStat := os.Stat(localPath); errStat != nil {
			if os.IsNotExist(errStat) {
				localFileMissing = true
				appendUpdateLog(&result, "info", "local management asset is missing")
			} else {
				appendUpdateLog(&result, "debug", fmt.Sprintf("failed to stat local management asset: %v", errStat))
			}
		}

		if errMkdirAll := os.MkdirAll(staticDir, 0o755); errMkdirAll != nil {
			result.Message = "failed to prepare static directory for management asset"
			result.Error = errMkdirAll.Error()
			appendUpdateLog(&result, "warn", fmt.Sprintf("%s: %v", result.Message, errMkdirAll))
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		releaseURL := resolveReleaseURL(panelRepository)
		appendUpdateLog(&result, "info", fmt.Sprintf("checking latest management release: %s", releaseURL))

		client := newHTTPClient(proxyURL)
		localHash, errHash := fileSHA256(localPath)
		if errHash != nil {
			if !os.IsNotExist(errHash) {
				appendUpdateLog(&result, "debug", fmt.Sprintf("failed to read local management asset hash: %v", errHash))
			}
			localHash = ""
		}

		asset, remoteHash, errFetch := fetchLatestAsset(ctx, client, releaseURL)
		if errFetch != nil {
			if localFileMissing {
				appendUpdateLog(&result, "warn", fmt.Sprintf("failed to fetch latest management release information, trying fallback page: %v", errFetch))
				fallbackHash, errFallback := syncFallbackManagementHTML(ctx, client, localPath, &result)
				if errFallback == nil {
					result.Success = true
					result.Updated = true
					result.Message = "management asset updated from fallback page successfully"
					appendUpdateLog(&result, "info", fmt.Sprintf("management asset updated from fallback page successfully (hash=%s)", fallbackHash))
					finalizeUpdateResult(&result, localPath)
					return result, nil
				}
			}
			result.Message = "failed to fetch latest management release information"
			result.Error = errFetch.Error()
			appendUpdateLog(&result, "warn", fmt.Sprintf("%s: %v", result.Message, errFetch))
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		if remoteHash != "" && localHash != "" && strings.EqualFold(remoteHash, localHash) {
			result.Success = true
			result.Message = "management asset is already up to date"
			appendUpdateLog(&result, "info", result.Message)
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		appendUpdateLog(&result, "info", fmt.Sprintf("downloading management asset: %s", asset.BrowserDownloadURL))
		data, downloadedHash, errDownload := downloadAsset(ctx, client, asset.BrowserDownloadURL)
		if errDownload != nil {
			if localFileMissing {
				appendUpdateLog(&result, "warn", fmt.Sprintf("failed to download management asset, trying fallback page: %v", errDownload))
				fallbackHash, errFallback := syncFallbackManagementHTML(ctx, client, localPath, &result)
				if errFallback == nil {
					result.Success = true
					result.Updated = true
					result.Message = "management asset updated from fallback page successfully"
					appendUpdateLog(&result, "info", fmt.Sprintf("management asset updated from fallback page successfully (hash=%s)", fallbackHash))
					finalizeUpdateResult(&result, localPath)
					return result, nil
				}
			}
			result.Message = "failed to download management asset"
			result.Error = errDownload.Error()
			appendUpdateLog(&result, "warn", fmt.Sprintf("%s: %v", result.Message, errDownload))
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		if remoteHash != "" && !strings.EqualFold(remoteHash, downloadedHash) {
			result.Message = "management asset digest mismatch"
			result.Error = fmt.Sprintf("expected %s got %s", remoteHash, downloadedHash)
			appendUpdateLog(&result, "error", fmt.Sprintf("management asset digest mismatch: expected %s got %s", remoteHash, downloadedHash))
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		if errWrite := atomicWriteFile(localPath, data); errWrite != nil {
			result.Message = "failed to update management asset on disk"
			result.Error = errWrite.Error()
			appendUpdateLog(&result, "warn", fmt.Sprintf("%s: %v", result.Message, errWrite))
			finalizeUpdateResult(&result, localPath)
			return result, nil
		}

		result.Success = true
		result.Updated = true
		result.Message = "management asset updated successfully"
		appendUpdateLog(&result, "info", fmt.Sprintf("management asset updated successfully (hash=%s)", downloadedHash))
		finalizeUpdateResult(&result, localPath)
		return result, nil
	})

	result, ok := value.(UpdateResult)
	if !ok {
		result = UpdateResult{
			FilePath:  localPath,
			Message:   "management asset sync returned an unexpected result",
			Error:     "unexpected sync result type",
			StartedAt: time.Now().UTC(),
		}
		appendUpdateLog(&result, "error", result.Message)
		finalizeUpdateResult(&result, localPath)
		return result
	}

	refreshUpdateResultExistence(&result, localPath)
	return result
}

func syncFallbackManagementHTML(ctx context.Context, client *http.Client, localPath string, result *UpdateResult) (string, error) {
	data, downloadedHash, err := downloadAsset(ctx, client, defaultManagementFallbackURL)
	if err != nil {
		appendUpdateLog(result, "warn", fmt.Sprintf("failed to download fallback management control panel page: %v", err))
		return "", err
	}

	appendUpdateLog(
		result,
		"warn",
		fmt.Sprintf(
			"management asset downloaded from fallback URL without digest verification (hash=%s) - enable verified GitHub updates by keeping disable-auto-update-panel set to false",
			downloadedHash,
		),
	)

	if err = atomicWriteFile(localPath, data); err != nil {
		appendUpdateLog(result, "warn", fmt.Sprintf("failed to persist fallback management control panel page: %v", err))
		return "", err
	}

	return downloadedHash, nil
}

func appendUpdateLog(result *UpdateResult, level string, message string) {
	if result == nil {
		return
	}

	entry := UpdateLogEntry{
		Time:    time.Now().UTC(),
		Level:   strings.ToLower(strings.TrimSpace(level)),
		Message: message,
	}
	result.Logs = append(result.Logs, entry)

	switch entry.Level {
	case "debug":
		log.Debug(message)
	case "warn", "warning":
		log.Warn(message)
	case "error":
		log.Error(message)
	default:
		log.Info(message)
	}
}

func finalizeUpdateResult(result *UpdateResult, localPath string) {
	if result == nil {
		return
	}
	if result.FilePath == "" {
		result.FilePath = localPath
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = time.Now().UTC()
	}
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now().UTC()
	}
	result.DurationMS = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	refreshUpdateResultExistence(result, localPath)
}

func refreshUpdateResultExistence(result *UpdateResult, localPath string) {
	if result == nil {
		return
	}
	if strings.TrimSpace(localPath) == "" {
		result.Exists = false
		return
	}
	_, err := os.Stat(localPath)
	result.Exists = err == nil
}
