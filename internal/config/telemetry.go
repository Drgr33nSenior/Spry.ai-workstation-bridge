package config

import (
	"errors"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Telemetry is administrator policy, never a management API write surface.
// Its zero value neither exports telemetry nor queries a backend.
type Telemetry struct {
	Enabled          bool    `json:"enabled"`
	OTLPEndpoint     string  `json:"otlp_endpoint"`
	TraceSampleRatio float64 `json:"trace_sample_ratio"`
	PrometheusURL    string  `json:"prometheus_url"`
}

func (c Telemetry) Validate() error {
	if math.IsNaN(c.TraceSampleRatio) || math.IsInf(c.TraceSampleRatio, 0) || c.TraceSampleRatio < 0 || c.TraceSampleRatio > 1 {
		return errors.New("telemetry trace_sample_ratio must be between 0 and 1")
	}
	if c.Enabled && c.OTLPEndpoint == "" {
		return errors.New("enabled telemetry requires an explicit otlp_endpoint")
	}
	for _, endpoint := range []string{c.OTLPEndpoint, c.PrometheusURL} {
		if endpoint != "" {
			if err := ValidateTelemetryEndpoint(endpoint); err != nil {
				return err
			}
		}
	}
	return nil
}

// Numeric private addresses avoid DNS rebinding. Link-local/metadata, public,
// wildcard, credentials and arbitrary paths are forbidden. LAN requires TLS.
func ValidateTelemetryEndpoint(endpoint string) error {
	invalid := errors.New("telemetry endpoint must be an explicit loopback HTTP(S) or private-IP HTTPS origin with port, without credentials, path, query or fragment")
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 256 || strings.ContainsAny(endpoint, "?#") || u.Opaque != "" || u.User != nil || u.RawPath != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return invalid
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) || err != nil || port < 1 || port > 65535 {
		return invalid
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && ip.IsLoopback()) {
		return invalid
	}
	return nil
}
