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

func TestCloudMemoryHasExplicitUnits(t *testing.T) {
	b, err := json.Marshal(cloudMemory(domain.MemorySummary{BaselineMiB: 38912, LimitedMiB: 38912, SharedMemoryMiB: 16384, OtherMiB: 8192, CandidateMiB: 33280, EnvelopeBytes: 123, HeadroomBytes: 456}))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{"requested_mib": 38912, "limited_mib": 38912, "shm_ceiling_mib": 16384, "other_mib": 8192, "candidate_mib": 33280, "envelope_bytes": 123, "headroom_bytes": 456} {
		if v[key] != want {
			t.Errorf("%s: got %v want %v", key, v[key], want)
		}
	}
	for _, key := range []string{"Requested", "Limited", "SHM", "Other", "Candidate", "Envelope", "Headroom"} {
		if _, ok := v[key]; ok {
			t.Errorf("ambiguous unit field %s retained", key)
		}
	}
}
