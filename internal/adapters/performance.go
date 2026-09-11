package adapters

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	perf "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/performance"
)

func (l *Live) PerformancePreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.PerformanceSummary, error) {
	return l.host.PerformancePreview(ctx, d, c)
}
func (l *Live) PerformanceArtifact(ctx context.Context, id, name string) (domain.Artifact, error) {
	return l.host.PerformanceArtifact(ctx, id, name)
}
func performanceArtifacts(data []byte, revision string) ([]domain.Artifact, error) {
	var s domain.PerformanceSummary
	if len(data) > 32<<10 {
		return nil, errors.New("performance summary exceeds bound")
	}
	if err := config.Decode(data, &s); err != nil {
		return nil, err
	}
	if err := perf.ValidateSummary(s); err != nil {
		return nil, err
	}
	for _, a := range s.Artifacts {
		if a.SourceRevision != revision {
			return nil, errors.New("performance artifact source changed")
		}
	}
	b, _ := json.Marshal(s)
	out := append([]domain.Artifact{}, s.Artifacts...)
	return append(out, domain.Artifact{Name: "performance-summary.json", SHA256: perf.Sum(b), Size: int64(len(b)), SourceRevision: revision, Qualification: s.Status, Content: string(b)}), nil
}
func (d *Demo) PerformancePreview(_ context.Context, draft domain.Draft, c domain.Configuration) (domain.PerformanceSummary, error) {
	if err := domain.ValidatePerformanceRequest(draft); err != nil {
		return domain.PerformanceSummary{}, err
	}
	p := draft.Performance
	s := domain.PerformanceSummary{Schema: 1, EvidenceID: p.EvidenceID, SHA256: p.EvidenceSHA256, Kind: p.Kind, Status: "demo-only-unqualified", Reason: "synthetic_evidence_not_hardware", Fields: []domain.PerformanceField{{Name: "throughput", Value: "unknown", Unit: "tokens/s", State: "not-measured"}}, Preconditions: map[string]string{"performance_manifest": p.EvidenceSHA256, "performance_source": c.Revision}, Limitations: []string{"DEMO: fixture only; no measured gain, code execution, inference, workload change, cache deletion or qualification."}}
	if p.EvidenceID == "demo-incomplete" {
		s.Status = "incomplete"
		s.Reason = "missing_required_observations"
	}
	if p.EvidenceID == "demo-refused" {
		return s, domain.Fail("invalid", "DEMO: incompatible model/workload identity or undeclared experiment difference")
	}
	if p.Kind == "profile-selection" {
		s.Status = "selected-unqualified"
		s.Reason = "DEMO_owner_selected_configuration_only_not_deployed"
	}
	return s, nil
}
func (d *Demo) executePerformance(ctx context.Context, x domain.Execution) (domain.Result, error) {
	s, err := d.PerformancePreview(ctx, x.Plan.Draft, x.Plan.Desired)
	if err != nil {
		return domain.Result{State: "failed", Phase: "performance-refused", Message: err.Error()}, err
	}
	b, _ := json.Marshal(s)
	artifacts, err := performanceArtifacts(b, x.Plan.Desired.Revision)
	if err != nil {
		return domain.Result{}, err
	}
	r := domain.Result{State: "succeeded", Phase: "demo-performance-export", Message: "DEMO evidence analysis only; no source, workload or cache mutation", Artifacts: artifacts}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Results[x.ID] = r
	return r, saveJSON(d.dir, "state.json", d.state)
}
func (d *Demo) PerformanceArtifact(_ context.Context, id, name string) (domain.Artifact, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.state.Results[id]
	if ok && r.State == "succeeded" {
		for _, a := range r.Artifacts {
			if a.Name == name && name == "performance-summary.json" {
				return a, nil
			}
		}
	}
	return domain.Artifact{}, domain.Fail("not_found", "demo artifact does not exist")
}
