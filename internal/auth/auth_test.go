package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func TestAuthenticationUsesSmallIndexAndPrunesExpiredSessions(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dir, 0700)
	db, err := store.Open(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cr, token, err := NewCredential("fixture", "owner", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Update(func(s *store.State) error {
		s.Credentials[cr.ID] = cr
		for i := 0; i < 128; i++ {
			id := store.ID()
			s.Sessions[id] = store.Session{Verifier: id, ExpiresAt: time.Now().Add(-time.Second)}
		}
		store.Event(s, "fixture", "large-retention", "object", "data")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	index := db.AuthState()
	if index.Operations != nil || index.Audit != nil || index.Plans != nil {
		t.Fatal("auth path includes retained management state")
	}
	delete(index.Credentials, cr.ID)
	if _, ok := db.AuthState().Credentials[cr.ID]; !ok {
		t.Fatal("auth snapshot aliases the store")
	}
	revision := db.View().Revision
	if _, _, _, err := Login(db, "invalid", "test-v2"); err == nil {
		t.Fatal("invalid login accepted")
	}
	if db.View().Revision != revision {
		t.Fatal("invalid login wrote the store")
	}
	if _, _, _, err := Login(db, token, "test-v2"); err != nil {
		t.Fatalf("expired sessions prevented login: %v", err)
	}
}
