# Optional Agents API adviser

The optional adviser explains selected, sanitized memory evidence and can request
a deterministic memory-plan export. It cannot approve or apply a plan, change
policy, qualify a workload, or execute commands. Deterministic evidence import
and planning remain available without an OpenAI account or provider access.

This guide covers Bridge's `memory-advice` feature, not Codex repository
instructions or GPU qualification. The integration uses the
[Agents API](https://developers.openai.com/api/docs/guides/agents-api/overview),
not the Agents SDK or Responses API. A typed standard-library REST adapter
handles function requests and tool results; no SDK dependency is required.

## Enable only with reviewed local policy

The adviser is disabled by default and forbidden in demo mode. Only a live
administrator can enable `advisor` in `/etc/bridge/server.json`. Changes require
the normal reviewed controller restart. The policy requires:

| Setting | Requirement |
| --- | --- |
| `model` | Explicit provider model |
| `api_key_file` | Canonical absolute path meeting the requirements below |
| `timeout_seconds` | 5–20 |
| `max_tool_calls` | 3–8 |
| `max_sessions_per_hour` | 1–60 |
| `project_budget_acknowledged` | `true`; acknowledgement, not a verified spending limit |

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

Bridge does not create, query, or verify provider spending limits. Configure
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

The value `8192` is an example, not a discovered budget. Run the owner-only
command:

```sh
bridgectl --context PRIVATE_CONTEXT memory-advice --file memory-request.json
```

An unavailable provider returns an explicit error. The same evidence remains
usable through `memory-preview` and the ordinary plan/export commands.

Bridge creates an `environment:none` session with fixed initial input. Creation
can start paid work or return a completed turn. Bridge checks the turn outcome;
an idle session alone does not prove success. See the
[session contract](https://developers.openai.com/api/docs/guides/agents-api/sessions).

The [function flow](https://developers.openai.com/api/docs/guides/agents-api/tools/functions)
exposes three empty-argument tools:

- `read_memory_evidence` returns the selected, validated memory preview.
- `explain_memory_capacity` reads the current local capacity snapshot.
- `request_memory_plan` uses normal Bridge validation to create an unapproved
  export plan under the requesting owner's identity.

Bridge validates evidence before dispatch and rechecks owner access before each
local tool executes. Each named tool executes locally at most once; repeated
requests reuse its result, and tool-result submission is idempotent. Successful
advice can omit a plan. Treat provider text as untrusted explanation; review the
local plan, not a plan ID or approval claimed in that text.

## Boundaries, accounting and audit

No public callback, MCP server, PromQL, shell, Kubernetes credential, or Bridge
management credential is exposed. Outbound HTTPS uses only `api.openai.com`,
normal certificate validation, and no redirect or environment proxy. Bridge
bounds request and response bytes, history, tool calls, concurrent work, and
duration. Hourly admission is process-local and resets after restart.

Memory values have explicit units: requested/limited RAM, shm ceiling,
other-workload allowance and candidate use MiB; envelope, headroom and
measurement-window memory use bytes. Unknown token usage is JSON `null`, not
zero. Session usage takes precedence over turn fallback; Bridge never adds the
two. These are provisional observations, not a final bill. See the
[usage guide](https://developers.openai.com/api/docs/guides/agents-api/observability).

The session-create contract has no per-session hard token or dollar ceiling.
Local limits, reported usage and best-effort cancellation do not create one.
Cancellation acceptance does not prove that remote computation stopped. Review
the provider's retention and data controls. Source checks do not test a key,
paid request, account capability or spending control.

Before contacting the provider, Bridge saves a `memory.advice.requested` audit
entry. A matching `memory.advice.finished` entry keeps only the fixed outcome,
known session ID, nullable usage/source, and creation/cancellation metadata. It
does not retain explanation text, raw provider errors, evidence, or credentials.
The 2000-entry audit bound applies. `X-Bridge-Advisory-ID` and the fixed failure
message identify the attempt. If the final audit write fails, Bridge retains
the requested entry and returns a storage refusal. A local plan can survive a
later provider or audit failure.

## Recover an interrupted or failed request

Do not automatically retry an interrupted advisory. A lost create response can
mean paid work started even when Bridge has no session ID. Bridge does not retry
creation or resume the advisory after a restart. Inspect retained plans and the
owner-only audit:

```sh
bridgectl --context PRIVATE_CONTEXT audit
```

Match requested and finished entries by `object`. An unmatched request has an
unknown outcome; it is neither free work nor confirmed cancellation. With a
known session ID, inspect that session in the provider's Agents logs. Otherwise
correlate the request time and project there. Only the owner can authorize
another paid request.

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

   Expect an explanation with nullable, provisional accounting and an optional
   unapproved export plan, or a fixed failure with retained attempt metadata.
   Check retained plans even after failure. Confirm that no plan was applied
   and no managed resource changed. Inspect the session in provider logs; local
   accounting is not an invoice.
3. If a second paid call is authorized, interrupt it only after provider logs
   show that its session started. Inspect the matched audit records and provider
   outcome. Cancellation requested/accepted and remote terminal state are
   separate observations. If completion precedes interruption, record
   cancellation as untested; do not retry to manufacture a result.
4. After an uncertain outcome, inspect retained plans, audit records and provider
   logs before another request. On failure, disable the adviser through the
   reviewed policy procedure and use deterministic planning. Do not delete
   audit records or change memory limits to make the test succeed.
