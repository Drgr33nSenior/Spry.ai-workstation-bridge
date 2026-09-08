//go:build linux

package worker

import (
	"context"
	"encoding/json"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerRestartRecoveryFencesAreIndependent(t *testing.T) {
	p := Policy{APIUID: 1234, StateDir: t.TempDir(), CgroupRoot: t.TempDir()}
	s := &Service{policy: p, recovery: map[string]bool{"recovery-first": true, "recovery-second": true}, persistenceFailed: true}
	for _, id := range []string{"recovery-first", "recovery-second"} {
		if e := s.save(Record{Request: Request{ID: id}, Result: domain.Result{State: "recovery-required", RecoveryRequired: true}}); e != nil {
			t.Fatal(e)
		}
	}
	r := httptest.NewRequest("GET", "http://worker/operations/recovery-first", nil)
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, p.APIUID))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var result domain.Result
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.State != "recovery-required" || !result.RecoveryRequired {
		t.Fatal("absent cgroup must retain the recovery fence", result, e)
	}
	if !s.recovery["recovery-first"] || !s.recovery["recovery-second"] || !s.persistenceFailed {
		t.Fatal("missing cgroup cleared a worker recovery or persistence fence")
	}
	group := filepath.Join(p.CgroupRoot, "bridge-recovery-first")
	if e := os.Mkdir(group, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(group, "cgroup.events"), []byte("populated 0\n"), 0600); e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "http://worker/operations/recovery-first", nil)
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, p.APIUID))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.State != "failed" || result.RecoveryRequired {
		t.Fatal("an empty observable cgroup must settle only this uncommitted build as failed", result, e)
	}
	if s.recovery["recovery-first"] || !s.recovery["recovery-second"] || !s.persistenceFailed {
		t.Fatal("empty cgroup resolution cleared another worker recovery or persistence fence")
	}
	request := Request{ID: "new-operation", Actor: "owner", Recipe: "llama-vulkan"}
	request.Hash = RequestHash(request)
	b, _ := json.Marshal(request)
	r = httptest.NewRequest("POST", "http://worker/execute", strings.NewReader(string(b)))
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, p.APIUID))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("unresolved worker accepted a new effect", w.Code)
	}
}

// This uses generated fixture data and the production namespace argument set.
// It never reads a developer credential or needs an AMD device or cluster.
func TestLinuxContainment(t *testing.T) {
	if os.Getenv("BRIDGE_FIXTURE_CHILD") == "1" {
		return
	}
	if _, e := os.Stat("/usr/bin/bwrap"); e != nil {
		t.Skip("NOT RUN — Linux bubblewrap unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	scratch := filepath.Join(root, "scratch")
	cache := filepath.Join(root, "cache")
	for _, p := range []string{source, scratch, cache} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	secret := filepath.Join(root, "fixture-private")
	if e := os.WriteFile(secret, []byte("generated fixture; not a credential"), 0600); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	args := sandboxArgs(Policy{SourceRoot: source, CacheRoot: cache, CacheGiB: 1}, scratch)
	probeArgs := append(append([]string{}, args...), "/usr/bin/true")
	probe := exec.Command("/usr/bin/bwrap", probeArgs...)
	if e := probe.Run(); e != nil {
		t.Skip("NOT RUN — unprivileged Linux namespace policy prevents bubblewrap; no control weakened")
	}
	args = append(args, "--ro-bind", exe, "/probe", "--setenv", "BRIDGE_FIXTURE_CHILD", "1", "--setenv", "BRIDGE_FIXTURE_PATH", secret, "--setenv", "BRIDGE_FIXTURE_ADDRESS", listener.Addr().String(), "/probe", "-test.run=^TestLinuxContainmentChild$")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", args...)
	cmd.Env = []string{"PATH=/usr/bin", "BRIDGE_FAKE_MANAGEMENT_SECRET=must-not-cross-boundary"}
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("containment probe failed: %v %s", e, out)
	}
	b, e := os.ReadFile(secret)
	if e != nil || string(b) != "generated fixture; not a credential" {
		t.Fatal("host fixture escaped")
	}
}
func TestLinuxContainmentChild(t *testing.T) {
	if os.Getenv("BRIDGE_FIXTURE_CHILD") != "1" {
		t.Skip("run through parent namespace test")
	}
	if os.Getenv("BRIDGE_FAKE_MANAGEMENT_SECRET") != "" {
		t.Fatal("credential environment inherited")
	}
	if _, e := os.ReadFile(os.Getenv("BRIDGE_FIXTURE_PATH")); e == nil {
		t.Fatal("unrelated host file readable")
	}
	if e := os.WriteFile(os.Getenv("BRIDGE_FIXTURE_PATH"), []byte("escaped"), 0600); e == nil {
		t.Fatal("host path writable")
	}
	if _, e := os.Stat("/dev/dri"); e == nil {
		t.Fatal("unrelated GPU device exposed")
	}
	if _, e := os.Stat("/run/spry-bridge/host.sock"); e == nil {
		t.Fatal("helper socket exposed")
	}
	conn, e := net.DialTimeout("tcp", os.Getenv("BRIDGE_FIXTURE_ADDRESS"), time.Second)
	if e == nil {
		conn.Close()
		t.Fatal("host network reachable")
	}
	if e := os.WriteFile("/source/escape", []byte("x"), 0600); e == nil {
		t.Fatal("source writable")
	}
}
