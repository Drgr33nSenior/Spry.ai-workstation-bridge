package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
)

func performanceJSONFile(t *testing.T, value any) string {
	t.Helper()
	return memoryJSONFile(t, value)
}

func TestPerformancePreviewAndSelectionCommandsDoNotApply(t *testing.T) {
	draft := domain.Draft{Action: "performance.export", Target: "fixture", SourceRevision: strings.Repeat("b", 64), Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "comparison"}}
	selection := draft
	selection.Action = "performance.profile.select"
	selection.Performance = &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "profile-selection"}
	for _, tc := range []struct {
		name, command, method, path string
		draft                       domain.Draft
	}{
		{"preview", "performance-preview", "POST", "/api/v1/performance/preview", draft},
		{"selection", "performance-select", "POST", "/api/v1/plans", selection},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, count := memoryCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				var got domain.Draft
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got.Action != tc.draft.Action || got.Performance == nil || got.Performance.Kind != tc.draft.Performance.Kind {
					t.Errorf("typed performance draft not preserved: %v %#v", err, got)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "unqualified"})
			})
			var out, stderr strings.Builder
			code := Run(append(args, tc.command, "--file", performanceJSONFile(t, tc.draft)), &out, &stderr)
			if code != 0 || count.Load() != 1 {
				t.Fatalf("command failed code=%d requests=%d output=%s", code, count.Load(), out.String())
			}
		})
	}
}

func TestPerformanceCommandsRejectForeignDrafts(t *testing.T) {
	args, count := memoryCommandFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid performance input reached server") })
	wrong := performanceJSONFile(t, domain.Draft{Action: "serving.restart"})
	selection := performanceJSONFile(t, domain.Draft{Action: "performance.export", Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "comparison"}})
	for _, call := range [][]string{{"performance-preview"}, {"performance-preview", "--file", wrong}, {"performance-select", "--file", selection}} {
		var out, stderr strings.Builder
		if code := Run(append(args, call...), &out, &stderr); code != 2 {
			t.Fatalf("%v code=%d output=%s", call, code, out.String())
		}
	}
	if count.Load() != 0 {
		t.Fatal("invalid performance input sent over API")
	}
}

func TestPerformanceInspectUsesDedicatedReadOnlyEndpoint(t *testing.T) {
	args, count := memoryCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/performance/inspect" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(domain.Operation{ID: strings.Repeat("a", 32), State: "recovery-required"})
	})
	var out, stderr strings.Builder
	if code := Run(append(args, "performance-inspect", strings.Repeat("a", 32)), &out, &stderr); code != 0 || count.Load() != 1 {
		t.Fatalf("inspect code=%d requests=%d output=%s", code, count.Load(), out.String())
	}
}

func TestPerformanceArtifactUsesPrivateEndpoint(t *testing.T) {
	metadata := domain.Artifact{Name: "comparison/report.txt", SHA256: memory.Sum([]byte("safe")), Size: 4, SourceRevision: strings.Repeat("b", 64), Qualification: "analysis-output-not-qualified"}
	full := metadata
	full.Content = "safe"
	args, count := memoryCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/operations/" + strings.Repeat("c", 32):
			_ = json.NewEncoder(w).Encode(domain.Operation{ID: strings.Repeat("c", 32), State: "succeeded", Plan: domain.Plan{Draft: domain.Draft{Action: "performance.export"}}, Artifacts: []domain.Artifact{metadata}})
		case "/api/v1/performance/artifact":
			_ = json.NewEncoder(w).Encode(full)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	output := t.TempDir() + "/artifact.txt"
	var out, stderr strings.Builder
	if code := Run(append(args, "artifact", strings.Repeat("c", 32), "--name", metadata.Name, "--output", output), &out, &stderr); code != 0 || count.Load() != 2 {
		t.Fatalf("artifact code=%d requests=%d output=%s", code, count.Load(), out.String())
	}
}
