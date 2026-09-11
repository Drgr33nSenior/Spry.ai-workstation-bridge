package domain

import (
	"context"
	"regexp"
	"time"
)

// PerformanceRequest selects an owner-provisioned analysis bundle, never a
// client executable, filesystem path, inference request or deployment patch.
type PerformanceRequest struct {
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	Kind           string `json:"kind"`
}
type PerformanceField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
	State string `json:"state"`
}
type PerformanceSummary struct {
	Schema        int                `json:"schema"`
	EvidenceID    string             `json:"evidence_id"`
	SHA256        string             `json:"sha256"`
	Kind          string             `json:"kind"`
	Status        string             `json:"status"`
	Reason        string             `json:"reason"`
	ObservedAt    time.Time          `json:"observed_at"`
	Fields        []PerformanceField `json:"fields"`
	Limitations   []string           `json:"limitations"`
	Preconditions map[string]string  `json:"preconditions"`
	Artifacts     []Artifact         `json:"artifacts"`
}
type ServingStatus struct {
	State                string    `json:"state"`
	KubernetesReady      *bool     `json:"kubernetes_ready"`
	RepresentativeWarmup string    `json:"representative_warmup"`
	Reason               string    `json:"reason"`
	ObservedAt           time.Time `json:"observed_at"`
	Identity             string    `json:"identity"`
}
type PerformanceAdapter interface {
	PerformancePreview(context.Context, Draft, Configuration) (PerformanceSummary, error)
	PerformanceArtifact(context.Context, string, string) (Artifact, error)
}

func PerformanceAction(action string) bool {
	return action == "performance.export" || action == "performance.profile.select"
}
func EvidenceAction(action string) bool { return MemoryAction(action) || PerformanceAction(action) }
func PerformanceKind(kind string) bool {
	switch kind {
	case "comparison", "coding-eval", "loading", "queue", "warm-status", "cache", "profile-selection", "profile-status":
		return true
	}
	return false
}
func ValidatePerformanceRequest(d Draft) error {
	if !PerformanceAction(d.Action) {
		if d.Performance != nil {
			return Fail("invalid", "performance fields belong only to performance operations")
		}
		return nil
	}
	p := d.Performance
	if p == nil || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{7,79}$`).MatchString(p.EvidenceID) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.EvidenceSHA256) || !PerformanceKind(p.Kind) {
		return Fail("invalid", "performance requires a reviewed kind, managed evidence ID and SHA-256")
	}
	if (d.Action == "performance.profile.select") != (p.Kind == "profile-selection") {
		return Fail("invalid", "measured profile selection requires its separate explicit owner action")
	}
	if d.Serving != nil || d.Resources != nil || d.Caches != nil || d.Memory != nil || d.Model != "" || d.Profile != "" || d.Recipe != "" || d.RecoveryID != "" {
		return Fail("invalid", "performance analysis cannot change live configuration, run a build or resolve GPU recovery")
	}
	return nil
}
