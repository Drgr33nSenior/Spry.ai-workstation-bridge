package adapters

import (
	"context"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
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
