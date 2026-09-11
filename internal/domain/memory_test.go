package domain

import (
	"strings"
	"testing"
)

func TestMemoryDraftOwnerOnlyAndBounds(t *testing.T) {
	c := Configuration{}
	c.Revision = c.ContentRevision()
	d := Draft{Action: "memory.plan.export", Target: "test", SourceRevision: c.Revision, Memory: &MemoryRequest{EvidenceID: "evidence-0001", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 8192}}
	inv := Inventory{Target: "test"}
	if e := ValidateDraft(d, c, inv); e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"viewer", "operator"} {
		if RoleAllows(role, d.Action) {
			t.Fatal("nonowner memory action")
		}
	}
	for _, edit := range []func(*Draft){func(d *Draft) { d.Memory = nil }, func(d *Draft) { d.Memory.EvidenceID = "../escape" }, func(d *Draft) { d.Memory.EvidenceSHA256 = "fake" }, func(d *Draft) { d.Memory.OtherMiB = -1 }, func(d *Draft) { d.Memory.OtherMiB = 1 << 31 }, func(d *Draft) { d.Resources = &Resources{} }, func(d *Draft) { d.RecoveryID = "unrelated" }, func(d *Draft) { d.Action = "resources.configure" }, func(d *Draft) { d.SourceRevision = "old" }} {
		x := d
		m := *d.Memory
		x.Memory = &m
		edit(&x)
		if e := ValidateDraft(x, c, inv); e == nil {
			t.Fatalf("invalid memory request accepted: %+v", x)
		}
	}
	next, _ := Desired(c, d)
	if next != c {
		t.Fatal("memory plan changed managed source")
	}
}
