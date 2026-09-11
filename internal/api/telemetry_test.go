package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestTelemetrySummaryOwnerPolicyAndUnavailable(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		role   string
		status int
	}{{"", 401}, {"viewer", 403}, {"operator", 403}, {"owner", 200}} {
		status, b, _ := f.request(t, "GET", "/api/v1/telemetry/summary", tc.role, nil, nil)
		if status != tc.status {
			t.Fatalf("role %s: %d %s", tc.role, status, b)
		}
		if status == 200 {
			var v domain.TelemetrySummary
			if json.Unmarshal(b, &v) != nil {
				t.Fatal("summary is not typed JSON")
			}
			if v.Backend.State != "not_configured" || v.MetricsExport.State != "not_configured" || !v.MutationStorageAvailable || len(v.Values) != 5 || v.ObservedAt.IsZero() {
				t.Fatalf("summary: %s", b)
			}
			for _, token := range f.tokens {
				if strings.Contains(string(b), token) {
					t.Fatal("credential in summary")
				}
			}
		}
	}
	status, _, _ := f.request(t, "GET", "/api/v1/telemetry/summary?query=SECRET_SENTINEL&url=http://169.254.169.254", "owner", nil, nil)
	if status != 400 {
		t.Fatalf("arbitrary query accepted: %d", status)
	}
}

func TestTelemetryAPIExcludesBodiesQueriesKeysAndIdentifiers(t *testing.T) {
	const sentinel = "SECRET_SENTINEL"
	var mu sync.Mutex
	var exports []byte
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		exports = append(exports, b...)
		exports = append(exports, fmt.Sprint(r.Header)...)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	f := setupTelemetry(t, config.Telemetry{Enabled: true, OTLPEndpoint: collector.URL, TraceSampleRatio: 1})
	f.request(t, "GET", "/api/v1/operations/"+sentinel+"?prompt="+sentinel, "owner", nil, map[string]string{"X-Secret": sentinel, "Baggage": "secret=" + sentinel})
	f.request(t, "POST", "/api/v1/plans", "owner", `{"action":"SECRET_SENTINEL","target":"demo-workstation"}`, map[string]string{"Idempotency-Key": sentinel})
	f.request(t, "GET", "/api/v1/status", "owner", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.eng.Telemetry.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(exports) == 0 {
		t.Fatal("enabled API exported no telemetry")
	}
	if strings.Contains(string(exports), sentinel) {
		t.Fatal("request contents escaped into telemetry")
	}
	for _, token := range f.tokens {
		if strings.Contains(string(exports), token) {
			t.Fatal("bearer token escaped into telemetry")
		}
	}
}

func TestTelemetryBackendFailureDoesNotBreakAPI(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); fmt.Fprint(w, "SECRET_SENTINEL") }))
	defer backend.Close()
	f := setupTelemetry(t, config.Telemetry{Enabled: true, OTLPEndpoint: backend.URL, PrometheusURL: backend.URL, TraceSampleRatio: 1})
	start := time.Now()
	status, b, _ := f.request(t, "GET", "/api/v1/telemetry/summary", "owner", nil, nil)
	if status != 200 || time.Since(start) > 3*time.Second || strings.Contains(string(b), "SECRET_SENTINEL") {
		t.Fatalf("unbounded/unsafe summary failure: %d %s", status, b)
	}
	var summary domain.TelemetrySummary
	_ = json.Unmarshal(b, &summary)
	if summary.Backend.State != "error" {
		t.Fatalf("backend failure hidden: %s", b)
	}
	status, b, _ = f.request(t, "GET", "/api/v1/status", "owner", nil, nil)
	if status != 200 {
		t.Fatalf("healthy API blocked: %d %s", status, b)
	}
}
