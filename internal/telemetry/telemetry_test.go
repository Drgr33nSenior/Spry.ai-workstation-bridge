package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestDisabledIgnoresTelemetryEnvironment(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer ts.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", ts.URL)
	r, err := New(context.Background(), config.Telemetry{})
	if err != nil {
		t.Fatal(err)
	}
	_, done := r.HTTP(context.Background(), "GET", "/api/v1/status")
	done(200)
	_, doneOp := r.Operation(context.Background(), "serving.start")
	doneOp("succeeded")
	if err = r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	metrics, traces := r.Status()
	if metrics.State != "not_configured" || traces.State != "not_configured" || calls.Load() != 0 || r.mp != nil || r.tp != nil {
		t.Fatal("disabled telemetry exported or initialized SDK")
	}
}

func TestMetricsOnlyProfileKeepsMetricsWithoutTraceRequests(t *testing.T) {
	var metrics, traces atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/metrics":
			metrics.Add(1)
		case "/v1/traces":
			traces.Add(1)
		default:
			t.Error("unexpected signal path")
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer s.Close()
	r, err := New(context.Background(), config.Telemetry{Enabled: true, OTLPEndpoint: s.URL, TraceSampleRatio: 0})
	if err != nil {
		t.Fatal(err)
	}
	_, finish := r.Operation(context.Background(), "performance.export")
	finish("succeeded")
	if err = r.mp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.tp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, state := r.Status()
	if state.State != "not_configured" || state.Reason != "sampling_disabled" || metrics.Load() == 0 || traces.Load() != 0 {
		t.Fatalf("wrong optional signal status %+v metrics=%d traces=%d", state, metrics.Load(), traces.Load())
	}
	if err = r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if traces.Load() != 0 {
		t.Fatal("shutdown exported disabled traces")
	}
}

func TestOTLPExportsOnlyAllowedAttributesAndBoundedCardinality(t *testing.T) {
	const secret = "SECRET_SENTINEL"
	var mu sync.Mutex
	traces := &collectortrace.ExportTraceServiceRequest{}
	metrics := &collectormetric.ExportMetricsServiceRequest{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		if strings.Contains(fmt.Sprint(req.Header), secret) {
			t.Error("environment headers escaped")
		}
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path == "/v1/traces" {
			if err := proto.Unmarshal(b, traces); err != nil {
				t.Error(err)
			}
		} else if req.URL.Path == "/v1/metrics" {
			if err := proto.Unmarshal(b, metrics); err != nil {
				t.Error(err)
			}
		} else {
			t.Error("unexpected export URL")
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer ts.Close()
	r, err := New(context.Background(), config.Telemetry{Enabled: true, OTLPEndpoint: ts.URL, TraceSampleRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		_, finish := r.HTTP(context.Background(), "GET", "/api/v1/operations/{id}")
		finish(200)
	}
	_, finish := r.HTTP(context.Background(), secret, "/api/v1/status")
	finish(400)
	_, op := r.Operation(context.Background(), secret)
	op(secret)
	for _, action := range []string{"performance.export", "performance.profile.select"} {
		_, finish := r.Operation(context.Background(), action)
		finish("succeeded")
	}
	if err = r.tp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.mp.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for _, data := range []proto.Message{traces, metrics} {
		b, err := protojson.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), secret) || !strings.Contains(string(b), ServiceName) {
			t.Errorf("unexpected exported resource/attributes: %s", b)
		}
		for _, action := range []string{"performance.export", "performance.profile.select"} {
			if !strings.Contains(string(b), action) {
				t.Errorf("missing fixed performance action %s", action)
			}
		}
	}
	points := 0
	for _, rm := range metrics.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == "bridge.http.requests" {
					points = len(m.GetSum().DataPoints)
				}
			}
		}
	}
	mu.Unlock()
	if points != 2 {
		t.Fatalf("HTTP label cardinality = %d; want 2", points)
	}
	if err = r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRejectTelemetryEnvironmentBeforeSDKParsing(t *testing.T) {
	if os.Getenv("BRIDGE_TELEMETRY_ENV_PROBE") == "1" {
		_, err := New(context.Background(), config.Telemetry{Enabled: true, OTLPEndpoint: "http://127.0.0.1:4318"})
		if err == nil {
			t.Fatal("environment override accepted")
		}
		fmt.Fprintln(os.Stderr, err)
		return
	}
	for _, entry := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://SECRET_SENTINEL:%ZZ@127.0.0.1:4318",
		"OTEL_EXPORTER_OTLP_HEADERS=SECRET_SENTINEL",
		"OTEL_RESOURCE_ATTRIBUTES=SECRET_SENTINEL",
		"OTEL_EXPORTER_OTLP_CLIENT_KEY=/SECRET_SENTINEL-key",
		"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE=/SECRET_SENTINEL-cert",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS=Authorization=SECRET_SENTINEL",
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRejectTelemetryEnvironmentBeforeSDKParsing$")
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "OTEL_") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "BRIDGE_TELEMETRY_ENV_PROBE=1", entry)
		out, err := cmd.CombinedOutput()
		if err != nil || strings.Contains(string(out), "SECRET_SENTINEL") || !strings.Contains(string(out), "environment overrides to be unset") {
			t.Fatalf("unsafe SDK environment handling: %v %s", err, out)
		}
	}
}

func TestExportFailuresAreSanitizedAndDoNotBlockRecording(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); fmt.Fprint(w, "SECRET_SENTINEL") }))
	defer ts.Close()
	r, err := New(context.Background(), config.Telemetry{Enabled: true, OTLPEndpoint: ts.URL, TraceSampleRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for range 1000 {
		_, finish := r.HTTP(context.Background(), "GET", "/api/v1/status")
		finish(200)
	}
	if time.Since(start) > time.Second {
		t.Fatal("recording blocked on failed exporter")
	}
	err = r.tp.ForceFlush(context.Background())
	// The async batcher may already have drained every span before ForceFlush.
	// Its recorded export state below must still retain the failure.
	if err != nil && strings.Contains(err.Error(), "SECRET_SENTINEL") {
		t.Fatalf("unsafe trace error: %v", err)
	}
	err = r.mp.ForceFlush(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET_SENTINEL") {
		t.Fatalf("unsafe/missing metric error: %v", err)
	}
	m, tr := r.Status()
	b, _ := json.Marshal([]any{m, tr})
	if m.State != "error" || tr.State != "error" || strings.Contains(string(b), "SECRET_SENTINEL") {
		t.Fatalf("export state: %s", b)
	}
	_ = r.Shutdown(context.Background())
}
