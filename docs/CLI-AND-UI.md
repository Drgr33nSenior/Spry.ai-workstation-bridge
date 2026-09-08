# CLI and web management

The CLI and browser use the same `/api/v1` service contracts. The server validates every draft, authorizes every action, and records operation intent before dispatch. The browser does not execute workstation or developer-client commands.

## Run the isolated demo

Run these commands from the repository's physical directory. `pwd -P` avoids a symlink in the managed-state path. Choose a new demo directory; initialization refuses to overwrite one.

```sh
go build -o bin/bridged ./cmd/bridged
go build -o bin/bridgectl ./cmd/bridgectl
demo_dir="$(pwd -P)/.demo"
bin/bridged --init-demo "$demo_dir" --demo-port 8743
bin/bridgectl admin bootstrap --config "$demo_dir/bridge.json" --output "$demo_dir/owner.token" --name demo-owner --ttl 24h
bin/bridged --config "$demo_dir/bridge.json"
```

The last command runs in the foreground. Stop this demo with Ctrl+C. Initialization and owner bootstrap do not start a network listener. The demo server binds only `127.0.0.1:8743`, uses separate state and credentials, and never opens the real adapters.

In another terminal, set `demo_dir` to the same absolute directory and run:

```sh
bin/bridgectl --context "$demo_dir/context.json" status
bin/bridgectl --context "$demo_dir/context.json" models
bin/bridgectl --context "$demo_dir/context.json" resources
```

Open the demo at `http://127.0.0.1:8743/`. Open the generated owner-only `owner.token` file in your local editor and enter its value in the sign-in field. Do not put the value in a URL, command argument, chat, ordinary terminal output, or inference application. The UI clears the field after exchange and uses a short-lived HttpOnly, SameSite=Strict demo session cookie. Logout revokes the browser session. The UI stores no bearer token in localStorage. Live browser sessions require a dedicated trusted HTTPS hostname and explicit `browser_sessions: true`; live HTTP loopback is CLI-only. Never use a live credential with the demo. See [browser migration](REMEDIATION.md#private-browser-migration).

The `DEMO · ISOLATED FIXTURES` label must remain visible. Model staging, builds, device observations, and handovers are simulated. A successful demo operation is not target qualification.

## Credentials and contexts

Local administration requires the configured owner UID or root and an exclusively locked store with the service stopped. There is no TCP bootstrap, registration, or recovery route, including on loopback. Root-owned live policy still requires the administrator to grant the operating-system access needed to open the stopped store.

To issue a credential while the service is stopped:

```sh
bin/bridgectl admin issue --config "$demo_dir/bridge.json" --role viewer --name read-only-client --ttl 8h --output "$demo_dir/viewer.token"
bin/bridgectl admin issue --config "$demo_dir/bridge.json" --role operator --name session-client --ttl 8h --output "$demo_dir/operator.token"
bin/bridgectl admin list --config "$demo_dir/bridge.json"
```

Credential commands return identity metadata, never the generated secret. Output must be a new file in an owner-only directory. The CLI refuses symlinked, foreign-owned, or group/world-readable credential files.

Owners can administer supported settings and operations. Operators can request the preconfigured AI, gaming, maintenance, and restore sessions. Viewers can read management state and non-secret exports. Neither role can submit arbitrary executables, builds, paths, URLs, or Kubernetes targets.

For stopped-service revocation, replace `CREDENTIAL_ID` with an ID returned by `admin list`:

```sh
bin/bridgectl admin revoke --config "$demo_dir/bridge.json" --name CREDENTIAL_ID
```

For a lost owner credential, explicitly stop the service and run:

```sh
bin/bridgectl admin recover --config "$demo_dir/bridge.json" --output "$demo_dir/recovered-owner.token" --name recovered-owner --ttl 24h
```

Recovery revokes all previous credentials and browser sessions. Update the client context's `credential_file` to the new file before restarting. Credential recovery does not resolve an unfinished workstation operation.

A client context contains only credential references:

```json
{
  "endpoint": "https://bridge.vpn.example:8743",
  "credential_file": "/absolute/private/bridge/owner.token",
  "ca_file": "/absolute/private/bridge/management-ca.pem",
  "timeout_seconds": 30
}
```

The certificate must identify `bridge.vpn.example`. Use an administrator-reviewed private interface or VPN address and private name resolution. The matching server policy uses an explicit address such as `10.77.0.2:8743`, an exact `external_url`, an `allowed_hosts` entry of `bridge.vpn.example:8743`, and certificate/key references. This example does not create a VPN, DNS record, firewall rule, public ingress, or certificate. VPN membership does not grant a management role. The CLI verifies TLS, refuses redirects, and does not use an environment-selected HTTP proxy. There is no insecure verification flag.

Global flags precede the command. `--endpoint`, `--credential-file`, and `--ca-file` explicitly override context fields. `--deadline` bounds the client request or wait, without cancelling accepted server work. Remote failure never selects a privileged local execution path.

## Configure serving through a plan

First inspect the current source and selected models:

```sh
bin/bridgectl --context "$demo_dir/context.json" config
bin/bridgectl --context "$demo_dir/context.json" models
```

Create `serving-draft.json` in your editor with the following content. Replace `SOURCE_REVISION_FROM_CONFIG` with the current `revision`. This example keeps the demo's reviewed model, quantization and context and changes concurrency from 2 to 3.

```json
{
  "action": "serving.configure",
  "target": "demo-workstation",
  "source_revision": "SOURCE_REVISION_FROM_CONFIG",
  "serving": {
    "model": "Qwen3.5-9B",
    "context": 4096,
    "concurrency": 3,
    "memory_fraction": 0.8,
    "cpu_offload_gib": 0,
    "max_request_tokens": 0,
    "max_output_tokens": 0
  }
}
```

Zero global request/output caps mean that the pinned deployment does not enforce these caps. A nonzero unsupported cap is refused. The exported client profile has separate request-level limits. Hardware topology, capacity, and live image/model qualification can also refuse a plan; Bridge does not reduce context or change quantization to pass it.

```sh
bin/bridgectl --context "$demo_dir/context.json" plan --file serving-draft.json
```

Review `preview.changes`, `consequences`, `warnings`, `preconditions`, target, and expiry. For this unchanged demo, the exact changed field is `serving.concurrency`, from 2 to 3. Use the returned plan ID below:

```sh
bin/bridgectl --context "$demo_dir/context.json" apply --plan PLAN_ID --target demo-workstation --idempotency-key serving-concurrency-3-attempt-1
bin/bridgectl --context "$demo_dir/context.json" operations OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" --deadline 2m wait OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" export-source --output managed-source-export.json
```

Keep the same idempotency key and payload when retrying an interrupted submission. Reusing that key for a different payload is refused. A changed source revision requires a fresh plan. Inspect `source_updated` and `live_applied` separately: a source update can succeed while the external apply needs recovery. The export command verifies the source content revision and refuses to overwrite the output file.

In the browser, open Models & serving, edit the labelled fields, select Preview serving changes, review the same server-generated diff, type the exact target, and apply. Operations & recovery shows progress and durable outcomes. Refreshing the browser reconnects to the existing session and operation records.

## Other typed drafts

Every draft has `action`, `target`, and the current `source_revision`. Add only the fields listed below; arbitrary JSON patches and executable arguments are refused.

| Workflow | Action | Additional fields |
| --- | --- | --- |
| Stage or verify a selected model | `model.stage`, `model.verify` | `model`: reviewed model ID |
| Start, stop, restart serving | `serving.start`, `serving.stop`, `serving.restart` | None |
| Workload resources | `resources.configure` | `resources`: complete CPU, RAM, shared-memory and GPU-count budget |
| Refresh target evidence | `hardware.refresh` | None |
| Export CPU Manager maintenance files | `cpu-policy.export` | None; export does not change host policy |
| Operating profile | `profile.switch` | `profile`: `ai`, `gaming`, or `maintenance` |
| Restore previous session | `profile.restore` | None for an ordinary restore; use recovery below for an uncertain operation |
| Named build | `build.start` | `recipe`: an ID returned by `builds` |
| Cache and compilation budgets | `caches.configure` | `caches`: complete budget object from `config` |

Copy the current `resources` or `caches` object from `config`, change the intended values, and preserve other fields. The server counts host/K3s reserves and other workloads. A physical GPU selection is unsupported; device-plugin counts do not identify cards. Unknown topology is a refusal, not guessed CPU affinity.

Build jobs use the same plan/apply/wait sequence. Inspect operation artifacts for source revision, digest, output identity and qualification status. Completion does not install, promote, or qualify a build. The UI's Persistent assets page shows bounded cleanup previews without deleting cache contents.

For an operation with downloadable content, use its exact artifact name:

```sh
bin/bridgectl --context "$demo_dir/context.json" artifact OPERATION_ID --name ARTIFACT_NAME --output local-maintenance-export.json
```

Both CLI and UI verify the content length and SHA-256 before download. Large build/model artifacts remain in their managed target locations; a provenance record alone is not downloadable content.

## AI/gaming transition and recovery

Create `gaming-draft.json` with current source revision:

```json
{
  "action": "profile.switch",
  "target": "demo-workstation",
  "source_revision": "SOURCE_REVISION_FROM_CONFIG",
  "profile": "gaming"
}
```

```sh
bin/bridgectl --context "$demo_dir/context.json" plan --file gaming-draft.json
bin/bridgectl --context "$demo_dir/context.json" apply --plan GAMING_PLAN_ID --target demo-workstation --idempotency-key gaming-transition-1
bin/bridgectl --context "$demo_dir/context.json" --deadline 3m wait GAMING_OPERATION_ID
```

On a qualified target this uses the existing shared transition lock and session gates. Gaming can unload all AI. The configured Sunshine or Steam Remote Play path remains explicit. Paused incoming requests are not proof that the old workload released a GPU.

If an operation is still running, inspect it before requesting cancellation:

```sh
bin/bridgectl --context "$demo_dir/context.json" operations GAMING_OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" cancel GAMING_OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" --deadline 3m wait GAMING_OPERATION_ID
```

Cancellation uses the observed operation revision. If progress changes it concurrently, the request returns a conflict; inspect the new state before retrying. A cancellation request or client timeout does not prove that descendant processes or device holders stopped.

If the state is `recovery-required`, inspect the message, phases and executor recovery evidence. Do not start AI over an uncertain GPU holder. Once the executor's outcome permits a reviewed restore:

```sh
bin/bridgectl --context "$demo_dir/context.json" recover GAMING_OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" apply --plan RECOVERY_PLAN_ID --target demo-workstation --idempotency-key gaming-recovery-1
bin/bridgectl --context "$demo_dir/context.json" --deadline 3m wait RECOVERY_OPERATION_ID
bin/bridgectl --context "$demo_dir/context.json" operations GAMING_OPERATION_ID
```

`recover` creates a plan; it does not silently restore. If a surviving executor has since completed, recovery can instead report that its result was recovered and require you to refresh. If it is still running or holder visibility is incomplete, recovery remains refused. The browser exposes the same Preview recovery action and exact-target confirmation.

For a source-update crash before any executor dispatch, owner recovery returns an `operation.reconcile` plan. Review and apply this generated plan through the same commands above. It checks whether durable source matches the previous or requested revision, records the observed source outcome, and leaves the original operation failed with its recovery requirement resolved. It does not dispatch a host action or change GPU workloads. A source revision matching neither side requires owner review; a dispatched operation requires independent executor evidence or a qualified restore.

If restore B also fails, recover A or B to retry the same linked chain. A successful
C settles the chain but retains A/B as failed history. Unrelated uncertain work
still blocks admission. Model operations use verified publication evidence, and
builds use worker journal/cgroup evidence; neither selects GPU restoration.
See [operation-specific recovery](REMEDIATION.md#recovery-and-baseline-session-restoration)
for exact commands, permission repair and refusals that need owner investigation.

## Developer clients

Export order is Qwen Code, DSH, then Hermes. Selection remains manual. Native bundle checks preserve the installer's schema, complete native configuration, SHA-256 values and reviewed source pins.

```sh
bin/bridgectl --context "$demo_dir/context.json" harness export qwen --output qwen-bundle.json
bin/bridgectl harness configure qwen --bundle qwen-bundle.json --directory ./qwen-client
bin/bridgectl harness launch qwen --directory ./qwen-client --mode cli
```

The launch command is explicitly client-local, unprivileged, and requires the selected client to be installed already. It uses fixed client-specific arguments; the management server cannot supply a program or command. Qwen refuses an existing system policy or conflicting `QWEN_CODE_SYSTEM_SETTINGS_PATH` until the owner reviews the merge.

For a direct authenticated export and local configuration in one command:

```sh
bin/bridgectl --context "$demo_dir/context.json" harness configure dsh --directory ./dsh-client
bin/bridgectl harness launch dsh --directory ./dsh-client --mode acp
```

DSH's pinned mode is ACP only; `--mode cli` is refused. Hermes supports `cli` and `acp`, but its output-token cap remains provider-owned. No client automatically inherits the RAG database or falls back to another harness.

Provide inference credentials through the client's supported local `WORKSTATION_AGENT_API_KEY` secret input. Do not put a secret value in a shell argument or bundle. HTTPS inference requires that input. For the existing local HTTP tunnel contract, an absent secret uses the public SDK marker `local-tunnel`; this is not authentication for a network endpoint. Management credentials, kubeconfig, and helper sockets are never exported to the harness. Bridge does not install clients or launch an IDE on the Mac from the server.

## Output and checks

Commands return JSON by default; `--json` is accepted for explicit scripting intent. `help` returns usage text. Stable exit codes are:

| Exit | Meaning |
| --- | --- |
| 0 | Request succeeded, or waited operation succeeded |
| 2 | Usage or validation failure |
| 3 | Authentication or authorization denied |
| 4 | Source, plan, payload or operation revision conflict |
| 5 | Transport, unavailable dependency or local administration failure |
| 6 | Waited operation failed or was cancelled |
| 7 | Waited operation requires recovery |
| 8 | Client deadline expired; accepted work can continue |

Repeat the executable and browser checks with:

```sh
go test ./internal/integration -count=1
go test -race ./internal/client ./internal/web ./internal/integration
node scripts/browser-test.mjs
```

The browser script uses Node's standard library and an installed Chrome executable. It creates temporary credentials, a temporary browser profile, and ephemeral loopback listeners, then removes those fixtures. It does not install browser packages or use your normal profile. Set `CHROME_BIN` only if the installed executable is at another path. If absent, the script reports `NOT RUN` and exits 77.

Verified on 2026-09-08 with Go 1.27.1, Node v26.8.1 and Chrome 152.0.7977.82. These identify the tested development environment; they do not install or update target software. The browser checks cover authentication, exact draft validation and preview, apply, all management pages, gaming, refresh/reconnect, process-crash recovery, a 390-pixel viewport and logout revocation. Ordinary handler tests are not reported as browser validation.

NOT RUN — target hardware unavailable: real GPU/model qualification, DRM handover, K3s scheduling/RBAC, contained workstation compilation and live systemd behavior. Follow the target qualification procedure before live use. Cross-built binaries and fixture successes do not establish these properties.
