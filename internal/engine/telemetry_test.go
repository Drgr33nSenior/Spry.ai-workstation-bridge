package engine_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/telemetry"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestTelemetryPreservesOperationAndCancellationOutcomes(t *testing.T) {
	for _, cancelOperation := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancellation"}[cancelOperation], func(t *testing.T) {
			var mu sync.Mutex
			var exported strings.Builder
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				if req.URL.Path == "/v1/traces" {
					var wire collectortrace.ExportTraceServiceRequest
					if err := proto.Unmarshal(body, &wire); err != nil {
						t.Error(err)
					}
					text, _ := protojson.Marshal(&wire)
					mu.Lock()
					exported.Write(text)
					mu.Unlock()
				}
				// The collector is deliberately unavailable; dispatch/cancellation
				// must retain their existing durable semantics.
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer ts.Close()
			recorder, err := telemetry.New(context.Background(), config.Telemetry{Enabled: true, OTLPEndpoint: ts.URL, TraceSampleRatio: 1})
			if err != nil {
				t.Fatal(err)
			}
			a := &controlled{result: domain.Result{State: "succeeded", Phase: "SECRET_SENTINEL"}}
			if cancelOperation {
				a.observed, a.release = make(chan struct{}), make(chan struct{})
			}
			e, owner, c := makeEngine(t, a)
			e.Telemetry = recorder
			op := apply(t, e, owner, plan(t, e, owner, c, "ai"), "SECRET_SENTINEL")
			start(t, e)
			want := "succeeded"
			if cancelOperation {
				select {
				case <-a.observed:
				case <-time.After(time.Second):
					t.Fatal("executor did not start")
				}
				current, _ := e.Operation(op.ID)
				if _, err := e.Cancel(owner, op.ID, current.Revision); err != nil {
					t.Fatal(err)
				}
				want = "recovery-required"
			}
			if got := wait(t, e, op.ID); got.State != want {
				t.Fatalf("telemetry changed operation state: %s", got.State)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := e.Close(ctx); err != nil {
				t.Fatal(err)
			}
			_ = recorder.Shutdown(ctx)
			mu.Lock()
			defer mu.Unlock()
			out := exported.String()
			if !strings.Contains(out, "bridge.operation") || !strings.Contains(out, want) || strings.Contains(out, "SECRET_SENTINEL") || strings.Contains(out, op.ID) {
				t.Fatalf("incorrect or unsafe operation span: %s", out)
			}
		})
	}
}
