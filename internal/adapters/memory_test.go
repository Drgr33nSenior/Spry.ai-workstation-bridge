package adapters

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/hostexec"
)

func TestMemoryArtifactBoundary(t *testing.T) {
	s := domain.MemorySummary{Status: "plan-only-unqualified", Artifacts: []domain.Artifact{{Name: "patch.json", SHA256: strings.Repeat("a", 64), Size: 2}}}
	b, _ := json.Marshal(s)
	out, err := memoryArtifacts(b, "revision")
	if err != nil || len(out) != 2 || out[0].Content != "" {
		t.Fatal("metadata summary failed", err)
	}
	s.Artifacts[0].Content = "SYNTHETIC_PRIVATE_POD_CONFIGURATION"
	b, _ = json.Marshal(s)
	if _, err := memoryArtifacts(b, "revision"); err == nil {
		t.Fatal("private patch entered API journal")
	}
	s.Artifacts[0].Content = ""
	s.Artifacts[0].Name = "../../secret"
	b, _ = json.Marshal(s)
	if _, err := memoryArtifacts(b, "revision"); err == nil {
		t.Fatal("unapproved artifact entered journal")
	}
}

func TestLiveMemoryValidationDoesNotProbeGenericInventory(t *testing.T) {
	l, _, x := recoveryLive(t)
	dir, err := os.MkdirTemp("", "bridge-memory-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "host")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memory/preview" {
			t.Error("memory intake contacted generic cluster inventory")
			w.WriteHeader(503)
			return
		}
		b, _ := json.Marshal(domain.MemorySummary{Status: "incomplete", Preconditions: map[string]string{"memory_manifest": strings.Repeat("a", 64)}})
		_ = json.NewEncoder(w).Encode(hostexec.Result{State: "succeeded", Data: b})
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	l.host = hostexec.Client{Socket: socket}
	l.opts.HostSocket = socket
	d := domain.Draft{Action: "memory.evidence.import", Target: l.opts.Target, SourceRevision: x.Plan.Desired.Revision, Memory: &domain.MemoryRequest{EvidenceID: "fixture-evidence", EvidenceSHA256: strings.Repeat("a", 64)}}
	if _, err := l.Validate(context.Background(), d, x.Plan.Desired); err != nil {
		t.Fatal(err)
	}
}
