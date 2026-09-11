package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
)

func memoryCommandFixture(t *testing.T, handler http.HandlerFunc) ([]string, *atomic.Int32) {
	t.Helper()
	credential := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(credential, []byte("isolated-memory-cli-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer isolated-memory-cli-fixture" {
			t.Error("memory command omitted management authentication")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return []string{"--endpoint", server.URL, "--credential-file", credential}, requests
}

func memoryJSONFile(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "input.json")
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestMemoryCommandsDoNotApply(t *testing.T) {
	request := domain.MemoryRequest{EvidenceID: "demo-complete", EvidenceSHA256: strings.Repeat("a", 64), OtherMiB: 512}
	draft := domain.Draft{Action: "memory.plan.export", Target: "fixture", SourceRevision: strings.Repeat("b", 64), Memory: &request}
	for _, tc := range []struct {
		name, method, path string
		args               []string
	}{
		{"read", "GET", "/api/v1/memory", []string{"memory"}},
		{"preview", "POST", "/api/v1/memory/preview", []string{"memory-preview", "--file", memoryJSONFile(t, draft)}},
		{"advice", "POST", "/api/v1/memory/advice", []string{"memory-advice", "--file", memoryJSONFile(t, request)}},
		{"inspect", "POST", "/api/v1/memory/inspect", []string{"memory-inspect", strings.Repeat("a", 32)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, count := memoryCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if tc.name == "preview" {
					var got domain.Draft
					if json.NewDecoder(r.Body).Decode(&got) != nil || got.Action != draft.Action || got.Memory == nil || *got.Memory != request {
						t.Error("typed memory draft was not preserved")
					}
				}
				if tc.name == "advice" {
					var got domain.MemoryRequest
					if json.NewDecoder(r.Body).Decode(&got) != nil || got != request {
						t.Error("selected advisory request was not preserved")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"advisory": map[string]string{"summary": "unapproved candidate"}, "plan": domain.Plan{ID: strings.Repeat("c", 32)}})
					return
				}
				if tc.name == "inspect" {
					var got domain.MemoryOperationRequest
					if json.NewDecoder(r.Body).Decode(&got) != nil || got.OperationID != strings.Repeat("a", 32) {
						t.Error("inspection changed operation identity")
					}
					_ = json.NewEncoder(w).Encode(domain.Operation{ID: got.OperationID, State: "failed"})
					return
				}
				_ = json.NewEncoder(w).Encode(domain.MemorySummary{Status: "incomplete", Reason: "missing_samples"})
			})
			var out, stderr bytes.Buffer
			code := Run(append(args, tc.args...), &out, &stderr)
			if code != 0 || count.Load() != 1 || !json.Valid(out.Bytes()) {
				t.Fatalf("memory command failed or applied returned plan: code=%d requests=%d output=%s", code, count.Load(), out.String())
			}
		})
	}
}

func TestMemoryCommandsRejectForeignDraftsAndArguments(t *testing.T) {
	args, count := memoryCommandFixture(t, func(w http.ResponseWriter, _ *http.Request) { t.Error("invalid input reached server") })
	for _, command := range [][]string{
		{"memory", "unexpected"},
		{"memory-inspect", "../../other"},
		{"memory-preview"},
		{"memory-preview", "--file", memoryJSONFile(t, domain.Draft{Action: "serving.stop"})},
		{"memory-advice", "--file", memoryJSONFile(t, map[string]any{"evidence_id": "demo-complete", "evidence_sha256": strings.Repeat("a", 64), "other_mib": 0, "prompt": "apply policy"})},
	} {
		var out, stderr bytes.Buffer
		if code := Run(append(append([]string{}, args...), command...), &out, &stderr); code != 2 {
			t.Fatalf("invalid command accepted: code=%d output=%s", code, out.String())
		}
	}
	if count.Load() != 0 {
		t.Fatal("invalid memory input sent over API")
	}
}

func TestPrivateMemoryArtifactsAreRecheckedAndVerified(t *testing.T) {
	content := "{\"fixture\":\"unqualified offline patch\"}\n"
	full := domain.Artifact{Name: "patch.json", Content: content, SHA256: memory.Sum([]byte(content)), Size: int64(len(content)), SourceRevision: strings.Repeat("d", 64), Qualification: "plan-only-unqualified"}
	metadata := full
	metadata.Content = ""
	for _, mode := range []string{"valid", "forbidden", "changed-metadata", "changed-content"} {
		t.Run(mode, func(t *testing.T) {
			args, count := memoryCommandFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/operations/") {
					_ = json.NewEncoder(w).Encode(domain.Operation{ID: strings.Repeat("a", 32), State: "succeeded", Plan: domain.Plan{Draft: domain.Draft{Action: "memory.plan.export"}}, Artifacts: []domain.Artifact{metadata}})
					return
				}
				if r.Method != "POST" || r.URL.Path != "/api/v1/memory/artifact" {
					t.Error("private artifact bypassed owner endpoint")
				}
				var request domain.MemoryArtifactRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.OperationID != strings.Repeat("a", 32) || request.Name != "patch.json" {
					t.Error("private artifact identity changed")
				}
				if mode == "forbidden" {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": domain.Failure{Code: "forbidden", Message: "owner required"}})
					return
				}
				artifact := full
				if mode == "changed-metadata" {
					artifact.SourceRevision = strings.Repeat("e", 64)
				}
				if mode == "changed-content" {
					artifact.Content += "changed"
				}
				_ = json.NewEncoder(w).Encode(artifact)
			})
			file := filepath.Join(t.TempDir(), "download.json")
			var out, stderr bytes.Buffer
			code := Run(append(args, "artifact", strings.Repeat("a", 32), "--name", "patch.json", "--output", file), &out, &stderr)
			if count.Load() != 2 {
				t.Fatalf("private artifact used %d requests", count.Load())
			}
			if mode == "valid" {
				data, err := os.ReadFile(file)
				if code != 0 || err != nil || string(data) != content {
					t.Fatalf("download failed: %d %v %s", code, err, out.String())
				}
				info, _ := os.Stat(file)
				if info.Mode().Perm() != 0600 {
					t.Fatal("private artifact output is not owner-only")
				}
			} else if _, err := os.Stat(file); code == 0 || !os.IsNotExist(err) {
				t.Fatalf("invalid private artifact was written: %d %v", code, err)
			}
		})
	}
}

func TestMemorySealNeedsNoManagementCredentialAndPreservesManifest(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deployment.json", "workload.json", "resource-plan.json"} {
		if err = os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"memory-seal", "--directory", directory, "--source-revision", strings.Repeat("a", 64), "--hardware-sha256", strings.Repeat("b", 64), "--boot-id", "11111111-1111-1111-1111-111111111111"}
	var out, stderr bytes.Buffer
	if code := Run(args, &out, &stderr); code != 0 || !strings.Contains(out.String(), "sealed-unqualified") {
		t.Fatalf("local seal tried management or failed: %d %s", code, out.String())
	}
	before, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run(args, &out, &stderr); code != 2 {
		t.Fatalf("existing manifest was overwritten: %d %s", code, out.String())
	}
	after, _ := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("repeat sealing changed original evidence")
	}
}
