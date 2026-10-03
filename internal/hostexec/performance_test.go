package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	perf "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/performance"
)

func TestPerformanceHelperAuthorityAndInterruptedHistory(t *testing.T) {
	e := fixtureExecutor(t)
	dir, err := filepath.EvalSymlinks(e.policy.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	e.policy.StateDir = dir
	e.policy.SourcePath = filepath.Join(dir, "source")
	c := domain.Configuration{}
	c.Revision = c.ContentRevision()
	if err = durableJSON(e.policy.SourcePath, c); err != nil {
		t.Fatal(err)
	}
	d := domain.Draft{Action: "performance.export", Target: e.policy.Target, SourceRevision: c.Revision, Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: strings.Repeat("a", 64), Kind: "comparison"}}
	e.policy.PerformanceSources = map[string]MemorySource{"evidence-001": {Path: "/approved/private/fixture", SHA256: d.Performance.EvidenceSHA256}}
	r := Request{Version: 1, ID: "performance-operation-001", ExecutionIdentity: "fixture-owner", Draft: d, Desired: c}
	r.PayloadHash = RequestHash(r)
	if _, err = e.Submit(e.policy.AllowedUID+1, r); err == nil {
		t.Fatal("wrong UID")
	}
	bad := r
	bad.Draft.Performance = &domain.PerformanceRequest{EvidenceID: "unknown-id", EvidenceSHA256: d.Performance.EvidenceSHA256, Kind: "comparison"}
	bad.PayloadHash = RequestHash(bad)
	if _, err = e.Submit(e.policy.AllowedUID, bad); err == nil {
		t.Fatal("API record forged policy source")
	}
	e.run = func(context.Context, Request, *os.File) (json.RawMessage, error) {
		return nil, errors.New("fixture interrupted analysis")
	}
	if _, err = e.Submit(e.policy.AllowedUID, r); err != nil {
		t.Fatal(err)
	}
	done := awaitResult(t, e, r.ID)
	if done.State != "failed" || done.Phase != "read-only-adapter-failed" {
		t.Fatalf("GPU fence from analysis %+v", done)
	}
	again, err := e.Submit(e.policy.AllowedUID, r)
	if err != nil || again.State != done.State {
		t.Fatal("duplicate", err)
	}
	old := e.records[r.ID]
	old.Result.State = "running"
	if err = e.save(old); err != nil {
		t.Fatal(err)
	}
	reopened := fixtureExecutor(t)
	reopened.policy = e.policy
	reopened.pathTrust = func(string, bool) error { return nil }
	if err = reopened.load(); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Status(r.ID)
	if err != nil || got.State != "failed" || got.Phase != "read-only-interrupted" {
		t.Fatal("restart", err)
	}
	// Independent unknown GPU effects still fence all new analysis; neither an
	// API performance record nor successful export can settle that history.
	old.Result.State = "recovery-required"
	old.Request.Draft = domain.Draft{Action: "serving.start", Target: e.policy.Target}
	reopened.records["unrelated-gpu"] = old
	r.ID = "performance-operation-002"
	r.PayloadHash = RequestHash(r)
	if _, err = reopened.Submit(e.policy.AllowedUID, r); err == nil {
		t.Fatal("analysis bypassed unrelated recovery fence")
	}
}

func TestPerformanceSummaryArtifactUsesIndependentRootRecord(t *testing.T) {
	e := fixtureExecutor(t)
	s := domain.PerformanceSummary{Schema: 1, Kind: "comparison", SHA256: strings.Repeat("a", 64), Status: "inconclusive", Reason: "insufficient_samples"}
	b, _ := json.Marshal(s)
	r := record{Request: Request{ID: "performance-result-001", Draft: domain.Draft{Action: "performance.export"}, Desired: domain.Configuration{Revision: strings.Repeat("b", 64)}}, Result: Result{State: "succeeded", Data: b}}
	e.records[r.Request.ID] = r
	a, err := e.PerformanceArtifact(context.Background(), r.Request.ID, "performance-summary.json")
	if err != nil || a.Content != string(b) || a.SHA256 != perf.Sum(b) || a.Size != int64(len(b)) || a.SourceRevision != r.Request.Desired.Revision {
		t.Fatal("independent summary export mismatch", err)
	}
	if _, err = e.PerformanceArtifact(context.Background(), r.Request.ID, "../summary.json"); err == nil {
		t.Fatal("unsafe summary path")
	}
	r.Result.State = "failed"
	e.records[r.Request.ID] = r
	if _, err = e.PerformanceArtifact(context.Background(), r.Request.ID, "performance-summary.json"); err == nil {
		t.Fatal("incomplete independent result exported")
	}
}

func profileStatusBundle(t *testing.T, root string, current json.RawMessage, currentName string) (string, string) {
	t.Helper()
	// Verify rejects every symlink in a private evidence tree. Resolve the
	// platform temporary parent before creating the bundle: on macOS /var is a
	// symlink to /private/var even though t.TempDir itself is otherwise private.
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(parent, filepath.Base(root))
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	selection := []byte(`{"schema":1,"kind":"workstation-measured-profile-selection"}`)
	write("selection.json", selection)
	if currentName != "missing.json" {
		write(currentName, current)
	}
	spec, err := json.Marshal(map[string]any{"schema": 1, "kind": "profile-status", "selection": "selection.json", "current_identity": currentName})
	if err != nil {
		t.Fatal(err)
	}
	write("spec.json", spec)
	files := map[string]string{"selection.json": perf.Sum(selection), "spec.json": perf.Sum(spec)}
	if currentName != "missing.json" {
		files[currentName] = perf.Sum(current)
	}
	manifest, err := json.Marshal(perf.Manifest{Schema: 1, Kind: "profile-status", Target: "fixture-node", SourceRevision: strings.Repeat("a", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", manifest)
	return root, perf.Sum(manifest)
}

func TestProfileStatusAllowsOnlyIncompleteSealedIdentityToReachCanonicalUnknown(t *testing.T) {
	e := fixtureExecutor(t)
	for _, tc := range []struct {
		name    string
		current json.RawMessage
	}{
		{"missing boot", json.RawMessage(`{}`)},
		{"malformed boot", json.RawMessage(`{"boot_id":42}`)},
		{"malformed boot token", json.RawMessage(`{"boot_id":"not-a-boot-id"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, digest := profileStatusBundle(t, filepath.Join(t.TempDir(), "bundle"), tc.current, "current.json")
			e.policy.PerformanceSources = map[string]MemorySource{"evidence-001": {Path: root, SHA256: digest}}
			d := domain.Draft{Action: "performance.export", Target: e.policy.Target, SourceRevision: strings.Repeat("a", 64), Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: digest, Kind: "profile-status"}}
			got, manifest, err := e.performanceInput(context.Background(), d)
			if err != nil || got != root || manifest.Kind != "profile-status" {
				t.Fatalf("incomplete sealed identity did not reach canonical unknown path: %q %+v %v", got, manifest, err)
			}
		})
	}
	for _, tc := range []struct {
		name       string
		root       string
		digest     string
		current    json.RawMessage
		currentRef string
	}{
		{"unsealed", filepath.Join(t.TempDir(), "unsealed"), strings.Repeat("a", 64), json.RawMessage(`{}`), "current.json"},
		{"missing sealed current file", "", "", json.RawMessage(`{}`), "missing.json"},
		{"usable mismatched boot", "", "", json.RawMessage(`{"boot_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}`), "current.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, digest := tc.root, tc.digest
			if root == "" {
				current := tc.current
				if tc.name == "usable mismatched boot" {
					if actual, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
						boot := strings.TrimSpace(string(actual))
						if strings.HasSuffix(boot, "0") {
							boot = boot[:len(boot)-1] + "1"
						} else {
							boot = boot[:len(boot)-1] + "0"
						}
						var marshalErr error
						current, marshalErr = json.Marshal(map[string]string{"boot_id": boot})
						if marshalErr != nil {
							t.Fatal(marshalErr)
						}
					}
				}
				root, digest = profileStatusBundle(t, filepath.Join(t.TempDir(), "bundle"), current, tc.currentRef)
			}
			e.policy.PerformanceSources = map[string]MemorySource{"evidence-001": {Path: root, SHA256: digest}}
			d := domain.Draft{Action: "performance.export", Target: e.policy.Target, SourceRevision: strings.Repeat("a", 64), Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: digest, Kind: "profile-status"}}
			if _, _, err := e.performanceInput(context.Background(), d); err == nil {
				t.Fatal("unsealed, missing, or usable mismatched boot identity was accepted")
			}
		})
	}
}

func TestProfileStatusUsableCurrentBootStillRequiresRootHardwareEvidence(t *testing.T) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Skip("kernel boot identity is unavailable on this platform")
	}
	e := fixtureExecutor(t)
	current, err := json.Marshal(map[string]string{"boot_id": strings.TrimSpace(string(boot))})
	if err != nil {
		t.Fatal(err)
	}
	root, digest := profileStatusBundle(t, filepath.Join(t.TempDir(), "bundle"), current, "current.json")
	e.policy.PerformanceSources = map[string]MemorySource{"evidence-001": {Path: root, SHA256: digest}}
	d := domain.Draft{Action: "performance.export", Target: e.policy.Target, SourceRevision: strings.Repeat("a", 64), Performance: &domain.PerformanceRequest{EvidenceID: "evidence-001", EvidenceSHA256: digest, Kind: "profile-status"}}
	// fixtureExecutor has no root-owned observed hardware report. A syntactically
	// valid boot must not bypass that independent current-hardware fence.
	if _, _, err = e.performanceInput(context.Background(), d); err == nil {
		t.Fatal("usable current boot bypassed root hardware evidence")
	}
}
