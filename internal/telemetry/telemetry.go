// Package telemetry exports only explicit, bounded management measurements.
// It does not replace the durable audit or instrument bodies, baggage, client
// trace context, headers or credentials. Export resources are explicitly filtered.
package telemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const ServiceName = "spry-workstation-bridge"

type Recorder struct {
	tp                *sdktrace.TracerProvider
	mp                *sdkmetric.MeterProvider
	tracer            trace.Tracer
	requests          metric.Int64Counter
	requestDuration   metric.Float64Histogram
	operations        metric.Int64Counter
	operationDuration metric.Float64Histogram
	mu                sync.Mutex
	metricsState      domain.TelemetryState
	tracesState       domain.TelemetryState
}

// HTTPClient deliberately ignores proxy environment and never follows a redirect.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("telemetry redirects forbidden") }, Transport: boundedTransport{&http.Transport{
		DialContext:         (&net.Dialer{Timeout: time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second,
		MaxResponseHeaderBytes: 16 << 10, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	}}}
}

type boundedTransport struct{ http.RoundTripper }

func (t boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(req)
	if err == nil {
		resp.Body = http.MaxBytesReader(nil, resp.Body, 64<<10)
	}
	return resp, err
}

func New(ctx context.Context, c config.Telemetry) (*Recorder, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	r := &Recorder{metricsState: domain.TelemetryState{State: "not_configured", Reason: "export_disabled"}, tracesState: domain.TelemetryState{State: "not_configured", Reason: "export_disabled"}}
	if !c.Enabled {
		return r, nil
	}
	// The SDK parses environment options before explicit overrides. Malformed
	// values can be logged and certificate paths opened during that parse. Refuse
	// competing configuration before constructing any SDK/exporter component.
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") && value != "" {
			return nil, errors.New("enabled telemetry requires OTEL_* environment overrides to be unset; use service policy")
		}
	}
	r.metricsState = domain.TelemetryState{State: "pending", Reason: "no_export_attempt"}
	r.tracesState = domain.TelemetryState{State: "pending", Reason: "no_export_attempt"}
	// Pin transport, encoding and retry policy independently of SDK defaults.
	me, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(c.OTLPEndpoint+"/v1/metrics"), otlpmetrichttp.WithHTTPClient(HTTPClient()), otlpmetrichttp.WithHeaders(map[string]string{}), otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression), otlpmetrichttp.WithTimeout(2*time.Second), otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}), otlpmetrichttp.WithMaxRequestSize(1<<20), otlpmetrichttp.WithTemporalitySelector(sdkmetric.DefaultTemporalitySelector))
	if err != nil {
		return nil, errors.New("telemetry metrics initialization failed")
	}
	te, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(c.OTLPEndpoint+"/v1/traces"), otlptracehttp.WithHTTPClient(HTTPClient()), otlptracehttp.WithHeaders(map[string]string{}), otlptracehttp.WithCompression(otlptracehttp.NoCompression), otlptracehttp.WithTimeout(2*time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}), otlptracehttp.WithMaxRequestSize(1<<20), otlptracehttp.WithEncoding(otlptracehttp.EncodingProtobuf))
	if err != nil {
		_ = me.Shutdown(ctx)
		return nil, errors.New("telemetry traces initialization failed")
	}
	res := resource.NewSchemaless(attribute.String("service.name", ServiceName))
	r.mp = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithCardinalityLimit(256), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(&metricExporter{Exporter: me, recorder: r}, sdkmetric.WithInterval(30*time.Second), sdkmetric.WithTimeout(2*time.Second))))
	r.tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.TraceIDRatioBased(c.TraceSampleRatio)), sdktrace.WithoutPanicRecording(), sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{AttributeCountLimit: 8, AttributeValueLengthLimit: 128}), sdktrace.WithBatcher(&spanExporter{SpanExporter: te, recorder: r}, sdktrace.WithMaxQueueSize(256), sdktrace.WithMaxExportBatchSize(64), sdktrace.WithBatchTimeout(5*time.Second), sdktrace.WithExportTimeout(2*time.Second)))
	r.tracer = r.tp.Tracer("bridge.management")
	m := r.mp.Meter("bridge.management")
	r.requests, _ = m.Int64Counter("bridge.http.requests", metric.WithUnit("{request}"))
	r.requestDuration, _ = m.Float64Histogram("bridge.http.request.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 15, 30))
	r.operations, _ = m.Int64Counter("bridge.operation.executions", metric.WithUnit("{execution}"))
	r.operationDuration, _ = m.Float64Histogram("bridge.operation.duration", metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(.1, 1, 5, 30, 60, 300, 1800, 3600, 86400))
	if c.TraceSampleRatio == 0 {
		r.tracesState = domain.TelemetryState{State: "not_configured", Reason: "sampling_disabled"}
	}
	return r, nil
}

func (r *Recorder) HTTP(ctx context.Context, method, route string) (context.Context, func(int)) {
	if r == nil || r.tracer == nil {
		return ctx, func(int) {}
	}
	if method != "GET" && method != "POST" && method != "HEAD" {
		method = "OTHER"
	}
	attrs := []attribute.KeyValue{attribute.String("http.request.method", method), attribute.String("http.route", route)}
	ctx, span := r.tracer.Start(ctx, "bridge.http", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))
	start := time.Now()
	return ctx, func(status int) {
		outcome := "success"
		if status >= 400 {
			outcome = "failure"
		}
		attrs = append(attrs, attribute.Int("http.response.status_code", status), attribute.String("outcome", outcome))
		span.SetAttributes(attrs...)
		if status >= 500 {
			span.SetStatus(codes.Error, "request_failed")
		}
		span.End()
		r.requests.Add(ctx, 1, metric.WithAttributes(attrs...))
		r.requestDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}

func (r *Recorder) Operation(ctx context.Context, action string) (context.Context, func(string)) {
	if r == nil || r.tracer == nil {
		return ctx, func(string) {}
	}
	switch action {
	case "serving.configure", "resources.configure", "caches.configure", "serving.start", "serving.stop", "serving.restart", "model.stage", "model.verify", "profile.switch", "profile.restore", "hardware.refresh", "build.start", "cpu-policy.export", "operation.reconcile", "memory.evidence.import", "memory.plan.export":
	default:
		action = "other"
	}
	attrs := []attribute.KeyValue{attribute.String("bridge.operation.action", action)}
	ctx, span := r.tracer.Start(ctx, "bridge.operation", trace.WithAttributes(attrs...))
	start := time.Now()
	return ctx, func(state string) {
		if !domain.Terminal(state) {
			state = "unknown"
		}
		attrs = append(attrs, attribute.String("outcome", state))
		span.SetAttributes(attrs...)
		if state != "succeeded" {
			span.SetStatus(codes.Error, "operation_not_succeeded")
		}
		span.End()
		r.operations.Add(ctx, 1, metric.WithAttributes(attrs...))
		r.operationDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}
}

func (r *Recorder) Status() (domain.TelemetryState, domain.TelemetryState) {
	if r == nil {
		s := domain.TelemetryState{State: "not_configured", Reason: "export_disabled"}
		return s, s
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metricsState, r.tracesState
}

func (r *Recorder) exported(metrics bool, err error) error {
	now := time.Now().UTC()
	s := domain.TelemetryState{State: "available", ObservedAt: &now, Reason: "last_export_succeeded"}
	if err != nil {
		s.State = "error"
		s.Reason = "export_failed"
	}
	r.mu.Lock()
	if metrics {
		r.metricsState = s
	} else if r.tracesState.Reason != "sampling_disabled" {
		r.tracesState = s
	}
	r.mu.Unlock()
	if err != nil {
		return errors.New("telemetry export failed")
	}
	return nil
}

func (r *Recorder) Shutdown(ctx context.Context) error {
	if r == nil || r.tp == nil {
		return nil
	}
	return errors.Join(r.tp.Shutdown(ctx), r.mp.Shutdown(ctx))
}

type metricExporter struct {
	sdkmetric.Exporter
	recorder *Recorder
}

func (e *metricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	// SDK WithResource merges OTEL_RESOURCE_ATTRIBUTES even with an explicit
	// resource. Replace that merge at the export boundary, without changing the
	// process environment or another library's providers.
	data.Resource = resource.NewSchemaless(attribute.String("service.name", ServiceName))
	return e.recorder.exported(true, e.Exporter.Export(ctx, data))
}
func (e *metricExporter) Shutdown(ctx context.Context) error {
	if e.Exporter.Shutdown(ctx) != nil {
		return errors.New("telemetry shutdown failed")
	}
	return nil
}

type spanExporter struct {
	sdktrace.SpanExporter
	recorder *Recorder
}

func (e *spanExporter) ExportSpans(ctx context.Context, data []sdktrace.ReadOnlySpan) error {
	filtered := make([]sdktrace.ReadOnlySpan, len(data))
	for i, span := range data {
		filtered[i] = privateSpan{ReadOnlySpan: span}
	}
	return e.recorder.exported(false, e.SpanExporter.ExportSpans(ctx, filtered))
}

type privateSpan struct{ sdktrace.ReadOnlySpan }

func (privateSpan) Resource() *resource.Resource {
	return resource.NewSchemaless(attribute.String("service.name", ServiceName))
}
func (e *spanExporter) Shutdown(ctx context.Context) error {
	if e.SpanExporter.Shutdown(ctx) != nil {
		return errors.New("telemetry shutdown failed")
	}
	return nil
}
