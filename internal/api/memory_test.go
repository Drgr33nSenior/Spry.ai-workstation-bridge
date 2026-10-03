package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestMemoryOwnerContractNoAutomaticApply(t *testing.T) {
	f := setup(t)
	d := domain.Draft{Action: "memory.plan.export", Target: "demo-workstation", SourceRevision: f.conf.Revision, Memory: &domain.MemoryRequest{EvidenceID: "demo-complete", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 8192}}
	for _, role := range []string{"viewer", "operator"} {
		for _, path := range []string{"/api/v1/memory/preview", "/api/v1/memory/advice", "/api/v1/memory/artifact", "/api/v1/memory/inspect"} {
			status, _, _ := f.request(t, "POST", path, role, d, nil)
			if status != 403 {
				t.Fatalf("%s %s status %d", role, path, status)
			}
		}
		status, _, _ := f.request(t, "GET", "/api/v1/memory", role, nil, nil)
		if status != 403 {
			t.Fatal(status)
		}
	}
	status, _, _ := f.request(t, "POST", "/api/v1/memory/preview", "owner", `{"action":"memory.plan.export","unknown":true}`, nil)
	if status != 400 {
		t.Fatal("unknown field not refused", status)
	}
	status, b, _ := f.request(t, "POST", "/api/v1/plans", "owner", d, nil)
	if status != 201 {
		t.Fatalf("plan %d %s", status, b)
	}
	var p domain.Plan
	_ = json.Unmarshal(b, &p)
	if p.Desired.Revision != f.conf.Revision || len(p.Preview.Changes) != 0 || len(f.eng.Operations()) != 0 {
		t.Fatal("plan implicitly applied")
	}
	status, b, _ = f.request(t, "POST", "/api/v1/operations", "owner", map[string]string{"plan_id": p.ID, "target": d.Target}, map[string]string{"Idempotency-Key": "memory-test-001"})
	if status != 202 {
		t.Fatalf("apply %d %s", status, b)
	}
	var o domain.Operation
	_ = json.Unmarshal(b, &o)
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		o, _ = f.eng.Operation(o.ID)
		if domain.Terminal(o.State) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if o.State != "succeeded" || o.SourceUpdated || o.LiveApplied || o.RecoveryRequired {
		t.Fatalf("invalid export outcome %+v", o)
	}
	current, _ := f.eng.Source.Read()
	if current.Revision != f.conf.Revision {
		t.Fatal("source modified")
	}
	for _, id := range []string{"demo-incomplete", "demo-refused"} {
		d.Memory.EvidenceID = id
		status, _, _ := f.request(t, "POST", "/api/v1/plans", "owner", d, nil)
		if status != 400 {
			t.Fatalf("%s accepted %d", id, status)
		}
	}
	status, _, _ = f.request(t, "POST", "/api/v1/memory/advice", "owner", *d.Memory, nil)
	if status != 503 {
		t.Fatal("demo provider contacted or silently fell back", status)
	}
}
