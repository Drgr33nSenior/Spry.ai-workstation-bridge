package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

var nativePaths = map[string]bool{"bundle.json": true, "qwen/settings.json": true, "dsh/settings.yaml": true, "hermes/config.yaml": true}

func WriteArtifact(artifact domain.Artifact, path string) error {
	if artifact.Content == "" {
		return errors.New("artifact has no downloadable content; inspect its managed output and provenance on the target")
	}
	b := []byte(artifact.Content)
	sum := sha256.Sum256(b)
	if int64(len(b)) != artifact.Size || hex.EncodeToString(sum[:]) != artifact.SHA256 {
		return errors.New("artifact size or SHA-256 verification failed")
	}
	return WriteNewFile(path, b)
}

func VerifyBundle(bundle domain.Bundle) (map[string][]byte, error) {
	if len(bundle.Files) != len(nativePaths) {
		return nil, errors.New("native bundle must contain the four reviewed files")
	}
	files := make(map[string][]byte, len(nativePaths))
	for _, f := range bundle.Files {
		if !nativePaths[f.Path] || len(f.Content) > 1<<20 {
			return nil, errors.New("native bundle contains an unsupported path or oversized file")
		}
		if _, exists := files[f.Path]; exists {
			return nil, errors.New("native bundle contains a duplicate file")
		}
		b := []byte(f.Content)
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return nil, errors.New("native bundle file digest mismatch")
		}
		files[f.Path] = b
	}
	for _, h := range []string{"qwen", "dsh", "hermes"} {
		mode := "cli"
		if h == "dsh" {
			mode = "acp"
		}
		if _, err := catalog.VerifyNative(files, h, mode); err != nil {
			return nil, err
		}
	}
	if bundle.Harness != "qwen" && bundle.Harness != "dsh" && bundle.Harness != "hermes" {
		return nil, errors.New("unknown bundle harness")
	}
	return files, nil
}

// WriteNewFile never overwrites a credential, context, or user configuration.
func WriteNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return errors.New("output must be a new file in an existing directory")
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("output write failed; inspect the incomplete owner-only file: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// ConfigureBundle publishes metadata last. A failed write cannot produce a launchable partial bundle.
func ConfigureBundle(bundle domain.Bundle, directory string) error {
	files, err := VerifyBundle(bundle)
	if err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return errors.New("bundle directory must be new; existing client configuration is never overwritten")
	}
	for _, h := range []string{"qwen", "dsh", "hermes"} {
		if err := os.Mkdir(filepath.Join(directory, h), 0700); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		if p != "bundle.json" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	paths = append(paths, "bundle.json")
	for _, p := range paths {
		if err := WriteNewFile(filepath.Join(directory, p), files[p]); err != nil {
			return fmt.Errorf("bundle incomplete; do not launch: %w", err)
		}
	}
	return nil
}

func ReadBundleFile(path string) (domain.Bundle, error) {
	var b domain.Bundle
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return b, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 5<<20 {
		return b, errors.New("bundle must be a bounded regular JSON file")
	}
	d := json.NewDecoder(io.LimitReader(f, 5<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return b, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return b, errors.New("bundle must contain one JSON object")
	}
	_, err = VerifyBundle(b)
	return b, err
}

func readNativeDirectory(directory string) (map[string][]byte, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("bundle directory must be a regular directory")
	}
	files := map[string][]byte{}
	for path := range nativePaths {
		if parent := filepath.Dir(path); parent != "." {
			info, err := os.Lstat(filepath.Join(directory, parent))
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("native configuration directory is missing or symlinked")
			}
		}
		p := filepath.Join(directory, path)
		f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, errors.New("native configuration file is missing or symlinked")
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			f.Close()
			return nil, errors.New("native configuration file is not a bounded regular file")
		}
		b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if err != nil || len(b) > 1<<20 {
			return nil, errors.New("cannot read native configuration file")
		}
		files[path] = b
	}
	return files, nil
}

type LaunchSpec struct {
	Executable  string
	Arguments   []string
	Environment []string
}

// PrepareLaunch accepts only pinned client names. The server cannot provide an executable or arguments.
func PrepareLaunch(directory, selected, mode string) (LaunchSpec, error) {
	if os.Geteuid() == 0 {
		return LaunchSpec{}, errors.New("developer clients must run as an unprivileged local user")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return LaunchSpec{}, errors.New("developer client launch supports macOS and Linux")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return LaunchSpec{}, err
	}
	files, err := readNativeDirectory(abs)
	if err != nil {
		return LaunchSpec{}, err
	}
	p, err := catalog.VerifyNative(files, selected, mode)
	if err != nil {
		return LaunchSpec{}, err
	}
	qwenPath := filepath.Join(abs, "qwen/settings.json")
	if p.Harness == "qwen" {
		policy := "/etc/qwen-code/settings.json"
		if runtime.GOOS == "darwin" {
			policy = "/Library/Application Support/QwenCode/settings.json"
		}
		if _, err := os.Lstat(policy); !os.IsNotExist(err) {
			return LaunchSpec{}, errors.New("existing or unreadable Qwen system policy requires an owner-reviewed merge")
		}
		if existing := os.Getenv("QWEN_CODE_SYSTEM_SETTINGS_PATH"); existing != "" && existing != qwenPath {
			return LaunchSpec{}, errors.New("existing QWEN_CODE_SYSTEM_SETTINGS_PATH requires owner review")
		}
	}
	key := os.Getenv("WORKSTATION_AGENT_API_KEY")
	if key == "" {
		if strings.HasPrefix(p.BaseURL, "https://") {
			return LaunchSpec{}, errors.New("HTTPS inference requires WORKSTATION_AGENT_API_KEY from a supported local secret input")
		}
		key = "local-tunnel"
	}
	executable, err := exec.LookPath(p.Harness)
	if err != nil {
		return LaunchSpec{}, errors.New("selected client is not installed locally; Bridge does not install harnesses")
	}
	env := []string{}
	for _, key := range []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "COLORTERM", "LANG", "LC_ALL", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "WORKSTATION_AGENT_API_KEY="+key, "OPENAI_API_KEY="+key, "OPENAI_BASE_URL="+p.BaseURL, "OPENAI_MODEL="+p.Model)
	var args []string
	switch p.Harness {
	case "qwen":
		env = append(env, "QWEN_CODE_SYSTEM_SETTINGS_PATH="+qwenPath, "QWEN_HOME="+filepath.Join(abs, "qwen"), "QWEN_RUNTIME_DIR="+filepath.Join(abs, "qwen"), "QWEN_MODEL="+p.Model, "QWEN_USAGE_STATISTICS_ENABLED=false", "QWEN_TELEMETRY_ENABLED=false")
		args = []string{"--auth-type", "openai", "--model", p.Model, "--approval-mode", "default"}
		if mode == "acp" {
			args = append(args, "--acp")
		}
	case "dsh":
		env = append(env, "DSH_HOME="+filepath.Join(abs, "dsh"), "DSH_PERMISSION_MODE=workspace-write", "DSH_TELEMETRY_MODE=DISABLED")
		args = []string{"--profile", "acp"}
	case "hermes":
		env = append(env, "HERMES_HOME="+filepath.Join(abs, "hermes"))
		args = []string{"chat"}
		if mode == "acp" {
			args = []string{"acp"}
		}
	}
	return LaunchSpec{executable, args, env}, nil
}

func Launch(directory, selected, mode string) error {
	spec, err := PrepareLaunch(directory, selected, mode)
	if err != nil {
		return err
	}
	return syscall.Exec(spec.Executable, append([]string{filepath.Base(spec.Executable)}, spec.Arguments...), spec.Environment)
}
