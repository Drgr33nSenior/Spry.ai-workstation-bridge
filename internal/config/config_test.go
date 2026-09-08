package config

import (
	"errors"
	"net/url"
	"testing"
)

func good() Config {
	return Config{Mode: "demo", StateDir: "/bridge-test/state", SourcePath: "/bridge-test/state/source.json", Listen: "127.0.0.1:8743", ExternalURL: "http://127.0.0.1:8743", AllowedHosts: []string{"127.0.0.1:8743"}, OwnerUID: 1000, Target: "fixture", Environment: "dev", QueueDepth: 4, OperationTimeoutSeconds: 300}
}

// goodLiveBrowser is complete for live validation. Platform-specific tests use
// it to reach the adapter checks; policy tests deliberately use it to avoid
// unrelated missing-field or platform failures.
func goodLiveBrowser() Config {
	c := good()
	c.Mode = "live"
	c.BrowserSessions = true
	c.Listen = "127.0.0.1:8743"
	c.ExternalURL = "https://bridge.management.example:8743"
	c.AllowedHosts = []string{"bridge.management.example:8743"}
	c.TLSCertFile = "/bridge-test/tls/server.crt"
	c.TLSKeyFile = "/bridge-test/tls/server.key"
	c.ReferenceRoot = "/bridge-test/reference"
	c.ModelRoot = "/bridge-test/models"
	c.CompilerCacheRoot = "/bridge-test/cache/compiler"
	c.ShaderCacheRoot = "/bridge-test/cache/shader"
	c.HostSocket = "/bridge-test/run/hostd.sock"
	c.WorkerSocket = "/bridge-test/run/worker.sock"
	return c
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

func TestLiveBrowserSessionsRejectUnsafeManagementIdentity(t *testing.T) {
	if err := validateBrowserSessionPolicy(goodLiveBrowser(), mustParseOrigin(t, "https://bridge.management.example:8743")); err != nil {
		t.Fatalf("complete dedicated DNS HTTPS browser configuration refused: %v", err)
	}

	for _, tc := range []struct {
		name     string
		origin   string
		certFile string
		keyFile  string
		want     error
	}{
		{"plaintext loopback", "http://127.0.0.1:8743", "", "", errLiveBrowserSessionsHTTPS},
		{"missing certificate", "https://bridge.management.example:8743", "", "/bridge-test/tls/server.key", errLiveBrowserSessionsHTTPS},
		{"missing key", "https://bridge.management.example:8743", "/bridge-test/tls/server.crt", "", errLiveBrowserSessionsHTTPS},
		{"HTTPS IP literal", "https://127.0.0.1:8743", "/bridge-test/tls/server.crt", "/bridge-test/tls/server.key", errLiveBrowserSessionsDNS},
		{"HTTPS localhost", "https://localhost:8743", "/bridge-test/tls/server.crt", "/bridge-test/tls/server.key", errLiveBrowserSessionsDNS},
		{"HTTPS single-label host", "https://bridge:8743", "/bridge-test/tls/server.crt", "/bridge-test/tls/server.key", errLiveBrowserSessionsDNS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := goodLiveBrowser()
			c.ExternalURL = tc.origin
			u, err := url.Parse(c.ExternalURL)
			if err != nil {
				t.Fatal(err)
			}
			c.AllowedHosts = []string{u.Host}
			c.TLSCertFile = tc.certFile
			c.TLSKeyFile = tc.keyFile
			if err := c.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tc.want)
			}
		})
	}

	cli := goodLiveBrowser()
	cli.BrowserSessions = false
	cli.ExternalURL = "http://127.0.0.1:8743"
	cli.AllowedHosts = []string{"127.0.0.1:8743"}
	cli.TLSCertFile = ""
	cli.TLSKeyFile = ""
	if err := validateBrowserSessionPolicy(cli, mustParseOrigin(t, cli.ExternalURL)); err != nil {
		t.Fatalf("CLI-only loopback live configuration refused: %v", err)
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

func mustParseOrigin(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func TestStrictJSON(t *testing.T) {
	for _, b := range []string{`{"mode":"demo","mode":"live"}`, `{"unknown":1}`, `{} {}`, `{"mode":`, `{"owner_uid":"0"}`} {
		var c Config
		if Decode([]byte(b), &c) == nil {
			t.Fatalf("accepted %s", b)
		}
	}
}
