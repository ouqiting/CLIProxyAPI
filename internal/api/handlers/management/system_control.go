package management

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/managementasset"
)

const systemRestartCommand = "systemctl restart cliproxyapi.service"

type webUIUpdateRequest struct {
	Force *bool `json:"force"`
}

// UpdateWebUI triggers a manual sync of the management control panel asset.
func (h *Handler) UpdateWebUI(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "management config not available"})
		return
	}
	if h.cfg.RemoteManagement.DisableControlPanel {
		c.JSON(http.StatusBadRequest, gin.H{"error": "control panel disabled"})
		return
	}

	force := false
	body, errRead := readOptionalJSONBody(c)
	if errRead != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body != nil && body.Force != nil {
		force = *body.Force
	}

	result := h.syncManagementHTML(
		c.Request.Context(),
		managementasset.StaticDir(h.configFilePath),
		h.cfg.ProxyURL,
		h.cfg.RemoteManagement.PanelGitHubRepository,
		managementasset.UpdateOptions{Force: force},
	)

	status := http.StatusOK
	if !result.Success {
		status = http.StatusInternalServerError
	}
	if result.Message == "" {
		if result.Success {
			result.Message = "webui update completed"
		} else {
			result.Message = "webui update failed"
		}
	}

	c.JSON(status, result)
}

// RestartSystem schedules a service restart on Linux hosts.
func (h *Handler) RestartSystem(c *gin.Context) {
	if errSchedule := h.scheduleRestart(); errSchedule != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to schedule restart: %v", errSchedule)})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"accepted": true,
		"message":  "restart scheduled",
		"command":  systemRestartCommand,
	})
}

func readOptionalJSONBody(c *gin.Context) (*webUIUpdateRequest, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}

	raw, err := c.GetRawData()
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var body webUIUpdateRequest
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return &body, nil
}

func scheduleSystemRestart() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("system restart endpoint is only supported on linux")
	}

	cmd := exec.Command("sh", "-c", strings.Join([]string{"sleep 1", systemRestartCommand}, "; "))
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
