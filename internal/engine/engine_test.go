package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/source"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type controlled struct {
	calls    atomic.Int32
	result   domain.Result
	inspect  domain.Result
	observed chan struct{}
	release  chan struct{}
}

func (a *controlled) Snapshot(context.Context) (domain.Inventory, error) {
	return domain.Inventory{Target: "fixture", Environment: "dev", Mode: "demo", SourceRevision: "reference"}, nil
}
func (a *controlled) Validate(context.Context, domain.Draft, domain.Configuration) (domain.Preview, error) {
	return domain.Preview{Preconditions: map[string]string{"revision": "reference"}}, nil
}
func (a *controlled) Execute(ctx context.Context, x domain.Execution, p func(domain.Progress) error) (domain.Result, error) {
	a.calls.Add(1)
	if a.observed != nil {
		close(a.observed)
	}
	if a.release != nil {
		select {
		case <-a.release:
		case <-ctx.Done():
			return domain.Result{State: "recovery-required", RecoveryRequired: true, Phase: "detached"}, nil
		}
	}
	if err := p(domain.Progress{Phase: "external-effect", Message: "fixture effect"}); err != nil {
		return domain.Result{}, err
	}
	return a.result, nil
}
func (a *controlled) Inspect(context.Context, string) (domain.Result, error) {
	if a.inspect.State == "" {
		return domain.Result{}, errors.New("unknown executor")
	}
	return a.inspect, nil
}
func (a *controlled) Export(context.Context, string) (domain.Bundle, error) {
	return domain.Bundle{}, nil
}
func makeEngine(t *testing.T, a *controlled) (*engine.Engine, auth.Actor, domain.Configuration) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0700)
	db, err := store.Open(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	src := source.New(filepath.Join(dir, "source.json"))
	c := domain.Configuration{Caches: domain.CacheBudgets{ModelsGiB: 128, CompilerGiB: 100, ShaderGiB: 16, BuildJobs: 8, BuildMemoryMiB: 32768, ScratchGiB: 64}}
	if err = src.Initialize(c); err != nil {
		t.Fatal(err)
	}
	c, _ = src.Read()
	cr, _, err := auth.NewCredential("fixture", "owner", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Update(func(s *store.State) error { s.Credentials[cr.ID] = cr; return nil }); err != nil {
		t.Fatal(err)
	}
	e := engine.New(db, src, a, "fixture", 4, time.Second)
	t.Cleanup(func() { _ = db.Close() })
	return e, auth.Actor{ID: cr.ID, Role: "owner", Name: "fixture"}, c
}
func plan(t *testing.T, e *engine.Engine, a auth.Actor, c domain.Configuration, profile string) domain.Plan {
	t.Helper()
	p, err := e.CreatePlan(context.Background(), a, domain.Draft{Action: "profile.switch", Target: "fixture", Profile: profile, SourceRevision: c.Revision})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func apply(t *testing.T, e *engine.Engine, a auth.Actor, p domain.Plan, key string) domain.Operation {
	t.Helper()
	o, err := e.Apply(a, p.ID, "fixture", key)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func start(t *testing.T, e *engine.Engine) {
	t.Helper()
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = e.Close(ctx)
	})
}
func wait(t *testing.T, e *engine.Engine, id string) domain.Operation {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		o, err := e.Operation(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(o.State) {
			return o
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("operation did not finish")
	return domain.Operation{}
}
func TestQueuedWorkFencedAfterUncertainEffect(t *testing.T) {
	a := &controlled{result: domain.Result{State: "recovery-required", RecoveryRequired: true, Phase: "uncertain"}}
	e, owner, c := makeEngine(t, a)
	first := apply(t, e, owner, plan(t, e, owner, c, "ai"), "first-queued")
	second := apply(t, e, owner, plan(t, e, owner, c, "gaming"), "second-queued")
	start(t, e)
	if !wait(t, e, first.ID).RecoveryRequired {
		t.Fatal("lost uncertain result")
	}
	o := wait(t, e, second.ID)
	if o.Phase != "blocked-by-recovery" || a.calls.Load() != 1 {
		t.Fatalf("queued work crossed recovery fence: %+v calls=%d", o, a.calls.Load())
	}
}
func TestPoisonedPersistenceStopsDispatch(t *testing.T) {
	a := &controlled{result: domain.Result{State: "succeeded"}}
	e, owner, c := makeEngine(t, a)
	o := apply(t, e, owner, plan(t, e, owner, c, "ai"), "storage-fault")
	var writes atomic.Int32
	e.DB.Write = func(string, []byte, os.FileMode) error { writes.Add(1); return errors.New("disk-full fixture") }
	start(t, e)
	time.Sleep(40 * time.Millisecond)
	if writes.Load() != 1 || a.calls.Load() != 0 {
		t.Fatal("failed storage caused dispatch or busy retry")
	}
	got, _ := e.Operation(o.ID)
	if got.State != "queued" {
		t.Fatal("uncommitted state was acknowledged")
	}
}
func TestCancellationDoesNotInventStoppedProcess(t *testing.T) {
	a := &controlled{observed: make(chan struct{}), release: make(chan struct{}), result: domain.Result{State: "succeeded"}}
	e, owner, c := makeEngine(t, a)
	o := apply(t, e, owner, plan(t, e, owner, c, "ai"), "cancel-running")
	start(t, e)
	<-a.observed
	current, _ := e.Operation(o.ID)
	if _, err := e.Cancel(owner, o.ID, current.Revision+1); err == nil {
		t.Fatal("stale cancellation revision accepted")
	}
	if _, err := e.Cancel(owner, o.ID, current.Revision); err != nil {
		t.Fatal(err)
	}
	got := wait(t, e, o.ID)
	if got.State != "recovery-required" || !got.CancelRequested {
		t.Fatalf("cancel falsely proved stopped %+v", got)
	}
}
func TestRestartInspectsWithoutRedispatch(t *testing.T) {
	for _, state := range []string{"succeeded", "running", ""} {
		t.Run(state, func(t *testing.T) {
			a := &controlled{inspect: domain.Result{State: state}}
			e, owner, c := makeEngine(t, a)
			o := apply(t, e, owner, plan(t, e, owner, c, "ai"), "restart-inspect")
			if err := e.DB.Update(func(s *store.State) error {
				v := s.Operations[o.ID]
				v.State = "running"
				v.Phase = "dispatch-intent"
				s.Operations[o.ID] = v
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			start(t, e)
			got := wait(t, e, o.ID)
			if a.calls.Load() != 0 {
				t.Fatal("restart replayed side effect")
			}
			if state == "succeeded" && got.State != "succeeded" {
				t.Fatal("proven helper completion lost")
			}
			if state != "succeeded" && !got.RecoveryRequired {
				t.Fatal("unknown/running result became success")
			}
		})
	}
}
func TestSourceUpdateCrashHasNoExternalDispatch(t *testing.T) {
	a := &controlled{result: domain.Result{State: "succeeded"}}
	e, owner, c := makeEngine(t, a)
	budgets := c.Caches
	budgets.ModelsGiB++
	p, err := e.CreatePlan(context.Background(), owner, domain.Draft{Action: "caches.configure", Target: "fixture", SourceRevision: c.Revision, Caches: &budgets})
	if err != nil {
		t.Fatal(err)
	}
	o := apply(t, e, owner, p, "source-crash")
	e.Source.Write = func(path string, b []byte, m os.FileMode) error {
		if err := safefile.Replace(path, b, m); err != nil {
			return err
		}
		return errors.New("crash after source rename")
	}
	start(t, e)
	got := wait(t, e, o.ID)
	if a.calls.Load() != 0 || !got.RecoveryRequired || got.LiveApplied {
		t.Fatal("source crash invented external success")
	}
	actual, err := e.Source.Read()
	if err != nil || actual.Caches.ModelsGiB != budgets.ModelsGiB {
		t.Fatal("fixture failed to cross source boundary")
	}
	recovery, err := e.Recover(context.Background(), owner, o.ID)
	if err != nil || recovery.Draft.Action != "operation.reconcile" {
		t.Fatalf("source-only recovery unavailable: %v %+v", err, recovery)
	}
	resolved := apply(t, e, owner, recovery, "source-reconcile")
	if got := wait(t, e, resolved.ID); got.State != "succeeded" || got.LiveApplied || a.calls.Load() != 0 {
		t.Fatalf("source reconciliation dispatched external work: %+v", got)
	}
	previous, _ := e.Operation(o.ID)
	if previous.State != "failed" || previous.RecoveryRequired || !previous.SourceUpdated {
		t.Fatalf("source outcome not accurately reconciled: %+v", previous)
	}
}
func TestExpiredQueuedIdentityRefused(t *testing.T) {
	a := &controlled{result: domain.Result{State: "succeeded"}}
	e, owner, c := makeEngine(t, a)
	o := apply(t, e, owner, plan(t, e, owner, c, "ai"), "revoked-queue")
	if err := e.DB.Update(func(s *store.State) error {
		c := s.Credentials[owner.ID]
		c.Revoked = true
		s.Credentials[owner.ID] = c
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	start(t, e)
	got := wait(t, e, o.ID)
	if got.Phase != "authorization-or-plan-expired" || a.calls.Load() != 0 {
		t.Fatal("revoked queued identity dispatched")
	}
}
