package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestPrivateOutputDurabilityAndExactTree(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plan.json", "patch.json", "rollback.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := SyncOutput(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "patch.json"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := SyncOutput(root); err == nil {
		t.Fatal("nonprivate output accepted")
	}
	if err := os.Chmod(filepath.Join(root, "patch.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "rollback.json"), filepath.Join(root, "rollback.retained")); err != nil {
		t.Fatal(err)
	}
	if err := SyncOutput(root); err == nil {
		t.Fatal("missing rollback/unexpected output accepted")
	}
	if err := os.Symlink(filepath.Join(root, "rollback.retained"), filepath.Join(root, "rollback.json")); err != nil {
		t.Fatal(err)
	}
	if err := SyncOutput(root); err == nil {
		t.Fatal("symlink output accepted")
	}
	if b, err := os.ReadFile(filepath.Join(root, "rollback.retained")); err != nil || string(b) != "{}" {
		t.Fatal("failure changed evidence")
	}
}

func TestPrivateArtifactEscapedWireBound(t *testing.T) {
	a := domain.Artifact{Name: "patch.json"}
	if err := checkArtifactWireSize(a, []byte(`[{"op":"test"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := checkArtifactWireSize(a, []byte(strings.Repeat("<", 1<<20))); err == nil {
		t.Fatal("JSON expansion can exceed helper transport after recording success")
	}
}
