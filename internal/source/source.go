package source

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

type Source struct {
	mu    sync.Mutex
	Path  string
	Write func(string, []byte, os.FileMode) error
}

func New(path string) *Source { return &Source{Path: path, Write: safefile.Replace} }
func (s *Source) Read() (domain.Configuration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}
func (s *Source) read() (domain.Configuration, error) {
	var c domain.Configuration
	b, e := safefile.Read(s.Path, 1<<20)
	if e != nil {
		return c, domain.Fail("source_unavailable", "managed source is unavailable; import the reviewed configuration locally")
	}
	if e = config.Decode(b, &c); e != nil {
		return c, e
	}
	if c.Revision != c.ContentRevision() {
		return c, domain.Fail("source_drift", "managed source content hash is invalid; re-import the reviewed source locally")
	}
	return c, nil
}
func (s *Source) Update(expected string, next domain.Configuration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e := s.read()
	if e != nil {
		return e
	}
	if current.Revision != expected {
		return domain.Fail("source_drift", "source changed since planning; no live effect dispatched")
	}
	next.Revision = next.ContentRevision()
	b, e := json.MarshalIndent(next, "", "  ")
	if e != nil {
		return e
	}
	return s.Write(s.Path, append(b, '\n'), 0600)
}
func (s *Source) Initialize(c domain.Configuration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := os.Lstat(s.Path); e == nil {
		return errors.New("source exists; import refuses overwrite")
	} else if !os.IsNotExist(e) {
		return e
	}
	c.Revision = c.ContentRevision()
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return safefile.CreateSecret(s.Path, append(b, '\n'), -1)
}
