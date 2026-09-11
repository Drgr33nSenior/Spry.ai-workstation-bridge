// Package memory validates the private, offline installer memory contract.
// Imported reference identity is evidence, never installed runtime authority.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
)

const MaxFile = 256 << 20
const MaxBundle = 1 << 30

var Digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{7,79}$`)
var ObservationID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,39}$`)
var Tools = []string{"serving_memory.py", "measurement.py", "performance.sh", "serving.py", "model_kernels.py"}
var ObservationFiles = []string{"startup/startup.json", "startup/memory.jsonl", "serving/result.json", "serving/host-telemetry.jsonl", "serving/after/runtime.json", "serving/after/pod.json"}

type Manifest struct {
	Schema         int               `json:"schema"`
	Kind           string            `json:"kind"`
	BootID         string            `json:"boot_id"`
	HardwareSHA256 string            `json:"hardware_sha256"`
	SourceRevision string            `json:"source_revision"`
	Observations   []string          `json:"observations"`
	Files          map[string]string `json:"files"`
}

func Sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Read(path string, limit int64) ([]byte, error) {
	// Refuse traversal through symlinks, even when the leaf itself is regular.
	for p := path; ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("memory evidence symlink refused")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Size() > limit {
		return nil, errors.New("memory file type or size refused")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("memory file grew beyond bound")
	}
	return b, e
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.Reader.Read(b)
}
func HashFile(ctx context.Context, path string) (string, int64, error) {
	// Parent path safety is established by Verify's exact tree walk.
	f, e := os.Open(path)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Size() > MaxFile {
		return "", 0, errors.New("memory file exceeds bounds")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(contextReader{ctx, f}, MaxFile+1))
	if n > MaxFile {
		return "", n, errors.New("memory file exceeds bounds")
	}
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func Verify(ctx context.Context, root, want string) (Manifest, error) {
	var m Manifest
	b, e := Read(filepath.Join(root, "manifest.json"), 64<<10)
	if e != nil {
		return m, e
	}
	if !Digest.MatchString(want) || Sum(b) != want {
		return m, errors.New("memory evidence manifest hash changed")
	}
	if e = config.Decode(b, &m); e != nil {
		return m, e
	}
	if m.Schema != 1 || m.Kind != "sglang-memory-evidence" || !Digest.MatchString(m.SourceRevision) || !Digest.MatchString(m.HardwareSHA256) || !regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`).MatchString(m.BootID) || len(m.Observations) > 20 {
		return m, errors.New("invalid memory evidence manifest")
	}
	allowed := map[string]bool{"deployment.json": true, "workload.json": true, "resource-plan.json": true}
	seen := map[string]bool{}
	for _, id := range m.Observations {
		if !ObservationID.MatchString(id) || seen[id] {
			return m, errors.New("invalid or duplicate observation ID")
		}
		seen[id] = true
		for _, f := range ObservationFiles {
			allowed["observations/"+id+"/"+f] = true
		}
	}
	// Missing phase files are retained as incomplete evidence. Baseline inputs are required.
	for _, name := range []string{"deployment.json", "workload.json", "resource-plan.json"} {
		if !Digest.MatchString(m.Files[name]) {
			return m, errors.New("missing baseline evidence")
		}
	}
	dirs := map[string]bool{".": true}
	for name, digest := range m.Files {
		if !allowed[name] || !Digest.MatchString(digest) {
			return m, errors.New("unapproved memory evidence file")
		}
		for d := filepath.Dir(name); d != "."; d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	var total int64
	count := 0
	e = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("memory evidence symlink refused")
		}
		if d.IsDir() {
			if !dirs[rel] {
				return errors.New("unexpected memory evidence directory")
			}
			return nil
		}
		if rel == "manifest.json" {
			return nil
		}
		want, ok := m.Files[rel]
		if !ok {
			return errors.New("unexpected memory evidence file")
		}
		got, n, err := HashFile(ctx, path)
		if err != nil {
			return err
		}
		total += n
		if total > MaxBundle {
			return errors.New("memory evidence bundle exceeds 1 GiB")
		}
		if got != want {
			return errors.New("memory evidence file hash changed")
		}
		count++
		return nil
	})
	if e == nil && count != len(m.Files) {
		e = errors.New("memory evidence files missing")
	}
	return m, e
}

// Copy preserves failure evidence; it never replaces an existing snapshot.
func Copy(ctx context.Context, src, dst, want string) (Manifest, error) {
	m, e := Verify(ctx, src, want)
	if e != nil {
		return m, e
	}
	if e = os.Mkdir(dst, 0700); e != nil {
		return m, e
	}
	names := []string{"manifest.json"}
	for n := range m.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		path := filepath.Join(dst, n)
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return m, e
		}
		in, err := os.Open(filepath.Join(src, n))
		if err != nil {
			return m, err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return m, err
		}
		_, err = io.Copy(out, io.LimitReader(contextReader{ctx, in}, MaxFile+1))
		in.Close()
		if err == nil {
			err = out.Sync()
		}
		ce := out.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			return m, err
		}
	}
	if _, e = Verify(ctx, src, want); e != nil {
		return m, e
	}
	if _, e = Verify(ctx, dst, want); e != nil {
		return m, e
	}
	e = filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return f.Sync()
	})
	return m, e
}
func Names(m Manifest) []string {
	r := make([]string, 0, len(m.Files))
	for n := range m.Files {
		r = append(r, n)
	}
	sort.Strings(r)
	return r
}
func Complete(m Manifest) bool {
	if len(m.Observations) < 4 {
		return false
	}
	for _, id := range m.Observations {
		for _, f := range ObservationFiles {
			if m.Files["observations/"+id+"/"+f] == "" {
				return false
			}
		}
	}
	return true
}
func PrivateName(name string) bool {
	return name == "plan.json" || name == "patch.json" || name == "rollback.json"
}
func JSON(path string, out any) error {
	b, e := Read(path, 2<<20)
	if e != nil {
		return e
	}
	return config.Decode(b, out)
}
func RawJSON(path string) (map[string]any, error) {
	b, e := Read(path, 2<<20)
	if e != nil {
		return nil, e
	}
	var out map[string]any
	e = json.Unmarshal(b, &out)
	return out, e
}
func Nested(v map[string]any, keys ...string) any {
	var x any = v
	for _, k := range keys {
		m, _ := x.(map[string]any)
		x = m[k]
	}
	return x
}
func MiB(v any) (int64, error) {
	s, ok := v.(string)
	if !ok {
		return 0, errors.New("memory limit must use integer Mi/Gi")
	}
	var n int64
	mult := int64(1)
	if strings.HasSuffix(s, "Gi") {
		mult = 1024
	} else if !strings.HasSuffix(s, "Mi") {
		return 0, errors.New("invalid memory units")
	}
	if e := json.Unmarshal([]byte(s[:len(s)-2]), &n); e != nil || n <= 0 || n > 1<<30 {
		return 0, errors.New("invalid memory quantity")
	}
	return n * mult, nil
}
