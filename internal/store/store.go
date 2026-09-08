// Package store is a bounded, single-writer embedded JSON store. Each update is
// a copy-on-write snapshot followed by fsync, rename and directory fsync.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

const MaxBytes = 32 << 20

type Credential struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Verifier  string    `json:"verifier"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
}
type Session struct {
	Verifier     string    `json:"verifier"`
	CredentialID string    `json:"credential_id"`
	CSRF         string    `json:"csrf"`
	ExpiresAt    time.Time `json:"expires_at"`
	// Purpose binds a persisted verifier to the v2 browser-session policy.
	// It is deliberately optional in the JSON schema so schema-1 records can
	// load and be rejected rather than requiring journal deletion.
	Purpose string `json:"purpose,omitempty"`
}
type Audit struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Object string    `json:"object"`
	Result string    `json:"result"`
}
type Idempotency struct {
	PayloadHash string `json:"payload_hash"`
	OperationID string `json:"operation_id"`
}
type State struct {
	Schema      int                         `json:"schema"`
	Mode        string                      `json:"mode"`
	Revision    uint64                      `json:"revision"`
	Credentials map[string]Credential       `json:"credentials"`
	Sessions    map[string]Session          `json:"sessions"`
	Plans       map[string]domain.Plan      `json:"plans"`
	Operations  map[string]domain.Operation `json:"operations"`
	Idempotency map[string]Idempotency      `json:"idempotency"`
	Audit       []Audit                     `json:"audit"`
}
type Store struct {
	mu       sync.Mutex
	path     string
	lock     *safefile.Lock
	state    State
	poisoned bool
	Write    func(string, []byte, os.FileMode) error
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b)
}
func NewState(mode string) State {
	return State{Schema: 1, Mode: mode, Credentials: map[string]Credential{}, Sessions: map[string]Session{}, Plans: map[string]domain.Plan{}, Operations: map[string]domain.Operation{}, Idempotency: map[string]Idempotency{}, Audit: []Audit{}}
}
func Open(dir, mode string) (*Store, error) {
	if mode != "demo" && mode != "live" {
		return nil, errors.New("explicit adapter mode required")
	}
	if e := safefile.CheckPath(dir); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	i, e := os.Stat(dir)
	if e != nil {
		return nil, e
	}
	if i.Mode().Perm()&0077 != 0 {
		return nil, errors.New("state directory must be owner-only (0700)")
	}
	l, e := safefile.Acquire(filepath.Join(dir, "store.lock"))
	if e != nil {
		return nil, e
	}
	s := &Store{path: filepath.Join(dir, "state.json"), lock: l, state: NewState(mode), Write: safefile.Replace}
	b, e := safefile.Read(s.path, MaxBytes)
	if os.IsNotExist(e) {
		if e = s.persist(s.state); e != nil {
			l.Close()
			return nil, e
		}
		return s, nil
	}
	if e == nil {
		e = config.Decode(b, &s.state)
	}
	if e != nil {
		l.Close()
		return nil, errors.New("state cannot be read; restore a verified stopped-service backup")
	}
	if s.state.Schema != 1 || s.state.Mode != mode || s.state.Credentials == nil || s.state.Sessions == nil || s.state.Plans == nil || s.state.Operations == nil || s.state.Idempotency == nil {
		l.Close()
		return nil, errors.New("state schema or demo/live mode mismatch")
	}
	return s, nil
}
func (s *Store) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.lock.Close() }
func clone(v State) State {
	b, _ := json.Marshal(v)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Store) View() State { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.state) }

// AuthState copies only the small authentication index. Unauthenticated traffic
// must never clone retained operation/audit payloads under the writer lock.
func (s *Store) AuthState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := State{Credentials: make(map[string]Credential, len(s.state.Credentials)), Sessions: make(map[string]Session, len(s.state.Sessions))}
	for k, v := range s.state.Credentials {
		out.Credentials[k] = v
	}
	for k, v := range s.state.Sessions {
		out.Sessions[k] = v
	}
	return out
}
func (s *Store) Healthy() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.poisoned }
func (s *Store) persist(v State) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) > MaxBytes {
		return errors.New("state budget exhausted")
	}
	return s.Write(s.path, b, 0600)
}
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poisoned {
		return domain.Fail("storage_unavailable", "durable storage failed; mutations disabled until recovery")
	}
	v := clone(s.state)
	prune(&v)
	if e := fn(&v); e != nil {
		return e
	}
	prune(&v)
	v.Revision++
	if e := s.persist(v); e != nil {
		s.poisoned = true
		return domain.Fail("storage_unavailable", "durable persistence failed; stop and inspect state before recovery")
	}
	s.state = v
	return nil
}
func Event(v *State, actor, action, object, result string) {
	v.Audit = append(v.Audit, Audit{time.Now().UTC(), actor, action, object, result})
}
func prune(v *State) {
	now := time.Now()
	for k, s := range v.Sessions {
		if now.After(s.ExpiresAt) {
			delete(v.Sessions, k)
		}
	}
	for k, p := range v.Plans {
		if now.After(p.ExpiresAt.Add(time.Hour)) {
			delete(v.Plans, k)
		}
	}
	if len(v.Audit) > 2000 {
		v.Audit = append([]Audit(nil), v.Audit[len(v.Audit)-2000:]...)
	}
	// Finished records expire after 30 days. Uncertain/executing operations never
	// disappear automatically; full capacity refuses new work.
	// Keep ancestors of retained attempts: pruning a resolved/old parent would
	// destroy the persisted relationship needed to validate later recovery.
	retained := map[string]bool{}
	for id, o := range v.Operations {
		if !domain.Terminal(o.State) || o.RecoveryRequired || now.Sub(o.UpdatedAt) <= 30*24*time.Hour {
			for id != "" && !retained[id] {
				retained[id] = true
				parent, ok := v.Operations[id]
				if !ok {
					break
				}
				id = parent.Plan.Draft.RecoveryID
			}
		}
	}
	for k, o := range v.Operations {
		if !retained[k] && domain.Terminal(o.State) && !o.RecoveryRequired && now.Sub(o.UpdatedAt) > 30*24*time.Hour {
			delete(v.Operations, k)
		}
	}
	for k, i := range v.Idempotency {
		if _, ok := v.Operations[i.OperationID]; !ok {
			delete(v.Idempotency, k)
		}
	}
}
