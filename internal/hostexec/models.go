package hostexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func (e *Executor) verifyModel(ctx context.Context, c domain.Configuration) error {
	q, ok := e.policy.QualifiedConfigurations[ConfigurationHash(&c.Serving, &c.Resources)]
	if !ok || !q.ExpiresAt.After(time.Now()) || len(q.Files) == 0 {
		return errors.New("owner-qualified model file integrity manifest is absent or expired")
	}
	rel, err := filepath.Rel(e.policy.ModelRoot, q.HostModelPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return errors.New("qualified model path is outside root policy model storage")
	}
	if q.ModelPath != "/models/"+filepath.ToSlash(rel) {
		return errors.New("qualified container model path does not match the managed PVC storage mapping")
	}
	info, err := os.Lstat(e.policy.ModelRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed model root is unavailable or symlinked")
	}
	root, err := os.OpenRoot(e.policy.ModelRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	current := ""
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("qualified model path is absent or symlinked")
		}
	}
	model, err := root.OpenRoot(rel)
	if err != nil {
		return err
	}
	defer model.Close()
	seen := map[string]bool{}
	err = fs.WalkDir(model.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("model contains a symlink")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("model contains a special file")
		}
		if name == ".bridge-receipt.json" {
			return nil
		}
		want, ok := q.Files[name]
		if !ok || len(want) != 64 {
			return errors.New("model contains an unreviewed file")
		}
		f, err := model.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		buffer := make([]byte, 1<<20)
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n, readErr := f.Read(buffer)
			if n > 0 {
				_, _ = h.Write(buffer[:n])
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		if hex.EncodeToString(h.Sum(nil)) != want {
			return errors.New("qualified model file integrity mismatch")
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(q.Files) {
		return errors.New("qualified model files are missing")
	}
	return nil
}
