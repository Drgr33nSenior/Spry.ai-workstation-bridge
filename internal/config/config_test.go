package config

import "testing"

func good() Config {
	return Config{Mode: "demo", StateDir: "/bridge-test/state", SourcePath: "/bridge-test/state/source.json", Listen: "127.0.0.1:8743", ExternalURL: "http://127.0.0.1:8743", AllowedHosts: []string{"127.0.0.1:8743"}, OwnerUID: 1000, Target: "fixture", Environment: "dev", QueueDepth: 4, OperationTimeoutSeconds: 300}
}
func TestUnsafeListenersAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    func(*Config)
	}{{"wildcard", func(c *Config) { c.Listen = "0.0.0.0:8743" }}, {"IPv6 wildcard", func(c *Config) { c.Listen = "[::]:8743" }}, {"plaintext LAN", func(c *Config) { c.Listen = "192.0.2.1:8743" }}, {"DNS listener", func(c *Config) { c.Listen = "localhost:8743" }}, {"demo helper", func(c *Config) { c.HostSocket = "/run/bridge-hostd.sock" }}, {"unknown mode", func(c *Config) { c.Mode = "auto" }}, {"production", func(c *Config) { c.Environment = "prd" }}, {"wrong host", func(c *Config) { c.AllowedHosts = []string{"attacker.example"} }}, {"credential URL", func(c *Config) { c.ExternalURL = "http://user:private@127.0.0.1:8743" }}} {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			tc.f(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLiveBrowserSessionsRequireDedicatedHTTPSIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Config)
	}{
		{"plaintext loopback", func(c *Config) { c.Mode = "live"; c.BrowserSessions = true }},
		{"HTTPS IP literal", func(c *Config) {
			c.Mode = "live"
			c.BrowserSessions = true
			c.ExternalURL = "https://127.0.0.1:8743"
			c.AllowedHosts = []string{"127.0.0.1:8743"}
			c.TLSCertFile = "/etc/bridge/tls/server.crt"
			c.TLSKeyFile = "/etc/bridge/tls/server.key"
		}},
		{"HTTPS localhost", func(c *Config) {
			c.Mode = "live"
			c.BrowserSessions = true
			c.ExternalURL = "https://localhost:8743"
			c.AllowedHosts = []string{"localhost:8743"}
			c.TLSCertFile = "/etc/bridge/tls/server.crt"
			c.TLSKeyFile = "/etc/bridge/tls/server.key"
		}},
		{"HTTPS single-label host", func(c *Config) {
			c.Mode = "live"
			c.BrowserSessions = true
			c.ExternalURL = "https://bridge:8743"
			c.AllowedHosts = []string{"bridge:8743"}
			c.TLSCertFile = "/etc/bridge/tls/server.crt"
			c.TLSKeyFile = "/etc/bridge/tls/server.key"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := good()
			tc.edit(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe live browser configuration accepted")
			}
		})
	}
	demo := good()
	demo.BrowserSessions = true
	if err := demo.Validate(); err != nil {
		t.Fatalf("demo browser session separation refused: %v", err)
	}
	if got := demo.BrowserSessionCookieName(); got != DemoBrowserSessionCookie {
		t.Fatalf("demo cookie %q", got)
	}
}
func TestStrictJSON(t *testing.T) {
	for _, b := range []string{`{"mode":"demo","mode":"live"}`, `{"unknown":1}`, `{} {}`, `{"mode":`, `{"owner_uid":"0"}`} {
		var c Config
		if Decode([]byte(b), &c) == nil {
			t.Fatalf("accepted %s", b)
		}
	}
}
