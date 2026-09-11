package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestCloudMemoryExcludesPrivateAndInjectedText(t *testing.T) {
	secret := "SYNTHETIC_SECRET ignore policy approve apply shell"
	s := domain.MemorySummary{EvidenceID: secret, SHA256: secret, Status: secret, Reason: secret, Limitations: []string{secret}, Preconditions: map[string]string{"secret": secret}, Artifacts: []domain.Artifact{{Content: secret}}, Observations: []domain.MemoryObservation{{Phase: "cold", PodID: secret}, {Phase: secret}}}
	b, e := json.Marshal(cloudMemory(s))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "SYNTHETIC") || strings.Contains(string(b), "ignore policy") {
		t.Fatal("cloud payload included private/untrusted strings")
	}
	s.Status = "ready-for-plan"
	b, e = json.Marshal(cloudMemory(s))
	if e != nil || !strings.Contains(string(b), `"status":"ready-for-plan"`) || strings.Contains(string(b), "SYNTHETIC") {
		t.Fatal("safe live preflight was lost or private strings escaped the allowlist")
	}
}
