package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// Expressions are compiled policy: neither URLs, selectors nor queries are
// accepted from API requests. Source timestamps distinguish old scrape data
// from a newly evaluated aggregate. No GPU name is assumed before qualification.
var summaryQueries = [...]struct{ name, unit, expression, freshness string }{
	{"host_memory_available", "By", `sum(node_memory_MemAvailable_bytes{job="node"})`, `min(timestamp(node_memory_MemAvailable_bytes{job="node"}))`},
	{"host_memory_pressure_waiting", "ratio", `sum(rate(node_pressure_memory_waiting_seconds_total{job="node"}[5m]))`, `min(timestamp(node_pressure_memory_waiting_seconds_total{job="node"}))`},
	// The pinned SGLang collector emits both totals and per-priority breakdowns.
	// An empty matcher selects totals, including when the label is absent.
	{"sglang_queued_requests", "{request}", `sum(sglang:num_queue_reqs{job="sglang",priority=""})`, `min(timestamp(sglang:num_queue_reqs{job="sglang",priority=""}))`},
	{"sglang_time_to_first_token_p95", "s", `histogram_quantile(0.95,sum by(le)(rate(sglang:time_to_first_token_seconds_bucket{job="sglang"}[5m])))`, `min(timestamp(sglang:time_to_first_token_seconds_count{job="sglang"}))`},
}

type Prometheus struct {
	endpoint string
	client   *http.Client
	mu       sync.Mutex
	until    time.Time
	state    domain.TelemetryState
	values   []domain.TelemetryValue
}

func NewPrometheus(endpoint string) *Prometheus {
	return &Prometheus{endpoint: endpoint, client: HTTPClient()}
}

func missingValues(reason string) []domain.TelemetryValue {
	values := make([]domain.TelemetryValue, 0, len(summaryQueries)+1)
	for _, q := range summaryQueries {
		values = append(values, domain.TelemetryValue{Name: q.name, Unit: q.unit, State: "missing", Reason: reason})
	}
	return append(values, domain.TelemetryValue{Name: "gpu_telemetry", Unit: "", State: "unavailable", Reason: "requires_qualified_gpu_metrics"})
}

func (p *Prometheus) Summary(ctx context.Context) (domain.TelemetryState, []domain.TelemetryValue) {
	if p == nil || p.endpoint == "" {
		return domain.TelemetryState{State: "not_configured", Reason: "prometheus_url_not_configured"}, missingValues("backend_not_configured")
	}
	if config.ValidateTelemetryEndpoint(p.endpoint) != nil {
		return domain.TelemetryState{State: "error", Reason: "invalid_backend_policy"}, missingValues("invalid_backend_policy")
	}
	// Concurrent API calls cannot create an unbounded fan-out to Prometheus.
	if !p.mu.TryLock() {
		return domain.TelemetryState{State: "unavailable", Reason: "refresh_in_progress"}, missingValues("refresh_in_progress")
	}
	defer p.mu.Unlock()
	if time.Now().Before(p.until) {
		return p.state, currentFreshness(p.values, time.Now())
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	now := time.Now().UTC()
	state := domain.TelemetryState{State: "available", ObservedAt: &now, Reason: "fixed_queries_completed"}
	values := missingValues("no_series")
	for i, q := range summaryQueries {
		value, err := p.query(ctx, q.expression)
		if err != nil {
			if errors.Is(err, errNoSeries) {
				continue
			}
			values[i].State = "error"
			values[i].Reason = "query_failed"
			state.State = "error"
			state.Reason = "one_or_more_queries_failed"
			continue
		}
		stamp, err := p.query(ctx, q.freshness)
		if err != nil && !errors.Is(err, errNoSeries) {
			values[i].State = "error"
			values[i].Reason = "source_timestamp_query_failed"
			state.State = "error"
			state.Reason = "one_or_more_queries_failed"
			continue
		}
		if err != nil || stamp < 0 || stamp > float64(now.Unix()+10) {
			values[i].Reason = "source_timestamp_unavailable"
			continue
		}
		observed := time.Unix(int64(stamp), 0).UTC()
		values[i].Value = &value
		values[i].ObservedAt = &observed
		values[i].State = "available"
		values[i].Reason = "recent_source_sample"
		if now.Sub(observed) > 90*time.Second {
			values[i].State = "stale"
			values[i].Reason = "source_sample_older_than_90_seconds"
		}
	}
	p.state, p.values, p.until = state, values, time.Now().Add(30*time.Second)
	return state, append([]domain.TelemetryValue(nil), values...)
}

func currentFreshness(values []domain.TelemetryValue, now time.Time) []domain.TelemetryValue {
	out := append([]domain.TelemetryValue(nil), values...)
	for i, v := range out {
		if v.State == "available" && v.ObservedAt != nil && now.Sub(*v.ObservedAt) > 90*time.Second {
			out[i].State = "stale"
			out[i].Reason = "source_sample_older_than_90_seconds"
		}
	}
	return out
}

var errNoSeries = errors.New("no finite aggregate series")

func (p *Prometheus) query(ctx context.Context, expression string) (float64, error) {
	u := p.endpoint + "/api/v1/query?" + url.Values{"query": {expression}, "timeout": {"1s"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, errors.New("invalid backend request")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, errors.New("backend request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("backend status failed")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(b) > 64<<10 {
		return 0, errors.New("backend response exceeds bound")
	}
	var wire struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &wire) != nil || wire.Status != "success" || wire.Data.ResultType != "vector" {
		return 0, errors.New("invalid backend response")
	}
	if len(wire.Data.Result) != 1 || len(wire.Data.Result[0].Value) != 2 {
		return 0, errNoSeries
	}
	var text string
	if json.Unmarshal(wire.Data.Result[0].Value[1], &text) != nil {
		return 0, errors.New("invalid backend value")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errNoSeries
	}
	return value, nil
}
