package domain

import (
	"context"
	"regexp"
)

// MemoryRequest names sealed evidence, never a client-supplied filesystem path.
type MemoryRequest struct {
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	OtherMiB       int64  `json:"other_mib"`
}
type MemoryWindow struct {
	Samples               int     `json:"samples"`
	DurationSeconds       float64 `json:"duration_seconds"`
	SampledPeakBytes      int64   `json:"sampled_peak_bytes"`
	LifetimePeakBytes     int64   `json:"lifetime_peak_bytes"`
	SharedMemoryBytes     int64   `json:"shared_memory_bytes"`
	HostAvailableMinBytes int64   `json:"host_available_min_bytes"`
}
type MemoryObservation struct {
	Phase   string       `json:"phase"`
	PodID   string       `json:"pod_id"`
	Startup MemoryWindow `json:"startup"`
	Steady  MemoryWindow `json:"steady"`
}

// No raw Pod spec, environment, traces, prompts or provider text belongs here.
type MemorySummary struct {
	EvidenceID                      string              `json:"evidence_id"`
	SHA256                          string              `json:"sha256"`
	Status                          string              `json:"status"`
	Reason                          string              `json:"reason"`
	BaselineMiB                     int64               `json:"baseline_mib"`
	LimitedMiB                      int64               `json:"limited_mib"`
	CandidateMiB                    int64               `json:"candidate_mib"`
	MinimumCandidateMiB             int64               `json:"minimum_candidate_mib"`
	SharedMemoryMiB                 int64               `json:"shared_memory_mib"`
	OtherMiB                        int64               `json:"other_mib"`
	TelemetryProfile                string              `json:"telemetry_profile,omitempty"`
	TelemetryReserveMiB             int64               `json:"telemetry_reserve_mib,omitempty"`
	TelemetryComponentLimitsMiB     map[string]int64    `json:"telemetry_component_limits_mib,omitempty"`
	TelemetryMarginMiB              int64               `json:"telemetry_margin_mib,omitempty"`
	TelemetryCalculatedAllowanceMiB int64               `json:"telemetry_calculated_allowance_mib,omitempty"`
	AllocatableMiB                  int64               `json:"allocatable_mib"`
	EnvelopeBytes                   int64               `json:"envelope_bytes"`
	HeadroomBytes                   int64               `json:"headroom_bytes"`
	Cold                            int                 `json:"cold"`
	Warm                            int                 `json:"warm"`
	Observations                    []MemoryObservation `json:"observations"`
	Preconditions                   map[string]string   `json:"preconditions"`
	Limitations                     []string            `json:"limitations"`
	Artifacts                       []Artifact          `json:"artifacts"`
}
type MemoryArtifactRequest struct {
	OperationID string `json:"operation_id"`
	Name        string `json:"name"`
}
type MemoryOperationRequest struct {
	OperationID string `json:"operation_id"`
}
type MemoryAdapter interface {
	MemoryPreview(context.Context, Draft, Configuration) (MemorySummary, error)
	MemoryArtifact(context.Context, string, string) (Artifact, error)
}

func MemoryAction(action string) bool {
	return action == "memory.evidence.import" || action == "memory.plan.export"
}
func ValidateMemoryRequest(d Draft) error {
	if !MemoryAction(d.Action) {
		if d.Memory != nil {
			return Fail("invalid", "memory fields belong only to memory evidence import or plan export")
		}
		return nil
	}
	m := d.Memory
	if m == nil || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{7,79}$`).MatchString(m.EvidenceID) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.EvidenceSHA256) || m.OtherMiB < 0 || m.OtherMiB > 1<<30 {
		return Fail("invalid", "memory requires a managed evidence ID, SHA-256 and bounded other-workload MiB")
	}
	if d.Serving != nil || d.Resources != nil || d.Caches != nil || d.Model != "" || d.Recipe != "" || d.Profile != "" || d.RecoveryID != "" {
		return Fail("invalid", "memory evidence operations cannot change configuration or recover GPU operations")
	}
	return nil
}
