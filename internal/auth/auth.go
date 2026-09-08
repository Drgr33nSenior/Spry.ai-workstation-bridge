package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type Actor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type CredentialInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
}

func Info(c store.Credential) CredentialInfo {
	return CredentialInfo{c.ID, c.Name, c.Role, c.CreatedAt, c.ExpiresAt, c.Revoked}
}
func Secret() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Verifier(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func NewCredential(name, role string, ttl time.Duration) (store.Credential, string, error) {
	if len(name) < 1 || len(name) > 80 {
		return store.Credential{}, "", errors.New("credential name must be 1..80 characters")
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return store.Credential{}, "", errors.New("credential name contains control characters")
		}
	}
	if role != "owner" && role != "operator" && role != "viewer" {
		return store.Credential{}, "", errors.New("role must be owner, operator or viewer")
	}
	if ttl < time.Minute || ttl > 90*24*time.Hour {
		return store.Credential{}, "", errors.New("credential TTL must be 1 minute..90 days")
	}
	token := Secret()
	now := time.Now().UTC()
	return store.Credential{ID: store.ID(), Name: name, Role: role, Verifier: Verifier(token), CreatedAt: now, ExpiresAt: now.Add(ttl)}, token, nil
}
func Bearer(s store.State, token string) (Actor, error) {
	if len(token) != 43 {
		return Actor{}, domain.Fail("unauthorized", "invalid or expired credential")
	}
	h := Verifier(token)
	for _, c := range s.Credentials {
		if subtle.ConstantTimeCompare([]byte(h), []byte(c.Verifier)) == 1 && !c.Revoked && time.Now().Before(c.ExpiresAt) {
			return Actor{c.ID, c.Name, c.Role}, nil
		}
	}
	return Actor{}, domain.Fail("unauthorized", "invalid or expired credential")
}
func Session(s store.State, token string) (Actor, store.Session, error) {
	var empty store.Session
	if len(token) != 43 {
		return Actor{}, empty, domain.Fail("unauthorized", "browser session expired; sign in again")
	}
	session, ok := s.Sessions[Verifier(token)]
	if !ok || time.Now().After(session.ExpiresAt) {
		return Actor{}, empty, domain.Fail("unauthorized", "browser session expired; sign in again")
	}
	c, ok := s.Credentials[session.CredentialID]
	if !ok || c.Revoked || time.Now().After(c.ExpiresAt) {
		return Actor{}, empty, domain.Fail("unauthorized", "browser credential revoked or expired")
	}
	return Actor{c.ID, c.Name, c.Role}, session, nil
}
func Login(db *store.Store, credential string) (Actor, string, store.Session, error) {
	if _, err := Bearer(db.AuthState(), credential); err != nil {
		return Actor{}, "", store.Session{}, err
	}
	var actor Actor
	var session store.Session
	token := Secret()
	err := db.Update(func(s *store.State) error {
		a, e := Bearer(*s, credential)
		if e != nil {
			return e
		}
		if len(s.Sessions) >= 128 {
			return domain.Fail("capacity", "browser session capacity reached; revoke old sessions")
		}
		actor = a
		c := s.Credentials[a.ID]
		expires := time.Now().UTC().Add(30 * time.Minute)
		if c.ExpiresAt.Before(expires) {
			expires = c.ExpiresAt
		}
		session = store.Session{Verifier: Verifier(token), CredentialID: a.ID, CSRF: Secret(), ExpiresAt: expires}
		s.Sessions[session.Verifier] = session
		store.Event(s, a.ID, "login", a.ID, "succeeded")
		return nil
	})
	return actor, token, session, err
}
