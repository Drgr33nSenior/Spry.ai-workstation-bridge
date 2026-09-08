// Package safefile implements bounded reads and durable local replacements.
package safefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// CheckPath rejects symlinks in existing components. Managed directories must
// additionally be writable only by their trusted owner to prevent path races.
func CheckPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("path must be absolute and canonical")
	}
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && i.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink paths are forbidden")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func Read(path string, max int64) ([]byte, error) {
	if e := CheckPath(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Size() > max {
		return nil, errors.New("not a bounded regular file")
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(b)) > max {
		return nil, errors.New("file exceeds size limit")
	}
	return b, e
}
func SyncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// Replace does not claim a transaction with external effects. A caller must
// persist intent first and treat a failure after rename as uncertain.
func Replace(path string, b []byte, mode os.FileMode) error {
	if e := CheckPath(path); e != nil {
		return e
	}
	var uid, gid int = -1, -1
	if i, e := os.Lstat(path); e == nil {
		if !i.Mode().IsRegular() {
			return errors.New("destination is not a regular file")
		}
		mode = i.Mode().Perm()
		if s, ok := i.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(s.Uid), int(s.Gid)
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if uid < 0 && os.Geteuid() == 0 {
		if parent, e := os.Stat(filepath.Dir(path)); e == nil {
			if s, ok := parent.Sys().(*syscall.Stat_t); ok {
				uid, gid = int(s.Uid), int(s.Gid)
			}
		}
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".bridge-write-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil && os.Geteuid() == 0 && uid >= 0 {
		e = f.Chown(uid, gid)
	}
	if e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	return SyncDir(filepath.Dir(path))
}
func CreateSecret(path string, b []byte, uid int) error {
	if e := CheckPath(path); e != nil {
		return e
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if dir.Mode().Perm()&0077 != 0 {
		return errors.New("credential output directory must be owner-only (0700)")
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if os.Geteuid() == 0 {
		if uid < 0 {
			if s, ok := dir.Sys().(*syscall.Stat_t); ok {
				uid = int(s.Uid)
			}
		}
		if uid >= 0 {
			if e = f.Chown(uid, -1); e != nil {
				return e
			}
		}
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = SyncDir(filepath.Dir(path)); e != nil {
		return e
	}
	ok = true
	return nil
}
func Owner(path string) (int, error) {
	i, e := os.Lstat(path)
	if e != nil {
		return 0, e
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("owner inspection unsupported")
	}
	return int(s.Uid), nil
}

type Lock struct{ f *os.File }

func Acquire(path string) (*Lock, error) {
	if e := CheckPath(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe lock file")
	}
	if os.Geteuid() == 0 {
		if parent, e := os.Stat(filepath.Dir(path)); e == nil {
			if s, ok := parent.Sys().(*syscall.Stat_t); ok {
				if e = f.Chown(int(s.Uid), int(s.Gid)); e != nil {
					f.Close()
					return nil, e
				}
			}
		}
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("state is exclusively locked; stop the service before local administration")
	}
	return &Lock{f}, nil
}
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}
