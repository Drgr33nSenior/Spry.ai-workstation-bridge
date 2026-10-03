package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func TestPerformanceExportDriftIdempotencyAndIndependentReconciliation(t *testing.T) {
	a := &controlled{result: domain.Result{State: "recovery-required", RecoveryRequired: true}}
	e, owner, c := makeEngine(t, a)
	e.Adapter = memoryNoInventory{a}
	d := domain.Draft{Action: "performance.export", Target: e.Target, SourceRevision: c.Revision, Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "comparison"}}
	p, err := e.CreatePlan(context.Background(), owner, d)
	if err != nil {
		t.Fatal(err)
	}
	if a.calls.Load() != 0 || p.Desired.Revision != c.Revision {
		t.Fatal("planning executed or changed configuration")
	}
	o := apply(t, e, owner, p, "performance-idempotency")
	duplicate, err := e.Apply(owner, p.ID, e.Target, "performance-idempotency")
	if err != nil || duplicate.ID != o.ID {
		t.Fatal("duplicate", err)
	}
	start(t, e)
	o = wait(t, e, o.ID)
	if o.RecoveryRequired || o.SourceUpdated || o.LiveApplied || o.State != "failed" {
		t.Fatalf("read-only mutation state: %+v", o)
	}
	a.inspect = domain.Result{State: "succeeded", Phase: "retained-analysis"}
	settled, err := e.InspectPerformance(context.Background(), owner, o.ID)
	if err != nil || settled.State != "succeeded" || a.calls.Load() != 1 {
		t.Fatal("reconciliation redispatched", err)
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
		s.Plans[p.ID] = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(owner, p.ID, e.Target, "performance-expired"); err == nil {
		t.Fatal("expired plan")
	}
	d.SourceRevision = "changed"
	if _, err = e.CreatePlan(context.Background(), owner, d); err == nil {
		t.Fatal("drift")
	}
}
