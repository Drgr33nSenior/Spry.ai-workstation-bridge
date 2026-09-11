// Package domain defines the shared versioned management contract.
package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const APIVersion = "v1"

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Failure) Error() string      { return e.Message }
func Fail(code, message string) error { return &Failure{code, message} }
func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Serving struct {
	Model            string  `json:"model"`
	Context          int     `json:"context"`
	Concurrency      int     `json:"concurrency"`
	MemoryFraction   float64 `json:"memory_fraction"`
	CPUOffloadGiB    int     `json:"cpu_offload_gib"`
	MaxRequestTokens int     `json:"max_request_tokens"`
	MaxOutputTokens  int     `json:"max_output_tokens"`
}
type Resources struct {
	CPU             int    `json:"cpu"`
	MemoryMiB       int64  `json:"memory_mib"`
	SharedMemoryMiB int64  `json:"shared_memory_mib"`
	GPUCount        int    `json:"gpu_count"`
	PhysicalGPU     string `json:"physical_gpu,omitempty"`
}
type CacheBudgets struct {
	ModelsGiB      int64 `json:"models_gib"`
	CompilerGiB    int64 `json:"compiler_gib"`
	ShaderGiB      int64 `json:"shader_gib"`
	BuildJobs      int   `json:"build_jobs"`
	BuildMemoryMiB int64 `json:"build_memory_mib"`
	ScratchGiB     int64 `json:"scratch_gib"`
}
type Configuration struct {
	Revision  string       `json:"revision"`
	Serving   Serving      `json:"serving"`
	Resources Resources    `json:"resources"`
	Caches    CacheBudgets `json:"caches"`
}

func (c Configuration) ContentRevision() string { c.Revision = ""; return Hash(c) }

type Draft struct {
	Action         string         `json:"action"`
	Target         string         `json:"target"`
	SourceRevision string         `json:"source_revision"`
	Model          string         `json:"model,omitempty"`
	Profile        string         `json:"profile,omitempty"`
	Recipe         string         `json:"recipe,omitempty"`
	RecoveryID     string         `json:"recovery_id,omitempty"`
	Serving        *Serving       `json:"serving,omitempty"`
	Resources      *Resources     `json:"resources,omitempty"`
	Caches         *CacheBudgets  `json:"caches,omitempty"`
	Memory         *MemoryRequest `json:"memory,omitempty"`
}
type Change struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type Preview struct {
	Changes       []Change          `json:"changes"`
	Consequences  []string          `json:"consequences"`
	Warnings      []string          `json:"warnings"`
	Preconditions map[string]string `json:"preconditions"`
}
type Plan struct {
	ID        string        `json:"id"`
	Actor     string        `json:"actor"`
	Draft     Draft         `json:"draft"`
	Desired   Configuration `json:"desired"`
	Preview   Preview       `json:"preview"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	Hash      string        `json:"hash"`
}
type Progress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Bytes   int64  `json:"bytes,omitempty"`
}
type Result struct {
	State            string     `json:"state"`
	Phase            string     `json:"phase"`
	Message          string     `json:"message"`
	RecoveryRequired bool       `json:"recovery_required"`
	Artifacts        []Artifact `json:"artifacts,omitempty"`
}
type Artifact struct {
	Name           string `json:"name"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	SourceRevision string `json:"source_revision"`
	Qualification  string `json:"qualification"`
	Content        string `json:"content,omitempty"`
}
type Execution struct {
	ID    string `json:"id"`
	Actor string `json:"actor"`
	Plan  Plan   `json:"plan"`
}
type Operation struct {
	ID               string     `json:"id"`
	Actor            string     `json:"actor"`
	Plan             Plan       `json:"plan"`
	State            string     `json:"state"`
	Phase            string     `json:"phase"`
	Message          string     `json:"message"`
	Revision         uint64     `json:"revision"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	SourceUpdated    bool       `json:"source_updated"`
	Dispatched       bool       `json:"dispatched"`
	LiveApplied      bool       `json:"live_applied"`
	RecoveryRequired bool       `json:"recovery_required"`
	CancelRequested  bool       `json:"cancel_requested"`
	Events           []Progress `json:"events"`
	Artifacts        []Artifact `json:"artifacts,omitempty"`
}

func Terminal(s string) bool {
	return s == "succeeded" || s == "failed" || s == "cancelled" || s == "recovery-required"
}

type ModelFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	GitSHA1 string `json:"git_sha1,omitempty"`
	Size    int64  `json:"size"`
	URL     string `json:"url,omitempty"`
}
type Model struct {
	ID            string      `json:"id"`
	Repository    string      `json:"repository"`
	Revision      string      `json:"revision"`
	Quantization  string      `json:"quantization"`
	License       string      `json:"license"`
	Size          int64       `json:"size"`
	Status        string      `json:"status"`
	Qualification string      `json:"qualification"`
	GPUCount      int         `json:"gpu_count"`
	Files         []ModelFile `json:"files"`
}
type GPU struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	RenderPath string `json:"render_path"`
	MemoryMiB  int64  `json:"memory_mib"`
}
type Hardware struct {
	Status           string    `json:"status"`
	ObservedAt       time.Time `json:"observed_at"`
	BootID           string    `json:"boot_id"`
	CurrentBootID    string    `json:"current_boot_id"`
	MemoryMiB        int64     `json:"memory_mib"`
	DIMMs            int       `json:"dimms"`
	TrainedSpeed     string    `json:"trained_speed"`
	Channels         string    `json:"channels"`
	CPUThreads       int       `json:"cpu_threads"`
	SMTWidth         int       `json:"smt_width"`
	TopologyKnown    bool      `json:"topology_known"`
	CPUManagerPolicy string    `json:"cpu_manager_policy"`
	FullPCPUsOnly    bool      `json:"full_pcpus_only"`
	HostReserveMiB   int64     `json:"host_reserve_mib"`
	KubeReserveMiB   int64     `json:"kube_reserve_mib"`
	OtherMemoryMiB   int64     `json:"other_memory_mib"`
	ReservedCPU      int       `json:"reserved_cpu"`
	OtherCPU         int       `json:"other_cpu"`
	OtherGPUs        int       `json:"other_gpus"`
	GPUs             []GPU     `json:"gpus"`
	Warnings         []string  `json:"warnings"`
}
type Recipe struct {
	ID            string `json:"id"`
	Revision      string `json:"revision"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	Output        string `json:"output"`
	Qualification string `json:"qualification"`
}
type Cache struct {
	Name           string   `json:"name"`
	UsedBytes      int64    `json:"used_bytes"`
	BudgetBytes    int64    `json:"budget_bytes"`
	Status         string   `json:"status"`
	CleanupPreview []string `json:"cleanup_preview"`
}
type Harness struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Modes       []string `json:"modes"`
	Limitations []string `json:"limitations"`
}
type Inventory struct {
	Mode             string    `json:"mode"`
	Target           string    `json:"target"`
	Environment      string    `json:"environment"`
	SourceRevision   string    `json:"source_revision"`
	ClusterAvailable bool      `json:"cluster_available"`
	ClusterMessage   string    `json:"cluster_message"`
	Profile          string    `json:"profile"`
	Hardware         Hardware  `json:"hardware"`
	Models           []Model   `json:"models"`
	Recipes          []Recipe  `json:"recipes"`
	Caches           []Cache   `json:"caches"`
	Harnesses        []Harness `json:"harnesses"`
}
type BundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}
type Bundle struct {
	Harness     string       `json:"harness"`
	Revision    string       `json:"revision"`
	Files       []BundleFile `json:"files"`
	Limitations []string     `json:"limitations"`
}
type Adapter interface {
	Snapshot(context.Context) (Inventory, error)
	Validate(context.Context, Draft, Configuration) (Preview, error)
	Execute(context.Context, Execution, func(Progress) error) (Result, error)
	Inspect(context.Context, string) (Result, error)
	Export(context.Context, string) (Bundle, error)
}

func RoleAllows(role, action string) bool {
	if role == "owner" {
		return true
	}
	return role == "operator" && (action == "profile.switch" || action == "profile.restore")
}
func Desired(current Configuration, d Draft) (Configuration, error) {
	c := current
	if d.Serving != nil {
		c.Serving = *d.Serving
	}
	if d.Resources != nil {
		c.Resources = *d.Resources
	}
	if d.Caches != nil {
		c.Caches = *d.Caches
	}
	if d.Model != "" && d.Action == "serving.configure" {
		c.Serving.Model = d.Model
	}
	c.Revision = c.ContentRevision()
	return c, nil
}
func ValidateDraft(d Draft, c Configuration, inv Inventory) error {
	if d.Target != inv.Target {
		return Fail("target_mismatch", "confirm the configured target exactly")
	}
	if d.SourceRevision != c.Revision {
		return Fail("source_drift", "source changed; refresh configuration and create a new plan")
	}
	switch d.Action {
	case "serving.configure", "resources.configure", "caches.configure", "serving.start", "serving.stop", "serving.restart", "model.stage", "model.verify", "profile.switch", "profile.restore", "hardware.refresh", "build.start", "cpu-policy.export", "operation.reconcile", "memory.evidence.import", "memory.plan.export":
	default:
		return Fail("unsupported", "unsupported operation")
	}
	if err := ValidateMemoryRequest(d); err != nil {
		return err
	}
	if d.Serving != nil && d.Action != "serving.configure" {
		return Fail("invalid", "serving fields belong only to serving.configure")
	}
	if d.Model != "" && d.Action != "serving.configure" && d.Action != "model.stage" && d.Action != "model.verify" {
		return Fail("invalid", "serving fields do not belong to this action")
	}
	if d.Serving != nil && d.Model != "" && d.Serving.Model != d.Model {
		return Fail("invalid", "model selection conflicts with serving.model")
	}
	if d.Resources != nil && d.Action != "resources.configure" && d.Action != "serving.configure" {
		return Fail("invalid", "resource fields do not belong to this action")
	}
	if d.Caches != nil && d.Action != "caches.configure" {
		return Fail("invalid", "cache fields do not belong to this action")
	}
	if d.Profile != "" && d.Action != "profile.switch" {
		return Fail("invalid", "profile does not belong to this action")
	}
	if d.Recipe != "" && d.Action != "build.start" {
		return Fail("invalid", "recipe does not belong to this action")
	}
	if d.Action == "profile.switch" && d.Profile != "ai" && d.Profile != "gaming" && d.Profile != "maintenance" {
		return Fail("invalid", "profile must be ai, gaming or maintenance")
	}
	if d.Action == "model.stage" || d.Action == "model.verify" {
		found := false
		for _, m := range inv.Models {
			if m.ID == d.Model {
				found = true
			}
		}
		if !found {
			return Fail("invalid", "select a reviewed model ID")
		}
	}
	if d.Action == "build.start" {
		found := false
		for _, r := range inv.Recipes {
			if r.ID == d.Recipe {
				found = true
				if r.Status != "available" {
					return Fail("unavailable", r.Reason)
				}
			}
		}
		if !found {
			return Fail("unsupported", "select a reviewed build recipe")
		}
	}
	next, _ := Desired(c, d)
	if d.Action == "serving.configure" || d.Action == "resources.configure" || d.Action == "serving.start" || d.Action == "serving.restart" {
		s := next.Serving
		r := next.Resources
		h := inv.Hardware
		if s.Context < 128 || s.Context > 131072 || s.Concurrency < 1 || s.Concurrency > 32 || s.MemoryFraction < 0.1 || s.MemoryFraction > 0.95 || s.CPUOffloadGiB < 0 || s.CPUOffloadGiB > 32 {
			return Fail("invalid", "serving options exceed reviewed limits")
		}
		if s.MaxRequestTokens != 0 || s.MaxOutputTokens != 0 {
			return Fail("unsupported", "the pinned deployment cannot enforce global per-request input/output caps; use client limits")
		}
		if r.PhysicalGPU != "" {
			return Fail("unsupported", "device-plugin GPU counts cannot select a physical card")
		}
		if r.CPU < 1 || r.CPU > 4096 || r.MemoryMiB < 1024 || r.MemoryMiB > 1<<30 || r.SharedMemoryMiB < 64 || r.SharedMemoryMiB >= r.MemoryMiB || r.GPUCount < 1 || r.GPUCount > 2 {
			return Fail("invalid", "invalid workload budget")
		}
		if !h.TopologyKnown || h.SMTWidth < 1 {
			return Fail("unavailable", "CPU topology is unknown; collect current hardware evidence")
		}
		if h.MemoryMiB <= 0 || h.MemoryMiB > 1<<30 || h.CPUThreads < 1 || h.CPUThreads > 4096 || h.HostReserveMiB < 0 || h.KubeReserveMiB < 0 || h.OtherMemoryMiB < 0 || h.ReservedCPU < 0 || h.OtherCPU < 0 || h.OtherGPUs < 0 {
			return Fail("unavailable", "invalid or incomplete resource accounting evidence")
		}
		if h.BootID == "" || h.BootID != h.CurrentBootID || time.Since(h.ObservedAt) > 24*time.Hour || time.Until(h.ObservedAt) > time.Minute {
			return Fail("stale_evidence", "hardware evidence is stale or belongs to another boot")
		}
		if h.CPUManagerPolicy != "static" || !h.FullPCPUsOnly || r.CPU%h.SMTWidth != 0 {
			return Fail("invalid", "Guaranteed QoS requires whole discovered SMT cores and static/full-pcpus-only CPU Manager; export a host maintenance plan")
		}
		if r.CPU+h.ReservedCPU+h.OtherCPU > h.CPUThreads || r.MemoryMiB+h.HostReserveMiB+h.KubeReserveMiB+h.OtherMemoryMiB > h.MemoryMiB || r.GPUCount+h.OtherGPUs > len(h.GPUs) {
			return Fail("capacity", "budget exceeds capacity after host, Kubernetes and other workload reserves")
		}
		if int64(s.CPUOffloadGiB)*1024+r.SharedMemoryMiB >= r.MemoryMiB {
			return Fail("capacity", "offload and shared-memory budget leave no process memory")
		}
		found := false
		for _, m := range inv.Models {
			if m.ID == s.Model {
				found = true
				if m.GPUCount > r.GPUCount {
					return Fail("capacity", "selected model requires its reviewed GPU count; quantization and context were not reduced")
				}
			}
		}
		if !found {
			return Fail("unsupported", "select a reviewed model")
		}
	}
	if d.Action == "caches.configure" {
		b := next.Caches
		if b.ModelsGiB < 1 || b.ModelsGiB > 8192 || b.CompilerGiB < 1 || b.CompilerGiB > 1024 || b.ShaderGiB < 1 || b.ShaderGiB > 1024 || b.BuildJobs < 1 || b.BuildJobs > 24 || b.BuildMemoryMiB < 2048 || b.BuildMemoryMiB > 49152 || b.ScratchGiB < 1 || b.ScratchGiB > 1024 {
			return Fail("invalid", "cache/build budgets exceed reviewed bounds")
		}
	}
	return nil
}
func Describe(d Draft) string { return fmt.Sprintf("%s on %s", d.Action, d.Target) }
