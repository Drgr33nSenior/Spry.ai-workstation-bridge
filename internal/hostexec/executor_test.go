package hostexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func fixtureExecutor(t *testing.T) *Executor {
	t.Helper()
	dir := t.TempDir()
	e := &Executor{policy: Policy{Version: 1, AllowedUID: 2345, Target: "fixture-node", StateDir: dir, SessionDir: dir, TimeoutSeconds: 2, MaxRecords: 32, SourcePath: filepath.Join(dir, "source")}, records: map[string]record{}, verify: func() error { return nil }, pathTrust: func(string, bool) error { return nil }, finalize: func() error { return nil }}
	e.lock = func() (*os.File, error) { return lockFile(filepath.Join(dir, "lock")) }
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) { return nil, nil }
	return e
}
func fixtureRequest(id string) Request {
	r := Request{Version: 1, ID: id, ExecutionIdentity: "test-owner", Draft: domain.Draft{Action: "hardware.refresh", Target: "fixture-node"}}
	r.PayloadHash = RequestHash(r)
	return r
}

func fixtureRecoveryRequest(t *testing.T, e *Executor, id, action, recoveryID string) Request {
	t.Helper()
	configuration := domain.Configuration{}
	configuration.Revision = configuration.ContentRevision()
	if err := durableJSON(e.policy.SourcePath, configuration); err != nil {
		t.Fatal(err)
	}
	r := Request{Version: ContractVersion, ID: id, ExecutionIdentity: "test-owner", Draft: domain.Draft{Action: action, Target: "fixture-node", RecoveryID: recoveryID, SourceRevision: configuration.Revision}, Desired: configuration}
	if action == "profile.switch" {
		r.Draft.Profile = "gaming"
	}
	r.PayloadHash = RequestHash(r)
	return r
}

func TestRecoveryCannotChangeRootJournalTarget(t *testing.T) {
	e := fixtureExecutor(t)
	original := fixtureRecoveryRequest(t, e, "operation-old-target", "profile.switch", "")
	original.Draft.Target = "previous-workstation"
	original.PayloadHash = RequestHash(original)
	if err := e.save(record{Request: original, Result: Result{ID: original.ID, State: "recovery-required"}}); err != nil {
		t.Fatal(err)
	}
	retry := fixtureRecoveryRequest(t, e, "operation-new-target", "profile.restore", original.ID)
	if _, err := e.Submit(e.policy.AllowedUID, retry); err == nil {
		t.Fatal("root helper restored an old journal onto a new policy target")
	}
}
func awaitResult(t *testing.T, e *Executor, id string) Result {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := e.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(r.State) {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return Result{}
}

func TestIndependentAuthorityAndIdempotency(t *testing.T) {
	e := fixtureExecutor(t)
	r := fixtureRequest("operation-0001")
	if _, err := e.Submit(2346, r); err == nil {
		t.Fatal("other peer authorized")
	}
	bad := r
	bad.Draft.Target = "production-node"
	bad.PayloadHash = RequestHash(bad)
	if _, err := e.Submit(2345, bad); err == nil {
		t.Fatal("API target broadened root policy")
	}
	bad = r
	bad.Desired.Resources.PhysicalGPU = "/dev/dri/renderD128"
	if _, err := e.Submit(2345, bad); err == nil {
		t.Fatal("modified desired payload accepted")
	}
	var executions atomic.Int32
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) { executions.Add(1); return nil, nil }
	if _, err := e.Submit(2345, r); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, r.ID); got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	if _, err := e.Submit(2345, r); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 1 {
		t.Fatal("duplicate execution")
	}
	r.ExecutionIdentity = "different-actor"
	r.PayloadHash = RequestHash(r)
	if _, err := e.Submit(2345, r); err == nil {
		t.Fatal("same ID with changed actor accepted")
	}
}

func TestContinuousLegacyLockAndDurableIntent(t *testing.T) {
	e := fixtureExecutor(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	r := fixtureRequest("operation-lock-0001")
	e.run = func(_ context.Context, request Request, _ *os.File) (json.RawMessage, error) {
		if _, err := os.Stat(filepath.Join(e.policy.StateDir, request.ID+".json")); err != nil {
			t.Error("intent not durable before dispatch")
		}
		if lock, err := lockFile(filepath.Join(e.policy.SessionDir, "lock")); err == nil {
			lock.Close()
			t.Error("legacy caller raced helper operation")
		}
		close(entered)
		<-release
		return nil, nil
	}
	if _, err := e.Submit(2345, r); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := e.Submit(2345, fixtureRequest("operation-lock-0002")); err == nil {
		t.Fatal("conflicting helper operation accepted")
	}
	close(release)
	if got := awaitResult(t, e, r.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	deadline := time.Now().Add(time.Second)
	for {
		lock, err := lockFile(filepath.Join(e.policy.SessionDir, "lock"))
		if err == nil {
			lock.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock retained after durable final state")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRestartDoesNotRetryAndFencesRecovery(t *testing.T) {
	e := fixtureExecutor(t)
	r := fixtureRequest("operation-crash-0001")
	r.Draft.Action = "profile.switch"
	r.Draft.Profile = "gaming"
	r.PayloadHash = RequestHash(r)
	if err := e.save(record{Request: r, PeerUID: 2345, Result: Result{ID: r.ID, State: "running", Phase: "external-effect"}}); err != nil {
		t.Fatal(err)
	}
	e.records = map[string]record{}
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Status(r.ID)
	if got.State != "recovery-required" {
		t.Fatal(got)
	}
	if _, err := e.Submit(2345, fixtureRequest("operation-after-crash")); err == nil {
		t.Fatal("uncertain external effects were not fenced")
	}
	// Altering the separate API record cannot make this root journal disappear.
	apiFile := filepath.Join(t.TempDir(), "api.json")
	if err := durableJSON(apiFile, map[string]string{"state": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	got, _ = e.Status(r.ID)
	if got.State != "recovery-required" {
		t.Fatal("API record authorized privileged work")
	}
}

func TestRecoveryAttemptsSettleOneDurableHelperChain(t *testing.T) {
	e := fixtureExecutor(t)
	var executions atomic.Int32
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		if executions.Add(1) < 3 {
			return nil, errors.New("fixture dispatched effect is uncertain")
		}
		return nil, nil
	}
	a := fixtureRecoveryRequest(t, e, "recovery-a-0001", "profile.switch", "")
	if _, err := e.Submit(2345, a); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, a.ID); got.State != "recovery-required" {
		t.Fatal(got)
	}
	b := fixtureRecoveryRequest(t, e, "recovery-b-0001", "profile.restore", a.ID)
	if _, err := e.Submit(2345, b); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, b.ID); got.State != "recovery-required" {
		t.Fatal(got)
	}
	c := fixtureRecoveryRequest(t, e, "recovery-c-0001", "profile.restore", b.ID)
	if _, err := e.Submit(2345, c); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, c.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	for _, id := range []string{a.ID, b.ID} {
		if got, err := e.Status(id); err != nil || got.State != "failed" || !strings.Contains(got.Phase, "recovered-by-"+c.ID) {
			t.Fatalf("%s not durably settled by recovery chain: %+v %v", id, got, err)
		}
	}
	if _, err := e.Submit(2345, c); err != nil || executions.Load() != 3 {
		t.Fatalf("recovery retry was not idempotent: executions=%d err=%v", executions.Load(), err)
	}
	after := fixtureRequest("operation-after-chain")
	if _, err := e.Submit(2345, after); err != nil {
		t.Fatalf("settled recovery chain still fenced unrelated operations: %v", err)
	}
	if got := awaitResult(t, e, after.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
}

func TestRecoverySettlementRollsForwardAfterParentPersistenceFault(t *testing.T) {
	e := fixtureExecutor(t)
	var executions atomic.Int32
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		if executions.Add(1) < 3 {
			return nil, errors.New("fixture dispatched effect is uncertain")
		}
		return nil, nil
	}
	a := fixtureRecoveryRequest(t, e, "recovery-fault-a", "profile.switch", "")
	b := fixtureRecoveryRequest(t, e, "recovery-fault-b", "profile.restore", a.ID)
	for _, request := range []Request{a, b} {
		if _, err := e.Submit(2345, request); err != nil {
			t.Fatal(err)
		}
		if got := awaitResult(t, e, request.ID); got.State != "recovery-required" {
			t.Fatal(got)
		}
	}
	var armFault atomic.Bool
	var failOnce atomic.Bool
	e.persist = func(path string, value any) error {
		if armFault.Load() && filepath.Base(path) == a.ID+".json" && !failOnce.Swap(true) {
			return errors.New("fixture parent journal write failure")
		}
		return durableJSON(path, value)
	}
	armFault.Store(true)
	c := fixtureRecoveryRequest(t, e, "recovery-fault-c", "profile.restore", b.ID)
	if _, err := e.Submit(2345, c); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, c.ID); got.State != "recovery-required" || got.Phase != "recovery-settlement-pending" {
		t.Fatalf("partially settled restore was reported successful: %+v", got)
	}
	if durable := e.records[c.ID].Result; durable.State != "succeeded" {
		t.Fatalf("successful recovery proof was lost: %+v", durable)
	}
	if got, err := e.Submit(2345, c); err != nil || got.State != "recovery-required" || got.Phase != "recovery-settlement-pending" {
		t.Fatalf("duplicate did not report settlement pending: %+v %v", got, err)
	}
	if !e.journalFailed {
		t.Fatal("failed parent recovery settlement did not fence the helper")
	}

	// This simulates a process restart after C was durable but before every
	// parent was settled. The replay writes journals only; it never calls run.
	reopened := fixtureExecutor(t)
	reopened.policy = e.policy
	reopened.persist = durableJSON
	if err := reopened.load(); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 3 {
		t.Fatal("recovery settlement re-dispatched an external effect")
	}
	for _, id := range []string{a.ID, b.ID} {
		if got, err := reopened.Status(id); err != nil || got.State != "failed" || got.Phase != "recovered-by-"+c.ID {
			t.Fatalf("restart did not roll forward %s: %+v %v", id, got, err)
		}
	}
	if got, err := reopened.Status(c.ID); err != nil || got.State != "succeeded" {
		t.Fatalf("settled restore did not become visible after restart: %+v %v", got, err)
	}
}

func TestRestartSettlementHoldsCanonicalLockAndFailsOnContention(t *testing.T) {
	e := fixtureExecutor(t)
	base := time.Now().UTC()
	a := fixtureRecoveryRequest(t, e, "recovery-lock-a", "profile.switch", "")
	c := fixtureRecoveryRequest(t, e, "recovery-lock-c", "profile.restore", a.ID)
	for _, entry := range []struct {
		request Request
		result  Result
		started time.Time
	}{
		{a, Result{ID: a.ID, State: "recovery-required"}, base.Add(-time.Minute)},
		{c, Result{ID: c.ID, State: "succeeded"}, base},
	} {
		if err := e.save(record{Request: entry.request, PeerUID: 2345, Result: entry.result, StartedAt: entry.started}); err != nil {
			t.Fatal(err)
		}
	}
	held, err := lockFile(filepath.Join(e.policy.SessionDir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	e.records = map[string]record{}
	if err := e.load(); err == nil || !e.journalFailed {
		t.Fatal("restart settlement ignored a competing canonical legacy lock")
	}
	if got, _ := e.Status(a.ID); got.State != "recovery-required" {
		t.Fatal("lock contention settled the original recovery")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := fixtureExecutor(t)
	reopened.policy = e.policy
	reopened.lock = func() (*os.File, error) { return lockFile(filepath.Join(reopened.policy.SessionDir, "lock")) }
	reopened.persist = func(path string, value any) error {
		if lock, err := lockFile(filepath.Join(reopened.policy.SessionDir, "lock")); err == nil {
			lock.Close()
			t.Fatal("restart settlement wrote journals without the canonical legacy lock")
		}
		return durableJSON(path, value)
	}
	if err := reopened.load(); err != nil {
		t.Fatal(err)
	}
	if got, _ := reopened.Status(a.ID); got.State != "failed" {
		t.Fatal("restart settlement did not finish after lock release")
	}
}

func TestRestartReadOnlyRecordsFailWithoutRecoveryFence(t *testing.T) {
	for _, action := range []string{"hardware.refresh", "cpu-policy.export"} {
		t.Run(action, func(t *testing.T) {
			e := fixtureExecutor(t)
			r := fixtureRequest("readonly-restart-" + strings.ReplaceAll(action, ".", "-"))
			r.Draft.Action = action
			r.PayloadHash = RequestHash(r)
			if err := e.save(record{Request: r, PeerUID: 2345, Result: Result{ID: r.ID, State: "running", Phase: "collecting"}, StartedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			e.records = map[string]record{}
			if err := e.load(); err != nil {
				t.Fatal(err)
			}
			got, err := e.Status(r.ID)
			if err != nil || got.State != "failed" || got.Phase != "read-only-interrupted" {
				t.Fatalf("unfinished read-only operation became a recovery fence: %+v %v", got, err)
			}
			next := fixtureRequest("readonly-after-restart")
			if _, err := e.Submit(2345, next); err != nil {
				t.Fatalf("read-only interruption fenced the next bounded diagnostic: %v", err)
			}
			if got := awaitResult(t, e, next.ID); got.State != "succeeded" {
				t.Fatal(got)
			}
		})
	}
}

func TestRecoveryChainRejectsUnrelatedOrPreflightRecords(t *testing.T) {
	e := fixtureExecutor(t)
	a := fixtureRecoveryRequest(t, e, "recovery-other-a", "profile.switch", "")
	if err := e.save(record{Request: a, PeerUID: 2345, Result: Result{ID: a.ID, State: "recovery-required"}}); err != nil {
		t.Fatal(err)
	}
	unrelated := fixtureRecoveryRequest(t, e, "recovery-other-b", "profile.switch", "")
	if err := e.save(record{Request: unrelated, PeerUID: 2345, Result: Result{ID: unrelated.ID, State: "recovery-required"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(2345, fixtureRecoveryRequest(t, e, "recovery-other-c", "profile.restore", a.ID)); err == nil {
		t.Fatal("restore accepted while an unrelated root helper fence remained")
	}

	preflight := fixtureExecutor(t)
	preflight.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		return nil, preflightFailure{errors.New("fixture preflight refusal")}
	}
	r := fixtureRecoveryRequest(t, preflight, "preflight-0001", "profile.switch", "")
	if _, err := preflight.Submit(2345, r); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, preflight, r.ID); got.State != "failed" || got.Phase != "preflight-refused" {
		t.Fatalf("preflight refusal became uncertain recovery: %+v", got)
	}
}

func TestRecoveryChainAllowsRetryFromOriginalRootAndRejectsCycles(t *testing.T) {
	e := fixtureExecutor(t)
	var executions atomic.Int32
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		if executions.Add(1) < 3 {
			return nil, errors.New("fixture dispatched effect is uncertain")
		}
		return nil, nil
	}
	a := fixtureRecoveryRequest(t, e, "recovery-root-a", "profile.switch", "")
	b := fixtureRecoveryRequest(t, e, "recovery-root-b", "profile.restore", a.ID)
	for _, request := range []Request{a, b} {
		if _, err := e.Submit(2345, request); err != nil {
			t.Fatal(err)
		}
		if got := awaitResult(t, e, request.ID); got.State != "recovery-required" {
			t.Fatal(got)
		}
	}
	// A retry may name the original failed operation or the latest failed
	// attempt. Both identify the same root-owned recovery chain.
	c := fixtureRecoveryRequest(t, e, "recovery-root-c", "profile.restore", a.ID)
	if _, err := e.Submit(2345, c); err != nil {
		t.Fatal(err)
	}
	if got := awaitResult(t, e, c.ID); got.State != "succeeded" {
		t.Fatal(got)
	}

	cycle := fixtureExecutor(t)
	first := fixtureRecoveryRequest(t, cycle, "recovery-cycle-a", "profile.restore", "recovery-cycle-b")
	second := fixtureRecoveryRequest(t, cycle, "recovery-cycle-b", "profile.restore", first.ID)
	for _, request := range []Request{first, second} {
		if err := cycle.save(record{Request: request, PeerUID: 2345, Result: Result{ID: request.ID, State: "recovery-required"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cycle.Submit(2345, fixtureRecoveryRequest(t, cycle, "recovery-cycle-c", "profile.restore", first.ID)); err == nil {
		t.Fatal("cyclic root helper recovery links were accepted")
	}
}

func TestRecoverySettlementRetainsLaterUnresolvedAttempt(t *testing.T) {
	e := fixtureExecutor(t)
	base := time.Now().UTC()
	a := fixtureRecoveryRequest(t, e, "recovery-order-a", "profile.switch", "")
	c := fixtureRecoveryRequest(t, e, "recovery-order-c", "profile.restore", a.ID)
	later := fixtureRecoveryRequest(t, e, "recovery-order-d", "profile.restore", a.ID)
	for _, entry := range []struct {
		request Request
		result  Result
		started time.Time
	}{
		{a, Result{ID: a.ID, State: "recovery-required"}, base.Add(-2 * time.Minute)},
		{c, Result{ID: c.ID, State: "succeeded"}, base.Add(-time.Minute)},
		{later, Result{ID: later.ID, State: "recovery-required"}, base},
	} {
		if err := e.save(record{Request: entry.request, PeerUID: 2345, Result: entry.result, StartedAt: entry.started}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.settleRecoveryChain(c.ID); err == nil {
		t.Fatal("older successful restore settled a later unresolved attempt")
	}
	for _, id := range []string{a.ID, later.ID} {
		if got, err := e.Status(id); err != nil || got.State != "recovery-required" {
			t.Fatalf("later-attempt fence was cleared for %s: %+v %v", id, got, err)
		}
	}
	if !e.journalFailed {
		t.Fatal("ambiguous recovery ordering did not fence the helper")
	}
}

func TestJournalFailureRefusesDispatch(t *testing.T) {
	e := fixtureExecutor(t)
	e.policy.StateDir = filepath.Join(t.TempDir(), "absent")
	var calls atomic.Int32
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) { calls.Add(1); return nil, nil }
	if _, err := e.Submit(2345, fixtureRequest("operation-full-0001")); err == nil {
		t.Fatal("failed durable intent accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("external effect ran without intent")
	}
}

func TestSourceAndQualificationCannotBeForged(t *testing.T) {
	e := fixtureExecutor(t)
	c := domain.Configuration{Serving: domain.Serving{Model: "reviewed", Context: 32768, Concurrency: 1, MemoryFraction: .8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 8192, GPUCount: 1}}
	c.Revision = c.ContentRevision()
	if err := durableJSON(e.policy.SourcePath, c); err != nil {
		t.Fatal(err)
	}
	r := fixtureRequest("operation-serving-0001")
	r.Draft.Action = "serving.configure"
	r.Desired = c
	r.Draft.SourceRevision = c.Revision
	r.PayloadHash = RequestHash(r)
	if err := e.validate(r); err == nil || !strings.Contains(err.Error(), "qualification") {
		t.Fatalf("unqualified API source accepted: %v", err)
	}
	e.policy.QualifiedConfigurations = map[string]Qualification{ConfigurationHash(&c.Serving, &c.Resources): {Image: "image@sha256:" + strings.Repeat("a", 64), ModelRevision: strings.Repeat("b", 40), ModelPath: "/models/reviewed", ExpiresAt: time.Now().Add(time.Hour)}}
	if err := e.validate(r); err != nil {
		t.Fatal(err)
	}
	c.Serving.Context = 65536
	c.Revision = c.ContentRevision()
	if err := durableJSON(e.policy.SourcePath, c); err != nil {
		t.Fatal(err)
	}
	if err := e.validate(r); err == nil {
		t.Fatal("source drift accepted")
	}
	if _, err := runFixed(context.Background(), "/definitely/not-an-installed-runtime", nil, nil, nil, nil, 100); err == nil {
		t.Fatal("missing runtime claimed success")
	}
}

func TestReadOnlyFailureDoesNotInventSuccess(t *testing.T) {
	e := fixtureExecutor(t)
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		return nil, errors.New("fixture collection incomplete")
	}
	r := fixtureRequest("operation-failure-0001")
	if _, err := e.Submit(2345, r); err != nil {
		t.Fatal(err)
	}
	got := awaitResult(t, e, r.ID)
	if got.State != "failed" {
		t.Fatal(got)
	}
}

func TestArtifactManifestRejectsSymlinkAndHashesEveryFile(t *testing.T) {
	dir := t.TempDir()
	if err := durableJSON(filepath.Join(dir, "artifact"), map[string]int{"version": 1}); err != nil {
		t.Fatal(err)
	}
	m, err := ArtifactManifest(dir)
	if err != nil || len(m) != 1 || len(m["artifact"]) != 64 {
		t.Fatal(m, err)
	}
	if err = os.Symlink("artifact", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err = ArtifactManifest(dir); err == nil {
		t.Fatal("symlinked runtime artifact accepted")
	}
}

func TestResourceAccountingIncludesInitAndOverhead(t *testing.T) {
	var pod map[string]any
	if err := json.Unmarshal([]byte(`{"spec":{"containers":[{"resources":{"requests":{"cpu":"500m","memory":"2Gi"}}}],"initContainers":[{"resources":{"requests":{"cpu":"2","memory":"1Gi"}}}],"overhead":{"cpu":"100m","memory":"1Gi"}}}`), &pod); err != nil {
		t.Fatal(err)
	}
	cpu, memory, gpu, err := podBudget(pod)
	if err != nil || cpu != 2100 || memory != 3<<30 || gpu != 0 {
		t.Fatal(cpu, memory, gpu, err)
	}
	object(list(nested(pod, "spec", "initContainers"))[0])["restartPolicy"] = "Always"
	if _, _, _, err = podBudget(pod); err == nil {
		t.Fatal("unsupported sidecar accounting guessed")
	}
}

func TestHardwareKeepsStableIdentityAndUnknowns(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"status":"observed","memory":{"total_bytes":68719476736,"dimms":[{"size":"32 GB"},{"size":"32 GB"}]},"pci_gpus":[{"bdf":"0000:41:00.0","device_id":"0x7551","render_nodes":["renderD130"]},{"bdf":"0000:81:00.0","device_id":"0x7551","render_nodes":["renderD128"]}]}`), &raw)
	h := SanitizeHardware(raw, "boot-fixture")
	if h.TopologyKnown || h.Channels != "unknown" || h.DIMMs != 2 || h.GPUs[0].ID == h.GPUs[1].ID || h.GPUs[0].RenderPath != "/dev/dri/renderD130" {
		t.Fatalf("%+v", h)
	}
}

func TestRootQualifiedModelIntegrityAndPathIsolation(t *testing.T) {
	e := fixtureExecutor(t)
	e.policy.ModelRoot = t.TempDir()
	modelPath := filepath.Join(e.policy.ModelRoot, "model", strings.Repeat("a", 40))
	if err := os.MkdirAll(modelPath, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("controlled fixture model bytes")
	if err := durableBytes(filepath.Join(modelPath, "weights.fixture"), content); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	c := domain.Configuration{Serving: domain.Serving{Model: "fixture"}, Resources: domain.Resources{GPUCount: 1}}
	q := Qualification{HostModelPath: modelPath, ModelPath: "/models/model/" + strings.Repeat("a", 40), Files: map[string]string{"weights.fixture": hex.EncodeToString(hash[:])}, ExpiresAt: time.Now().Add(time.Hour)}
	e.policy.QualifiedConfigurations = map[string]Qualification{ConfigurationHash(&c.Serving, &c.Resources): q}
	if err := e.verifyModel(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := durableBytes(filepath.Join(modelPath, "weights.fixture"), []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	if err := e.verifyModel(context.Background(), c); err == nil {
		t.Fatal("corrupt model authorized")
	}
	q.HostModelPath = t.TempDir()
	e.policy.QualifiedConfigurations[ConfigurationHash(&c.Serving, &c.Resources)] = q
	if err := e.verifyModel(context.Background(), c); err == nil {
		t.Fatal("model path escaped scoped storage")
	}
}

func TestTemplateQualificationExpiryAndRecoveryWindow(t *testing.T) {
	e := fixtureExecutor(t)
	target := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{}}}}}
	q := SessionQualification{TemplateHash: domain.Hash(nested(target, "spec", "template")), ExpiresAt: time.Now().Add(time.Hour)}
	e.policy.SessionQualifications = map[string]SessionQualification{"ai/previous": q}
	if err := e.qualifySession(context.Background(), "ai", target, nil); err != nil {
		t.Fatal(err)
	}
	q.ExpiresAt = time.Now().Add(-time.Minute)
	e.policy.SessionQualifications["ai/previous"] = q
	if err := e.qualifySession(context.Background(), "ai", target, nil); err == nil {
		t.Fatal("expired root qualification accepted")
	}
}

func TestIncompleteTopologyIsNotGuessed(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"cpu_topology":{"online_cpus":[0,1],"cpus":[{"cpu":0,"core_id":0,"socket_id":0,"thread_siblings":[0,1]},{"cpu":1,"core_id":1,"socket_id":0,"thread_siblings":[0,1]}]}}`), &raw)
	if SanitizeHardware(raw, "").TopologyKnown {
		t.Fatal("inconsistent SMT groups treated as known")
	}
}

func TestLegacyCanonicalTemplateHashContract(t *testing.T) {
	v := map[string]any{"spec": map[string]any{"workers": float64(2), "value": "<x>&"}}
	sum := sha256.Sum256([]byte(`{"spec":{"value":"<x>&","workers":2}}`))
	if canonicalTemplateHash(v) != hex.EncodeToString(sum[:]) {
		t.Fatal("Go template expectation does not match newline-free jq -cS contract")
	}
}
