// Package admin is deliberately offline. No HTTP route invokes it.
package admin

import (
	"errors"
	"os"
	"sort"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func Authorized(uid, owner int) bool { return uid == 0 || uid == owner }
func Run(configPath, action, role, name, outputPath string, ttl time.Duration) (any, error) {
	c, e := config.Load(configPath)
	if e != nil {
		return nil, e
	}
	if !Authorized(os.Geteuid(), c.OwnerUID) {
		return nil, errors.New("offline administration requires root or the policy's explicit owner UID")
	}
	db, e := store.Open(c.StateDir, c.Mode)
	if e != nil {
		return nil, e
	}
	defer db.Close()
	if action == "list" {
		items := []auth.CredentialInfo{}
		for _, v := range db.View().Credentials {
			items = append(items, auth.Info(v))
		}
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
		return items, nil
	}
	if action == "revoke" {
		e = db.Update(func(s *store.State) error {
			cr, ok := s.Credentials[name]
			if !ok {
				return errors.New("credential ID does not exist")
			}
			cr.Revoked = true
			s.Credentials[name] = cr
			for key, session := range s.Sessions {
				if session.CredentialID == name {
					delete(s.Sessions, key)
				}
			}
			store.Event(s, "local-owner", "credential.revoke", name, "succeeded")
			return nil
		})
		return map[string]string{"id": name, "state": "revoked"}, e
	}
	if action != "bootstrap" && action != "recover" && action != "issue" {
		return nil, errors.New("admin action must be bootstrap, recover, issue, list or revoke")
	}
	if action == "bootstrap" && len(db.View().Credentials) != 0 {
		return nil, errors.New("owner already bootstrapped; use explicit stopped-service recovery")
	}
	if action == "issue" && len(db.View().Credentials) == 0 {
		return nil, errors.New("bootstrap an owner first")
	}
	if action == "bootstrap" || action == "recover" {
		role = "owner"
		if name == "" {
			name = "local-owner"
		}
	}
	cr, token, e := auth.NewCredential(name, role, ttl)
	if e != nil {
		return nil, e
	}
	// Write the new secret first. A crash before the store update leaves an inert
	// file, never an undisclosed active credential. Existing files are refused.
	if e = safefile.CreateSecret(outputPath, []byte(token+"\n"), c.OwnerUID); e != nil {
		return nil, e
	}
	e = db.Update(func(s *store.State) error {
		if len(s.Credentials) >= 128 && action != "recover" {
			return errors.New("credential capacity reached; use local recovery to reset expired history")
		}
		if action == "recover" {
			s.Credentials = map[string]store.Credential{}
			s.Sessions = map[string]store.Session{}
		}
		s.Credentials[cr.ID] = cr
		store.Event(s, "local-owner", "credential."+action, cr.ID, "succeeded")
		return nil
	})
	if e != nil {
		return nil, errors.New("credential activation failed; output file may exist but must not be used; inspect stopped-service state")
	}
	return auth.Info(cr), nil
}
