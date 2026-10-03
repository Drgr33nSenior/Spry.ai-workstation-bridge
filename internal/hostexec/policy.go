package hostexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type Qualification struct {
	Image         string            `json:"image"`
	ModelRevision string            `json:"model_revision"`
	ModelPath     string            `json:"model_path"`
	ExpiresAt     time.Time         `json:"expires_at"`
	HostModelPath string            `json:"host_model_path"`
	Files         map[string]string `json:"files"`
}
type SessionQualification struct {
	TemplateHash string            `json:"template_hash"`
	ExpiresAt    time.Time         `json:"expires_at"`
	ConfigMaps   map[string]string `json:"config_maps"`
}
type Policy struct {
	PerformanceSources      map[string]MemorySource         `json:"performance_sources,omitempty"`
	MemorySources           map[string]MemorySource         `json:"memory_sources,omitempty"`
	Version                 int                             `json:"version"`
	AllowedUID              uint32                          `json:"allowed_uid"`
	Socket                  string                          `json:"socket"`
	StateDir                string                          `json:"state_dir"`
	SessionDir              string                          `json:"session_dir"`
	RuntimeRoot             string                          `json:"runtime_root"`
	Artifacts               map[string]string               `json:"artifacts"`
	SystemExecutables       map[string]string               `json:"system_executables"`
	WorkstationConfig       string                          `json:"workstation_config"`
	SourcePath              string                          `json:"source_path"`
	HardwarePath            string                          `json:"hardware_path"`
	ModelRoot               string                          `json:"model_root"`
	WorkerEvidenceDir       string                          `json:"worker_evidence_dir,omitempty"`
	Kubeconfig              string                          `json:"kubeconfig"`
	Kubectl                 string                          `json:"kubectl"`
	Context                 string                          `json:"context"`
	Target                  string                          `json:"target"`
	Environment             string                          `json:"environment"`
	Namespace               string                          `json:"namespace"`
	AIDeployment            string                          `json:"ai_deployment"`
	AIDeploymentUID         string                          `json:"ai_deployment_uid"`
	GameDeployment          string                          `json:"game_deployment"`
	GameDeploymentUID       string                          `json:"game_deployment_uid"`
	TimeoutSeconds          int                             `json:"timeout_seconds"`
	QualifiedConfigurations map[string]Qualification        `json:"qualified_configurations"`
	SessionQualifications   map[string]SessionQualification `json:"session_qualifications"`
	MaxRecords              int                             `json:"max_records"`
}

func LoadPolicy(path string) (Policy, error) {
	var p Policy
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return p, errors.New("live host executor requires Linux and root")
	}
	if err := trustedPath(path, false); err != nil {
		return p, err
	}
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	if err := strictDecode(io.LimitReader(f, 1<<20), &p); err != nil {
		return p, fmt.Errorf("invalid executor policy: %w", err)
	}
	if err := p.validate(); err != nil {
		return p, err
	}
	return p, p.verifyRuntime()
}
func (p Policy) validate() error {
	if err := p.validatePerformancePolicy(); err != nil {
		return err
	}
	if err := p.validateMemoryPolicy(); err != nil {
		return err
	}
	if p.Version != ContractVersion || p.AllowedUID == 0 {
		return errors.New("contract v1 and a dedicated non-root API UID are required")
	}
	if p.Environment != "dev" && p.Environment != "tst" && p.Environment != "int" {
		return errors.New("executor target must explicitly be dev, tst or int")
	}
	if p.TimeoutSeconds < 30 || p.TimeoutSeconds > 1800 {
		return errors.New("executor timeout must be 30..1800 seconds")
	}
	if p.MaxRecords < 16 || p.MaxRecords > 10000 {
		return errors.New("executor max_records must be 16..10000")
	}
	for _, v := range []string{p.Target, p.Context, p.Namespace, p.AIDeployment, p.GameDeployment, p.AIDeploymentUID, p.GameDeploymentUID} {
		if !safeIdentity(v) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$`).MatchString(v) {
			return errors.New("explicit target, context, namespace and deployment identities are required")
		}
	}
	for _, v := range []string{p.Socket, p.StateDir, p.SessionDir, p.RuntimeRoot, p.WorkstationConfig, p.SourcePath, p.HardwarePath, p.ModelRoot, p.Kubeconfig, p.Kubectl} {
		if !filepath.IsAbs(v) || filepath.Clean(v) != v || v == "/" || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("policy paths must be canonical absolute paths")
		}
	}
	if p.AIDeployment == p.GameDeployment || p.AIDeploymentUID == p.GameDeploymentUID {
		return errors.New("AI and gaming deployments must differ")
	}
	if p.WorkerEvidenceDir != "" && p.WorkerEvidenceDir != "/var/lib/spry-bridge-evidence" {
		return errors.New("worker evidence export requires the dedicated /var/lib/spry-bridge-evidence directory")
	}
	if p.Kubectl != "/usr/bin/kubectl" {
		return errors.New("only the installed /usr/bin/kubectl is supported")
	}
	if len(p.Artifacts) == 0 {
		return errors.New("reviewed installed runtime artifact hashes are required")
	}
	for _, name := range []string{"bin/workstationctl", "lib/common.sh", "lib/workstation/runtime.sh", "lib/workstation/session.sh", "versions.lock"} {
		if len(p.Artifacts[name]) != 64 {
			return fmt.Errorf("missing runtime provenance: %s", name)
		}
	}
	return nil
}

// Every path component must be root-owned and non-writable by another UID.
// This also forbids symlink traversal, including an otherwise trusted leaf.
func trustedPath(path string, directory bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("non-canonical trusted path")
	}
	for current := path; ; current = filepath.Dir(current) {
		s, err := os.Lstat(current)
		if err != nil {
			return err
		}
		owner, ok := s.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 || s.Mode()&os.ModeSymlink != 0 || s.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("untrusted root policy/runtime path: %s", current)
		}
		if current == path && ((directory && !s.IsDir()) || (!directory && !s.Mode().IsRegular())) {
			return errors.New("unexpected trusted path type")
		}
		if current == "/" {
			break
		}
	}
	return nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (p Policy) verifyRuntime() error {
	if err := verifySystemExecutables(p.SystemExecutables); err != nil {
		return err
	}
	if err := trustedPath(p.RuntimeRoot, true); err != nil {
		return err
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(p.RuntimeRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = trustedPath(path, d.IsDir()); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(p.RuntimeRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want, ok := p.Artifacts[rel]
		if !ok {
			return fmt.Errorf("unreviewed installed runtime file: %s", rel)
		}
		got, err := fileHash(path)
		if err != nil {
			return err
		}
		if want != got {
			return fmt.Errorf("installed runtime provenance mismatch: %s", rel)
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(p.Artifacts) {
		return errors.New("installed runtime manifest includes missing files")
	}
	for _, path := range []string{p.WorkstationConfig, p.Kubeconfig, p.Kubectl, "/etc/workstation/session-policy.conf"} {
		if err := trustedPath(path, false); err != nil {
			return err
		}
	}
	workstation, err := os.ReadFile(p.WorkstationConfig)
	if err != nil {
		return err
	}
	configured := map[string]string{}
	for _, line := range strings.Split(string(workstation), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return errors.New("invalid literal workstation configuration")
		}
		if _, exists := configured[k]; exists {
			return errors.New("duplicate workstation configuration key")
		}
		configured[k] = v
	}
	for key, want := range map[string]string{"SESSION_CONTEXT": p.Context, "SESSION_NODE": p.Target, "SESSION_NAMESPACE": p.Namespace, "SESSION_ENVIRONMENT": p.Environment, "SESSION_AI_DEPLOYMENT": p.AIDeployment, "SESSION_GAME_DEPLOYMENT": p.GameDeployment} {
		if configured[key] != want {
			return errors.New("installed workstation target differs from independent helper policy")
		}
	}
	identity, err := os.ReadFile(p.Kubeconfig)
	if err != nil {
		return err
	}
	if regexp.MustCompile(`(?m)^\s*(exec|auth-provider|proxy-url|insecure-skip-tls-verify)\s*:`).Match(identity) {
		return errors.New("helper Kubernetes identity must use reviewed static TLS credentials, without exec/auth-provider, proxy or insecure TLS options")
	}
	// The installed Bash parser has the same literal allowlist; never source it.
	b, err := os.ReadFile("/etc/workstation/session-policy.conf")
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || values[k] != "" || (k != "SESSION_STATE_DIRECTORY" && k != "SESSION_KUBECONFIG") {
			return errors.New("invalid installed legacy session policy")
		}
		values[k] = v
	}
	if values["SESSION_STATE_DIRECTORY"] != p.SessionDir || values["SESSION_KUBECONFIG"] != p.Kubeconfig {
		return errors.New("helper and legacy session policy differ")
	}
	return nil
}

var requiredSystemTools = []string{"bash", "env", "jq", "kubectl", "stat", "flock", "findmnt", "systemd-detect-virt", "sync", "chmod", "mkdir", "mktemp", "mv", "date", "uname", "awk", "sort", "tr", "grep", "head", "dirname", "basename", "readlink", "cat", "cp", "rm", "sleep", "sha256sum"}
var collectorSystemTools = []string{"lscpu", "lspci", "free", "lsblk", "gcc", "clang", "rocminfo", "rocm-smi", "pacman", "dmidecode", "systemctl", "nvme", "smartctl", "python3"}

func systemToolPath(name string) string {
	path := "/usr/bin/" + name
	if name == "rocminfo" || name == "rocm-smi" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "/opt/rocm/bin/" + name
		}
	}
	return path
}

func verifySystemExecutables(expected map[string]string) error {
	for _, name := range append(append([]string{}, requiredSystemTools...), collectorSystemTools...) {
		path := systemToolPath(name)
		_, err := os.Stat(path)
		optional := false
		for _, v := range collectorSystemTools {
			if v == name {
				optional = true
			}
		}
		if os.IsNotExist(err) && optional {
			continue
		}
		if err != nil {
			return errors.New("reviewed system executable is unavailable: " + name)
		}
		want := expected[name]
		if len(want) != 64 {
			return errors.New("system executable provenance is absent: " + name)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if err = trustedPath(resolved, false); err != nil {
			return err
		}
		got, err := fileHash(resolved)
		if err != nil {
			return err
		}
		if got != want {
			return errors.New("installed system executable changed since root policy review: " + name)
		}
	}
	return nil
}

// SystemManifest reads the fixed system executable closure on the target. It
// does not execute tools or authorize their installation or resulting hashes.
func SystemManifest() (map[string]string, error) {
	m := map[string]string{}
	for _, name := range append(append([]string{}, requiredSystemTools...), collectorSystemTools...) {
		path := systemToolPath(name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		h, err := fileHash(path)
		if err != nil {
			return nil, err
		}
		m[name] = h
	}
	return m, nil
}

// ArtifactManifest is an offline packaging operation; it does not install or
// bless the result. An owner reviews and copies its output into root policy.
func ArtifactManifest(root string) (map[string]string, error) {
	m := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("runtime bundle cannot contain symlinks")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("runtime bundle requires regular files")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		h, err := fileHash(path)
		if err != nil {
			return err
		}
		m[filepath.ToSlash(rel)] = h
		return nil
	})
	return m, err
}
func policyRevision(p Policy) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
