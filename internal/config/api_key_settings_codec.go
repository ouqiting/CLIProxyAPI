package config

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

type apiKeySettingsAlias APIKeySettings

// UnmarshalYAML accepts both disable-logging and disableLogging for compatibility
// with config files that use kebab-case or camelCase keys.
func (s *APIKeySettings) UnmarshalYAML(value *yaml.Node) error {
	var aux struct {
		apiKeySettingsAlias `yaml:",inline"`
		DisableLoggingCamel *bool `yaml:"disableLogging"`
	}
	if err := value.Decode(&aux); err != nil {
		return err
	}

	*s = APIKeySettings(aux.apiKeySettingsAlias)
	if aux.DisableLoggingCamel != nil {
		s.DisableLogging = *aux.DisableLoggingCamel
	}
	return nil
}

// UnmarshalJSON accepts both disable-logging and disableLogging for management API payloads.
func (s *APIKeySettings) UnmarshalJSON(data []byte) error {
	var aux struct {
		apiKeySettingsAlias
		DisableLoggingCamel *bool `json:"disableLogging"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*s = APIKeySettings(aux.apiKeySettingsAlias)
	if aux.DisableLoggingCamel != nil {
		s.DisableLogging = *aux.DisableLoggingCamel
	}
	return nil
}
