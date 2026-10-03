// Package performance checks transport/provenance bounds for installer-owned
// analysis. Comparison, sizing, warmup and cache algorithms stay in the installer.
package performance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

const MaxFile = 64 << 20
const MaxBundle = 256 << 20
const MaxArtifact = 512 << 10

var Digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var Name = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}$`)
var Tools = []string{"performance_bundle.py", "performance_profiles.py", "coding_eval.py", "coding_tasks.json", "serving_runtime.py", "inference_cache.py", "serving_sweep.py", "model_kernels.py", "serving.py", "measurement.py"}

type Manifest struct {
	Schema         int               `json:"schema"`
	Kind           string            `json:"kind"`
	Target         string            `json:"target"`
	SourceRevision string            `json:"source_revision"`
	Files          map[string]string `json:"files"`
}

func Sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Relative(name string) bool {
	if name == "" || len(name) > 240 || filepath.ToSlash(filepath.Clean(name)) != name || filepath.IsAbs(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if !Name.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func Verify(ctx context.Context, root, digest string) (Manifest, error) {
	var m Manifest
	b, err := safefile.Read(filepath.Join(root, "manifest.json"), 64<<10)
	if err != nil {
		return m, err
	}
	if !Digest.MatchString(digest) || Sum(b) != digest {
		return m, errors.New("performance manifest hash changed")
	}
	if err = config.Decode(b, &m); err != nil {
		return m, err
	}
	if m.Schema != 1 || !domain.PerformanceKind(m.Kind) || !Digest.MatchString(m.SourceRevision) || !Name.MatchString(m.Target) || len(m.Files) == 0 || len(m.Files) > 256 || !Digest.MatchString(m.Files["spec.json"]) {
		return m, errors.New("unsupported or incomplete performance bundle")
	}
	dirs := map[string]bool{".": true}
	for name, hash := range m.Files {
		if !Relative(name) || name == "manifest.json" || !Digest.MatchString(hash) {
			return m, errors.New("unsafe performance manifest entry")
		}
		for dir := filepath.Dir(name); dir != "."; dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	var total int64
	count := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		i, err := d.Info()
		if err != nil {
			return err
		}
		if i.Mode()&os.ModeSymlink != 0 || i.Mode().Perm()&0077 != 0 {
			return errors.New("performance evidence must be private and symlink-free")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !dirs[rel] {
				return errors.New("unexpected performance directory")
			}
			return nil
		}
		if rel == "manifest.json" {
			return nil
		}
		want, ok := m.Files[rel]
		if !ok {
			return errors.New("unexpected performance evidence file")
		}
		b, err := safefile.Read(path, MaxFile)
		if err != nil {
			return err
		}
		total += int64(len(b))
		if total > MaxBundle {
			return errors.New("performance evidence exceeds storage bound")
		}
		if Sum(b) != want {
			return errors.New("performance evidence file changed")
		}
		count++
		return nil
	})
	if err == nil && count != len(m.Files) {
		err = errors.New("performance evidence file missing")
	}
	return m, err
}
func ValidateSummary(s domain.PerformanceSummary) error {
	if s.Schema != 1 || !domain.PerformanceKind(s.Kind) || !Digest.MatchString(s.SHA256) || len(s.Status) == 0 || len(s.Status) > 80 || len(s.Reason) == 0 || len(s.Reason) > 300 || len(s.Fields) > 40 || len(s.Artifacts) > 100 || len(s.Limitations) > 20 || len(s.Preconditions) > 20 {
		return errors.New("invalid bounded performance summary")
	}
	for _, f := range s.Fields {
		if len(f.Name) > 80 || len(f.Value) > 256 || len(f.Unit) > 40 || len(f.State) > 80 {
			return errors.New("performance summary field exceeds bounds")
		}
	}
	for _, note := range s.Limitations {
		if len(note) > 500 {
			return errors.New("performance limitation exceeds bound")
		}
	}
	for k, v := range s.Preconditions {
		if !Name.MatchString(k) || len(v) > 256 {
			return errors.New("invalid performance precondition")
		}
	}
	seen := map[string]bool{}
	for _, a := range s.Artifacts {
		if seen[a.Name] || !Relative(a.Name) || a.Content != "" || !Digest.MatchString(a.SHA256) || a.Size < 1 || a.Size > MaxArtifact {
			return errors.New("invalid private performance artifact metadata")
		}
		seen[a.Name] = true
	}
	return nil
}

func Copy(ctx context.Context, source, destination, digest string) error {
	m, err := Verify(ctx, source, digest)
	if err != nil {
		return err
	}
	if err = safefile.CheckPath(destination); err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	names := []string{"manifest.json"}
	for name := range m.Files {
		names = append(names, name)
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(destination, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		b, err := safefile.Read(filepath.Join(source, name), MaxFile)
		if err != nil {
			return err
		}
		if err = safefile.CreateSecret(path, b, -1); err != nil {
			return err
		}
	}
	_, err = Verify(ctx, destination, digest)
	if err != nil {
		return err
	}
	_, err = Verify(ctx, source, digest)
	return err
}
