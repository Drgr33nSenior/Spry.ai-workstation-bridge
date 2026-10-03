package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestScopedSnapshotsAreDetached(t *testing.T) {
	s := &Store{state: NewState("demo")}
	s.state.Operations["operation"] = domain.Operation{
		ID:        "operation",
		Plan:      domain.Plan{Preview: domain.Preview{Preconditions: map[string]string{"source": "original"}}},
		Events:    []domain.Progress{{Phase: "original"}},
		Artifacts: []domain.Artifact{{Name: "original"}},
	}
	s.state.Plans["plan"] = domain.Plan{ID: "plan", Preview: domain.Preview{Preconditions: map[string]string{"target": "original"}}}
	s.state.Audit = []Audit{{Actor: "original"}}

	op, ok := s.Operation("operation")
	if !ok {
		t.Fatal("operation missing")
	}
	op.Plan.Preview.Preconditions["source"] = "changed"
	op.Events[0].Phase = "changed"
	op.Artifacts[0].Name = "changed"
	fresh, _ := s.Operation("operation")
	if fresh.Plan.Preview.Preconditions["source"] != "original" || fresh.Events[0].Phase != "original" || fresh.Artifacts[0].Name != "original" {
		t.Fatalf("operation snapshot shared state: %+v", fresh)
	}
	records := s.OperationRecords()
	records["operation"] = domain.Operation{ID: "replacement"}
	if fresh, _ := s.Operation("operation"); fresh.ID != "operation" {
		t.Fatalf("operation record map shared state: %+v", fresh)
	}

	p, ok := s.Plan("plan")
	if !ok {
		t.Fatal("plan missing")
	}
	p.Preview.Preconditions["target"] = "changed"
	freshPlan, _ := s.Plan("plan")
	if freshPlan.Preview.Preconditions["target"] != "original" {
		t.Fatalf("plan snapshot shared state: %+v", freshPlan)
	}

	audit := s.AuditRecords()
	audit[0].Actor = "changed"
	if got := s.AuditRecords()[0].Actor; got != "original" {
		t.Fatalf("audit snapshot shared state: %q", got)
	}
}

func TestAuditRecordsPreserveNilAndEmptyJSONShape(t *testing.T) {
	s := &Store{state: NewState("demo")}
	for _, tc := range []struct {
		name  string
		audit []Audit
		want  string
	}{
		{name: "empty", audit: []Audit{}, want: "[]"},
		{name: "nil", audit: nil, want: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.state.Audit = tc.audit
			got, err := json.Marshal(s.AuditRecords())
			if err != nil || string(got) != tc.want {
				t.Fatalf("audit JSON = %q, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestScopedSnapshotsRejectUnknownIDs(t *testing.T) {
	s := &Store{state: NewState("demo")}
	if _, ok := s.Operation("missing"); ok {
		t.Fatal("unknown operation returned")
	}
	if _, ok := s.Plan("missing"); ok {
		t.Fatal("unknown plan returned")
	}
}

func TestOldestQueuedOperationSelection(t *testing.T) {
	s := &Store{state: NewState("demo")}
	now := time.Now().UTC()
	s.state.Operations["current"] = domain.Operation{ID: "current", State: "running", CreatedAt: now.Add(-time.Hour)}
	s.state.Operations["oldest"] = domain.Operation{ID: "oldest", State: "queued", CreatedAt: now.Add(-time.Minute), Events: []domain.Progress{{Phase: "original"}}}
	s.state.Operations["newer"] = domain.Operation{ID: "newer", State: "queued", CreatedAt: now}

	op, ok := s.OldestQueuedOperation()
	if !ok || op.ID != "oldest" {
		t.Fatalf("got %+v, want oldest queued operation", op)
	}
	op.Events[0].Phase = "changed"
	again, _ := s.Operation("oldest")
	if again.Events[0].Phase != "original" {
		t.Fatal("oldest queued read was not detached")
	}

	delete(s.state.Operations, "oldest")
	s.state.Operations["tie-a"] = domain.Operation{ID: "tie-a", State: "queued", CreatedAt: now}
	s.state.Operations["tie-b"] = domain.Operation{ID: "tie-b", State: "queued", CreatedAt: now}
	op, ok = s.OldestQueuedOperation()
	if !ok || (op.ID != "newer" && op.ID != "tie-a" && op.ID != "tie-b") {
		t.Fatalf("equal-time queue selection changed: %+v", op)
	}
}

func TestScopedOperationReadAllocationDoesNotScaleWithHistory(t *testing.T) {
	small := retainedFixtureSize(1)
	large := retainedFixtureSize(500)
	smallAllocs := testing.AllocsPerRun(10, func() { _, _ = small.Operation("operation-000") })
	largeAllocs := testing.AllocsPerRun(10, func() { _, _ = large.Operation("operation-000") })
	// encoding/json uses sync.Pool, whose race-enabled implementation randomly
	// drops cached entries. Test growth across 500 histories, not an exact pool
	// hit count: a full-store copy grows by hundreds of times, not less than two.
	if largeAllocs > 2*smallAllocs {
		t.Fatalf("point read allocations scaled with unrelated history: small=%v large=%v", smallAllocs, largeAllocs)
	}
}
