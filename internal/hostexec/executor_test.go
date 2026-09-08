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
