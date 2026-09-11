package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	mem "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
)

func TestMemoryHelperAuthorityInterruptionAndHistory(t *testing.T) {
	for _, action := range []string{"memory.evidence.import", "memory.plan.export"} {
		t.Run(action, func(t *testing.T) {
			e := fixtureExecutor(t)
			dir, err := filepath.EvalSymlinks(e.policy.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			e.policy.StateDir = dir
			e.policy.SourcePath = filepath.Join(dir, "source")
			c := domain.Configuration{}
			c.Revision = c.ContentRevision()
			if err := durableJSON(e.policy.SourcePath, c); err != nil {
				t.Fatal(err)
			}
			m := &domain.MemoryRequest{EvidenceID: "fixture-evidence", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 8192}
			e.policy.MemorySources = map[string]MemorySource{m.EvidenceID: {Path: "/approved/private/evidence", SHA256: m.EvidenceSHA256}}
			r := Request{Version: 1, ID: "memory-operation-001", ExecutionIdentity: "fixture-owner", Draft: domain.Draft{Action: action, Target: e.policy.Target, SourceRevision: c.Revision, Memory: m}, Desired: c}
			r.PayloadHash = RequestHash(r)
			if _, err := e.Submit(e.policy.AllowedUID+1, r); err == nil {
				t.Fatal("wrong peer authorized")
			}
			bad := r
			bad.Draft.Target = "other-target"
			bad.PayloadHash = RequestHash(bad)
			if _, err := e.Submit(e.policy.AllowedUID, bad); err == nil {
				t.Fatal("wrong target authorized")
			}
			e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
				return nil, errors.New("fixture interrupted")
			}
			if _, err := e.Submit(e.policy.AllowedUID, r); err != nil {
				t.Fatal(err)
			}
			done := awaitResult(t, e, r.ID)
			if done.State != "failed" || done.Phase != "read-only-adapter-failed" {
				t.Fatalf("read-only became GPU recovery %+v", done)
			}
			again, err := e.Submit(e.policy.AllowedUID, r)
			if err != nil || again.State != done.State {
				t.Fatal("duplicate did not retain result")
			}
			// Restart only loads journal records. Interrupted read-only history cannot
			// authorize a restore or become an uncertain GPU mutation.
			old := e.records[r.ID]
			old.Result.State = "running"
			if err = e.save(old); err != nil {
				t.Fatal(err)
			}
			reopened := fixtureExecutor(t)
			reopened.policy = e.policy
			reopened.pathTrust = func(string, bool) error { return nil }
			if err = reopened.load(); err != nil {
				t.Fatal(err)
			}
			result, err := reopened.Status(r.ID)
			if err != nil || result.State != "failed" || result.Phase != "read-only-interrupted" {
				t.Fatal("read-only restart lost evidence", err)
			}
		})
	}
}

func TestMemoryDeploymentAndObservedNodeBinding(t *testing.T) {
	e := fixtureExecutor(t)
	baseline := map[string]any{"metadata": map[string]any{"name": e.policy.AIDeployment, "namespace": e.policy.Namespace, "uid": e.policy.AIDeploymentUID}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"approved": "original"}}, "spec": map[string]any{"containers": []any{}}}}}
	clone := func() map[string]any {
		b, _ := json.Marshal(baseline)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		return v
	}
	if err := e.memoryDeploymentIdentity(clone(), baseline); err != nil {
		t.Fatal(err)
	}
	live := clone()
	object(nested(live, "spec", "template", "metadata", "annotations"))["approved"] = "changed"
	if err := e.memoryDeploymentIdentity(live, baseline); err == nil {
		t.Fatal("metadata-only template drift accepted")
	}
	for _, field := range []string{"name", "namespace", "uid"} {
		foreign := clone()
		object(foreign["metadata"])[field] = "foreign"
		if err := e.memoryDeploymentIdentity(clone(), foreign); err == nil {
			t.Fatalf("foreign evidence %s accepted", field)
		}
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := mem.Manifest{Observations: []string{"obs-01"}}
	pod := map[string]any{"node": e.policy.Target}
	paths := []string{"startup/startup.json", "serving/result.json", "serving/after/pod.json"}
	values := []map[string]any{{"pod_before": pod, "pod": pod}, {"pod": pod}, pod}
	write := func(index int, value any) {
		t.Helper()
		path := filepath.Join(root, "observations", "obs-01", paths[index])
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := durableJSON(path, value); err != nil {
			t.Fatal(err)
		}
	}
	for i, v := range values {
		write(i, v)
	}
	if err := memoryObservationTargets(root, m, e.policy.Target); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		index int
		key   string
	}{{0, "pod_before"}, {0, "pod"}, {1, "pod"}, {2, ""}} {
		v := map[string]any{}
		for k, x := range values[tc.index] {
			v[k] = x
		}
		if tc.key == "" {
			v["node"] = "foreign-node"
		} else {
			v[tc.key] = map[string]any{"node": "foreign-node"}
		}
		write(tc.index, v)
		if err := memoryObservationTargets(root, m, e.policy.Target); err == nil {
			t.Fatal("foreign observation target accepted")
		}
		write(tc.index, values[tc.index])
	}
}
func TestMemoryPolicyAndArtifactRefusals(t *testing.T) {
	e := fixtureExecutor(t)
	if _, err := e.MemoryArtifact(context.Background(), "../../escape", "patch.json"); err == nil {
		t.Fatal("artifact traversal accepted")
	}
	if _, err := e.MemoryArtifact(context.Background(), "operation-001", "source.json"); err == nil {
		t.Fatal("non-allowlisted artifact accepted")
	}
	p := Policy{MemorySources: map[string]MemorySource{"approved-source": {Path: "relative", SHA256: strings.Repeat("a", 64)}}}
	if p.validateMemoryPolicy() == nil {
		t.Fatal("relative source accepted")
	}
	p.MemorySources["approved-source"] = MemorySource{Path: "/approved/evidence", SHA256: strings.Repeat("a", 64)}
	if p.validateMemoryPolicy() == nil {
		t.Fatal("missing installed tool closure accepted")
	}
	d := domain.Draft{Action: "memory.plan.export", Target: e.policy.Target, Memory: &domain.MemoryRequest{EvidenceID: "fabricated-import", EvidenceSHA256: strings.Repeat("a", 64)}}
	if _, _, err := e.memoryInput(d); err == nil {
		t.Fatal("API invented helper import")
	}
	if _, err := e.MemoryPreview(context.Background(), domain.Draft{Action: "memory.plan.export"}, domain.Configuration{}); err == nil {
		t.Fatal("nil memory accepted")
	}
}

func TestMemoryRealIntakeRetainsFailedEvidenceWithoutCluster(t *testing.T) {
	e := fixtureExecutor(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	e.policy.StateDir = root
	e.policy.SourcePath = filepath.Join(root, "source.json")
	c := domain.Configuration{}
	c.Revision = c.ContentRevision()
	if err = durableJSON(e.policy.SourcePath, c); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(root, "inbox")
	if err = os.Mkdir(inbox, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deployment.json", "workload.json", "resource-plan.json"} {
		if err = durableJSON(filepath.Join(inbox, name), map[string]string{"status": "failed", "reason": "SYNTHETIC_PRIVATE_FAILURE"}); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := mem.Seal(context.Background(), inbox, c.Revision, strings.Repeat("a", 64), "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	e.policy.MemorySources = map[string]MemorySource{"fixture-evidence": {Path: inbox, SHA256: digest}}
	r := Request{Version: 1, ID: "memory-intake-actual", ExecutionIdentity: "fixture-owner", Desired: c, Draft: domain.Draft{Action: "memory.evidence.import", Target: e.policy.Target, SourceRevision: c.Revision, Memory: &domain.MemoryRequest{EvidenceID: "fixture-evidence", EvidenceSHA256: digest}}}
	r.PayloadHash = RequestHash(r)
	e.run = e.execute // Execute the actual bounded intake adapter, not a success stub.
	if _, err = e.Submit(e.policy.AllowedUID, r); err != nil {
		t.Fatal(err)
	}
	done := awaitResult(t, e, r.ID)
	if done.State != "succeeded" {
		t.Fatalf("intake failed: %+v", done)
	}
	var summary domain.MemorySummary
	if err = json.Unmarshal(done.Data, &summary); err != nil || summary.Status != "incomplete" || strings.Contains(string(done.Data), "SYNTHETIC_PRIVATE_FAILURE") {
		t.Fatal("failed input relabelled or private bytes exported", err)
	}
	retained := filepath.Join(root, "memory", r.ID, "inputs")
	if _, err = mem.Verify(context.Background(), retained, digest); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Submit(e.policy.AllowedUID, r); err != nil {
		t.Fatal("repeat intake did not retain identity", err)
	}
	if err = os.WriteFile(filepath.Join(inbox, "workload.json"), []byte("corrupt retained source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.MemoryPreview(context.Background(), r.Draft, c); err == nil {
		t.Fatal("changed approved inbox accepted")
	}
	if _, err = mem.Verify(context.Background(), retained, digest); err != nil {
		t.Fatal("source drift altered retained snapshot", err)
	}
	// No hardware path, kubectl or K3s exists in this fixture. Root ownership
	// verification is fixture-injected; this is not installed-helper validation.
}
func TestMemoryBaselineBindsQualifiedLaunchAndSharedMemory(t *testing.T) {
	e := fixtureExecutor(t)
	c := domain.Configuration{Serving: domain.Serving{Model: "fixture-model", Context: 4096, Concurrency: 2, MemoryFraction: .8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 38912, SharedMemoryMiB: 16384, GPUCount: 2}}
	q := Qualification{Image: "fixture@sha256:" + strings.Repeat("a", 64), ModelRevision: strings.Repeat("b", 40), ModelPath: "/models/fixture/revision", ExpiresAt: time.Now().Add(time.Hour)}
	e.policy.QualifiedConfigurations = map[string]Qualification{ConfigurationHash(&c.Serving, &c.Resources): q}
	args := []any{"--model-path", q.ModelPath, "--revision", q.ModelRevision, "--served-model-name", c.Serving.Model, "--context-length", "4096", "--max-running-requests", "2", "--tp", "2", "--mem-fraction-static", "0.8"}
	budget := map[string]any{"cpu": "20", "memory": "38Gi", "amd.com/gpu": "2"}
	container := map[string]any{"name": "sglang", "image": q.Image, "args": args, "resources": map[string]any{"requests": budget, "limits": budget}}
	shm := map[string]any{"name": "shm", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "16Gi"}}
	dep := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{container}, "volumes": []any{shm}}}}}
	qualify := func() {
		e.policy.SessionQualifications = map[string]SessionQualification{"ai": {TemplateHash: domain.Hash(nested(dep, "spec", "template")), ExpiresAt: time.Now().Add(time.Hour), ConfigMaps: map[string]string{}}}
	}
	qualify()
	if err := e.memoryBaseline(context.Background(), dep, c); err != nil {
		t.Fatal(err)
	}
	args[1] = "/models/old-model"
	qualify()
	if err := e.memoryBaseline(context.Background(), dep, c); err == nil {
		t.Fatal("desired B accepted old deployed A")
	}
	args[1] = q.ModelPath
	args[7] = "8192"
	qualify()
	if err := e.memoryBaseline(context.Background(), dep, c); err == nil {
		t.Fatal("changed context accepted")
	}
	args[7] = "4096"
	object(shm["emptyDir"])["sizeLimit"] = "8Gi"
	qualify()
	if err := e.memoryBaseline(context.Background(), dep, c); err == nil {
		t.Fatal("changed shm accepted")
	}
	object(shm["emptyDir"])["sizeLimit"] = "16Gi"
	container["envFrom"] = []any{map[string]any{"configMapRef": map[string]any{"name": "unqualified-config"}}}
	qualify()
	if err := e.memoryBaseline(context.Background(), dep, c); err == nil {
		t.Fatal("unqualified configmap accepted")
	}
	// Failed output remains owner private and is never promoted into a journal.
	if err := durableJSON(filepath.Join(e.policy.StateDir, "retained.json"), map[string]string{"status": "failed"}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryObservedRuntimeBindsQualifiedFilesAndLaunch(t *testing.T) {
	e := fixtureExecutor(t)
	c := domain.Configuration{Serving: domain.Serving{Model: "fixture-model", Context: 4096, Concurrency: 2, MemoryFraction: .8}, Resources: domain.Resources{GPUCount: 2}}
	q := Qualification{ModelPath: "/models/fixture/revision", ModelRevision: strings.Repeat("a", 40), Files: map[string]string{"model.safetensors": strings.Repeat("b", 64)}, ExpiresAt: time.Now().Add(time.Hour)}
	e.policy.QualifiedConfigurations = map[string]Qualification{ConfigurationHash(&c.Serving, &c.Resources): q}
	files := map[string]any{"model.safetensors": map[string]any{"sha256": q.Files["model.safetensors"]}}
	launch := map[string]any{"--model-path": q.ModelPath, "--revision": q.ModelRevision, "--served-model-name": "fixture-model", "--context-length": "4096", "--max-running-requests": "2", "--tp": "2", "--mem-fraction-static": "0.8"}
	runtime := map[string]any{"model_files": files, "settings": map[string]any{"MODEL_PATH": q.ModelPath, "MODEL_REVISION": q.ModelRevision, "SERVED_MODEL_NAME": "fixture-model", "CONTEXT_LENGTH": "4096", "MAX_RUNNING_REQUESTS": "2", "TENSOR_PARALLEL": "2", "MEM_FRACTION_STATIC": "0.8"}, "launch": []any{launch}, "devices": []any{map[string]any{"uuid": "GPU-a", "gfx": "gfx1201"}, map[string]any{"uuid": "GPU-b", "gfx": "gfx1201"}}}
	if err := e.memoryRuntime(runtime, c); err != nil {
		t.Fatal(err)
	}
	files[".bridge-receipt.json"] = map[string]any{"sha256": strings.Repeat("d", 64)}
	if err := e.memoryRuntime(runtime, c); err != nil {
		t.Fatal("collector receipt conflicts with existing qualification contract", err)
	}
	files["unreviewed.safetensors"] = map[string]any{"sha256": strings.Repeat("d", 64)}
	if err := e.memoryRuntime(runtime, c); err == nil {
		t.Fatal("unreviewed model file treated as receipt")
	}
	delete(files, "unreviewed.safetensors")
	object(files["model.safetensors"])["sha256"] = strings.Repeat("c", 64)
	if err := e.memoryRuntime(runtime, c); err == nil {
		t.Fatal("changed model bytes accepted")
	}
	object(files["model.safetensors"])["sha256"] = q.Files["model.safetensors"]
	launch["--context-length"] = "8192"
	if err := e.memoryRuntime(runtime, c); err == nil {
		t.Fatal("observed launch conflicts with baseline")
	}
	launch["--context-length"] = "4096"
	object(list(runtime["devices"])[1])["uuid"] = "GPU-a"
	if err := e.memoryRuntime(runtime, c); err == nil {
		t.Fatal("duplicate GPU identity accepted")
	}
}
