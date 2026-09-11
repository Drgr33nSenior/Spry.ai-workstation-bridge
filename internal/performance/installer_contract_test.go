package performance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// This selected-pair source gate runs tiny deterministic inputs through the
// actual workstationctl dispatcher. It cannot qualify installed artifacts.
func TestCandidateInstallerPerformanceContract(t *testing.T) {
	root := os.Getenv("BRIDGE_INSTALLER_PERFORMANCE_CANDIDATE")
	if root == "" {
		t.Skip("NOT RUN — set BRIDGE_INSTALLER_PERFORMANCE_CANDIDATE to the exact reviewed installer source")
	}
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	manifest, err := os.ReadFile(filepath.Join(root, "infrastructure/packages/bootstrap/bridge-runtime.files"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range Tools {
		path := "lib/workstation/" + name
		if strings.Count("\n"+string(manifest), "\n"+path+"\n") != 1 {
			t.Fatal("runtime source closure", path)
		}
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("candidate source %s sha256=%s; not installed runtime approval", path, Sum(b))
	}
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(temp, "input"), filepath.Join(temp, "output")
	run := func(command string, args ...string) []byte {
		t.Helper()
		c := exec.CommandContext(ctx, command, args...)
		c.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("bounded source fixture failed: %v\n%s", err, b)
		}
		return b
	}
	run("python3", "-B", "-c", `import os,sys
from pathlib import Path
os.umask(0o077)
root,out=map(Path,sys.argv[1:3]);sys.path[:0]=[str(root/'tests/hardware'),str(root/'lib/workstation')]
from test_performance_bundle import coding_bundle
out.mkdir(mode=0o700);coding_bundle(out)
`, root, input)
	run("bash", filepath.Join(root, "bin/workstationctl"), "performance", "seal", input, "coding-eval", "fixture-target", strings.Repeat("a", 64))
	b, err := os.ReadFile(filepath.Join(input, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sha := Sum(b)
	if _, err = Verify(ctx, input, sha); err != nil {
		t.Fatal(err)
	}
	raw := run("bash", filepath.Join(root, "bin/workstationctl"), "performance", "inspect", input, sha)
	var summary domain.PerformanceSummary
	if err = config.Decode(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if err = ValidateSummary(summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status != "sealed-unanalysed" {
		t.Fatal("inspect invented measurements")
	}
	run("bash", filepath.Join(root, "bin/workstationctl"), "performance", "export", input, sha, output, "--target", "fixture-target", "--source-revision", strings.Repeat("a", 64), "--owner", "fixture-owner")
	raw, err = os.ReadFile(filepath.Join(output, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = config.Decode(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if err = ValidateSummary(summary); err != nil {
		t.Fatal(err)
	}
	if summary.Status != "incomplete-unqualified" || len(summary.Artifacts) == 0 || summary.SHA256 != sha {
		t.Fatalf("incorrect analysis status %+v", summary)
	}
	for _, a := range summary.Artifacts {
		b, err := os.ReadFile(filepath.Join(output, a.Name))
		if err != nil || Sum(b) != a.SHA256 || int64(len(b)) != a.Size {
			t.Fatal("artifact mismatch", err)
		}
	}
	for _, name := range []string{"test_performance_bundle.py", "test_performance_profiles.py", "test_coding_eval.py", "test_serving_runtime.py", "test_inference_cache.py"} {
		run("python3", "-B", "-m", "unittest", "discover", "-s", filepath.Join(root, "tests/hardware"), "-p", name)
	}
}
