package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// TestCLIAndDaemon uses the actual executable entry points and a real loopback
// listener. It never opens kubeconfig, model origins or hardware adapters.
func TestCLIAndDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("full executable integration omitted by -short")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverBin, cliBin := filepath.Join(tmp, "bridged"), filepath.Join(tmp, "bridgectl")
	for path, pkg := range map[string]string{serverBin: "./cmd/bridged", cliBin: "./cmd/bridgectl"} {
		cmd := exec.Command("go", "build", "-o", path, pkg)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, b)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	demo := filepath.Join(tmp, "demo")
	if b, err := exec.Command(serverBin, "--init-demo", demo, "--demo-port", strconv.Itoa(port)).CombinedOutput(); err != nil {
		t.Fatalf("demo init: %v %s", err, b)
	}
	policy, token, contextFile := filepath.Join(demo, "bridge.json"), filepath.Join(demo, "owner.token"), filepath.Join(demo, "context.json")
	run := func(want int, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(cliBin, args...)
		b, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ok bool
			var exit *exec.ExitError
			exit, ok = err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != want {
			t.Fatalf("CLI %v exit %d want %d: %s", args, code, want, b)
		}
		return b
	}
	metadata := run(0, "admin", "bootstrap", "--config", policy, "--output", token)
	secret, err := os.ReadFile(token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(metadata, bytes.TrimSpace(secret)) {
		t.Fatal("bootstrap printed its credential")
	}
	viewer, operator := filepath.Join(demo, "viewer.token"), filepath.Join(demo, "operator.token")
	run(0, "admin", "issue", "--config", policy, "--role", "viewer", "--name", "fixture-viewer", "--output", viewer)
	run(0, "admin", "issue", "--config", policy, "--role", "operator", "--name", "fixture-operator", "--output", operator)
	start := func() *exec.Cmd {
		cmd := exec.Command(serverBin, "--config", policy)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	server := start()
	defer func() {
		if server.Process != nil {
			_ = server.Process.Signal(syscall.SIGTERM)
			_ = server.Wait()
		}
	}()
	ready := false
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		cmd := exec.Command(cliBin, "--context", contextFile, "status")
		if cmd.Run() == nil {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		t.Fatal("demo daemon did not become ready")
	}
	owner := func(want int, args ...string) []byte {
		return run(want, append([]string{"--context", contextFile, "--deadline", "10s"}, args...)...)
	}
	var inventory domain.Inventory
	if err := json.Unmarshal(owner(0, "status"), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Mode != "demo" || inventory.Target != "demo-workstation" {
		t.Fatal("demo identity missing")
	}
	var cfg domain.Configuration
	readConfig := func() {
		t.Helper()
		if err := json.Unmarshal(owner(0, "config"), &cfg); err != nil {
			t.Fatal(err)
		}
	}
	readConfig()
	createPlan := func(draft domain.Draft) domain.Plan {
		t.Helper()
		path := filepath.Join(tmp, fmt.Sprintf("draft-%d.json", time.Now().UnixNano()))
		b, _ := json.Marshal(draft)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		var plan domain.Plan
		if err := json.Unmarshal(owner(0, "plan", "--file", path), &plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	apply := func(plan domain.Plan, key string) domain.Operation {
		t.Helper()
		var op domain.Operation
		if err := json.Unmarshal(owner(0, "apply", "--plan", plan.ID, "--target", inventory.Target, "--idempotency-key", key), &op); err != nil {
			t.Fatal(err)
		}
		return op
	}
	// Exercise new commands against the real daemon/CLI, not a response handler.
	memoryDraft := domain.Draft{Action: "memory.evidence.import", Target: inventory.Target, SourceRevision: cfg.Revision, Memory: &domain.MemoryRequest{EvidenceID: "demo-complete", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 8192}}
	memoryPath := filepath.Join(tmp, "memory-draft.json")
	writeMemory := func() {
		t.Helper()
		b, _ := json.Marshal(memoryDraft)
		if err := os.WriteFile(memoryPath, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeMemory()
	owner(0, "memory-preview", "--file", memoryPath)
	for _, credential := range []string{viewer, operator} {
		run(3, "--endpoint", "http://127.0.0.1:"+strconv.Itoa(port), "--credential-file", credential, "memory")
	}
	imported := apply(createPlan(memoryDraft), "fixture-memory-import")
	owner(0, "wait", imported.ID)
	memoryDraft.Action = "memory.plan.export"
	memoryDraft.Memory.EvidenceID = imported.ID
	writeMemory()
	owner(0, "memory-preview", "--file", memoryPath)
	memoryOp := apply(createPlan(memoryDraft), "fixture-memory-export")
	var memoryDone domain.Operation
	if err := json.Unmarshal(owner(0, "wait", memoryOp.ID), &memoryDone); err != nil {
		t.Fatal(err)
	}
	if memoryDone.SourceUpdated || memoryDone.LiveApplied || memoryDone.RecoveryRequired {
		t.Fatal("memory export mutated or qualified workload")
	}
	owner(0, "memory")
	owner(0, "memory-inspect", memoryOp.ID)
	owner(0, "artifact", memoryOp.ID, "--name", "memory-summary.json", "--output", filepath.Join(tmp, "memory-summary.json"))
	readConfig()
	if cfg.Revision != memoryDraft.SourceRevision {
		t.Fatal("memory workflow changed source")
	}
	s := cfg.Serving
	s.Concurrency++
	draft := domain.Draft{Action: "serving.configure", Target: inventory.Target, SourceRevision: cfg.Revision, Serving: &s}
	plan := createPlan(draft)
	if len(plan.Preview.Changes) != 1 || plan.Preview.Changes[0].Field != "serving.concurrency" {
		t.Fatalf("preview did not show exact change: %+v", plan.Preview.Changes)
	}
	owner(2, "apply", "--plan", plan.ID, "--target", "another-target", "--idempotency-key", "fixture-wrong-target")
	op := apply(plan, "fixture-serving-change")
	replay := apply(plan, "fixture-serving-change")
	if replay.ID != op.ID {
		t.Fatal("idempotency replay duplicated execution")
	}
	var completed domain.Operation
	if err := json.Unmarshal(owner(0, "wait", op.ID), &completed); err != nil {
		t.Fatal(err)
	}
	if !completed.SourceUpdated || !completed.LiveApplied {
		t.Fatalf("separate durable results absent: %+v", completed)
	}
	readConfig()
	if cfg.Serving.Concurrency != s.Concurrency {
		t.Fatal("source update not visible")
	}
	path := filepath.Join(tmp, "stale.json")
	b, _ := json.Marshal(draft)
	os.WriteFile(path, b, 0600)
	owner(4, "plan", "--file", path)
	for _, credential := range []string{viewer, operator} {
		run(3, "--endpoint", "http://127.0.0.1:"+strconv.Itoa(port), "--credential-file", credential, "plan", "--file", path)
	}
	resources := cfg.Resources
	resources.CPU += 2
	p := createPlan(domain.Draft{Action: "resources.configure", Target: inventory.Target, SourceRevision: cfg.Revision, Resources: &resources})
	resourceOp := apply(p, "fixture-resource-budget")
	owner(0, "wait", resourceOp.ID)
	readConfig()
	budgets := cfg.Caches
	budgets.CompilerGiB++
	p = createPlan(domain.Draft{Action: "caches.configure", Target: inventory.Target, SourceRevision: cfg.Revision, Caches: &budgets})
	cacheOp := apply(p, "fixture-cache-budget")
	owner(0, "wait", cacheOp.ID)
	readConfig()
	owner(0, "export-source", "--output", filepath.Join(tmp, "managed-source-export.json"))
	for i, action := range []string{"model.verify", "model.stage", "model.verify", "hardware.refresh", "serving.start", "serving.restart", "serving.stop"} {
		draft := domain.Draft{Action: action, Target: inventory.Target, SourceRevision: cfg.Revision}
		if strings.HasPrefix(action, "model.") {
			draft.Model = cfg.Serving.Model
		}
		p := createPlan(draft)
		o := apply(p, fmt.Sprintf("fixture-management-%d", i))
		exit := 0
		if i == 0 {
			exit = 6
		} // Unstaged verification must fail, with a stable CLI terminal exit.
		owner(exit, "wait", o.ID)
	}
	if len(inventory.Recipes) == 0 {
		t.Fatal("demo lacks named build recipes")
	}
	p = createPlan(domain.Draft{Action: "build.start", Target: inventory.Target, SourceRevision: cfg.Revision, Recipe: inventory.Recipes[0].ID})
	buildOp := apply(p, "fixture-reviewed-build")
	if err := json.Unmarshal(owner(0, "wait", buildOp.ID), &completed); err != nil {
		t.Fatal(err)
	}
	if len(completed.Artifacts) == 0 || completed.Artifacts[0].SourceRevision == "" {
		t.Fatal("build result lacks artifact provenance")
	}
	for _, profile := range []string{"ai", "gaming", "maintenance"} {
		readConfig()
		p := createPlan(domain.Draft{Action: "profile.switch", Target: inventory.Target, SourceRevision: cfg.Revision, Profile: profile})
		o := apply(p, "fixture-profile-"+profile)
		owner(0, "wait", o.ID)
	}
	for _, h := range []string{"qwen", "dsh", "hermes"} {
		outputPath := filepath.Join(tmp, h+".json")
		owner(0, "harness", "export", h, "--output", outputPath)
		native := filepath.Join(tmp, h+"-native")
		run(0, "harness", "configure", h, "--directory", native, "--bundle", outputPath)
		file, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(file, bytes.TrimSpace(secret)) {
			t.Fatal("management credential exported in client bundle")
		}
	}
	run(5, "admin", "recover", "--config", policy, "--output", filepath.Join(tmp, "must-not-create.token"))
	if _, err := os.Stat(filepath.Join(tmp, "must-not-create.token")); !os.IsNotExist(err) {
		t.Fatal("online recovery created a credential")
	}
	if b := owner(2, "plan", "--file", "/missing-fixture.json"); strings.Contains(string(b), strings.TrimSpace(string(secret))) {
		t.Fatal("CLI error leaked token")
	}
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := server.Wait(); err != nil {
		t.Fatal(err)
	}
	server = start()
	for until := time.Now().Add(10 * time.Second); ; {
		cmd := exec.Command(cliBin, "--context", contextFile, "operations", op.ID)
		if cmd.Run() == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("restart unavailable")
		}
		time.Sleep(25 * time.Millisecond)
	}
	owner(0, "wait", op.ID)
	owner(0, "memory-inspect", memoryOp.ID)
}
