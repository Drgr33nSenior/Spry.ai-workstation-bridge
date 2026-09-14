package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFixedSummaryQueriesFreshnessAndCache(t *testing.T) {
	var calls atomic.Int32
	allowed := map[string]bool{}
	for _, q := range summaryQueries {
		allowed[q.expression] = true
		allowed[q.freshness] = true
	}
	now := time.Now().Unix()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query().Get("query")
		if !allowed[q] || r.Method != "GET" || r.URL.Path != "/api/v1/query" || len(r.URL.Query()) != 2 {
			t.Error("unexpected backend query")
		}
		// Keep these expectations independent of summaryQueries: SGLang can
		// expose total priority="" and per-priority counts simultaneously.
		// Both value and freshness must select only the total series.
		if strings.Contains(q, "sglang:num_queue_reqs") &&
			q != `sum(sglang:num_queue_reqs{job="sglang",priority=""})` &&
			q != `min(timestamp(sglang:num_queue_reqs{job="sglang",priority=""}))` {
			t.Error("queue query includes per-priority breakdowns")
		}
		value := "42"
		if strings.Contains(q, "timestamp(") || strings.Contains(q, "_sample_timestamp_seconds") {
			value = fmt.Sprint(now)
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"private":"SECRET_SENTINEL"},"value":[%d,%q]}]}}`, now, value)
	}))
	defer ts.Close()
	p := NewPrometheus(ts.URL)
	for range 2 {
		state, values := p.Summary(context.Background())
		if state.State != "available" || len(values) != 8 {
			t.Fatalf("summary: %#v %#v", state, values)
		}
		for _, v := range values[:7] {
			if v.State != "available" || v.Value == nil || *v.Value != 42 || v.ObservedAt.Unix() != now {
				t.Fatalf("value: %#v", v)
			}
		}
		b, _ := json.Marshal(values)
		if strings.Contains(string(b), "SECRET_SENTINEL") {
			t.Fatal("backend attributes leaked")
		}
		if values[7].State != "unavailable" || values[7].Value != nil {
			t.Fatal("unqualified GPU data advertised")
		}
	}
	if calls.Load() != 14 {
		t.Fatalf("cache/fixed query bound: %d requests", calls.Load())
	}
}

func TestPSUSummaryUsesSensorTimeAndPreservesUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, result, want string
		age                time.Duration
	}{
		{"available", `[{"value":[1,"240"]}]`, "available", 0},
		{"measured-zero", `[{"value":[1,"0"]}]`, "available", 0},
		{"frozen-textfile", `[{"value":[1,"240"]}]`, "stale", 2 * time.Minute},
		{"unavailable", `[]`, "missing", 0},
		{"ambiguous-node", `[{"value":[1,"240"]},{"value":[1,"240"]}]`, "missing", 0},
		{"nonfinite", `[{"value":[1,"+Inf"]}]`, "missing", 0},
		{"future-clock", `[{"value":[1,"240"]}]`, "missing", -time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Unix()
			observed := time.Now().Add(-tc.age).Unix()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query().Get("query")
				result := `[]`
				if strings.Contains(q, "workstation_psu_") {
					availability := `workstation_psu_available{job="node"} == 1`
					if strings.Contains(q, "energy_") {
						availability = `workstation_psu_energy_available{job="node"} == 1`
					}
					if strings.Contains(q, "sum(") || strings.Contains(q, "timestamp(") ||
						!strings.Contains(q, `and on(job,instance) (`+availability+`)`) {
						t.Errorf("PSU query lacks single-node availability/sensor-age semantics: %s", q)
					}
					result = tc.result
					if strings.Contains(q, "_sample_timestamp_seconds") {
						// Prometheus's vector timestamp is current even when the
						// textfile's actual sensor timestamp remains old.
						result = fmt.Sprintf(`[{"value":[%d,"%d"]}]`, now, observed)
					}
				}
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":%s}}`, result)
			}))
			defer server.Close()
			_, values := NewPrometheus(server.URL).Summary(context.Background())
			for _, v := range values[4:7] {
				if v.State != tc.want {
					t.Fatalf("%s: %#v", v.Name, v)
				}
				if tc.want == "missing" && (v.Value != nil || v.ObservedAt != nil) {
					t.Fatal("unknown PSU measurement was converted to a number")
				}
				if tc.want != "missing" && (v.Value == nil || v.ObservedAt.Unix() != observed) {
					t.Fatal("PSU source timestamp lost")
				}
			}
		})
	}
}

func TestSummaryUnavailableMissingStaleAndMalformed(t *testing.T) {
	valid := `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"42"]}]}}`
	for _, tc := range []struct {
		name, value string
		status      int
		want        string
	}{
		{"empty", `{"status":"success","data":{"resultType":"vector","result":[]}}`, 200, "missing"},
		{"nan", `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"NaN"]}]}}`, 200, "missing"},
		{"error", "SECRET_SENTINEL", 503, "error"},
		{"malformed", "SECRET_SENTINEL", 200, "error"},
		{"oversize", strings.Repeat("x", (64<<10)+1), 200, "error"},
		{"oversize-valid-prefix", valid + strings.Repeat(" ", (64<<10)-len(valid)) + "SECRET_SENTINEL", 200, "error"},
		{"stale", `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"1"]}]}}`, 200, "stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.value) }))
			defer ts.Close()
			state, values := NewPrometheus(ts.URL).Summary(context.Background())
			if values[0].State != tc.want {
				t.Fatalf("state = %s", values[0].State)
			}
			b, _ := json.Marshal(struct {
				State  any
				Values any
			}{state, values})
			if strings.Contains(string(b), "SECRET_SENTINEL") {
				t.Fatal("raw backend failure leaked")
			}
		})
	}
	state, values := NewPrometheus("").Summary(context.Background())
	if state.State != "not_configured" || values[0].Value != nil {
		t.Fatal("missing backend was not explicit")
	}
}

func TestBackendRedirectCancellationAndFanout(t *testing.T) {
	var followed atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	state, _ := NewPrometheus(redirect.URL).Summary(context.Background())
	if state.State != "error" || followed.Load() != 0 {
		t.Fatal("backend redirect followed")
	}
	p := NewPrometheus(redirect.URL)
	p.mu.Lock()
	state, _ = p.Summary(context.Background())
	p.mu.Unlock()
	if state.Reason != "refresh_in_progress" {
		t.Fatal("parallel queries admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	state, _ = NewPrometheus(redirect.URL).Summary(ctx)
	if state.State != "error" || time.Since(start) > time.Second {
		t.Fatal("cancelled summary blocked")
	}
}

func TestCachedSourceAgeAndFreshnessQueryFailure(t *testing.T) {
	old := time.Now().Add(-91 * time.Second)
	p := NewPrometheus("http://127.0.0.1:19090")
	p.until = time.Now().Add(time.Minute)
	p.values = missingValues("no_series")
	p.values[0].State = "available"
	p.values[0].ObservedAt = &old
	_, values := p.Summary(context.Background())
	if values[0].State != "stale" {
		t.Fatal("cache presented old source as fresh")
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("query"), "timestamp(") {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"42"]}]}}`)
	}))
	defer ts.Close()
	state, values := NewPrometheus(ts.URL).Summary(context.Background())
	if state.State != "error" || values[0].State != "error" || values[0].Reason != "source_timestamp_query_failed" {
		t.Fatalf("freshness failure hidden: %#v %#v", state, values[0])
	}
}
