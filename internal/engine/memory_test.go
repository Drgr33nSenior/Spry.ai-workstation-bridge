package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type memoryNoInventory struct{ *controlled }

func (a memoryNoInventory) Snapshot(context.Context) (domain.Inventory, error) {
	return domain.Inventory{}, errors.New("cluster unavailable")
}

func TestMemoryPreflightDoesNotRequireGenericInventory(t *testing.T) {
	a := &controlled{}
	e, owner, c := makeEngine(t, a)
	e.Adapter = memoryNoInventory{a}
	d := domain.Draft{Action: "memory.evidence.import", Target: e.Target, SourceRevision: c.Revision, Memory: &domain.MemoryRequest{EvidenceID: "fixture-evidence", EvidenceSHA256: strings.Repeat("a", 64)}}
	if _, err := e.CreatePlan(context.Background(), owner, d); err != nil {
		t.Fatal("read-only intake depended on generic cluster inventory", err)
	}
}

func TestMemoryIdempotencyDriftAndReadOnlyReconciliation(t *testing.T) {
	a := &controlled{result: domain.Result{State: "recovery-required", RecoveryRequired: true}}
	e, owner, c := makeEngine(t, a)
	d := domain.Draft{Action: "memory.plan.export", Target: "fixture", SourceRevision: c.Revision, Memory: &domain.MemoryRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 8192}}
	p, err := e.CreatePlan(context.Background(), owner, d)
	if err != nil {
		t.Fatal(err)
	}
	if a.calls.Load() != 0 {
		t.Fatal("planning dispatched")
	}
	o := apply(t, e, owner, p, "memory-idempotency")
	duplicate, err := e.Apply(owner, p.ID, "fixture", "memory-idempotency")
	if err != nil || duplicate.ID != o.ID {
		t.Fatal("idempotency failed")
	}
	start(t, e)
	o = wait(t, e, o.ID)
	if o.RecoveryRequired || o.SourceUpdated || o.LiveApplied || o.State != "failed" {
		t.Fatalf("read-only got mutation semantics %+v", o)
	}
	a.inspect = domain.Result{State: "succeeded", Phase: "read-only-completed"}
	settled, err := e.InspectMemory(context.Background(), owner, o.ID)
	if err != nil || settled.State != "succeeded" || a.calls.Load() != 1 {
		t.Fatal("helper completion lost or redispatched", err)
	}
	p, err = e.CreatePlan(context.Background(), owner, d)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.DB.Update(func(s *store.State) error {
		x := s.Plans[p.ID]
		x.ExpiresAt = time.Now().Add(-time.Minute)
		x.Hash = ""
		x.Hash = domain.Hash(x)
		s.Plans[x.ID] = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(owner, p.ID, "fixture", "memory-expired"); err == nil {
		t.Fatal("stale memory plan applied")
	}
	d.SourceRevision = "changed"
	if _, err = e.CreatePlan(context.Background(), owner, d); err == nil {
		t.Fatal("source drift accepted")
	}
}
