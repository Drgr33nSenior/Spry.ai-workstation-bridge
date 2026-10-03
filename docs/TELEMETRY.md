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
states, backend state and eight named measurements. It returns HTTP 200 with
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
| `psu_output_power` | One available `workstation_psu_output_power_watts{job="node"}` series, in DC output watts |
| `psu_output_energy_estimated` | One available `workstation_psu_output_energy_joules_total{job="node"}` series, in estimated DC output joules |
| `psu_energy_covered` | One available `workstation_psu_energy_covered_seconds_total{job="node"}` series, in covered observation seconds |
| `gpu_telemetry` | Explicitly unavailable until target GPU metrics are qualified |

Use a Prometheus backend dedicated to this workstation. The fixed `node` and
`sglang` jobs must contain only its trusted targets. These queries aggregate
matching series; pointing them at a multi-workstation backend would combine
machines. The queue selector uses total counts, not the additional per-priority
breakdowns; it also matches when priority scheduling adds no label. SGLang
metrics must be explicitly enabled and scraped under that job.
No GPU metric names or Radeon exporter support are assumed.

Each refresh makes at most 14 fixed instant queries: seven values and their
source timestamps. TTFT freshness uses the histogram count source. PSU queries
do not sum targets: duplicate matching series are ambiguous and remain missing.
The fixed selectors require the collector's corresponding availability gauge
to equal one, matched on `job,instance`. PSU freshness uses the value of
`workstation_psu_sample_timestamp_seconds` or
`workstation_psu_energy_sample_timestamp_seconds`, not its scrape timestamp.
A repeatedly scraped, frozen textfile therefore becomes stale.
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

## PSU power and energy

The Resources page includes an owner-only **PSU power and energy** panel. Use
**Refresh power telemetry** to fetch the same bounded summary used by the CLI:

```sh
bridgectl --context /path/to/owner-context.json --json telemetry
```

The installer sampler reads the Linux `corsair-psu` hwmon driver through the
existing private telemetry stack. Both `full` and `metrics` profiles carry these
metrics. Bridge does not access USB, change PSU controls, load kernel modules or
configure the collector. The exact PSU revision, USB connection, kernel support
and unprivileged sensor access require the installer's
[PSU qualification procedure](https://github.com/Drgr33nSenior/ArchLinuxThreadripper/blob/main/docs/TELEMETRY.md).
Absent sensors and unconfigured telemetry remain unknown; Demo does not invent
PSU readings.

Power is PSU **DC output**, not AC wall-input power. The panel converts the
sampler's cumulative estimated joules to kWh and shows the covered observation
time beside it. These totals start with the sampler's retained accounting state.
They integrate observed intervals only, without extrapolating through missing
samples, reboots or identity changes. Covered time is not workstation uptime.
Keep the accounting state when upgrading; the installer runbook defines its
backup and recovery boundary.

Use the private Grafana dashboard for power history. Read coverage and
freshness with every total. Retained counters do
not reconstruct missing history, and a stale value is only the last observation.
This is not a wall-power meter or electricity-billing record. PSU conversion
losses, displays and other wall-powered equipment are excluded. Use a separately
qualified AC energy meter for wall consumption or cost measurements. No hardware
accuracy or workload performance claim follows from the UI or source fixtures.

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
