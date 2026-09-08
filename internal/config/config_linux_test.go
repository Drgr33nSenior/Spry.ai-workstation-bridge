//go:build linux

package config

import "testing"

func TestCompleteLiveBrowserConfigurationOnLinux(t *testing.T) {
	if err := goodLiveBrowser().Validate(); err != nil {
		t.Fatalf("complete live browser configuration refused: %v", err)
	}
}

func TestCompleteLiveCLILoopbackConfigurationOnLinux(t *testing.T) {
	c := goodLiveBrowser()
	c.BrowserSessions = false
	c.Listen = "127.0.0.1:8743"
	c.ExternalURL = "http://127.0.0.1:8743"
	c.AllowedHosts = []string{"127.0.0.1:8743"}
	c.TLSCertFile = ""
	c.TLSKeyFile = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("complete CLI-only loopback live configuration refused: %v", err)
	}
}
