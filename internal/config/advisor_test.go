package config

import "testing"

func TestAdvisorPolicy(t *testing.T) {
	if err := (Advisor{}).Validate(); err != nil {
		t.Fatal(err)
	}
	valid := Advisor{Enabled: true, Model: "gpt-6-astra", APIKeyFile: "/run/credentials/bridged.service/openai-api-key", TimeoutSeconds: 20, MaxToolCalls: 6, MaxSessionsPerHour: 4, ProjectBudgetAcknowledged: true}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Advisor)
	}{
		{"model missing", func(c *Advisor) { c.Model = "" }},
		{"model path", func(c *Advisor) { c.Model = "../other" }},
		{"credential relative", func(c *Advisor) { c.APIKeyFile = "credential" }},
		{"credential directory", func(c *Advisor) { c.APIKeyFile = "/" }},
		{"credential traversal", func(c *Advisor) { c.APIKeyFile = "/run/../credential" }},
		{"credential control", func(c *Advisor) { c.APIKeyFile = "/run/credential\n" }},
		{"timeout absent", func(c *Advisor) { c.TimeoutSeconds = 0 }},
		{"timeout exceeds guard", func(c *Advisor) { c.TimeoutSeconds = 21 }},
		{"tools absent", func(c *Advisor) { c.MaxToolCalls = 0 }},
		{"tools excessive", func(c *Advisor) { c.MaxToolCalls = 9 }},
		{"rate absent", func(c *Advisor) { c.MaxSessionsPerHour = 0 }},
		{"rate excessive", func(c *Advisor) { c.MaxSessionsPerHour = 61 }},
		{"budget unacknowledged", func(c *Advisor) { c.ProjectBudgetAcknowledged = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			tc.edit(&c)
			if c.Validate() == nil {
				t.Fatal("invalid advisor policy accepted")
			}
		})
	}
}
