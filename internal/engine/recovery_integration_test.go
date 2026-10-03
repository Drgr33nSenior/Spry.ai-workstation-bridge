package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func seedRecovery(t *testing.T, e *engine.Engine, owner auth.Actor, c domain.Configuration, action, parent string, dispatched bool) string {
	t.Helper()
	id := store.ID()
	p := domain.Plan{Draft: domain.Draft{Action: action, Target: "fixture", SourceRevision: c.Revision, RecoveryID: parent}, Desired: c}
	p.Hash = domain.Hash(p)
	if err := e.DB.Update(func(s *store.State) error {
		s.Operations[id] = domain.Operation{ID: id, Actor: owner.ID, Dispatched: dispatched, RecoveryRequired: true, State: "recovery-required", UpdatedAt: time.Now(), Plan: p}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReconciliationRequiresExecutorEvidence(t *testing.T) {
	for _, state := range []string{"", "running", "queued", "recovery-required", "failed", "succeeded", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			a := &controlled{inspect: domain.Result{State: state}}
			e, owner, c := makeEngine(t, a)
			id := seedRecovery(t, e, owner, c, "model.stage", "", true)
			d := domain.Draft{Action: "profile.restore", Target: "fixture", SourceRevision: c.Revision, RecoveryID: id}
			if _, err := e.CreatePlan(context.Background(), owner, d); err == nil {
				t.Fatal("manual local-to-host recovery accepted")
			}
			d.Action = "operation.reconcile"
			p, err := e.CreatePlan(context.Background(), owner, d)
			if state != "failed" && state != "succeeded" && state != "cancelled" {
				if err == nil {
					t.Fatal("source configuration dismissed unknown external effects")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			o := apply(t, e, owner, p, "explicit-local-reconcile")
			start(t, e)
			if got := wait(t, e, o.ID); got.State != "succeeded" || got.Dispatched || a.calls.Load() != 0 {
				t.Fatalf("reconciliation redispatched: %+v", got)
			}
			old, _ := e.Operation(id)
			if old.State != "failed" || old.RecoveryRequired {
				t.Fatal("historical failure was lost")
			}
		})
	}
}

func TestRecoveryCompetingRetryAndUnrelatedFence(t *testing.T) {
	a := &controlled{}
	e, owner, c := makeEngine(t, a)
	id := seedRecovery(t, e, owner, c, "profile.switch", "", true)
	d := domain.Draft{Action: "profile.restore", Target: "fixture", SourceRevision: c.Revision, RecoveryID: id}
	p, err := e.CreatePlan(context.Background(), owner, d)
	if err != nil {
		t.Fatal(err)
	}
	first := apply(t, e, owner, p, "first-chain-retry")
	if _, err := e.Apply(owner, p.ID, "fixture", "competing-chain-retry"); err == nil {
		t.Fatal("competing retry queued")
	}
	if _, err := e.Cancel(owner, first.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	other := seedRecovery(t, e, owner, c, "profile.switch", "", true)
	if _, err := e.Apply(owner, p.ID, "fixture", "unrelated-fence-retry"); err == nil {
		t.Fatal("unrelated GPU fence crossed")
	}
	for _, id := range []string{id, other} {
		if o, _ := e.Operation(id); !o.RecoveryRequired {
			t.Fatal("fence cleared without effects")
		}
	}
}

func TestRecoveryCannotChangePersistedTarget(t *testing.T) {
	a := &controlled{}
	e, owner, c := makeEngine(t, a)
	id := seedRecovery(t, e, owner, c, "profile.switch", "", true)
	if err := e.DB.Update(func(s *store.State) error {
		o := s.Operations[id]
		o.Plan.Draft.Target = "another-workstation"
		s.Operations[id] = o
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Recover(context.Background(), owner, id); err == nil {
		t.Fatal("recovery moved external effects to another target")
	}
}

func TestRecoverySettlementPersistenceFault(t *testing.T) {
	for _, afterRename := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-rename", true: "after-rename"}[afterRename], func(t *testing.T) {
			a := &controlled{result: domain.Result{State: "succeeded"}, inspect: domain.Result{State: "succeeded"}}
			e, owner, c := makeEngine(t, a)
			first := seedRecovery(t, e, owner, c, "profile.switch", "", true)
			second := seedRecovery(t, e, owner, c, "profile.restore", first, true)
			p, err := e.CreatePlan(context.Background(), owner, domain.Draft{Action: "profile.restore", Target: "fixture", SourceRevision: c.Revision, RecoveryID: second})
			if err != nil {
				t.Fatal(err)
			}
			third := apply(t, e, owner, p, "durable-chain-settlement")
			e.DB.Write = func(path string, b []byte, m os.FileMode) error {
				var state store.State
				if err := json.Unmarshal(b, &state); err != nil {
					return err
				}
				if state.Operations[third.ID].State == "succeeded" {
					if afterRename {
						if err := safefile.Replace(path, b, m); err != nil {
							return err
						}
					}
					return errors.New("injected recovery settlement persistence fault")
				}
				return safefile.Replace(path, b, m)
			}
			start(t, e)
			deadline := time.Now().Add(3 * time.Second)
			for e.DB.Healthy() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if e.DB.Healthy() {
				t.Fatal("fault boundary not reached")
			}
			for _, id := range []string{first, second} {
				if o, _ := e.Operation(id); !o.RecoveryRequired {
					t.Fatal("uncommitted settlement acknowledged")
				}
			}
			e = restart(t, e, a)
			for _, id := range []string{first, second} {
				if o, _ := e.Operation(id); o.RecoveryRequired || o.State != "failed" {
					t.Fatal("durable restart settlement failed")
				}
			}
			if a.calls.Load() != 1 {
				t.Fatal("restart redispatched restoration")
			}
		})
	}
}

func TestPredispatchRecoveryDoesNotTrustExecutorOrSourceDrift(t *testing.T) {
	a := &controlled{inspect: domain.Result{State: "succeeded"}}
	e, owner, c := makeEngine(t, a)
	id := seedRecovery(t, e, owner, c, "serving.configure", "", false)
	p, err := e.Recover(context.Background(), owner, id)
	if err != nil || p.Draft.Action != "operation.reconcile" {
		t.Fatal("predispatch request trusted unrelated executor result")
	}
	next := c
	next.Caches.ModelsGiB++
	if err := e.Source.Update(c.Revision, next); err != nil {
		t.Fatal(err)
	}
	start(t, e)
	o := apply(t, e, owner, p, "source-drift-recovery")
	if got := wait(t, e, o.ID); got.State != "failed" || got.Dispatched {
		t.Fatal("source drift caused external effect")
	}
	if original, _ := e.Operation(id); !original.RecoveryRequired {
		t.Fatal("source drift cleared fence")
	}
}
