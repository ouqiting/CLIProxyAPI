package handlers

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
)

const disabledModelsMetadataKey = "disabled_models"

// FilterModelsForRequest removes models disabled for the current authenticated API key.
func FilterModelsForRequest(c *gin.Context, models []map[string]any) []map[string]any {
	disabled := DisabledModelsFromRequest(c)
	if len(disabled) == 0 {
		return models
	}

	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if !isModelEntryDisabled(disabled, model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// IsModelDisabledForRequest reports whether the model is disabled for the current request.
func IsModelDisabledForRequest(c *gin.Context, modelName string) bool {
	return modelDisabled(DisabledModelsFromRequest(c), modelName)
}

// IsModelDisabledForContext reports whether the model is disabled for the current request context.
func IsModelDisabledForContext(ctx context.Context, modelName string) bool {
	if ctx == nil {
		return false
	}
	ginCtx, _ := ctx.Value("gin").(*gin.Context)
	return IsModelDisabledForRequest(ginCtx, modelName)
}

func DisabledModelsFromRequest(c *gin.Context) map[string]struct{} {
	if c == nil {
		return nil
	}
	raw, exists := c.Get("accessMetadata")
	if !exists {
		return nil
	}
	metadata, ok := raw.(map[string]string)
	if !ok {
		return nil
	}
	return disabledModelsFromMetadata(metadata)
}

func disabledModelsFromMetadata(metadata map[string]string) map[string]struct{} {
	if len(metadata) == 0 {
		return nil
	}
	raw := strings.TrimSpace(metadata[disabledModelsMetadataKey])
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, entry := range strings.Split(raw, ",") {
		name := normalizeRestrictedModelName(entry)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isModelEntryDisabled(disabled map[string]struct{}, model map[string]any) bool {
	if len(disabled) == 0 || len(model) == 0 {
		return false
	}
	if id, _ := model["id"].(string); modelDisabled(disabled, id) {
		return true
	}
	if name, _ := model["name"].(string); modelDisabled(disabled, name) {
		return true
	}
	return false
}

func modelDisabled(disabled map[string]struct{}, modelName string) bool {
	if len(disabled) == 0 {
		return false
	}
	normalized := normalizeRestrictedModelName(modelName)
	if normalized == "" {
		return false
	}
	_, blocked := disabled[normalized]
	return blocked
}

func normalizeRestrictedModelName(modelName string) string {
	trimmed := strings.TrimSpace(modelName)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "models/")
	parsed := thinking.ParseSuffix(trimmed)
	base := strings.TrimSpace(parsed.ModelName)
	if base == "" {
		base = trimmed
	}
	base = strings.TrimPrefix(base, "models/")
	return strings.ToLower(strings.TrimSpace(base))
}
