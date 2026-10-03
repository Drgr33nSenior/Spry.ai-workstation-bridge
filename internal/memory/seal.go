package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

// Seal is an explicit client-local owner command, never an HTTP action. It
// creates a new manifest; failed observations remain failed bytes. A manifest
// does not approve the source for the root helper or qualify its observations.
func Seal(ctx context.Context, root, source, hardware, boot string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("evidence directory must be canonical and absolute")
	}
	if err := safefile.CheckPath(root); err != nil {
		return "", err
	}
	if !Digest.MatchString(source) || !Digest.MatchString(hardware) {
		return "", errors.New("source revision and hardware SHA-256 are required")
	}
	m := Manifest{Schema: 1, Kind: "sglang-memory-evidence", SourceRevision: source, HardwareSHA256: hardware, BootID: boot, Files: map[string]string{}}
	ids := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return errors.New("evidence must already be owner-private, without symlinks")
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return errors.New("evidence is already sealed; never overwrite its manifest")
		}
		parts := strings.Split(rel, "/")
		if len(parts) >= 3 && parts[0] == "observations" && ObservationID.MatchString(parts[1]) {
			ids[parts[1]] = true
		}
		hash, _, err := HashFile(ctx, path)
		if err != nil {
			return err
		}
		m.Files[rel] = hash
		return nil
	})
	if err != nil {
		return "", err
	}
	for id := range ids {
		m.Observations = append(m.Observations, id)
	}
	sort.Strings(m.Observations)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if err = safefile.CreateSecret(filepath.Join(root, "manifest.json"), b, -1); err != nil {
		return "", err
	}
	hash := Sum(b)
	if _, err = Verify(ctx, root, hash); err != nil {
		return hash, errors.New("sealed evidence retained but invalid; inspect its bounded layout, do not approve it")
	}
	return hash, nil
}
