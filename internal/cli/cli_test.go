package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestStableWaitExitCodes(t *testing.T) {
	credential := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(credential, []byte("isolated-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for state, want := range map[string]int{"succeeded": 0, "failed": 6, "cancelled": 6, "recovery-required": 7} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(domain.Operation{ID: strings.Repeat("a", 32), State: state})
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			got := Run([]string{"--endpoint", server.URL, "--credential-file", credential, "wait", strings.Repeat("a", 32)}, &out, &stderr)
			if got != want {
				t.Fatalf("exit %d want %d: %s", got, want, out.String())
			}
			var operation domain.Operation
			if json.Unmarshal(out.Bytes(), &operation) != nil || operation.State != state {
				t.Fatal("wait did not return machine-readable terminal operation")
			}
		})
	}
}

func TestDeadlineDoesNotClaimCancellation(t *testing.T) {
	credential := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(credential, []byte("isolated-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	var out, stderr bytes.Buffer
	got := Run([]string{"--endpoint", server.URL, "--credential-file", credential, "--deadline", "10ms", "status"}, &out, &stderr)
	if got != 8 || !strings.Contains(out.String(), "accepted") && !strings.Contains(out.String(), "submitted operations continue") {
		t.Fatalf("deadline exit %d: %s", got, out.String())
	}
}

func TestHelpAndSecretFlags(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatal("help unavailable")
	}
	out.Reset()
	if code := Run([]string{"--token", "do-not-accept-secret-flags", "status"}, &out, &stderr); code != 2 {
		t.Fatal("accepted secret command argument")
	}
	if strings.Contains(out.String(), "do-not-accept-secret-flags") {
		t.Fatal("unknown flag error echoed secret value")
	}
}
