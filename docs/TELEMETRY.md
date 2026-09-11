# Private telemetry

## Optional metrics-only composition

The installer supports `TELEMETRY_PROFILE=metrics`; `full` stays default. Select
the matching reviewed host Alloy configuration from its runbook. Bridge service
policy remains separate: for metrics-only operation, retain the private metrics
endpoint and set existing `telemetry.trace_sample_ratio` to `0`. Trace exports
are disabled and their state is `not_configured`, not an exporter failure.
Selecting a source render does not update installed Bridge policy.

The installer calculates component limits plus explicit margin. Bridge memory
planning can bind sealed telemetry evidence and enforce its reserve inside
`other_mib`. Allowance is not measured RSS or permission to allocate apparent
savings to inference. See [PERFORMANCE.md](PERFORMANCE.md) and
[MEMORY-BUDGETS.md](MEMORY-BUDGETS.md).

Bridge can send management metrics and sampled traces to a private OpenTelemetry
Protocol (OTLP) collector, such as Grafana Alloy. Export is disabled by default.
The API does not add a metrics listener or public ingress. Telemetry is lossy
diagnostic data, not the durable audit, an approval mechanism or a GPU profiler.
The separate, optional [Agents API adviser](MEMORY-BUDGETS.md#optional-agents-api-advisory)
uses selected sanitized memory evidence. It cannot query arbitrary telemetry,
approve plans or execute workload changes.

## Owner configuration

Keep the `telemetry` object omitted, or use the disabled object in the deployment
examples, until the owner has provided a private collector route. To enable
export and the optional Prometheus summary, add this object to the existing
root-owned server policy and restart the controller through the normal owner
workflow:

```json
"telemetry": {
  "enabled": true,
  "otlp_endpoint": "http://127.0.0.1:4318",
  "trace_sample_ratio": 0.1,
  "prometheus_url": "http://127.0.0.1:19090"
}
```

These addresses require an owner-provided loopback route: for example, a host
Alloy receiver or explicit loopback port-forwards to the installer stack's
ClusterIP services. A ClusterIP name is not a host-service route. Bridge does
not establish tunnels, start collectors or deploy the stack. A port-forward
ends when its owning process exits; use an independently reviewed host route
for unattended operation.

`enabled` controls OTLP export only. An empty `prometheus_url` disables backend
queries independently. A zero sample ratio disables traces but retains metrics
when export is enabled. No endpoint, ratio or backend is inferred from the
environment. Before enabled startup, unset nonempty `OTEL_*` variables in the
service environment. Bridge rejects them with a fixed error before the SDK can
parse or log their contents. Disabled export does not construct SDK providers.

Both URLs require an explicit port and numeric loopback or private IP address.
HTTP is accepted only for loopback. A private LAN endpoint requires HTTPS with
a certificate valid for its IP address and trusted by the system. Credentials,
DNS names, paths, queries, fragments, public addresses, link-local addresses and
wildcard addresses are rejected. This increment has no endpoint headers,
custom CA files or client-certificate options. Redirects and proxy environment
variables are ignored or refused; TLS verification is never disabled.

## Export contract and bounds

The resource contains only `service.name=spry-workstation-bridge`; the
instrumentation scope is `bridge.management`. OTLP/HTTP protobuf requests go to
`/v1/metrics` and `/v1/traces`. No client trace context or baggage is extracted.
The API records registered route templates, never raw request paths. Host/origin
guard rejections and unmatched routes are outside this instrumentation.

| Instrument | Unit | Allowed datapoint attributes |
|---|---|---|
| `bridge.http.requests` | `{request}` | `http.request.method`, `http.route`, `http.response.status_code`, `outcome` |
| `bridge.http.request.duration` | `s` | Same HTTP attributes |
| `bridge.operation.executions` | `{execution}` | `bridge.operation.action`, `outcome` |
| `bridge.operation.duration` | `s` | Same operation attributes |

HTTP spans use the fixed name `bridge.http` and the HTTP attributes above.
Execution spans use `bridge.operation` and the operation attributes above.
Execution spans are independent of the initiating HTTP request; disconnects do
not cancel operations. Only fixed action names and terminal outcomes are
recorded. Queue counts are available in the summary; the execution instruments
count execution attempts, not every state transition.

Bodies, raw queries, prompts, keys, authorization headers, operation IDs,
idempotency keys, model names, user names, backend labels and raw errors are not
exported. Span events and links are disabled. Error status text is fixed.
Bridge does not add an OpenTelemetry log exporter or export the state store.

Metrics use cumulative counters and explicit histograms, a 30-second interval
and a 256-series cardinality limit per instrument. Traces use a nonblocking
256-span queue, batches of up to 64 and a five-second batch interval. Queue
overflow can drop spans. Sampling is bounded from zero to one. Each span allows
at most eight attributes of at most 128 characters each. Requests are capped at
1 MiB: oversized requests are rejected, not split. Response bodies are limited
to 64 KiB and headers to 16 KiB. Export has a
two-second timeout and no retries. Shutdown has a separate three-second bound.
These are software bounds, not measured workstation RAM or CPU budgets.

A missing or failed collector does not block API requests or operation dispatch.
The SDK can report a fixed `telemetry export failed` message; it does not receive
raw backend errors from Bridge's exporter wrapper. The summary exposes the last
export attempt state, not an assertion that the backend retained all data.

## Owner-only summary

`GET /api/v1/telemetry/summary` uses the existing authenticated API and requires
the `owner` role. Viewer and operator credentials receive 403. Request query
parameters are rejected. No new credential mechanism or unauthenticated route
is provided. The additive wire contract is generated in
[OpenAPI](../api/openapi.json) from `internal/contract` and Go domain types.

The response contains `observed_at`, adapter `mode`, local mutation-storage
availability, counts of queued/running/recovery-required operations, export
states, backend state and five named measurements. It returns HTTP 200 with
explicit unavailable/error states when Prometheus fails. It never returns raw
operation records, audit entries, configuration, backend URLs or backend labels.
Operation counts and storage health use a small snapshot under the store lock;
summary requests do not copy or sort retained operation events.

| Measurement | Fixed source and interpretation |
|---|---|
| `host_memory_available` | Sum of `node_memory_MemAvailable_bytes{job="node"}`, in bytes |
| `host_memory_pressure_waiting` | Five-minute rate of `node_pressure_memory_waiting_seconds_total{job="node"}`, as a ratio |
| `sglang_queued_requests` | Sum of `sglang:num_queue_reqs{job="sglang",priority=""}` |
| `sglang_time_to_first_token_p95` | 95th percentile from five-minute rates of `sglang:time_to_first_token_seconds_bucket{job="sglang"}`, in seconds |
| `gpu_telemetry` | Explicitly unavailable until target GPU metrics are qualified |

Use a Prometheus backend dedicated to this workstation. The fixed `node` and
`sglang` jobs must contain only its trusted targets. These queries aggregate
matching series; pointing them at a multi-workstation backend would combine
machines. The queue selector uses total counts, not the additional per-priority
breakdowns; it also matches when priority scheduling adds no label. SGLang
metrics must be explicitly enabled and scraped under that job.
No GPU metric names or Radeon exporter support are assumed.

Each refresh makes at most eight fixed instant queries: four aggregates and
their oldest source timestamps. TTFT freshness uses the histogram count source.
All queries share a two-second deadline and each requests a one-second backend
timeout. Responses must contain one finite aggregate; empty/NaN series are
`missing`, not zero. A missing timestamp leaves the value missing; timestamp
query failures produce an error. Responses are cached for 30 seconds, and source
age is rechecked on cached reads. Samples older than 90 seconds are `stale`.
Concurrent refreshes return `unavailable` with `refresh_in_progress` instead of
starting more backend requests.

State fields distinguish `not_configured`, `pending`, `available`, `unavailable`,
`missing`, `stale` and `error` where applicable. `reason` is a fixed diagnostic
code; `value` and `observed_at` can be null. An available backend means its fixed
queries completed, not that every measurement exists. Read each measurement's
state and source timestamp before using its value.

## Dependency and verification boundary

Bridge uses the official Apache-2.0-licensed
[OpenTelemetry Go v1.46.0 release](https://github.com/open-telemetry/opentelemetry-go/releases/tag/v1.46.0),
source commit `58db4c898f5b5594f8ba78f156475bf48486e2f2`. The SDK requires Go 1.25
or newer and this repository pins Go 1.27.1. `go.mod` and `go.sum` pin the SDK,
OTLP HTTP exporters and their protocol dependencies. The official SDK supplies
bounded batching and OTLP encoding without adding another runtime or CGO.

Local tests cover opt-in/disabled behavior, owner authorization, fixed queries,
freshness, cancellation, unavailable exporters and synthetic secret exclusion.
These tests do not qualify a running Alloy/Prometheus deployment, Radeon GPU
exporter, Arch kernel or SGLang workload. Retention, disk placement, resource
admission and private service exposure remain installer/owner responsibilities.
