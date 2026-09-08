package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDemoLifecycleRecoveryAndIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, e := NewDemo(dir, "demo-target")
	if e != nil {
		t.Fatal(e)
	}
	c := defaultConfig()
	draft := domain.Draft{Action: "profile.switch", Target: "demo-target", SourceRevision: c.Revision, Profile: "ai"}
	p, e := d.Validate(ctx, draft, c)
	if e != nil {
		t.Fatal(e)
	}
	execution := domain.Execution{ID: "fixture-0001", Actor: "owner", Plan: domain.Plan{Draft: draft, Desired: c, Preview: p}}
	r, e := d.Execute(ctx, execution, nil)
	if e != nil || r.State != "succeeded" {
		t.Fatal(r, e)
	}
	d.Fault = "occupied-gpu"
	execution.ID = "fixture-0002"
	execution.Plan.Draft.Profile = "gaming"
	r, e = d.Execute(ctx, execution, nil)
	if e != nil || r.State != "recovery-required" {
		t.Fatal(r, e)
	}
	inv, _ := d.Snapshot(ctx)
	if inv.Profile != "ai" {
		t.Fatal("lost previous state")
	}
	d2, e := NewDemo(dir, "demo-target")
	if e != nil {
		t.Fatal(e)
	}
	r, e = d2.Inspect(ctx, "fixture-0002")
	if e != nil || !r.RecoveryRequired {
		t.Fatal(r, e)
	}
	if _, e = NewLive(LiveOptions{StateDir: dir, Target: "target", Environment: "prd"}); e == nil {
		t.Fatal("accepted production relabel")
	}
	inv, _ = d2.Snapshot(ctx)
	if inv.Mode != "demo" {
		t.Fatal("demo mode changed")
	}
}
func TestResourcesUnknownTopologySMTAndPhysicalSelection(t *testing.T) {
	d, e := NewDemo(t.TempDir(), "demo")
	if e != nil {
		t.Fatal(e)
	}
	c := defaultConfig()
	draft := domain.Draft{Action: "resources.configure", Target: "demo", SourceRevision: c.Revision, Resources: &c.Resources}
	for _, mutate := range []func(*domain.Hardware){func(h *domain.Hardware) { h.TopologyKnown = false }, func(h *domain.Hardware) { h.MemoryMiB = 32768 }, func(h *domain.Hardware) { h.CurrentBootID = "different" }} {
		h := demoHardware()
		mutate(&h)
		d.Hardware = &h
		if _, e = d.Validate(context.Background(), draft, c); e == nil {
			t.Fatal("accepted invalid hardware")
		}
	}
	h := demoHardware()
	h.DIMMs = 4
	d.Hardware = &h
	if _, e = d.Validate(context.Background(), draft, c); e != nil {
		t.Fatal("DIMM count alone must not change capacity", e)
	}
	c.Resources.PhysicalGPU = "0000:41:00.0"
	draft.Resources = &c.Resources
	if _, e = d.Validate(context.Background(), draft, c); e == nil {
		t.Fatal("accepted physical device selection")
	}
}

func recoveryLive(t *testing.T) (*Live, catalog.Model, domain.Execution) {
	t.Helper()
	root := t.TempDir()
	models := catalog.Selected()
	lines := []string{}
	for i, key := range []string{"SGLANG_DUAL_MODEL", "SGLANG_SINGLE_MODEL", "RAG_EMBEDDING"} {
		lines = append(lines, key+"_REPOSITORY="+models[i].Repository, key+"_REVISION="+models[i].Revision)
	}
	pins := catalog.HarnessPins()
	keys := make([]string, 0, len(pins))
	for key := range pins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+"="+pins[key])
	}
	if err := os.WriteFile(filepath.Join(root, "versions.lock"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := []byte("small model recovery fixture")
	s, _ := fixtureStager(t, body)
	digest := sha256.Sum256(body)
	m := catalog.Model{ID: "fixture-model", Repository: "Qwen/fixture-model", Revision: strings.Repeat("a", 40), Files: []catalog.File{{Path: "nested/weights.safetensors", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(digest[:])}}}
	l, err := NewLive(LiveOptions{StateDir: filepath.Join(root, "state"), Target: "fixture", Environment: "dev", ReferenceRoot: root, ModelRoot: s.root, SourcePath: filepath.Join(root, "source.json"), ModelBudgetBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	l.stager = s
	l.findModel = func(id string) (catalog.Model, error) {
		if id != m.ID {
			return catalog.Model{}, os.ErrNotExist
		}
		return m, nil
	}
	ref, err := catalog.Import(root)
	if err != nil {
		t.Fatal(err)
	}
	c := defaultConfig()
	x := domain.Execution{ID: "recovery-model-0001", Actor: "owner", Plan: domain.Plan{Draft: domain.Draft{Action: "model.stage", Target: "fixture", Model: m.ID}, Desired: c, Preview: domain.Preview{Preconditions: map[string]string{"reference_revision": ref.SourceRevision}}}}
	return l, m, x
}

func saveRecoveryDispatch(t *testing.T, l *Live, x domain.Execution, r dispatch) {
	t.Helper()
	if r.Kind == "" {
		r.Kind = "local"
	}
	if r.Hash == "" {
		r.Hash = domain.Hash(x)
	}
	if r.Result.State == "" {
		r.Result = domain.Result{State: "running", Phase: "dispatch-intent"}
	}
	if err := saveJSON(filepath.Join(l.opts.StateDir, "live-dispatch"), x.ID+".json", r); err != nil {
		t.Fatal(err)
	}
}

func TestLiveInspectExecutionReconcilesLocalPublication(t *testing.T) {
	l, m, x := recoveryLive(t)
	// This was the baseline failure: a dispatched local stage reached the old
	// generic Inspect path and remained local-effect-uncertain despite a valid
	// publication. The permanent regression uses the original execution hash.
	if _, err := l.stager.Stage(context.Background(), m, nil); err != nil {
		t.Fatal(err)
	}
	saveRecoveryDispatch(t, l, x, dispatch{Action: "model.stage", SourceRevision: x.Plan.Desired.Revision})
	r, err := l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "succeeded" || r.Phase != "local-publication-verified" {
		t.Fatal(r, err)
	}
	prefix, _ := modelPrefix(m)
	modelFile := filepath.Join(l.stager.root, prefix, "nested", "weights.safetensors")
	if err = os.Chmod(modelFile, 0600); err != nil {
		t.Fatal(err)
	}
	r, err = l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "failed" || r.Phase != "local-publication-permissions-invalid" {
		t.Fatal(r, err)
	}
	if info, statErr := os.Stat(modelFile); statErr != nil || info.Mode().Perm() != 0600 {
		t.Fatal("read-only inspection changed publication mode", info, statErr)
	}
	if _, err = l.stager.Stage(context.Background(), m, nil); err != nil {
		t.Fatal("explicit repeat model.stage did not repair verified publication", err)
	}
	// Older dispatch records did not carry the redundant fields. Their original
	// immutable hash remains sufficient to bind the same execution.
	saveRecoveryDispatch(t, l, x, dispatch{})
	r, err = l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "succeeded" {
		t.Fatal(r, err)
	}
	bad := x
	bad.Plan.Draft.Action = "model.verify"
	if _, err = l.InspectExecution(context.Background(), bad); err == nil {
		t.Fatal("accepted a different original execution for the same dispatch record")
	}
}

func TestLiveInspectExecutionKeepsUncertainLocalEffectsFenced(t *testing.T) {
	l, m, x := recoveryLive(t)
	// An older API record can say dispatched while this adapter journal was never
	// written. Local evidence still permits only a no-publication failure here.
	r, err := l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "failed" || r.Phase != "local-interrupted-no-publication" {
		t.Fatal(r, err)
	}
	saveRecoveryDispatch(t, l, x, dispatch{Action: "model.stage", SourceRevision: x.Plan.Desired.Revision})
	r, err = l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "failed" || r.Phase != "local-interrupted-no-publication" {
		t.Fatal(r, err)
	}
	if _, err = l.stager.Stage(context.Background(), m, nil); err != nil {
		t.Fatal(err)
	}
	prefix, _ := modelPrefix(m)
	if err = os.WriteFile(filepath.Join(l.stager.root, prefix, ".bridge-receipt.json"), []byte("malformed"), 0640); err != nil {
		t.Fatal(err)
	}
	r, err = l.InspectExecution(context.Background(), x)
	if err != nil || !r.RecoveryRequired || r.Phase != "local-publication-uncertain" {
		t.Fatal(r, err)
	}
	drift := x
	drift.Plan.Preview.Preconditions["reference_revision"] = "sha256:changed"
	r, err = l.InspectExecution(context.Background(), drift)
	if err == nil || r.State != "" {
		t.Fatal("accepted an altered execution hash", r, err)
	}
	// Use the original binding and a changed reference precondition to prove the
	// local source-drift fence rather than permitting a stale recheck.
	x.Plan.Preview.Preconditions["reference_revision"] = "sha256:changed"
	saveRecoveryDispatch(t, l, x, dispatch{Action: "model.stage", SourceRevision: x.Plan.Desired.Revision})
	r, err = l.InspectExecution(context.Background(), x)
	if err != nil || !r.RecoveryRequired || r.Phase != "source-drift" {
		t.Fatal(r, err)
	}
}

func TestLiveInspectExecutionDoesNotRaceLocalWorkOrUseHost(t *testing.T) {
	l, _, x := recoveryLive(t)
	l.mu.Lock()
	l.active[x.ID] = true
	l.mu.Unlock()
	r, err := l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "running" || r.Phase != "local-execution-active" {
		t.Fatal(r, err)
	}
}

func TestLiveInspectExecutionUsesWorkerStatusOnly(t *testing.T) {
	l, _, x := recoveryLive(t)
	x.ID = "recovery-build-0001"
	x.Plan.Draft = domain.Draft{Action: "build.start", Target: "fixture", Recipe: "llama-hip"}
	socketDir, err := os.MkdirTemp("/tmp", "bridge-worker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "worker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/operations/"+x.ID {
			http.Error(w, "unexpected worker recovery path", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(domain.Result{State: "recovery-required", Phase: "worker-cgroup-unknown", RecoveryRequired: true})
	})}
	go server.Serve(listener)
	t.Cleanup(func() { _ = server.Close() })
	l.worker.Socket = socket
	saveRecoveryDispatch(t, l, x, dispatch{Kind: "worker", Action: "build.start", SourceRevision: x.Plan.Desired.Revision})
	r, err := l.InspectExecution(context.Background(), x)
	if err != nil || !r.RecoveryRequired || r.Phase != "worker-cgroup-unknown" {
		t.Fatal(r, err)
	}
}

func TestLiveInspectExecutionRefusesMissingWorkerJournal(t *testing.T) {
	l, _, x := recoveryLive(t)
	x.ID = "recovery-build-absent"
	x.Plan.Draft = domain.Draft{Action: "build.start", Target: "fixture", Recipe: "llama-hip"}
	if r, err := l.InspectExecution(context.Background(), x); err == nil || r.State != "" {
		t.Fatal("invented a worker completion", r, err)
	}
}

func TestLiveInspectExecutionReconcilesLocalCacheBudgetFromSource(t *testing.T) {
	l, _, x := recoveryLive(t)
	x.ID = "recovery-cache-0001"
	x.Plan.Draft = domain.Draft{Action: "caches.configure", Target: "fixture"}
	b, err := json.Marshal(x.Plan.Desired)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(l.opts.SourcePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	saveRecoveryDispatch(t, l, x, dispatch{Kind: "local", Action: "caches.configure", SourceRevision: x.Plan.Desired.Revision})
	r, err := l.InspectExecution(context.Background(), x)
	if err != nil || r.State != "succeeded" || r.Phase != "local-cache-budget-observed" {
		t.Fatal(r, err)
	}
}
