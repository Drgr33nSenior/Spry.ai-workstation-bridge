package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	mem "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

type MemorySource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func (p Policy) validateMemoryPolicy() error {
	if len(p.MemorySources) > 20 {
		return errors.New("at most twenty reviewed memory evidence sources")
	}
	for id, s := range p.MemorySources {
		if !mem.ID.MatchString(id) || !mem.Digest.MatchString(s.SHA256) || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || s.Path == "/" {
			return errors.New("memory source requires a reviewed ID, canonical private path and manifest SHA-256")
		}
	}
	if len(p.MemorySources) > 0 {
		for _, name := range mem.Tools {
			if !mem.Digest.MatchString(p.Artifacts["lib/workstation/"+name]) {
				return errors.New("memory planner requires independently reviewed installed tool hashes")
			}
		}
	}
	return nil
}
func (e *Executor) validateMemoryRequest(r Request) error {
	if r.Desired.Revision != r.Desired.ContentRevision() || r.Draft.SourceRevision != r.Desired.Revision {
		return errors.New("memory plan source revision mismatch")
	}
	var c domain.Configuration
	if err := mem.JSON(e.policy.SourcePath, &c); err != nil || c.ContentRevision() != r.Desired.Revision {
		return errors.New("memory plan canonical source changed")
	}
	if r.Draft.Action == "memory.evidence.import" {
		s, ok := e.policy.MemorySources[r.Draft.Memory.EvidenceID]
		if !ok || s.SHA256 != r.Draft.Memory.EvidenceSHA256 {
			return errors.New("memory source is not approved by root policy")
		}
	}
	return nil
}
func (e *Executor) memoryInput(d domain.Draft) (string, string, error) {
	if d.Action == "memory.evidence.import" {
		s, ok := e.policy.MemorySources[d.Memory.EvidenceID]
		if !ok || s.SHA256 != d.Memory.EvidenceSHA256 {
			return "", "", errors.New("unapproved memory evidence source")
		}
		return s.Path, s.SHA256, nil
	}
	r, ok := e.records[d.Memory.EvidenceID]
	if !ok || r.Request.Draft.Action != "memory.evidence.import" || r.Result.State != "succeeded" || r.Request.Draft.Target != d.Target || r.Request.Draft.Memory.EvidenceSHA256 != d.Memory.EvidenceSHA256 {
		return "", "", errors.New("memory plan requires a successful import in the independent helper journal")
	}
	return filepath.Join(e.policy.StateDir, "memory", d.Memory.EvidenceID, "inputs"), d.Memory.EvidenceSHA256, nil
}
func (e *Executor) memoryPreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.MemorySummary, error) {
	s := domain.MemorySummary{EvidenceID: d.Memory.EvidenceID, SHA256: d.Memory.EvidenceSHA256, Status: "incomplete", Reason: "phase_files_missing_or_not_yet_validated", OtherMiB: d.Memory.OtherMiB, Preconditions: map[string]string{}}
	if err := e.validate(Request{Draft: d, Desired: c}); err != nil {
		return s, err
	}
	if err := e.verify(); err != nil {
		return s, err
	}
	root, digest, err := e.memoryInput(d)
	if err != nil {
		return s, err
	}
	if err = e.pathTrust(root, true); err != nil {
		return s, errors.New("memory evidence needs owner-reviewed protected paths")
	}
	if err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = e.pathTrust(path, d.IsDir()); err != nil {
			return errors.New("memory evidence tree must be root-owned and protected")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0077 != 0 {
			return errors.New("memory evidence tree must be owner-private")
		}
		return nil
	}); err != nil {
		return s, err
	}
	m, err := mem.Verify(ctx, root, digest)
	if err != nil {
		return s, err
	}
	if d.Action == "memory.evidence.import" {
		// Intake preserves stale/failed observations. Only candidate generation
		// requires fresh hardware and matching source; nothing is relabelled.
		s.Preconditions = map[string]string{"memory_manifest": digest, "memory_source": c.Revision, "memory_policy": policyRevision(e.policy)}
		// These are retained deployment declarations, not observed consumption.
		dep, readErr := mem.RawJSON(filepath.Join(root, "deployment.json"))
		if readErr == nil {
			containers := list(nested(dep, "spec", "template", "spec", "containers"))
			if len(containers) == 1 {
				container := object(containers[0])
				s.BaselineMiB, _ = mem.MiB(nested(container, "resources", "requests", "memory"))
				s.LimitedMiB, _ = mem.MiB(nested(container, "resources", "limits", "memory"))
			}
			for _, v := range list(nested(dep, "spec", "template", "spec", "volumes")) {
				volume := object(v)
				if str(volume["name"]) == "shm" {
					s.SharedMemoryMiB, _ = mem.MiB(nested(volume, "emptyDir", "sizeLimit"))
				}
			}
		}
		s.Limitations = []string{"Private intake only: retained bytes are not qualification or complete sizing evidence.", "Stale, failed and missing observations require recollection before candidate generation."}
		if mem.Complete(m) {
			rows, err := mem.Observe(ctx, root, m, s.LimitedMiB, s.SharedMemoryMiB)
			if err == nil {
				s.Status = "imported-unqualified"
				s.Reason = "phase_files_present_deterministic_validation_required"
				s.Observations = rows
				for _, o := range rows {
					if o.Phase == "cold" {
						s.Cold++
					} else {
						s.Warm++
					}
				}
			}
		}
		if m.SourceRevision != c.Revision {
			s.Status = "incomplete"
			s.Reason = "source_revision_stale"
		}
		return s, nil
	}
	if m.SourceRevision != c.Revision {
		return s, errors.New("memory evidence source revision is stale")
	}
	if err = e.checkHardware(); err != nil {
		return s, err
	}
	hardware, _, err := mem.HashFile(ctx, e.policy.HardwarePath)
	if err != nil || hardware != m.HardwareSHA256 {
		return s, errors.New("memory hardware/DIMM evidence changed; collect and regenerate resource plan")
	}
	boot, err := os.ReadFile(filepath.Join(filepath.Dir(e.policy.HardwarePath), "boot-id.txt"))
	if err != nil || strings.TrimSpace(string(boot)) != m.BootID {
		return s, errors.New("memory evidence boot identity changed")
	}
	deployment, err := mem.RawJSON(filepath.Join(root, "deployment.json"))
	if err != nil {
		return s, err
	}
	spec := mem.Nested(deployment, "spec", "template", "spec")
	s.Preconditions = map[string]string{"memory_manifest": digest, "memory_hardware": hardware, "memory_boot": m.BootID, "memory_source": c.Revision, "memory_policy": policyRevision(e.policy), "memory_baseline_spec": mem.Identity(spec)}
	s.BaselineMiB = c.Resources.MemoryMiB
	s.LimitedMiB = c.Resources.MemoryMiB
	s.SharedMemoryMiB = c.Resources.SharedMemoryMiB
	s.Limitations = []string{"Imported evidence is not qualification or measured RAM saving.", "Startup/serving collection remains a non-root owner workflow; no automatic collection or application.", "Prometheus summaries and unmeasured averages cannot size this workload."}
	if mem.Complete(m) {
		s.Status = "imported-unqualified"
		s.Reason = "phase_files_present_deterministic_validation_required"
		observations, observeErr := mem.Observe(ctx, root, m, c.Resources.MemoryMiB, c.Resources.SharedMemoryMiB)
		if observeErr != nil {
			s.Status = "incomplete"
			s.Reason = "invalid_missing_or_pressure_affected_observation"
		} else {
			s.Observations = observations
			for _, o := range observations {
				if o.Phase == "cold" {
					s.Cold++
				} else {
					s.Warm++
				}
			}
		}
	}
	if d.Action == "memory.plan.export" {
		if !mem.Complete(m) {
			return s, errors.New("memory observations incomplete: need two cold and two warm fresh Pods with complete files")
		}
		if s.Status != "imported-unqualified" {
			return s, errors.New("memory samples are incomplete, invalid or affected by pressure; no export plan")
		}
		if err = memoryObservationTargets(root, m, e.policy.Target); err != nil {
			return s, err
		}
		inv, err := e.Snapshot(ctx)
		if err != nil || !inv.ClusterAvailable {
			return s, errors.New("current resource consumers unavailable; no memory plan exported")
		}
		h := inv.Hardware
		if !h.TopologyKnown || h.SMTWidth < 1 || c.Resources.CPU%h.SMTWidth != 0 || h.CPUManagerPolicy != "static" || !h.FullPCPUsOnly || d.Memory.OtherMiB < h.OtherMemoryMiB {
			return s, errors.New("memory plan requires current whole-core policy and other-workload budget covering observed consumers")
		}
		resource, err := mem.RawJSON(filepath.Join(root, "resource-plan.json"))
		if err != nil {
			return s, err
		}
		total, ok := mem.Nested(resource, "memory", "total_mib").(float64)
		alloc, allocOK := mem.Nested(resource, "memory", "allocatable_mib").(float64)
		if !ok || !allocOK || int64(total) != h.MemoryMiB || alloc <= 0 || int64(alloc) > h.MemoryMiB-h.HostReserveMiB-h.KubeReserveMiB {
			return s, errors.New("resource plan differs from current discovered host and node headroom")
		}
		s.AllocatableMiB = int64(alloc)
		s.Preconditions["memory_live_capacity"] = domain.Hash(struct {
			Memory, Other, Reserved        int64
			CPU, OtherCPU, GPUs, OtherGPUs int
		}{h.MemoryMiB, h.OtherMemoryMiB, h.HostReserveMiB + h.KubeReserveMiB, h.CPUThreads, h.OtherCPU, len(h.GPUs), h.OtherGPUs})
		ai, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "deployment", e.policy.AIDeployment, "-o", "json")
		if err != nil {
			return s, errors.New("current baseline Deployment unavailable; no plan exported")
		}
		if err = e.memoryDeploymentIdentity(ai, deployment); err != nil {
			return s, err
		}
		if err = e.memoryBaseline(ctx, ai, c); err != nil {
			return s, err
		}
		for _, id := range m.Observations {
			runtime, err := mem.RawJSON(filepath.Join(root, "observations", id, "serving", "after", "runtime.json"))
			if err != nil {
				return s, err
			}
			if err = e.memoryRuntime(runtime, c); err != nil {
				return s, err
			}
		}
		s.Status = "ready-for-plan"
		s.Reason = "baseline_preflight_passed_deterministic_candidate_not_yet_generated"
	}
	return s, nil
}

func (e *Executor) memoryRuntime(runtime map[string]any, c domain.Configuration) error {
	q, ok := e.policy.QualifiedConfigurations[ConfigurationHash(&c.Serving, &c.Resources)]
	if !ok || len(q.Files) == 0 || !q.ExpiresAt.After(time.Now()) {
		return errors.New("memory runtime needs current qualified model file hashes")
	}
	files := object(runtime["model_files"])
	count := len(files)
	if receipt, present := files[".bridge-receipt.json"]; present {
		// The installed collector hashes every file, while the existing helper
		// explicitly excludes this non-authoritative stager receipt from q.Files.
		// Its hash stays bound by the sealed evidence/runtime identity; it cannot
		// replace a model-file hash or authorize a snapshot.
		if !mem.Digest.MatchString(str(object(receipt)["sha256"])) {
			return errors.New("observed staging receipt digest is malformed")
		}
		if _, qualified := q.Files[".bridge-receipt.json"]; !qualified {
			count--
		}
	}
	if count != len(q.Files) {
		return errors.New("observed memory model file set differs from qualification")
	}
	for name, hash := range q.Files {
		if str(nested(files, name, "sha256")) != hash {
			return errors.New("observed memory model hash differs from qualification")
		}
	}
	// The selected collector does not capture CPU-offload launch evidence. Do not
	// infer its effective value from the environment or an absent observed flag.
	if c.Serving.CPUOffloadGiB != 0 {
		return errors.New("memory collector cannot attest nonzero CPU offload; recollect with a separately reviewed compatible collector")
	}
	want := map[string]string{"--model-path": q.ModelPath, "--revision": q.ModelRevision, "--served-model-name": c.Serving.Model, "--context-length": strconv.Itoa(c.Serving.Context), "--max-running-requests": strconv.Itoa(c.Serving.Concurrency), "--tp": strconv.Itoa(c.Resources.GPUCount), "--mem-fraction-static": strconv.FormatFloat(c.Serving.MemoryFraction, 'f', -1, 64)}
	settings := object(runtime["settings"])
	for flag, key := range map[string]string{"--model-path": "MODEL_PATH", "--revision": "MODEL_REVISION", "--served-model-name": "SERVED_MODEL_NAME", "--context-length": "CONTEXT_LENGTH", "--max-running-requests": "MAX_RUNNING_REQUESTS", "--tp": "TENSOR_PARALLEL", "--mem-fraction-static": "MEM_FRACTION_STATIC"} {
		if str(settings[key]) != want[flag] {
			return errors.New("observed memory runtime settings differ from qualified source")
		}
	}
	launch := list(runtime["launch"])
	if len(launch) == 0 || len(launch) > 64 {
		return errors.New("memory observed launch evidence missing or unbounded")
	}
	for _, row := range launch {
		for flag, value := range want {
			if str(object(row)[flag]) != value {
				return errors.New("observed memory launch differs from qualified baseline")
			}
		}
	}
	devices := list(runtime["devices"])
	seen := map[string]bool{}
	if len(devices) != c.Resources.GPUCount {
		return errors.New("observed memory GPU allocation differs from baseline")
	}
	for _, device := range devices {
		d := object(device)
		id := str(d["uuid"])
		if id == "" || id == "unknown" || seen[id] || strings.Split(str(d["gfx"]), ":")[0] != "gfx1201" {
			return errors.New("memory observation needs distinct stable supported GPU identities")
		}
		seen[id] = true
	}
	return nil
}
func (e *Executor) memoryDeploymentIdentity(live, evidence map[string]any) error {
	for _, dep := range []map[string]any{live, evidence} {
		if str(nested(dep, "metadata", "uid")) != e.policy.AIDeploymentUID || str(nested(dep, "metadata", "name")) != e.policy.AIDeployment || str(nested(dep, "metadata", "namespace")) != e.policy.Namespace {
			return errors.New("memory baseline Deployment identity differs from root policy")
		}
	}
	if !reflect.DeepEqual(nested(live, "spec", "template"), nested(evidence, "spec", "template")) {
		return errors.New("memory baseline complete Pod template drift")
	}
	return nil
}

func memoryObservationTargets(root string, manifest mem.Manifest, target string) error {
	for _, id := range manifest.Observations {
		dir := filepath.Join(root, "observations", id)
		start, err := mem.RawJSON(filepath.Join(dir, "startup", "startup.json"))
		if err != nil {
			return err
		}
		result, err := mem.RawJSON(filepath.Join(dir, "serving", "result.json"))
		if err != nil {
			return err
		}
		after, err := mem.RawJSON(filepath.Join(dir, "serving", "after", "pod.json"))
		if err != nil {
			return err
		}
		for _, pod := range []map[string]any{object(start["pod_before"]), object(start["pod"]), object(result["pod"]), after} {
			if target == "" || str(pod["node"]) != target {
				return errors.New("memory observation Pod node differs from root-policy target")
			}
		}
	}
	return nil
}

func (e *Executor) memoryBaseline(ctx context.Context, dep map[string]any, c domain.Configuration) error {
	containers := list(nested(dep, "spec", "template", "spec", "containers"))
	if len(containers) != 1 {
		return errors.New("memory plan requires the single SGLang container")
	}
	container := object(containers[0])
	rs := object(container["resources"])
	limit, err := mem.MiB(nested(rs, "limits", "memory"))
	if err != nil || limit != c.Resources.MemoryMiB || !reflect.DeepEqual(rs["requests"], rs["limits"]) || number(nested(rs, "limits", "cpu")) != float64(c.Resources.CPU) || number(nested(rs, "limits", "amd.com/gpu")) != float64(c.Resources.GPUCount) {
		return errors.New("desired and observed memory/CPU/GPU budgets differ")
	}
	// This read-only path checks qualification identity, but never creates one.
	q, ok := e.policy.QualifiedConfigurations[ConfigurationHash(&c.Serving, &c.Resources)]
	if !ok || !q.ExpiresAt.After(time.Now()) || container["image"] != q.Image {
		return errors.New("baseline image/configuration needs current independent qualification")
	}
	if err = e.qualifySession(ctx, "ai", dep, nil); err != nil {
		return err
	}
	values := map[string]string{}
	for _, from := range list(container["envFrom"]) {
		ref := object(from)
		name := str(nested(ref, "configMapRef", "name"))
		if name == "" || ref["prefix"] != nil {
			return errors.New("memory baseline supports only qualified ConfigMap environment sources")
		}
		cm, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "configmap", name, "-o", "json")
		if err != nil {
			return errors.New("memory baseline ConfigMap unavailable")
		}
		for k, v := range object(cm["data"]) {
			values[k] = str(v)
		}
	}
	for _, entry := range list(container["env"]) {
		v := object(entry)
		name := str(v["name"])
		value, ok := v["value"].(string)
		if ok {
			values[name] = value
		} else {
			values[name] = ""
		}
	}
	expected := map[string]string{"--model-path": q.ModelPath, "--revision": q.ModelRevision, "--served-model-name": c.Serving.Model, "--context-length": strconv.Itoa(c.Serving.Context), "--max-running-requests": strconv.Itoa(c.Serving.Concurrency), "--tp": strconv.Itoa(c.Resources.GPUCount), "--mem-fraction-static": strconv.FormatFloat(c.Serving.MemoryFraction, 'f', -1, 64), "--cpu-offload-gb": strconv.Itoa(c.Serving.CPUOffloadGiB)}
	seen := map[string]bool{}
	args := list(container["args"])
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(str(args[i]), "=")
		if flag == "--tensor-parallel-size" || flag == "--tp-size" {
			return errors.New("memory baseline TP alias requires reviewed adapter support")
		}
		want, managed := expected[flag]
		if !managed {
			continue
		}
		if seen[flag] {
			return errors.New("duplicate managed serving flag")
		}
		seen[flag] = true
		if !equals {
			i++
			if i == len(args) {
				return errors.New("missing managed serving flag value")
			}
			value = str(args[i])
		}
		if strings.HasPrefix(value, "$(") && strings.HasSuffix(value, ")") {
			value = values[value[2:len(value)-1]]
		}
		if value != want {
			return errors.New("observed launch settings differ from desired memory baseline")
		}
	}
	for flag := range expected {
		if !seen[flag] && !(flag == "--cpu-offload-gb" && c.Serving.CPUOffloadGiB == 0) {
			return errors.New("managed launch flag missing from memory baseline")
		}
	}
	found := false
	for _, v := range list(nested(dep, "spec", "template", "spec", "volumes")) {
		volume := object(v)
		if str(volume["name"]) == "shm" {
			shm, err := mem.MiB(nested(volume, "emptyDir", "sizeLimit"))
			if err != nil || shm != c.Resources.SharedMemoryMiB || str(nested(volume, "emptyDir", "medium")) != "Memory" {
				return errors.New("memory baseline shm ceiling changed")
			}
			found = true
		}
	}
	if !found {
		return errors.New("memory baseline shm missing")
	}
	return nil
}
func (e *Executor) executeMemory(ctx context.Context, r Request, lock *os.File) (json.RawMessage, error) {
	// Called with the canonical session lock continuously held.
	s, err := e.memoryPreview(ctx, r.Draft, r.Desired)
	if err != nil {
		return nil, err
	}
	root, digest, err := e.memoryInput(r.Draft)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(e.policy.StateDir, "memory")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	// Retained failures consume the same fixed four-GiB evidence admission budget.
	// No API cleanup or automatic deletion can discard that evidence.
	used, err := memoryTreeBytes(parent)
	if err != nil {
		return nil, err
	}
	required := int64(8 << 20)
	if r.Draft.Action == "memory.evidence.import" {
		required, err = memoryTreeBytes(root)
		if err != nil {
			return nil, err
		}
		required += 8 << 20
	}
	var fs syscall.Statfs_t
	if err = syscall.Statfs(parent, &fs); err != nil || used+required > 4<<30 || uint64(fs.Bavail)*uint64(fs.Bsize) < uint64(required+32<<20) {
		return nil, errors.New("memory evidence storage budget/free-space reserve unavailable; retain evidence and archive only through reviewed offline maintenance")
	}
	dir := filepath.Join(parent, r.ID)
	if err = os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	if err = safefile.SyncDir(e.policy.StateDir); err != nil {
		return nil, err
	}
	if err = safefile.SyncDir(parent); err != nil {
		return nil, err
	}
	if r.Draft.Action == "memory.evidence.import" {
		_, err = mem.Copy(ctx, root, filepath.Join(dir, "inputs"), digest)
		if err != nil {
			return nil, err
		}
		s.EvidenceID = r.ID
	} else {
		m, err := mem.Verify(ctx, root, digest)
		if err != nil {
			return nil, err
		}
		output := filepath.Join(dir, "output")
		args := []string{"rocm", "serving-memory-plan", filepath.Join(root, "deployment.json"), filepath.Join(root, "workload.json"), filepath.Join(root, "resource-plan.json"), output, "--other-mib", strconv.FormatInt(r.Draft.Memory.OtherMiB, 10)}
		for _, id := range m.Observations {
			args = append(args, "--observation", filepath.Join(root, "observations", id, "startup"), filepath.Join(root, "observations", id, "serving"))
		}
		env := append(e.environment(), "PYTHONDONTWRITEBYTECODE=1")
		_, err = runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), args, env, lock, nil, 16<<10)
		if err != nil {
			// Raw planner stderr may contain baseline material; keep only a fixed refusal.
			return nil, errors.New("memory planner refused or was interrupted; retained evidence needs complete matching cold/warm phases, pressure-free counters and conservative headroom; inspect owner collection records")
		}
		if _, err = mem.Verify(ctx, root, digest); err != nil {
			return nil, err
		}
		checked, err := e.memoryPreview(ctx, r.Draft, r.Desired)
		if err != nil || !reflect.DeepEqual(checked.Preconditions, s.Preconditions) {
			return nil, errors.New("memory identity changed during generation; output retained but not exportable")
		}
		s, err = mem.ValidateOutput(ctx, output, root, e.policy.RuntimeRoot, m, r.Draft.Memory.OtherMiB)
		if err != nil {
			return nil, err
		}
		if err = mem.SyncOutput(output); err != nil {
			return nil, err
		}
		s.EvidenceID = r.Draft.Memory.EvidenceID
		s.SHA256 = digest
		s.Preconditions = checked.Preconditions
	}
	if err = durableJSON(filepath.Join(dir, "summary.json"), s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

func memoryTreeBytes(root string) (int64, error) {
	var size int64
	entries := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > 40000 || d.Type()&os.ModeSymlink != 0 {
			return errors.New("memory storage tree unsafe or over entry bound")
		}
		if d.IsDir() {
			return nil
		}
		s, err := d.Info()
		if err != nil || !s.Mode().IsRegular() {
			return errors.New("memory storage inode unavailable")
		}
		size += s.Size()
		if size > 4<<30 {
			return errors.New("memory storage exceeds four-GiB admission budget")
		}
		return nil
	})
	return size, err
}
func (e *Executor) MemoryPreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.MemorySummary, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !domain.MemoryAction(d.Action) || d.Memory == nil {
		return domain.MemorySummary{}, errors.New("invalid memory preview")
	}
	return e.memoryPreview(ctx, d, c)
}
func (e *Executor) MemoryArtifact(ctx context.Context, id, name string) (domain.Artifact, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !validID(id) || !mem.PrivateName(name) {
		return domain.Artifact{}, errors.New("unapproved memory artifact")
	}
	r, ok := e.records[id]
	if !ok || r.Request.Draft.Action != "memory.plan.export" || r.Result.State != "succeeded" {
		return domain.Artifact{}, errors.New("memory artifact requires proven helper completion")
	}
	// Recheck source, current baseline, hardware, runtime and evidence on every export.
	if _, err := e.memoryPreview(ctx, r.Request.Draft, r.Request.Desired); err != nil {
		return domain.Artifact{}, err
	}
	root, digest, err := e.memoryInput(r.Request.Draft)
	if err != nil {
		return domain.Artifact{}, err
	}
	m, err := mem.Verify(ctx, root, digest)
	if err != nil {
		return domain.Artifact{}, err
	}
	dir := filepath.Join(e.policy.StateDir, "memory", id, "output")
	s, err := mem.ValidateOutput(ctx, dir, root, e.policy.RuntimeRoot, m, r.Request.Draft.Memory.OtherMiB)
	if err != nil {
		return domain.Artifact{}, err
	}
	var retained domain.MemorySummary
	if json.Unmarshal(r.Result.Data, &retained) != nil || !reflect.DeepEqual(s.Artifacts, retained.Artifacts) {
		return domain.Artifact{}, errors.New("memory artifact no longer matches durable helper result")
	}
	for _, a := range s.Artifacts {
		if a.Name == name {
			b, err := mem.Read(filepath.Join(dir, name), 2<<20)
			if err != nil {
				return a, err
			}
			a.Content = string(b)
			return a, nil
		}
	}
	return domain.Artifact{}, errors.New("memory artifact missing")
}
