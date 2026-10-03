package memory

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Uses the candidate installer's own tiny fixtures, never the installed target.
// The separate 67a5060 harness/catalog fixture is deliberately unchanged.
func TestCandidateInstallerMemoryContract(t *testing.T) {
	root := os.Getenv("BRIDGE_INSTALLER_MEMORY_CANDIDATE")
	if root == "" {
		t.Skip("NOT RUN — set BRIDGE_INSTALLER_MEMORY_CANDIDATE to the reviewed source checkout")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("candidate contract requires prepared Python")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	manifest, err := os.ReadFile(filepath.Join(root, "infrastructure/packages/bootstrap/bridge-runtime.files"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append([]string{"bin/workstationctl", "lib/common.sh", "lib/workstation/runtime.sh", "versions.lock"}, toolPaths()...) {
		if strings.Count("\n"+string(manifest), "\n"+name+"\n") != 1 {
			t.Fatalf("candidate runtime source closure missing or duplicates %s", name)
		}
		hash, _, err := HashFile(ctx, filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("candidate source %s sha256=%s (not runtime authority)", name, hash)
	}
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(temp, "inputs")
	materialize := `import hashlib,json,shutil,sys,os
from pathlib import Path
os.umask(0o077)
root,out=map(lambda x:Path(x).resolve(),sys.argv[1:3])
sys.path[:0]=[str(root/'tests'/'hardware'),str(root/'lib'/'workstation')]
import test_serving_memory as tm
case=tm.MemoryTests('runTest'); case.setUp()
try:
 out.mkdir(mode=0o700)
 for old,new in [('deployment.json','deployment.json'),('workload.json','workload.json'),('resource.json','resource-plan.json')]: shutil.copyfile(case.root/old,out/new)
 (out/'telemetry').mkdir(mode=0o700)
 telemetry={'schema':2,'status':'generated-not-deployed','hardware_qualification':'NOT RUN','enabled':True,'profile':'full','gpu_exporter':False,'sglang_trace':False,'kubelet':False,'node_name':'fixture','api_address':'10.0.0.1','workstation_address':'10.0.0.2','reserve_mib':6144,'stack_limit_mib':4736,'component_limits_mib':{'cluster_stack_mib':4736,'host_alloy_mib':512,'hardware_sampler_mib':128},'margin_mib':768,'calculated_allowance_mib':6144,'planned_workloads':['sglang'],'workload_overlay':'apps/overlays/dual-gpu','stack_images':[],'workload_images':[],'source_identity':{},'source_sha256':{}}
 (out/'telemetry'/'evidence.json').write_text(json.dumps(telemetry)+'\n')
 ids=[]
 for i,(start,serving) in enumerate(case.observations,1):
  ident=f'obs-{i:02d}'; ids.append(ident)
  shutil.copytree(start,out/'observations'/ident/'startup')
  shutil.copytree(serving,out/'observations'/ident/'serving')
 files={p.relative_to(out).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(out.rglob('*')) if p.is_file()}
 m={'schema':1,'kind':'sglang-memory-evidence','boot_id':'00000000-0000-0000-0000-000000000001','hardware_sha256':'a'*64,'source_revision':'b'*64,'observations':ids,'files':files}
 (out/'manifest.json').write_text(json.dumps(m)+'\n')
finally: case.doCleanups()
`
	run := func(command string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "WORKSTATION_PYTHON="+python)
		if err := cmd.Run(); err != nil {
			t.Fatalf("bounded candidate fixture/planner command failed: %v", err)
		}
	}
	run(python, "-c", materialize, root, input)
	b, err := Read(filepath.Join(input, "manifest.json"), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	digest := Sum(b)
	m, err := Verify(ctx, input, digest)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(temp, "plan")
	args := []string{filepath.Join(root, "bin/workstationctl"), "rocm", "serving-memory-plan", filepath.Join(input, "deployment.json"), filepath.Join(input, "workload.json"), filepath.Join(input, "resource-plan.json"), output, "--other-mib", "8192"}
	args = append(args, "--telemetry-evidence", filepath.Join(input, "telemetry", "evidence.json"))
	for _, id := range m.Observations {
		args = append(args, "--observation", filepath.Join(input, "observations", id, "startup"), filepath.Join(input, "observations", id, "serving"))
	}
	run("bash", args...)
	summary, err := ValidateOutput(ctx, output, input, root, m, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "plan-only-unqualified" || summary.Cold != 2 || summary.Warm != 2 || summary.CandidateMiB != 33280 || len(summary.Artifacts) != 3 {
		t.Fatalf("invalid synthetic result: %+v", summary)
	}
	for _, a := range summary.Artifacts {
		if a.Content != "" {
			t.Fatal("private content in summary")
		}
	}
	planPath := filepath.Join(output, "plan.json")
	original, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"runtime", func(p map[string]any) { p["runtime_identity"] = strings.Repeat("c", 64) }},
		{"inflated_capacity", func(p map[string]any) { p["budget"].(map[string]any)["allocatable_mib"] = float64(999999) }},
		{"average_not_peak", func(p map[string]any) { p["budget"].(map[string]any)["observed_envelope_bytes"] = float64(8 << 30) }},
		{"missing_phase", func(p map[string]any) { p["observations"] = p["observations"].([]any)[:3] }},
		{"forged_pod", func(p map[string]any) {
			p["observations"].([]any)[0].(map[string]any)["pod_uid"] = "00000000-0000-0000-0000-000000000999"
		}},
		{"unknown_field", func(p map[string]any) { p["approved"] = true }},
		{"shm_subtracted", func(p map[string]any) { p["budget"].(map[string]any)["shm_limit_mib"] = float64(1024) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p map[string]any
			_ = json.Unmarshal(original, &p)
			tc.edit(p)
			b, _ := json.Marshal(p)
			if err := os.WriteFile(planPath, b, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateOutput(ctx, output, input, root, m, 8192); err == nil {
				t.Fatal("mutated plan accepted")
			}
			if err := os.WriteFile(planPath, original, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Execute the actual candidate's negative corpus (cold/warm repetition,
	// cgroups, final samples, pressure, model/CPU drift), not a copied weaker set.
	run(python, "-B", "-m", "unittest", "discover", "-s", filepath.Join(root, "tests/hardware"), "-p", "test_serving_memory.py")
}
func toolPaths() []string {
	out := []string{}
	for _, name := range Tools {
		out = append(out, "lib/workstation/"+name)
	}
	return out
}

func TestEvidenceBoundsPathsRetentionAndCancellation(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	m := Manifest{Schema: 1, Kind: "sglang-memory-evidence", BootID: "00000000-0000-0000-0000-000000000001", HardwareSHA256: strings.Repeat("a", 64), SourceRevision: strings.Repeat("b", 64), Files: map[string]string{}}
	for _, name := range []string{"deployment.json", "workload.json", "resource-plan.json"} {
		b := []byte(`{"status":"failed","private":"SYNTHETIC_SENTINEL"}`)
		if e = os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
		m.Files[name] = Sum(b)
	}
	b, _ := json.Marshal(m)
	if e = os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	hash := Sum(b)
	ctx := context.Background()
	if _, e = Verify(ctx, root, hash); e != nil {
		t.Fatal(e)
	}
	if Complete(m) {
		t.Fatal("missing observations became complete")
	}
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dst := filepath.Join(base, "retained")
	if _, e = Copy(ctx, root, dst, hash); e != nil {
		t.Fatal(e)
	}
	if _, e = Copy(ctx, root, dst, hash); e == nil {
		t.Fatal("overwrote retained evidence")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = Verify(cancelled, root, hash); e == nil {
		t.Fatal("cancelled hashing succeeded")
	}
	if e = os.WriteFile(filepath.Join(root, "extra"), []byte("unrelated"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(ctx, root, hash); e == nil {
		t.Fatal("unexpected file accepted")
	}
	// Each mutation uses a separate fixture file; existing evidence is retained.
	m.Files["../escape"] = strings.Repeat("a", 64)
	b, _ = json.Marshal(m)
	if e = os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(ctx, root, Sum(b)); e == nil {
		t.Fatal("path escape accepted")
	}
}
