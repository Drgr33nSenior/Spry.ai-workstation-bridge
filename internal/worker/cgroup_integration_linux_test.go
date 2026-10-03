//go:build linux

package worker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLinuxDelegatedCgroupIntegration is deliberately opt-in. The caller must
// start this test in the supervisor child of an owner-created, disposable,
// delegated cgroup v2 subtree. It neither creates a systemd unit nor writes
// outside the one supplied subtree.
func TestLinuxDelegatedCgroupIntegration(t *testing.T) {
	if os.Getenv("BRIDGE_CGROUP_TEST_ALLOW") != "disposable-delegated-subtree" {
		t.Skip("NOT RUN — set BRIDGE_CGROUP_TEST_ALLOW=disposable-delegated-subtree only in an owner-created disposable delegated subtree")
	}
	root := filepath.Clean(os.Getenv("BRIDGE_CGROUP_TEST_ROOT"))
	if !strings.HasPrefix(filepath.Base(root), "bridge-cgroup-test-") {
		t.Fatal("BRIDGE_CGROUP_TEST_ROOT must name a disposable bridge-cgroup-test-* subtree")
	}
	p := Policy{CgroupRoot: root, WorkerUID: os.Geteuid(), MemoryMiB: 64, Jobs: 1}
	group, err := prepareJobCgroup(p, Request{ID: "cgroup-test-operation"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if empty, err := groupEmpty(group); err != nil || !empty {
			t.Errorf("disposable test cgroup did not become empty: empty=%v err=%v", empty, err)
			return
		}
		if err := os.Remove(group); err != nil {
			t.Errorf("remove disposable test cgroup: %v", err)
		}
	}()
	for name, want := range map[string]string{
		"memory.max":      "67108864",
		"memory.swap.max": "0",
		"pids.max":        "256",
		"cpu.max":         "100000 100000",
	} {
		got, err := os.ReadFile(filepath.Join(group, name))
		if err != nil || strings.Join(strings.Fields(string(got)), " ") != want {
			t.Fatalf("effective child %s=%q err=%v; want %q", name, got, err, want)
		}
	}
	g, err := os.Open(group)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, UseCgroupFD: true, CgroupFD: int(g.Fd())}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if waited || cmd.Process == nil {
			return
		}
		// Every failure path after Start reaps the trivial fixture before the
		// disposable cgroup is checked or removed. cgroup.kill reaches the
		// shell and its descendant; the process-group kill is a bounded fallback.
		_ = os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
			waited = true
		case <-time.After(3 * time.Second):
			t.Errorf("bounded wait did not reap the disposable cgroup payload")
		}
	}()
	time.Sleep(100 * time.Millisecond)
	if empty, err := groupEmpty(group); err != nil || empty {
		t.Fatalf("trivial payload was not observable in its child cgroup: empty=%v err=%v", empty, err)
	}
	if err := os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	waitErr := cmd.Wait()
	waited = true
	if waitErr == nil {
		t.Fatal("cgroup.kill did not terminate the trivial payload")
	}
	if empty, err := groupEmpty(group); err != nil || !empty {
		t.Fatalf("descendants remained after cgroup.kill: empty=%v err=%v", empty, err)
	}
}
