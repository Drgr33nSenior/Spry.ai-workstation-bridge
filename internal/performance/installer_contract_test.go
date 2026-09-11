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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("candidate contract requires Python 3", err)
	}
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
	run := func(t *testing.T, command string, args ...string) []byte {
		t.Helper()
		c := exec.CommandContext(ctx, command, args...)
		c.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "HOME_LAB_PYTHON="+python)
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("bounded source fixture failed: %v\n%s", err, b)
		}
		return b
	}
	for _, tc := range []struct{ kind, variant, status string }{
		{"coding-eval", "normal", "incomplete-unqualified"},
		{"coding-eval", "refused", "failed"},
		{"comparison", "normal", "comparison-not-qualified"},
		{"comparison", "incomplete", "comparison-not-qualified"},
		{"comparison", "refused", "failed"},
		{"profile-selection", "normal", "selected-unqualified"},
		{"profile-selection", "refused", "failed"},
		{"profile-status", "normal", "current-unqualified"},
		{"profile-status", "stale", "stale"},
		{"profile-status", "stale-weights", "stale"},
		{"profile-status", "stale-resources", "stale"},
		{"profile-status", "legacy-selection", "unknown"},
		{"profile-status", "legacy-observation", "unknown"},
		{"profile-status", "malformed-conditions", "unknown"},
		{"profile-status", "unknown", "unknown"},
		{"profile-status", "refused", "failed"},
		{"loading", "normal", "plan-only-unqualified"},
		{"loading", "unsupported", "failed"},
		{"loading", "refused", "failed"},
		{"queue", "normal", "plan-only-unqualified"},
		{"queue", "unsupported", "failed"},
		{"queue", "refused", "failed"},
		{"warm-status", "normal", "healthy"},
		{"warm-status", "unknown", "unknown"},
		{"warm-status", "refused", "failed"},
		{"cache", "normal", "plan-only-no-prune-required"},
		{"cache", "incomplete", "blocked-insufficient-disposable-space"},
		{"cache", "refused", "failed"},
	} {
		t.Run(tc.kind+"/"+tc.variant, func(t *testing.T) {
			temp, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			input, output := filepath.Join(temp, "input"), filepath.Join(temp, "output")
			run(t, "python3", "-B", "-c", `import os,sys,re
from pathlib import Path
os.umask(0o077)
root,out=map(Path,sys.argv[1:3]);sys.path[:0]=[str(root/'tests/hardware'),str(root/'lib/workstation')]
from test_performance_bundle import candidate_bundle
out.mkdir(mode=0o700);reserve=candidate_bundle(out,sys.argv[3],sys.argv[4])
config=(root/'config/workstation.conf.example').read_text()
config=re.sub(r'^INFERENCE_CACHE_FREE_RESERVE_MIB=.*$', 'INFERENCE_CACHE_FREE_RESERVE_MIB='+str(reserve), config, flags=re.M)
(out.parent/'fixture.conf').write_text(config)
`, root, input, tc.kind, tc.variant)
			run(t, "bash", filepath.Join(root, "bin/workstationctl"), "performance", "seal", input, tc.kind, "fixture-target", strings.Repeat("a", 64))
			b, err := os.ReadFile(filepath.Join(input, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			sha := Sum(b)
			if _, err = Verify(ctx, input, sha); err != nil {
				t.Fatal(err)
			}
			raw := run(t, "bash", filepath.Join(root, "bin/workstationctl"), "performance", "inspect", input, sha)
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
			run(t, "bash", filepath.Join(root, "bin/workstationctl"), "--config", filepath.Join(temp, "fixture.conf"), "performance", "export", input, sha, output, "--target", "fixture-target", "--source-revision", strings.Repeat("a", 64), "--owner", "fixture-owner")
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
			if summary.Status != tc.status || summary.Kind != tc.kind || len(summary.Artifacts) == 0 || summary.SHA256 != sha {
				t.Fatalf("incorrect analysis status %+v", summary)
			}
			if tc.variant == "incomplete" && tc.kind == "comparison" && !strings.Contains(summary.Reason, "inconclusive") {
				t.Fatal("incomplete comparison invented eligibility", summary.Reason)
			}
			for _, a := range summary.Artifacts {
				b, err := os.ReadFile(filepath.Join(output, a.Name))
				if err != nil || Sum(b) != a.SHA256 || int64(len(b)) != a.Size {
					t.Fatal("artifact mismatch", err)
				}
			}
			if tc.status == "failed" && (len(summary.Artifacts) != 1 || summary.Artifacts[0].Name != "failure.json") {
				t.Fatal("refused analysis exposed candidate artifacts")
			}
		})
	}
	for _, name := range []string{"test_performance_bundle.py", "test_performance_profiles.py", "test_coding_eval.py", "test_serving_runtime.py", "test_serving_orchestration.py", "test_inference_cache.py"} {
		run(t, "python3", "-B", "-m", "unittest", "discover", "-s", filepath.Join(root, "tests/hardware"), "-p", name)
	}
	run(t, "bash", filepath.Join(root, "tests/test_serving_runtime.sh"))
}
