//go:build linux

package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type peerKey struct{}

type osCgroupFiles struct{}

func (osCgroupFiles) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
func (osCgroupFiles) WriteFile(name string, data []byte, mode os.FileMode) error {
	return os.WriteFile(name, data, mode)
}
func (osCgroupFiles) Mkdir(name string, mode os.FileMode) error { return os.Mkdir(name, mode) }

func ConnContext(ctx context.Context, c net.Conn) context.Context {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	raw, e := u.SyscallConn()
	if e != nil {
		return ctx
	}
	uid := -1
	raw.Control(func(fd uintptr) {
		cred, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if e == nil {
			uid = int(cred.Uid)
		}
	})
	return context.WithValue(ctx, peerKey{}, uid)
}
func peerAuthorized(ctx context.Context, uid int) bool {
	actual, ok := ctx.Value(peerKey{}).(int)
	return ok && actual == uid
}
func trustedFile(path string) error {
	for p := path; p != "/"; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil {
			return e
		}
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || i.Mode()&022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return errors.New("worker installed policy/source is not root-owned and immutable to worker")
		}
	}
	return nil
}

type cappedWriter struct {
	mu        sync.Mutex
	w         io.Writer
	remaining int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if int64(n) > w.remaining {
		if w.remaining > 0 {
			_, _ = w.w.Write(p[:w.remaining])
			w.remaining = 0
		}
		return n, nil
	}
	_, e := w.w.Write(p)
	w.remaining -= int64(n)
	return n, e
}
func runContained(ctx context.Context, p Policy, r Request) (domain.Result, error) {
	var result domain.Result
	if p.WorkerUID != os.Geteuid() || p.WorkerUID == 0 {
		return result, errors.New("build executor must run as separate unprivileged worker")
	}
	if e := trustedFile("/usr/bin/bwrap"); e != nil {
		return result, errors.New("reviewed root-owned bubblewrap is unavailable")
	}
	if p.GPUTarget != "gfx1201" {
		return result, errors.New("discovered GPU compilation target requires policy review")
	}
	if _, e := os.Lstat(p.InhibitPath); e == nil || !os.IsNotExist(e) {
		return result, errors.New("managed build is inhibited")
	}
	if e := verifySource(p); e != nil {
		return result, e
	}
	if e := verifyBuildInputs(p, r.Recipe); e != nil {
		return result, e
	}
	mem, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return result, errors.New("available host build memory is unknown")
	}
	available := int64(0)
	for _, line := range strings.Split(string(mem), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				available = kb / 1024
			}
		}
	}
	jobs, e := buildJobs(available, p.MemoryMiB, p.Jobs)
	if e != nil {
		return result, e
	}
	p.Jobs = jobs
	var disk syscall.Statfs_t
	if e := syscall.Statfs(p.ScratchRoot, &disk); e != nil {
		return result, e
	}
	if int64(disk.Blocks)*int64(disk.Bsize) > p.ScratchGiB<<30 {
		return result, errors.New("worker scratch must be a dedicated filesystem with capacity within scratch budget")
	}
	var cache syscall.Statfs_t
	if e := syscall.Statfs(p.CacheRoot, &cache); e != nil {
		return result, e
	}
	if int64(cache.Blocks)*int64(cache.Bsize) > p.CacheGiB<<30 {
		return result, errors.New("worker ccache must be a dedicated filesystem within cache budget")
	}
	job := filepath.Join(p.ScratchRoot, r.ID)
	if e := os.Mkdir(job, 0700); e != nil {
		return result, e
	}
	group, e := prepareJobCgroup(p, r)
	if e != nil {
		return result, e
	}
	g, e := os.Open(group)
	if e != nil {
		return result, e
	}
	defer g.Close()
	kill := func() { _ = os.WriteFile(filepath.Join(group, "cgroup.kill"), []byte("1"), 0600) }
	defer kill()
	log, e := os.OpenFile(filepath.Join(job, "build.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return result, e
	}
	defer log.Close()
	writer := &cappedWriter{w: log, remaining: p.MaxLogBytes}
	base := sandboxArgs(p, job)
	ccachePolicy, e := os.CreateTemp(p.StateDir, ".ccache-policy-")
	if e != nil {
		return result, e
	}
	defer os.Remove(ccachePolicy.Name())
	if e = ccachePolicy.Chmod(0400); e == nil {
		_, e = fmt.Fprintf(ccachePolicy, "cache_dir = /ccache\nmax_size = %dG\ncompression = true\ncompiler_check = content\nhard_link = false\nsloppiness =\nremote_storage =\n", p.CacheGiB)
	}
	ce := ccachePolicy.Close()
	if e != nil {
		return result, e
	}
	if ce != nil {
		return result, ce
	}
	base = append(base, "--ro-bind", ccachePolicy.Name(), "/etc/bridge-ccache.conf", "--setenv", "CCACHE_CONFIGPATH", "/etc/bridge-ccache.conf")
	if r.Recipe == "llama-hip" {
		if p.ROCmRoot == "" || trustedFile(p.ROCmRoot) != nil {
			return result, errors.New("reviewed ROCm SDK root is unavailable")
		}
		base = append(base, "--ro-bind", p.ROCmRoot, "/opt/rocm")
	}
	// /dev/null plus fixed environment prevents inherited cache/remote configuration.
	cmake := []string{"/usr/bin/cmake", "-S", "/source", "-B", "/build/build", "-G", "Ninja", "-DCMAKE_BUILD_TYPE=Release", "-DGGML_NATIVE=ON", "-DCMAKE_FIND_USE_PACKAGE_REGISTRY=FALSE", "-DCMAKE_FIND_USE_SYSTEM_PACKAGE_REGISTRY=FALSE", "-DCMAKE_FIND_USE_PACKAGE_ROOT_PATH=FALSE", "-DCMAKE_FIND_USE_CMAKE_ENVIRONMENT_PATH=FALSE", "-DCMAKE_C_COMPILER=/usr/bin/cc", "-DCMAKE_CXX_COMPILER=/usr/bin/c++", "-DCMAKE_C_COMPILER_LAUNCHER=ccache", "-DCMAKE_CXX_COMPILER_LAUNCHER=ccache", fmt.Sprintf("-DCMAKE_JOB_POOLS=compile=%d;link=1", p.Jobs), "-DCMAKE_JOB_POOL_COMPILE=compile", "-DCMAKE_JOB_POOL_LINK=link"}
	if r.Recipe == "llama-hip" {
		cmake = append(cmake, "-DGGML_HIP=ON", "-DCMAKE_HIP_ARCHITECTURES="+p.GPUTarget, "-DCMAKE_PREFIX_PATH=/opt/rocm", "-DCMAKE_HIP_COMPILER=/opt/rocm/bin/amdclang++")
	} else {
		cmake = append(cmake, "-DGGML_VULKAN=ON", "-DVulkan_GLSLC_EXECUTABLE=/usr/bin/glslc")
	}
	commands := [][]string{cmake, {"/usr/bin/ninja", "-C", "/build/build", "-j", strconv.Itoa(p.Jobs), "llama-cli", "llama-bench"}}
	if r.Recipe == "sunshine-image" {
		if trustedFile("/etc/containers/policy.json") != nil {
			return result, errors.New("installed container signature policy is unavailable")
		}
		base = append(base, "--uid", "0", "--gid", "0", "--dir", "/etc/containers", "--ro-bind", "/etc/containers/policy.json", "/etc/containers/policy.json", "--setenv", "TMPDIR", "/tmp", "--setenv", "BUILDAH_ISOLATION", "chroot")
		commands, e = sunshineCommands(p, job)
		if e != nil {
			return result, e
		}
	}
	for _, args := range commands {
		if _, e := os.Lstat(p.InhibitPath); e == nil || !os.IsNotExist(e) {
			return result, errors.New("managed build inhibited before compilation phase")
		}
		cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", append(append([]string{}, base...), args...)...)
		cmd.Env = []string{"PATH=/usr/bin", "LANG=C.UTF-8"}
		cmd.Stdout = writer
		cmd.Stderr = writer
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, UseCgroupFD: true, CgroupFD: int(g.Fd())}
		cmd.Cancel = func() error {
			kill()
			if cmd.Process != nil {
				return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		cmd.WaitDelay = 3 * time.Second
		if e = cmd.Run(); e != nil {
			kill()
			empty, checkErr := groupEmpty(group)
			if checkErr != nil || !empty {
				return domain.Result{State: "recovery-required", Phase: "descendant-check", Message: "build cancellation could not prove every descendant stopped", RecoveryRequired: true}, nil
			}
			return result, errors.New("contained compiler failed; inspect bounded worker build log locally")
		}
	}
	kill()
	empty, checkErr := groupEmpty(group)
	if checkErr != nil || !empty {
		return domain.Result{State: "recovery-required", Phase: "descendant-check", Message: "worker descendants cannot be proved stopped", RecoveryRequired: true}, nil
	}
	outputs := []string{"build/bin/llama-cli", "build/bin/llama-bench"}
	if r.Recipe == "sunshine-image" {
		outputs = []string{"sunshine.oci"}
	}
	for _, relative := range outputs {
		name := filepath.Base(relative)
		fpath := filepath.Join(job, relative)
		info, err := os.Lstat(fpath)
		if err != nil || !info.Mode().IsRegular() {
			return result, errors.New("build output is missing or symlinked")
		}
		f, e := os.Open(fpath)
		if e != nil {
			return result, errors.New("named build output missing")
		}
		info, e = f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			f.Close()
			return result, errors.New("invalid build output")
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return result, e
		}
		result.Artifacts = append(result.Artifacts, domain.Artifact{Name: name, SHA256: hex.EncodeToString(h.Sum(nil)), Size: info.Size(), SourceRevision: p.SourceRevision, Qualification: "candidate-not-installed-not-qualified"})
	}
	result.State = "succeeded"
	result.Phase = "candidate-built"
	result.Message = "named recipe built; no installation, promotion or qualification"
	provenance := map[string]any{"contract": 1, "recipe": r.Recipe, "source_revision": p.SourceRevision, "source_manifest_sha256": p.SourceManifestSHA256, "toolchain_sha256": p.ToolchainSHA256, "gpu_target": p.GPUTarget, "effective_jobs": p.Jobs, "memory_mib": p.MemoryMiB, "scratch_gib": p.ScratchGiB, "ccache_gib": p.CacheGiB, "commands": commands, "outputs": result.Artifacts, "qualification": "built-not-installed-not-qualified"}
	if r.Recipe == "sunshine-image" {
		sum := sha256.Sum256(sunshineContainerfile)
		provenance["containerfile_sha256"] = hex.EncodeToString(sum[:])
		provenance["base_manifest_sha256"] = steamBaseDigest
	}
	data, e := json.MarshalIndent(provenance, "", "  ")
	if e != nil {
		return result, e
	}
	out, e := os.OpenFile(filepath.Join(job, "provenance.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return result, e
	}
	_, e = out.Write(data)
	if e == nil {
		e = out.Sync()
	}
	ce = out.Close()
	if e != nil {
		return result, e
	}
	if ce != nil {
		return result, ce
	}
	sum := sha256.Sum256(data)
	result.Artifacts = append(result.Artifacts, domain.Artifact{Name: "provenance.json", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), SourceRevision: p.SourceRevision, Qualification: "built-not-installed-not-qualified"})
	return result, nil
}
func prepareJobCgroup(p Policy, r Request) (string, error) {
	root := filepath.Clean(p.CgroupRoot)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("worker delegated cgroup root is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != p.WorkerUID {
		return "", errors.New("worker delegated cgroup root is not owned by the configured worker")
	}
	files := osCgroupFiles{}
	if err := initializeDelegatedCgroup(files, root, os.Getpid()); err != nil {
		return "", err
	}
	group, err := createLimitedJobCgroup(files, root, r.ID, p.MemoryMiB, p.Jobs)
	if err != nil {
		return "", err
	}
	return group, nil
}

func groupEmpty(group string) (bool, error) {
	for i := 0; i < 30; i++ {
		events, e := os.ReadFile(filepath.Join(group, "cgroup.events"))
		if e != nil {
			return false, e
		}
		for _, line := range strings.Split(string(events), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "populated" {
				if fields[1] == "0" {
					return true, nil
				}
				if fields[1] == "1" {
					break
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, nil
}
func restartDisposition(p Policy, id string) domain.Result {
	group := filepath.Join(p.CgroupRoot, "bridge-"+id)
	info, e := os.Lstat(group)
	if os.IsNotExist(e) {
		return domain.Result{State: "recovery-required", Phase: "worker-restart", Message: "prior build cgroup is absent; descendant termination cannot be proved", RecoveryRequired: true}
	}
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.Result{State: "recovery-required", Phase: "worker-restart", Message: "prior build cgroup cannot be inspected; descendant termination cannot be proved", RecoveryRequired: true}
	}
	empty, e := groupEmpty(group)
	if e != nil {
		return domain.Result{State: "recovery-required", Phase: "worker-restart", Message: "prior build cgroup state is unreadable; descendant termination cannot be proved", RecoveryRequired: true}
	}
	if empty {
		return domain.Result{State: "failed", Phase: "worker-restart", Message: "prior build outcome was not committed; its empty cgroup proves descendants stopped and artifacts remain unqualified"}
	}
	return domain.Result{State: "recovery-required", Phase: "worker-restart", Message: "prior build descendants cannot be proved stopped; inspect delegated cgroup", RecoveryRequired: true}
}
func sandboxArgs(p Policy, job string) []string {
	return []string{"--die-with-parent", "--new-session", "--unshare-all", "--clearenv", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib", "/lib64", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/etc", "--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache", "--ro-bind", p.SourceRoot, "/source", "--bind", job, "/build", "--bind", p.CacheRoot, "/ccache", "--setenv", "PATH", "/usr/bin", "--setenv", "HOME", "/tmp", "--setenv", "LANG", "C.UTF-8", "--setenv", "CCACHE_DIR", "/ccache", "--setenv", "CCACHE_CONFIGPATH", "/dev/null", "--setenv", "CCACHE_MAXSIZE", fmt.Sprintf("%dG", p.CacheGiB), "--chdir", "/build"}
}
func verifyBuildInputs(p Policy, recipe string) error {
	if e := trustedFile(p.HardwarePath); e != nil {
		return errors.New("fresh root-owned hardware build evidence is unavailable")
	}
	b, e := os.ReadFile(p.HardwarePath)
	if e != nil || len(b) > 4<<20 {
		return errors.New("hardware build evidence is unavailable")
	}
	var h struct {
		Status      string    `json:"status"`
		CollectedAt time.Time `json:"collected_at"`
		GPUTarget   string    `json:"gpu_target"`
		GPUs        []struct {
			BDF    string `json:"bdf"`
			Driver string `json:"driver"`
		} `json:"pci_gpus"`
	}
	if json.Unmarshal(b, &h) != nil || h.Status != "observed" || h.GPUTarget != p.GPUTarget || len(h.GPUs) != 2 || h.GPUs[0].BDF == h.GPUs[1].BDF || h.GPUs[0].Driver != "amdgpu" || h.GPUs[1].Driver != "amdgpu" || time.Since(h.CollectedAt) > 24*time.Hour || time.Until(h.CollectedAt) > time.Minute {
		return errors.New("hardware build evidence failed, stale or target changed")
	}
	bootPath := filepath.Join(filepath.Dir(p.HardwarePath), "boot-id.txt")
	if e = trustedFile(bootPath); e != nil {
		return e
	}
	boot, e := os.ReadFile(bootPath)
	if e != nil {
		return e
	}
	current, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil || strings.TrimSpace(string(current)) != strings.TrimSpace(string(boot)) {
		return errors.New("build evidence belongs to another boot")
	}
	names := []string{"cmake", "ninja", "ccache", "cc", "c++", "bwrap"}
	if recipe == "llama-vulkan" {
		names = append(names, "glslc")
	} else if recipe == "sunshine-image" {
		names = []string{"bwrap", "buildah"}
	} else {
		names = append(names, "amdclang++")
	}
	for _, name := range names {
		path := filepath.Join("/usr/bin", name)
		if name == "amdclang++" {
			path = filepath.Join(p.ROCmRoot, "bin", name)
		}
		resolved, e := filepath.EvalSymlinks(path)
		if e != nil || trustedFile(resolved) != nil {
			return errors.New("installed compiler provenance unavailable")
		}
		f, e := os.Open(resolved)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, f)
		f.Close()
		if e != nil || hex.EncodeToString(hash.Sum(nil)) != p.ToolchainSHA256[name] {
			return errors.New("installed compiler/toolchain changed since policy review")
		}
	}
	return nil
}
