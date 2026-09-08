package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func TestAtomicSourceDriftPermissionsAndSymlink(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	_ = os.Chmod(dir, 0700)
	p := filepath.Join(dir, "source.json")
	s := New(p)
	initial := domain.Configuration{Serving: domain.Serving{Model: "reviewed"}}
	if e = s.Initialize(initial); e != nil {
		t.Fatal(e)
	}
	_ = os.Chmod(p, 0640)
	old, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	next := old
	next.Serving.Context = 8192
	if e = s.Update(old.Revision, next); e != nil {
		t.Fatal(e)
	}
	if e = s.Update(old.Revision, next); e == nil {
		t.Fatal("conflicting external source update accepted")
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	link := filepath.Join(dir, "link.json")
	if e = os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e = New(link).Read(); e == nil {
		t.Fatal("symlink accepted")
	}
	if e = s.Initialize(initial); e == nil {
		t.Fatal("import overwrote existing source")
	}
}
