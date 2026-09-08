package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

func dir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestExclusiveStoreAndMode(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Open(p, "demo"); e == nil {
		other.Close()
		t.Fatal("concurrent store admitted")
	}
	if e = s.Update(func(v *State) error { Event(v, "fixture", "write", "id", "ok"); return nil }); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if other, e := Open(p, "live"); e == nil {
		other.Close()
		t.Fatal("demo credentials crossed into live state")
	}
	s, e = Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if len(s.View().Audit) != 1 {
		t.Fatal("durable record lost")
	}
}
func TestDiskFullPoisonsMutationWithoutInventedSuccess(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Write = func(string, []byte, os.FileMode) error { return errors.New("disk full injected") }
	if s.Update(func(v *State) error { Event(v, "test", "effect", "x", "success"); return nil }) == nil {
		t.Fatal("failed storage acknowledged")
	}
	if s.Healthy() || len(s.View().Audit) != 0 {
		t.Fatal("failed commit became authoritative")
	}
	s.Write = safefile.Replace
	if s.Update(func(v *State) error { return nil }) == nil {
		t.Fatal("failed store resumed mutation without recovery")
	}
}
func TestCrashAfterRenameRequiresRestartRead(t *testing.T) {
	p := dir(t)
	s, e := Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	s.Write = func(path string, b []byte, m os.FileMode) error {
		if e := safefile.Replace(path, b, m); e != nil {
			return e
		}
		return errors.New("injected lost directory sync result")
	}
	if s.Update(func(v *State) error { Event(v, "test", "intent", "x", "persisted"); return nil }) == nil {
		t.Fatal("uncertain persistence accepted")
	}
	s.Close()
	s, e = Open(p, "demo")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if len(s.View().Audit) != 1 {
		t.Fatal("reopen failed to inspect durable reality")
	}
}
