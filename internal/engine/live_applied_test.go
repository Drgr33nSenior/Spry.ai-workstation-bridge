package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func TestFinishLiveAppliedMatchesHostRestoreRequirement(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := &Engine{DB: db}
	for _, tc := range []struct {
		action string
		state  string
		want   bool
	}{
		{"profile.switch", "succeeded", true},
		{"profile.restore", "succeeded", true},
		{"serving.configure", "succeeded", true},
		{"resources.configure", "succeeded", true},
		{"serving.start", "succeeded", true},
		{"serving.stop", "succeeded", true},
		{"serving.restart", "succeeded", true},
		{"model.stage", "succeeded", false},
		{"model.verify", "succeeded", false},
		{"build.start", "succeeded", false},
		{"hardware.refresh", "succeeded", false},
		{"memory.plan.export", "succeeded", false},
		{"performance.export", "succeeded", false},
		{"operation.reconcile", "succeeded", false},
		{"serving.start", "failed", false},
		{"serving.start", "cancelled", false},
		{"serving.start", "recovery-required", false},
	} {
		t.Run(tc.action+"/"+tc.state, func(t *testing.T) {
			id := store.ID()
			if err := db.Update(func(s *store.State) error {
				s.Operations[id] = domain.Operation{ID: id, State: "running", Plan: domain.Plan{Draft: domain.Draft{Action: tc.action}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := e.finish(id, domain.Result{State: tc.state}); err != nil {
				t.Fatal(err)
			}
			op, ok := db.Operation(id)
			if !ok {
				t.Fatal("finished operation missing")
			}
			if op.LiveApplied != tc.want {
				t.Fatalf("live_applied=%v want=%v", op.LiveApplied, tc.want)
			}
		})
	}
}
