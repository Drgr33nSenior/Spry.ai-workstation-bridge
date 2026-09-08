package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

type Config struct {
	Mode         string   `json:"mode"`
	StateDir     string   `json:"state_dir"`
	SourcePath   string   `json:"source_path"`
	Listen       string   `json:"listen"`
	AllowedHosts []string `json:"allowed_hosts"`
	ExternalURL  string   `json:"external_url"`
	TLSCertFile  string   `json:"tls_cert_file"`
	TLSKeyFile   string   `json:"tls_key_file"`
	// BrowserSessions is an explicit live-policy opt-in. A live loopback HTTP
	// listener remains available to the bearer-authenticated CLI only.
	BrowserSessions         bool   `json:"browser_sessions"`
	OwnerUID                int    `json:"owner_uid"`
	Target                  string `json:"target"`
	Environment             string `json:"environment"`
	ReferenceRoot           string `json:"reference_root"`
	ModelRoot               string `json:"model_root"`
	CompilerCacheRoot       string `json:"compiler_cache_root"`
	ShaderCacheRoot         string `json:"shader_cache_root"`
	HostSocket              string `json:"host_socket"`
	WorkerSocket            string `json:"worker_socket"`
	ClientBaseURL           string `json:"client_base_url"`
	QueueDepth              int    `json:"queue_depth"`
	OperationTimeoutSeconds int    `json:"operation_timeout_seconds"`
}

const (
	LiveBrowserSessionCookie   = "__Host-bridge_session_v2"
	DemoBrowserSessionCookie   = "bridge_demo_session_v2"
	LegacyBrowserSessionCookie = "bridge_session"
	LiveBrowserSessionPurpose  = "live-browser-v2"
	DemoBrowserSessionPurpose  = "demo-browser-v2"
)

var (
	errLiveBrowserSessionsHTTPS = errors.New("live browser sessions require HTTPS with certificate and key files")
	errLiveBrowserSessionsDNS   = errors.New("live browser sessions require a dedicated trusted DNS management hostname, not localhost, a single-label host or an IP literal")
)

// BrowserSessionsEnabled separates disposable HTTP demo sessions from live
// management sessions. Live browser sessions require the explicit policy flag.
func (c Config) BrowserSessionsEnabled() bool {
	return c.Mode == "demo" || c.BrowserSessions
}

// BrowserSessionCookieName separates live and demo cookie policies. Authentication
// also checks BrowserSessionPurpose: changing a cookie name alone cannot revoke
// a captured pre-upgrade session token.
func (c Config) BrowserSessionCookieName() string {
	if c.Mode == "live" {
		return LiveBrowserSessionCookie
	}
	return DemoBrowserSessionCookie
}

func (c Config) BrowserSessionPurpose() string {
	if c.Mode == "live" {
		return LiveBrowserSessionPurpose
	}
	return DemoBrowserSessionPurpose
}

// validateBrowserSessionPolicy keeps the live browser-session trust boundary
// independent of the host platform. Validate applies the rest of the live
// adapter policy afterwards. Cookies are host-scoped, not port-scoped: an
// enabled hostname therefore identifies a single trusted HTTPS management
// boundary, including every service that can receive its cookies.
func validateBrowserSessionPolicy(c Config, origin *url.URL) error {
	if c.Mode != "live" || !c.BrowserSessions {
		return nil
	}
	if origin.Scheme != "https" || c.TLSCertFile == "" || c.TLSKeyFile == "" {
		return errLiveBrowserSessionsHTTPS
	}
	host := origin.Hostname()
	if host == "" || !strings.Contains(host, ".") || strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return errLiveBrowserSessionsDNS
	}
	return nil
}

func Decode(b []byte, v any) error {
	if err := uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("invalid JSON or unknown field")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("one JSON document is required")
	}
	return nil
}
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting limit exceeded")
	}
	t, e := d.Token()
	if e != nil {
		return errors.New("invalid JSON")
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return errors.New("invalid JSON object")
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate JSON key")
			}
			seen[name] = true
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, e = d.Token()
	return e
}
func Load(path string) (Config, error) {
	var c Config
	b, e := safefile.Read(path, 65536)
	if e != nil {
		return c, e
	}
	if e = Decode(b, &c); e != nil {
		return c, e
	}
	if e = c.Validate(); e != nil {
		return c, e
	}
	i, e := os.Stat(path)
	if e != nil {
		return c, e
	}
	uid, e := safefile.Owner(path)
	if e != nil {
		return c, e
	}
	if i.Mode().Perm()&0022 != 0 {
		return c, errors.New("configuration must not be group/world writable")
	}
	if c.Mode == "live" && uid != 0 {
		return c, errors.New("live service policy must be root-owned")
	}
	if c.Mode == "live" {
		for p := path; ; p = filepath.Dir(p) {
			i, e := os.Lstat(p)
			if e != nil {
				return c, e
			}
			owner, e := safefile.Owner(p)
			if e != nil {
				return c, e
			}
			if owner != 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
				return c, errors.New("live policy and all parent directories must be root-owned and protected")
			}
			if p == filepath.Dir(p) {
				break
			}
		}
	}
	if c.Mode == "demo" && uid != c.OwnerUID && uid != 0 {
		return c, errors.New("demo policy must belong to its explicitly allowed owner")
	}
	return c, nil
}
func (c Config) Validate() error {
	if c.Mode != "demo" && c.Mode != "live" {
		return errors.New("mode must explicitly be demo or live")
	}
	if c.OwnerUID < 0 || c.Target == "" || strings.ContainsAny(c.Target, " /\\\t\n") || len(c.Target) > 63 {
		return errors.New("explicit owner UID and target are required")
	}
	if c.QueueDepth < 1 || c.QueueDepth > 16 || c.OperationTimeoutSeconds < 10 || c.OperationTimeoutSeconds > 86400 {
		return errors.New("queue depth must be 1..16 and timeout 10..86400 seconds")
	}
	if c.Environment != "dev" && c.Environment != "tst" && c.Environment != "int" {
		return errors.New("target requires explicit dev, tst or int classification; production is not supported")
	}
	for _, p := range []string{c.StateDir, c.SourcePath} {
		if e := safefile.CheckPath(p); e != nil {
			return e
		}
		if p == "/" || p == os.Getenv("HOME") {
			return errors.New("use a dedicated managed directory")
		}
	}
	if filepath.Dir(c.SourcePath) != c.StateDir {
		return errors.New("source_path must be a file directly inside the dedicated state_dir")
	}
	host, port, e := net.SplitHostPort(c.Listen)
	if e != nil {
		return errors.New("listen requires a literal IP and port")
	}
	ip := net.ParseIP(host)
	p, e := strconv.Atoi(port)
	if ip == nil || ip.IsUnspecified() || e != nil || p < 1 || p > 65535 {
		return errors.New("wildcard/DNS listeners are forbidden; choose an explicit IP and port")
	}
	u, e := url.Parse(c.ExternalURL)
	if e != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("external_url must be the exact management origin")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("HTTP or HTTPS origin required")
	}
	if len(c.AllowedHosts) == 0 || len(c.AllowedHosts) > 8 {
		return errors.New("explicit allowed_hosts required")
	}
	found := false
	for _, h := range c.AllowedHosts {
		if h == u.Host {
			found = true
		}
		if h == "" || strings.ContainsAny(h, "/*\\\r\n\t@ ") {
			return errors.New("invalid allowed host")
		}
	}
	if !found {
		return errors.New("external_url host must be in allowed_hosts")
	}
	if e := validateBrowserSessionPolicy(c, u); e != nil {
		return e
	}
	if !ip.IsLoopback() && (u.Scheme != "https" || c.TLSCertFile == "" || c.TLSKeyFile == "") {
		return errors.New("non-loopback management requires authenticated HTTPS and a reviewed explicit interface")
	}
	if u.Scheme == "http" {
		externalIP := net.ParseIP(u.Hostname())
		if !ip.IsLoopback() || !(u.Hostname() == "localhost" || externalIP != nil && externalIP.IsLoopback()) {
			return errors.New("plaintext management must remain loopback")
		}
	}
	if u.Scheme == "https" && (c.TLSCertFile == "" || c.TLSKeyFile == "") {
		return errors.New("HTTPS requires certificate and key files")
	}
	if c.Mode == "demo" {
		if !ip.IsLoopback() {
			return errors.New("demo must bind loopback")
		}
		if c.HostSocket != "" || c.WorkerSocket != "" || c.ReferenceRoot != "" || c.ModelRoot != "" || c.CompilerCacheRoot != "" || c.ShaderCacheRoot != "" {
			return errors.New("demo policy must not configure live adapters")
		}
	}
	if c.Mode == "live" {
		if runtime.GOOS != "linux" {
			return errors.New("live controller requires Linux; use the remote CLI on macOS")
		}
		for _, p := range []string{c.ReferenceRoot, c.ModelRoot, c.HostSocket, c.WorkerSocket, c.CompilerCacheRoot, c.ShaderCacheRoot} {
			if e := safefile.CheckPath(p); e != nil {
				return e
			}
		}
	}
	return nil
}
