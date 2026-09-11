package config

import (
	"errors"
	"path/filepath"
	"regexp"
)

// Advisor is local administrator policy. Its zero value makes no cloud calls.
// ProjectBudgetAcknowledged records an administrator's acknowledgement only;
// it neither creates nor verifies an OpenAI project spend limit.
type Advisor struct {
	Enabled                   bool   `json:"enabled"`
	Model                     string `json:"model"`
	APIKeyFile                string `json:"api_key_file"`
	TimeoutSeconds            int    `json:"timeout_seconds"`
	MaxToolCalls              int    `json:"max_tool_calls"`
	MaxSessionsPerHour        int    `json:"max_sessions_per_hour"`
	ProjectBudgetAcknowledged bool   `json:"project_budget_acknowledged"`
}

func (c Advisor) Validate() error {
	if !c.Enabled {
		return nil
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(c.Model) {
		return errors.New("enabled advisor requires an explicit model identifier")
	}
	if !filepath.IsAbs(c.APIKeyFile) || filepath.Clean(c.APIKeyFile) != c.APIKeyFile || c.APIKeyFile == "/" || regexp.MustCompile(`[\x00-\x20\x7f]`).MatchString(c.APIKeyFile) {
		return errors.New("enabled advisor requires a canonical absolute api_key_file")
	}
	if c.TimeoutSeconds < 5 || c.TimeoutSeconds > 20 || c.MaxToolCalls < 3 || c.MaxToolCalls > 8 || c.MaxSessionsPerHour < 1 || c.MaxSessionsPerHour > 60 {
		return errors.New("advisor requires timeout_seconds 5..20, max_tool_calls 3..8, and max_sessions_per_hour 1..60")
	}
	if !c.ProjectBudgetAcknowledged {
		return errors.New("enabled advisor requires acknowledgement of a separately managed provider project budget; local limits are not a dollar ceiling")
	}
	return nil
}
