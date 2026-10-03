package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func performanceDraft(f *fixture, action, kind, evidenceID string) domain.Draft {
	return domain.Draft{
		Action:         action,
		Target:         "demo-workstation",
		SourceRevision: f.conf.Revision,
		Performance: &domain.PerformanceRequest{
			EvidenceID:     evidenceID,
			EvidenceSHA256: strings.Repeat("a", 64),
			Kind:           kind,
		},
	}
}

func TestPerformanceOwnerPreviewAndExportRemainUnqualified(t *testing.T) {
	f := setup(t)
	d := performanceDraft(f, "performance.export", "comparison", "demo-complete")
	for _, role := range []string{"viewer", "operator"} {
		for _, path := range []string{"/api/v1/performance/preview", "/api/v1/performance/artifact", "/api/v1/performance/inspect"} {
			status, _, _ := f.request(t, "POST", path, role, d, nil)
			if status != 403 {
				t.Fatalf("%s %s status %d", role, path, status)
			}
		}
	}
	status, _, _ := f.request(t, "POST", "/api/v1/performance/preview", "owner", `{"action":"performance.export","unexpected":true}`, nil)
	if status != 400 {
		t.Fatalf("unknown preview input status %d", status)
	}
	status, _, _ = f.request(t, "POST", "/api/v1/performance/inspect", "owner", map[string]string{"operation_id": "not-an-operation"}, nil)
	if status != 400 {
		t.Fatalf("invalid inspection ID status %d", status)
	}
	status, b, _ := f.request(t, "POST", "/api/v1/performance/preview", "owner", d, nil)
	if status != 200 {
		t.Fatalf("preview %d %s", status, b)
	}
	var preview domain.PerformanceSummary
	if err := json.Unmarshal(b, &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Status != "demo-only-unqualified" || preview.EvidenceID != "demo-complete" || preview.Kind != "comparison" {
		t.Fatalf("unexpected preview %+v", preview)
	}
	if len(f.eng.Operations()) != 0 {
		t.Fatal("preview created an operation")
	}
	status, b, _ = f.request(t, "POST", "/api/v1/plans", "owner", d, nil)
	if status != 201 {
		t.Fatalf("plan %d %s", status, b)
	}
	var plan domain.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"viewer", "operator"} {
		status, _, _ := f.request(t, "GET", "/api/v1/plans/"+plan.ID, role, nil, nil)
		if status != 403 {
			t.Fatalf("%s read owner-only performance evidence plan: %d", role, status)
		}
	}
	if plan.Desired.Revision != f.conf.Revision || len(plan.Preview.Changes) != 0 {
		t.Fatal("performance analysis plan changed desired source")
	}
	status, b, _ = f.request(t, "POST", "/api/v1/operations", "owner", map[string]string{"plan_id": plan.ID, "target": d.Target}, map[string]string{"Idempotency-Key": "performance-export-001"})
	if status != 202 {
		t.Fatalf("apply %d %s", status, b)
	}
	var operation domain.Operation
	if err := json.Unmarshal(b, &operation); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		operation, _ = f.eng.Operation(operation.ID)
		if domain.Terminal(operation.State) {
			break
		}
	}
	if operation.State != "succeeded" || operation.SourceUpdated || operation.LiveApplied || operation.RecoveryRequired {
		t.Fatalf("invalid performance export outcome %+v", operation)
	}
	for _, role := range []string{"viewer", "operator"} {
		status, _, _ := f.request(t, "GET", "/api/v1/operations/"+operation.ID, role, nil, nil)
		if status != 403 {
			t.Fatalf("%s read performance evidence operation: %d", role, status)
		}
		status, body, _ := f.request(t, "GET", "/api/v1/operations", role, nil, nil)
		if status != 200 {
			t.Fatalf("%s list performance evidence operation: %d", role, status)
		}
		if strings.Contains(string(body), operation.ID) {
			t.Fatalf("%s listed owner-only performance evidence operation", role)
		}
	}
	var metadata *domain.Artifact
	for i := range operation.Artifacts {
		if operation.Artifacts[i].Name == "performance-summary.json" {
			metadata = &operation.Artifacts[i]
		}
	}
	if metadata == nil || metadata.Content == "" {
		t.Fatal("bounded performance summary was absent from the owner-only operation")
	}
	status, b, _ = f.request(t, "POST", "/api/v1/performance/artifact", "owner", map[string]string{"operation_id": operation.ID, "name": metadata.Name}, nil)
	if status != 200 {
		t.Fatalf("private artifact %d %s", status, b)
	}
	var artifact domain.Artifact
	if err := json.Unmarshal(b, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Name != metadata.Name || artifact.SHA256 != metadata.SHA256 || artifact.Content == "" {
		t.Fatal("private artifact did not preserve recorded identity and content")
	}
	current, err := f.eng.Source.Read()
	if err != nil || current.Revision != f.conf.Revision {
		t.Fatal("performance export modified authoritative source")
	}
}

func TestPerformanceDraftKindsAndRefusals(t *testing.T) {
	f := setup(t)
	cases := []struct {
		name  string
		draft domain.Draft
		want  int
	}{
		{"refused evidence", performanceDraft(f, "performance.export", "comparison", "demo-refused"), 400},
		{"selection action required", performanceDraft(f, "performance.export", "profile-selection", "demo-complete"), 400},
		{"selection cannot use comparison", performanceDraft(f, "performance.profile.select", "comparison", "demo-complete"), 400},
		{"selection preview", performanceDraft(f, "performance.profile.select", "profile-selection", "demo-complete"), 200},
		{"profile status preview", performanceDraft(f, "performance.export", "profile-status", "demo-complete"), 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _ := f.request(t, "POST", "/api/v1/performance/preview", "owner", tc.draft, nil)
			if status != tc.want {
				t.Fatalf("status %d want %d", status, tc.want)
			}
		})
	}
}
