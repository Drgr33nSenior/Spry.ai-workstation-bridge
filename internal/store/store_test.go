package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

func TestPruningRetainsRecoveryAncestors(t *testing.T) {
	s := NewState("demo")
	s.Operations["A"] = domain.Operation{ID: "A", State: "failed", UpdatedAt: time.Now().Add(-40 * 24 * time.Hour)}
	s.Operations["B"] = domain.Operation{ID: "B", State: "recovery-required", RecoveryRequired: true, Plan: domain.Plan{Draft: domain.Draft{RecoveryID: "A"}}}
	prune(&s)
	if _, ok := s.Operations["A"]; !ok {
		t.Fatal("pruning destroyed recovery linkage")
	}
}

func TestTelemetrySnapshotCountsAndPersistenceFailure(t *testing.T) {
	s, err := Open(dir(t), "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Update(func(v *State) error {
		for _, state := range []string{"queued", "running", "cancel-requested", "recovery-required", "failed", "cancelled", "succeeded"} {
			v.Operations[state] = domain.Operation{State: state, UpdatedAt: time.Now(), RecoveryRequired: state == "recovery-required" || state == "failed"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := domain.TelemetryOperations{Queued: 1, Running: 2, RecoveryRequired: 2}
	if got, healthy := s.TelemetrySnapshot(); got != want || !healthy {
		t.Fatalf("got %+v healthy=%v", got, healthy)
	}
	s.Write = func(string, []byte, os.FileMode) error { return errors.New("fixture disk full") }
	if err = s.Update(func(v *State) error { v.Operations = map[string]domain.Operation{}; return nil }); err == nil {
		t.Fatal("storage fault accepted")
	}
	if got, healthy := s.TelemetrySnapshot(); got != want || healthy {
		t.Fatalf("invented state after failed persistence: %+v healthy=%v", got, healthy)
	}
	if allocs := testing.AllocsPerRun(5, func() { s.TelemetrySnapshot() }); allocs != 0 {
		t.Fatalf("snapshot allocated: %v", allocs)
	}
}

func dir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestExclusiveStoreAndMode(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Open(p, "demo"); e == nil {
		other.Close()
		t.Fatal("concurrent store admitted")
	}
	if e = s.Update(func(v *State) error { Event(v, "fixture", "write", "id", "ok"); return nil }); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if other, e := Open(p, "live"); e == nil {
		other.Close()
		t.Fatal("demo credentials crossed into live state")
	}
	s, e = Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if len(s.View().Audit) != 1 {
		t.Fatal("durable record lost")
	}
}
func TestDiskFullPoisonsMutationWithoutInventedSuccess(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Write = func(string, []byte, os.FileMode) error { return errors.New("disk full injected") }
	if s.Update(func(v *State) error { Event(v, "test", "effect", "x", "success"); return nil }) == nil {
		t.Fatal("failed storage acknowledged")
	}
	if s.Healthy() || len(s.View().Audit) != 0 {
		t.Fatal("failed commit became authoritative")
	}
	s.Write = safefile.Replace
	if s.Update(func(v *State) error { return nil }) == nil {
		t.Fatal("failed store resumed mutation without recovery")
	}
}
func TestCrashAfterRenameRequiresRestartRead(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	s.Write = func(path string, b []byte, m os.FileMode) error {
		if e := safefile.Replace(path, b, m); e != nil {
			return e
		}
		return errors.New("injected lost directory sync result")
	}
	if s.Update(func(v *State) error { Event(v, "test", "intent", "x", "persisted"); return nil }) == nil {
		t.Fatal("uncertain persistence accepted")
	}
	s.Close()
	s, e = Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if len(s.View().Audit) != 1 {
		t.Fatal("reopen failed to inspect durable reality")
	}
}
