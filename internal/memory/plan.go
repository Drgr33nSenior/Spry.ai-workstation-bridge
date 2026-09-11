package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"unicode/utf16"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type Window struct {
	Samples   int              `json:"samples"`
	Duration  float64          `json:"duration_seconds"`
	CgroupID  string           `json:"cgroup_id"`
	FirstMono int64            `json:"first_monotonic_ns"`
	LastMono  int64            `json:"last_monotonic_ns"`
	Path      string           `json:"path"`
	FirstUnix int64            `json:"first_unix_ns"`
	LastUnix  int64            `json:"last_unix_ns"`
	Sampled   int64            `json:"sampled_current_max_bytes"`
	Peak      int64            `json:"lifetime_peak_bytes"`
	Stat      map[string]int64 `json:"sampled_stat_max_bytes"`
	Available int64            `json:"host_available_min_bytes"`
	Envelope  int64            `json:"full_shm_envelope_bytes"`
}
type Observation struct {
	Phase   string `json:"phase"`
	PodUID  string `json:"pod_uid"`
	Startup Window `json:"startup"`
	Steady  Window `json:"steady"`
}
type Plan struct {
	Schema          int    `json:"schema"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	Baseline        int64  `json:"baseline_mib"`
	Candidate       int64  `json:"candidate_mib"`
	Minimum         int64  `json:"minimum_candidate_mib"`
	WorkloadHash    string `json:"workload_sha256"`
	RuntimeIdentity string `json:"runtime_identity"`
	BaselineHash    string `json:"baseline_spec_sha256"`
	CandidateHash   string `json:"candidate_spec_sha256"`
	Budget          struct {
		Other       int64 `json:"other_workloads_mib"`
		Allocatable int64 `json:"allocatable_mib"`
		SHM         int64 `json:"shm_limit_mib"`
		Envelope    int64 `json:"observed_envelope_bytes"`
		Headroom    int64 `json:"headroom_bytes"`
	} `json:"budget"`
	Observations []Observation     `json:"observations"`
	Evidence     map[string]string `json:"evidence_sha256"`
	Tools        map[string]string `json:"tool_sha256"`
	Artifacts    map[string]string `json:"artifacts_sha256"`
	Limitations  []string          `json:"limitations"`
}
type Patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// Identity matches model_kernels.identity (sorted compact ASCII JSON, no newline).
func Identity(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	raw := bytes.TrimSuffix(b.Bytes(), []byte{'\n'})
	var ascii bytes.Buffer
	for _, r := range string(raw) {
		if r < 128 {
			ascii.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&ascii, `\u%04x`, r)
		} else {
			a, b := utf16.EncodeRune(r)
			fmt.Fprintf(&ascii, `\u%04x\u%04x`, a, b)
		}
	}
	return Sum(ascii.Bytes())
}
func summaryWindow(w Window) domain.MemoryWindow {
	return domain.MemoryWindow{Samples: w.Samples, DurationSeconds: w.Duration, SampledPeakBytes: w.Sampled, LifetimePeakBytes: w.Peak, SharedMemoryBytes: w.Stat["shmem"], HostAvailableMinBytes: w.Available}
}

// Observe summarizes only bounded, structurally valid pod-cgroup windows. It is
// not the planner's full workload/correctness qualification.
func Observe(ctx context.Context, input string, m Manifest, limit, shm int64) ([]domain.MemoryObservation, error) {
	r, e := RawJSON(filepath.Join(input, "resource-plan.json"))
	if e != nil {
		return nil, e
	}
	total, ok := Nested(r, "memory", "total_mib").(float64)
	if !ok || total <= 0 || total > 1<<30 {
		return nil, errors.New("resource memory missing")
	}
	rows := []domain.MemoryObservation{}
	for _, id := range m.Observations {
		start, e := RawJSON(filepath.Join(input, "observations", id, "startup/startup.json"))
		if e != nil {
			return rows, e
		}
		phase, _ := start["cache_state"].(string)
		pod, _ := Nested(start, "pod", "uid").(string)
		if (phase != "cold" && phase != "warm") || len(pod) != 36 || start["status"] != "observed-not-qualified" {
			return rows, errors.New("failed or incomplete startup")
		}
		result, err := RawJSON(filepath.Join(input, "observations", id, "serving/result.json"))
		if err != nil || result["status"] != "measured-not-qualified" {
			return rows, errors.New("failed or incomplete serving observation")
		}
		a, e := observeWindow(ctx, filepath.Join(input, "observations", id, "startup/memory.jsonl"), limit, shm, int64(total))
		if e != nil {
			return rows, e
		}
		b, e := observeWindow(ctx, filepath.Join(input, "observations", id, "serving/host-telemetry.jsonl"), limit, shm, int64(total))
		if e != nil {
			return rows, e
		}
		rows = append(rows, domain.MemoryObservation{Phase: phase, PodID: pod, Startup: summaryWindow(a), Steady: summaryWindow(b)})
	}
	return rows, nil
}
func ValidateOutput(ctx context.Context, dir, input, runtime string, m Manifest, other int64) (domain.MemorySummary, error) {
	var p Plan
	s := domain.MemorySummary{}
	if err := checkOutputTree(dir); err != nil {
		return s, err
	}
	fail := func() (domain.MemorySummary, error) {
		return s, errors.New("installer memory plan schema, identity or conservative-budget verification failed")
	}
	if e := JSON(filepath.Join(dir, "plan.json"), &p); e != nil {
		return s, e
	}
	if p.Schema != 1 || p.Kind != "sglang-host-memory-plan" || p.Status != "plan-only-unqualified" || p.Baseline <= 0 || p.Baseline > 1<<30 || p.Candidate <= 0 || p.Candidate >= p.Baseline || p.Candidate != p.Minimum || p.Candidate%256 != 0 || len(p.Observations) < 4 || len(p.Observations) > 20 || len(p.Limitations) != 6 {
		return fail()
	}
	for _, h := range []string{p.WorkloadHash, p.RuntimeIdentity, p.BaselineHash, p.CandidateHash} {
		if !Digest.MatchString(h) {
			return fail()
		}
	}
	if p.WorkloadHash != m.Files["workload.json"] || p.Budget.Other != other || other < 0 || p.Budget.Allocatable <= 0 || p.Candidate+other > p.Budget.Allocatable || p.Budget.SHM <= 0 || p.Budget.SHM >= p.Baseline {
		return fail()
	}
	expected := map[string]string{filepath.Join(runtime, "versions.lock"): ""}
	lock, _, e := HashFile(ctx, filepath.Join(runtime, "versions.lock"))
	if e != nil {
		return s, e
	}
	expected[filepath.Join(runtime, "versions.lock")] = lock
	for name, digest := range m.Files {
		expected[filepath.Join(input, name)] = digest
	}
	if !reflect.DeepEqual(expected, p.Evidence) || len(p.Tools) != len(Tools) {
		return fail()
	}
	for _, name := range Tools {
		h, _, e := HashFile(ctx, filepath.Join(runtime, "lib/workstation", name))
		if e != nil {
			return s, e
		}
		if p.Tools[name] != h {
			return fail()
		}
	}
	baseline, e := RawJSON(filepath.Join(input, "deployment.json"))
	if e != nil {
		return s, e
	}
	spec := Nested(baseline, "spec", "template", "spec")
	resource, e := RawJSON(filepath.Join(input, "resource-plan.json"))
	if e != nil {
		return s, e
	}
	if Nested(resource, "memory", "allocatable_mib") != float64(p.Budget.Allocatable) || len(m.Observations) != len(p.Observations) {
		return fail()
	}
	volumes, _ := Nested(baseline, "spec", "template", "spec", "volumes").([]any)
	shmFound := false
	for _, v := range volumes {
		volume, _ := v.(map[string]any)
		if volume["name"] == "shm" {
			n, err := MiB(Nested(volume, "emptyDir", "sizeLimit"))
			if err != nil || n != p.Budget.SHM {
				return fail()
			}
			shmFound = true
		}
	}
	if !shmFound {
		return fail()
	}
	var patch, rollback []Patch
	if e = JSON(filepath.Join(dir, "patch.json"), &patch); e != nil {
		return s, e
	}
	if e = JSON(filepath.Join(dir, "rollback.json"), &rollback); e != nil {
		return s, e
	}
	if len(patch) != 2 || len(rollback) != 2 || len(p.Artifacts) != 2 {
		return fail()
	}
	for _, rows := range [][]Patch{patch, rollback} {
		if rows[0].Op != "test" || rows[0].Path != "/spec/template/spec" || rows[1].Op != "replace" || rows[1].Path != "/spec/template/spec/containers/0/resources" {
			return fail()
		}
	}
	if !reflect.DeepEqual(patch[0].Value, spec) || Identity(spec) != p.BaselineHash {
		return fail()
	}
	b, _ := json.Marshal(spec)
	var candidate map[string]any
	_ = json.Unmarshal(b, &candidate)
	containers, ok := candidate["containers"].([]any)
	if !ok || len(containers) != 1 {
		return fail()
	}
	container, ok := containers[0].(map[string]any)
	if !ok || container["name"] != "sglang" {
		return fail()
	}
	original := container["resources"]
	resources, ok := original.(map[string]any)
	if !ok {
		return fail()
	}
	if !reflect.DeepEqual(resources["requests"], resources["limits"]) {
		return fail()
	}
	old, e := MiB(Nested(resources, "limits", "memory"))
	if e != nil || old != p.Baseline {
		return fail()
	}
	origBytes, _ := json.Marshal(original)
	for _, key := range []string{"requests", "limits"} {
		r, ok := resources[key].(map[string]any)
		if !ok {
			return fail()
		}
		r["memory"] = strconv.FormatInt(p.Candidate, 10) + "Mi"
	}
	var originalResources any
	_ = json.Unmarshal(origBytes, &originalResources)
	if !reflect.DeepEqual(patch[1].Value, resources) || !reflect.DeepEqual(rollback[0].Value, candidate) || !reflect.DeepEqual(rollback[1].Value, originalResources) || Identity(candidate) != p.CandidateHash {
		return fail()
	}
	seen := map[string]bool{}
	var envelope int64
	for index, o := range p.Observations {
		start, err := RawJSON(filepath.Join(input, "observations", m.Observations[index], "startup/startup.json"))
		if err != nil {
			return s, err
		}
		result, err := RawJSON(filepath.Join(input, "observations", m.Observations[index], "serving/result.json"))
		if err != nil {
			return s, err
		}
		key := map[string]any{"runtime": result["runtime"], "image_id": Nested(result, "pod", "image_id"), "node": Nested(result, "pod", "node")}
		if Identity(key) != p.RuntimeIdentity || start["cache_state"] != o.Phase || Nested(result, "pod", "uid") != o.PodUID || Nested(start, "pod", "uid") != o.PodUID || start["status"] != "observed-not-qualified" || result["status"] != "measured-not-qualified" {
			return fail()
		}
		total, ok := Nested(resource, "memory", "total_mib").(float64)
		if !ok || total <= 0 || total > 1<<30 {
			return fail()
		}
		startup, err := observeWindow(ctx, filepath.Join(input, "observations", m.Observations[index], "startup/memory.jsonl"), p.Baseline, p.Budget.SHM, int64(total))
		if err != nil {
			return s, err
		}
		steady, err := observeWindow(ctx, filepath.Join(input, "observations", m.Observations[index], "serving/host-telemetry.jsonl"), p.Baseline, p.Budget.SHM, int64(total))
		if err != nil {
			return s, err
		}
		if !reflect.DeepEqual(startup, o.Startup) || !reflect.DeepEqual(steady, o.Steady) {
			return fail()
		}
		if seen[o.PodUID] || len(o.PodUID) != 36 {
			return fail()
		}
		seen[o.PodUID] = true
		switch o.Phase {
		case "cold":
			s.Cold++
		case "warm":
			s.Warm++
		default:
			return fail()
		}
		for i, w := range []Window{o.Startup, o.Steady} {
			if w.Samples < 2 || w.Samples > 100000 || w.Duration < 0 || math.IsNaN(w.Duration) || math.IsInf(w.Duration, 0) || w.FirstMono <= 0 || w.LastMono <= w.FirstMono || w.FirstUnix <= 0 || w.LastUnix <= w.FirstUnix || w.Sampled <= 0 || w.Sampled > w.Peak || w.Peak > p.Baseline<<20 || w.Available < 0 || len(w.Stat) != 3 || w.Stat["anon"] < 0 || w.Stat["file"] < 0 || w.Stat["shmem"] < 0 || w.Stat["shmem"] > w.Stat["file"] || w.Envelope != w.Peak+(p.Budget.SHM<<20) || w.CgroupID == "" || w.Path == "" {
				return fail()
			}
			if i == 1 && w.Duration < 60 {
				return fail()
			}
			if w.Envelope > envelope {
				envelope = w.Envelope
			}
		}
		if o.Startup.CgroupID != o.Steady.CgroupID || o.Startup.Path != o.Steady.Path || o.Startup.LastUnix > o.Steady.FirstUnix {
			return fail()
		}
		s.Observations = append(s.Observations, domain.MemoryObservation{Phase: o.Phase, PodID: o.PodUID, Startup: summaryWindow(o.Startup), Steady: summaryWindow(o.Steady)})
	}
	headroom := max(int64(2048<<20), (envelope+3)/4)
	candidateMiB := ((envelope + headroom + (256 << 20) - 1) / (256 << 20)) * 256
	if s.Cold < 2 || s.Warm < 2 || p.Budget.Envelope != envelope || p.Budget.Headroom != headroom || candidateMiB != p.Minimum {
		return fail()
	}
	for _, name := range []string{"patch.json", "rollback.json"} {
		b, e := Read(filepath.Join(dir, name), 2<<20)
		if e != nil {
			return s, e
		}
		if Sum(b) != p.Artifacts[name] {
			return fail()
		}
	}
	for _, name := range []string{"plan.json", "patch.json", "rollback.json"} {
		b, e := Read(filepath.Join(dir, name), 2<<20)
		if e != nil {
			return s, e
		}
		a := domain.Artifact{Name: name, SHA256: Sum(b), Size: int64(len(b)), SourceRevision: m.SourceRevision, Qualification: "plan-only-unqualified"}
		if e := checkArtifactWireSize(a, b); e != nil {
			return s, e
		}
		s.Artifacts = append(s.Artifacts, a)
	}
	s.Status = p.Status
	s.Reason = "conservative_lifetime_peak_plus_full_shm_and_headroom"
	s.BaselineMiB = p.Baseline
	s.LimitedMiB = p.Baseline
	s.CandidateMiB = p.Candidate
	s.MinimumCandidateMiB = p.Minimum
	s.SharedMemoryMiB = p.Budget.SHM
	s.OtherMiB = other
	s.AllocatableMiB = p.Budget.Allocatable
	s.EnvelopeBytes = envelope
	s.HeadroomBytes = headroom
	s.Limitations = []string{"Unqualified offline candidate; no resize, restart, source update or qualification.", "Cold/warm labels are owner declarations; lifetime peak includes unsampled startup.", "Full shm growth is reserved inside the Pod budget; sampled shmem is not /dev/shm attribution.", "Repeat startup, steady memory, numerical/coding correctness and latency/throughput at the smaller limit.", "On OOM, PSI or quality/performance regression retain and use the exact preconditioned rollback.", "Regenerate compilation-worker resource plans after resizing."}
	return s, nil
}

func checkArtifactWireSize(a domain.Artifact, content []byte) error {
	a.Content = string(content)
	wire, err := json.Marshal(a)
	// The helper protocol has a four-MiB response limit. JSON escaping can
	// enlarge a private Pod spec; leave room for the result envelope, too.
	if err != nil || len(wire) > 3<<20 {
		return errors.New("private memory artifact exceeds bounded helper export response")
	}
	return nil
}
