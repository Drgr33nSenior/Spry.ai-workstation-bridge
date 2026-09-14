# Optional Agents API adviser

The optional adviser explains selected, sanitized memory evidence and can request
a deterministic memory-plan export. It cannot approve or apply a plan, change
policy, qualify a workload, or execute commands. Deterministic evidence import
and planning remain available without an OpenAI account or provider access.

This integration uses the [Agents API](https://developers.openai.com/api/docs/guides/agents-api/overview),
not the Agents SDK or Responses API. Its documented
[function flow](https://developers.openai.com/api/docs/guides/agents-api/tools/functions)
uses application-handled `required_actions` and submitted tool-result events.
The narrow Bridge adapter uses typed standard-library REST instead of adding an
SDK dependency.

## Enable only with reviewed local policy

Only a live administrator can enable `advisor` in `/etc/bridge/server.json`.
The policy requires an explicit model, a canonical absolute `api_key_file`,
`timeout_seconds` 5..20, `max_tool_calls` 3..8, `max_sessions_per_hour` 1..60,
and `project_budget_acknowledged: true`. It is disabled by default and forbidden
in demo mode. A policy change requires the normal reviewed controller restart.

`api_key_file` is read when `bridged` starts. It must be a private, regular,
non-symlink file of at most 4096 bytes, owned by the account running `bridged`
(`bridge` in the packaged unit), and readable only by that account. Provision it
through the owner's secret process in a protected directory reachable by that
service, for example beneath `/var/lib/bridge/credentials`. Do not put a key in
the JSON policy, a command argument, a context file, a log, or this repository.

The packaged `bridged.service` does not declare `LoadCredential=`. A
`/run/credentials/bridged.service/...` path is valid only when a separately
reviewed owner provisioning method and systemd drop-in create a file that meets
the same owner, mode and path requirements. Do not assume a root-owned file or
an unconfigured systemd credential directory is readable by the service.

`project_budget_acknowledged` records an owner's acknowledgement only. It does
not create, query, or verify a provider project spending limit. Configure any
provider spending controls independently before enabling the adviser.

## Request advice

Create a private request containing only the successful import operation ID,
its sealed manifest digest, and the independently reviewed other-workload
allowance:

```json
{
  "evidence_id": "MEMORY_IMPORT_OPERATION_ID",
  "evidence_sha256": "SHA256_OF_REVIEWED_MANIFEST_JSON",
  "other_mib": 8192
}
```

`other_mib` is an example, not a discovered budget. Then run the owner-only
client command:

```sh
bridgectl --context PRIVATE_CONTEXT memory-advice --file memory-request.json
```

An unavailable provider returns an explicit error. The same evidence remains
usable through `memory-preview` and the ordinary plan/export commands.

Bridge includes fixed initial input when it creates an `environment:none`
session, as required by the [session contract](https://developers.openai.com/api/docs/guides/agents-api/sessions).
The create response can already describe active work or a completed turn. Bridge
inspects the returned session and turn state; an idle state alone is not proof of
success.

Only `read_memory_evidence`, `explain_memory_capacity`, and
`request_memory_plan` exist. Each has empty arguments and can run at most once.
The last tool creates a plan under the requesting owner's actor identity; it
cannot apply or approve it. Each tool call re-enters deterministic Bridge
validation. Provider text is untrusted explanation text. Review the local plan,
not a plan ID or approval claimed in provider output.

## Boundaries, accounting and audit

No public callback, MCP server, PromQL, shell, Kubernetes credential, or Bridge
management credential is exposed. Outbound HTTPS uses only `api.openai.com`,
normal certificate validation, and no redirect or environment proxy. Bridge
bounds request and response bytes, history, tool calls, concurrent work, and
duration. Hourly admission is process-local and resets after restart.

Cloud memory fields use explicit units: requested/limited RAM, shm ceiling,
other-workload allowance, and candidate use `_mib`; envelope, headroom, and
measurement windows use bytes. Unknown token usage is JSON `null`, never zero.
Reported session usage takes precedence over turn fallback; Bridge does not add
the two. Both are provisional observations, not a final bill, as the
[usage guide](https://developers.openai.com/api/docs/guides/agents-api/observability)
explains. Missing accounting cannot establish a token or spending limit.

Before contacting the provider, Bridge saves a `memory.advice.requested` audit
entry. A matching `memory.advice.finished` entry keeps only the fixed outcome,
known session ID, nullable usage/source, and creation/cancellation metadata. It
does not retain explanation text, raw provider errors, evidence, or credentials.
The 2000-entry audit bound applies. `X-Bridge-Advisory-ID` and the fixed failure
message identify the attempt. A failed final write preserves the requested entry
and the normal storage refusal.

Do not automatically retry an interrupted advisory. A lost create response can
mean inference started even when Bridge has no session ID. Bridge does not retry
creation or resubmit after a restart. Inspect the owner-only audit:

```sh
bridgectl --context PRIVATE_CONTEXT audit
```

Match requested and finished entries by `object`. An unmatched request has an
unknown outcome; it is neither free work nor confirmed cancellation. If the
session ID is known, inspect that session in the provider's Agents logs.
Otherwise correlate the request time and project in those logs. Only the owner
can decide whether another paid request is appropriate.

The session-create contract has no per-session hard token or dollar ceiling.
Local bounds, reported usage, and best-effort cancellation do not create one.
Cancellation acceptance is not proof that remote computation stopped. Sessions
have provider retention; review the provider's data controls. No key, paid
request, account capability, or spending control is tested by source checks.

## Live acceptance

This procedure requires explicit owner approval for paid calls, a provisioned
project with reviewed spending controls, permitted model access, private HTTPS
management with an owner credential, and validated non-sensitive evidence.
Source tests use synthetic provider responses; they do not authorize this test.

1. Keep independent workstation recovery access available. Record the selected
   source identities and configured provider model without copying credentials.
2. Make one owner-requested call and inspect its audit record:

   ```sh
   bridgectl --context PRIVATE_CONTEXT --deadline 30s memory-advice --file memory-request.json
   bridgectl --context PRIVATE_CONTEXT audit
   ```

   Expect an explanation with nullable, provisional accounting and an unapproved
   export plan, or a fixed failure with retained attempt metadata. Confirm that
   no operation changed a resource. Inspect the same session in provider logs;
   local accounting is not an invoice.
3. If a second paid call is authorized, interrupt it only after provider logs
   show that its session started. Inspect the matched audit records and provider
   outcome. Cancellation requested/accepted and remote terminal state are
   separate observations. If completion precedes interruption, record
   cancellation as untested; do not retry to manufacture a result.
4. After an uncertain outcome, inspect the provider before another request. On
   failure, disable the adviser through the reviewed policy procedure and use
   deterministic planning. Do not delete audit records or change memory limits
   to make the adviser test succeed.
