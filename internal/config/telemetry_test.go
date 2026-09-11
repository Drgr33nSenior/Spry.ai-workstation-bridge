package config

import (
	"math"
	"strings"
	"testing"
)

func TestTelemetryPolicy(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:4318", "http://[::1]:4318", "https://10.0.0.2:4318", "https://[fd00::2]:4318"} {
		c := good()
		c.Telemetry = Telemetry{Enabled: true, OTLPEndpoint: endpoint, TraceSampleRatio: .1, PrometheusURL: "http://127.0.0.1:19090"}
		if err := c.Validate(); err != nil {
			t.Fatalf("accepted endpoint refused: %v", err)
		}
	}
	for _, endpoint := range []string{"http://localhost:4318", "http://0.0.0.0:4318", "https://8.8.8.8:4318", "http://10.0.0.2:4318", "http://169.254.169.254:80", "https://[fe80::1]:4318", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "file:///tmp/test", "http://sentinel:secret@127.0.0.1:4318", "http://127.0.0.1:4318/v1/traces", "http://127.0.0.1:4318/", "http://127.0.0.1:4318?query=secret", "http://127.0.0.1:4318?", "http://127.0.0.1:4318#secret"} {
		for _, enabled := range []bool{true, false} {
			c := Telemetry{Enabled: enabled, OTLPEndpoint: endpoint}
			if err := c.Validate(); err == nil || strings.Contains(err.Error(), "sentinel") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("invalid endpoint accepted or echoed: %q, %v", endpoint, err)
			}
			c = Telemetry{PrometheusURL: endpoint}
			if c.Validate() == nil {
				t.Fatalf("invalid backend accepted: %q", endpoint)
			}
		}
	}
	for _, ratio := range []float64{-1, 1.01, math.NaN(), math.Inf(1)} {
		if (Telemetry{TraceSampleRatio: ratio}).Validate() == nil {
			t.Fatal("invalid sampling ratio accepted")
		}
	}
	if (Telemetry{Enabled: true}).Validate() == nil {
		t.Fatal("implicit export endpoint accepted")
	}
	if err := (Telemetry{}).Validate(); err != nil {
		t.Fatal(err)
	}
}
