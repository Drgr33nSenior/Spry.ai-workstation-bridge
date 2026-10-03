package domain

import (
	"strings"
	"testing"
)

func TestPerformanceActionsAreTypedOwnerOnlyExports(t *testing.T) {
	for _, kind := range []string{"comparison", "coding-eval", "loading", "queue", "warm-status", "cache", "profile-selection", "profile-status"} {
		action := "performance.export"
		if kind == "profile-selection" {
			action = "performance.profile.select"
		}
		d := Draft{Action: action, Performance: &PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: kind}}
		if err := ValidatePerformanceRequest(d); err != nil {
			t.Fatal(err)
		}
		if !RoleAllows("owner", action) || RoleAllows("operator", action) || RoleAllows("viewer", action) {
			t.Fatal("role boundary")
		}
		d.Resources = &Resources{MemoryMiB: 1}
		if ValidatePerformanceRequest(d) == nil {
			t.Fatal("export accepted mutation")
		}
		d.Resources = nil
		d.RecoveryID = "old-operation"
		if ValidatePerformanceRequest(d) == nil {
			t.Fatal("export accepted recovery authority")
		}
	}
	for _, p := range []PerformanceRequest{{EvidenceID: "../escape", EvidenceSHA256: strings.Repeat("a", 64), Kind: "cache"}, {EvidenceID: "evidence-001", EvidenceSHA256: "bad", Kind: "cache"}, {EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "shell"}, {EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "profile-selection"}} {
		if ValidatePerformanceRequest(Draft{Action: "performance.export", Performance: &p}) == nil {
			t.Fatal("invalid performance request accepted")
		}
	}
}
