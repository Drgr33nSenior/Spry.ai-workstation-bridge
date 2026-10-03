package memory

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

func checkOutputTree(dir string) error {
	if err := safefile.CheckPath(dir); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("memory output directory must remain private")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 3 {
		return errors.New("memory output requires exactly three retained artifacts")
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !PrivateName(entry.Name()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > 2<<20 {
			return errors.New("memory output must contain only bounded private regular artifacts")
		}
	}
	return nil
}

// SyncOutput precedes durable helper success. The installed planner closes its
// outputs but does not fsync them; syncing summary.json alone is insufficient.
func SyncOutput(dir string) error {
	if err := checkOutputTree(dir); err != nil {
		return err
	}
	for _, name := range []string{"plan.json", "patch.json", "rollback.json"} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return safefile.SyncDir(dir)
}
