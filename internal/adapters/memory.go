package adapters

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	mem "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
)

func (l *Live) MemoryPreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.MemorySummary, error) {
	return l.host.MemoryPreview(ctx, d, c)
}
func (l *Live) MemoryArtifact(ctx context.Context, id, name string) (domain.Artifact, error) {
	return l.host.MemoryArtifact(ctx, id, name)
}
func memoryArtifacts(data []byte, revision string) ([]domain.Artifact, error) {
	var s domain.MemorySummary
	if len(data) > 128<<10 {
		return nil, errors.New("memory summary exceeds bound")
	}
	if e := config.Decode(data, &s); e != nil {
		return nil, e
	}
	for _, a := range s.Artifacts {
		if !mem.PrivateName(a.Name) || a.Content != "" || !mem.Digest.MatchString(a.SHA256) || a.Size <= 0 || a.Size > 2<<20 {
			return nil, errors.New("private memory artifacts must contain only bounded metadata")
		}
	}
	b, _ := json.Marshal(s)
	out := append([]domain.Artifact{}, s.Artifacts...)
	out = append(out, domain.Artifact{Name: "memory-summary.json", SHA256: mem.Sum(b), Size: int64(len(b)), SourceRevision: revision, Qualification: s.Status, Content: string(b)})
	return out, nil
}
func (d *Demo) MemoryPreview(_ context.Context, draft domain.Draft, c domain.Configuration) (domain.MemorySummary, error) {
	if err := domain.ValidateMemoryRequest(draft); err != nil {
		return domain.MemorySummary{}, err
	}
	m := draft.Memory
	s := domain.MemorySummary{EvidenceID: m.EvidenceID, SHA256: m.EvidenceSHA256, Status: "demo-only", Reason: "synthetic_memory_fixture_not_hardware", BaselineMiB: c.Resources.MemoryMiB, LimitedMiB: c.Resources.MemoryMiB, SharedMemoryMiB: c.Resources.SharedMemoryMiB, OtherMiB: m.OtherMiB, Preconditions: map[string]string{"memory_manifest": m.EvidenceSHA256, "memory_source": c.Revision}, Limitations: []string{"DEMO: no host observation, cloud call, planner execution, RAM saving or qualification."}}
	if m.EvidenceID == "demo-incomplete" {
		s.Status = "incomplete"
		s.Reason = "missing_cold_warm_final_samples"
		if draft.Action == "memory.plan.export" {
			return s, domain.Fail("invalid", "DEMO: missing cold/warm observations and final telemetry")
		}
	}
	if m.EvidenceID == "demo-refused" && draft.Action == "memory.plan.export" {
		return s, domain.Fail("invalid", "DEMO: memory PSI/OOM event; reduction is not justified")
	}
	if draft.Action == "memory.plan.export" {
		s.Status = "plan-only-unqualified"
		s.Reason = "DEMO_synthetic_candidate"
		s.CandidateMiB = c.Resources.MemoryMiB - 256
		s.MinimumCandidateMiB = s.CandidateMiB
		s.Cold = 2
		s.Warm = 2
	}
	return s, nil
}
func (d *Demo) MemoryArtifact(_ context.Context, id, name string) (domain.Artifact, error) {
	return domain.Artifact{}, domain.Fail("unavailable", "demo does not export executable Pod patches; use the source contract fixtures")
}
func (d *Demo) executeMemory(ctx context.Context, x domain.Execution) (domain.Result, error) {
	s, e := d.MemoryPreview(ctx, x.Plan.Draft, x.Plan.Desired)
	if e != nil {
		return domain.Result{State: "failed", Phase: "memory-refused", Message: e.Error()}, e
	}
	if x.Plan.Draft.Action == "memory.evidence.import" {
		s.EvidenceID = x.ID
	}
	b, _ := json.Marshal(s)
	a, e := memoryArtifacts(b, x.Plan.Desired.Revision)
	r := domain.Result{State: "succeeded", Phase: "demo-memory-export", Message: "DEMO: unqualified fixture evidence only; no source or workload change", Artifacts: a}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Results[x.ID] = r
	return r, saveJSON(d.dir, "state.json", d.state)
}
