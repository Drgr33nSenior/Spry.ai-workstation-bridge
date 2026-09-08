package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type domainConfiguration = domain.Configuration

type record struct {
	Request        Request   `json:"request"`
	Result         Result    `json:"result"`
	PeerUID        uint32    `json:"peer_uid"`
	PolicyRevision string    `json:"policy_revision"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type Executor struct {
	policy        Policy
	mu            sync.Mutex
	records       map[string]record
	active        bool
	journalFailed bool
	verify        func() error
	run           func(context.Context, Request, *os.File) (json.RawMessage, error)
	lock          func() (*os.File, error)
	pathTrust     func(string, bool) error
	finalize      func() error
	persist       func(string, any) error
}

func NewExecutor(p Policy) (*Executor, error) {
	for _, dir := range []string{p.StateDir, p.SessionDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		if err := trustedPath(dir, true); err != nil {
			return nil, err
		}
		s, _ := os.Stat(dir)
		if s.Mode().Perm() != 0700 {
			return nil, errors.New("helper journal and legacy session directories must be mode 0700")
		}
	}
	e := &Executor{policy: p, records: map[string]record{}, verify: p.verifyRuntime, persist: durableJSON}
	e.run = e.execute
	e.pathTrust = trustedPath
	e.finalize = e.finishBuildGate
	e.lock = func() (*os.File, error) { return lockFile(filepath.Join(p.SessionDir, "lock")) }
	if err := e.load(); err != nil {
		return nil, err
	}
	return e, nil
}

func lockFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another legacy CLI or helper GPU operation holds the canonical lock")
	}
	return f, nil
}

func durableJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".journal-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (e *Executor) save(r record) error {
	r.UpdatedAt = time.Now().UTC()
	persist := e.persist
	if persist == nil {
		persist = durableJSON
	}
	if err := persist(filepath.Join(e.policy.StateDir, r.Request.ID+".json"), r); err != nil {
		e.journalFailed = true
		return errors.New("root recovery journal could not be persisted; mutations are fenced")
	}
	e.records[r.Request.ID] = r
	return nil
}
func (e *Executor) load() error {
	entries, err := os.ReadDir(e.policy.StateDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(e.policy.StateDir, entry.Name())
		if err := e.pathTrust(path, false); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		var r record
		err = strictDecode(io.LimitReader(f, 1<<20), &r)
		f.Close()
		if err != nil {
			return errors.New("invalid root recovery journal; owner inspection is required")
		}
		if !validID(r.Request.ID) || entry.Name() != r.Request.ID+".json" || r.Request.PayloadHash != RequestHash(r.Request) {
			return errors.New("root recovery journal identity/hash mismatch")
		}
		e.records[r.Request.ID] = r
		if r.Result.State == "running" || r.Result.State == "queued" {
			if readOnlyAction(r.Request.Draft.Action) {
				r.Result.State = "failed"
				r.Result.Phase = "read-only-interrupted"
				r.Result.Message = "Executor restarted before a bounded diagnostic completed; its output is not valid and no host recovery is required."
			} else {
				r.Result.State = "recovery-required"
				r.Result.Phase = "executor-restart"
				r.Result.Message = "Executor restarted with uncertain external effects; inspect the legacy session state and request an explicit restore. No work was retried."
			}
			if err := e.save(r); err != nil {
				return err
			}
		}
	}
	if e.recoverySettlementPending() {
		lock, err := e.lock()
		if err != nil {
			e.journalFailed = true
			return errors.New("root recovery settlement cannot acquire the canonical legacy lock; mutations remain fenced")
		}
		defer lock.Close()
	}
	return e.rollForwardRecoverySettlements()
}
func (e *Executor) Status(id string) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.records[id]
	if !ok {
		return Result{}, errors.New("unknown helper operation")
	}
	return e.visibleResult(r), nil
}

func (e *Executor) Submit(peer uint32, req Request) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if peer != e.policy.AllowedUID {
		return Result{}, errors.New("Unix peer is not the configured management service UID")
	}
	if req.Version != ContractVersion || !validID(req.ID) || !safeIdentity(req.ExecutionIdentity) || req.PayloadHash != RequestHash(req) {
		return Result{}, errors.New("invalid helper contract, operation identity or payload hash")
	}
	if old, ok := e.records[req.ID]; ok {
		if old.Request.PayloadHash != req.PayloadHash || old.PeerUID != peer {
			return Result{}, errors.New("operation ID payload conflict")
		}
		return e.visibleResult(old), nil
	}
	if e.active || e.journalFailed {
		return Result{}, errors.New("executor is busy or fenced by unavailable journal persistence")
	}
	if len(e.records) >= e.policy.MaxRecords {
		return Result{}, errors.New("root journal retention limit reached; archive it during stopped-service maintenance")
	}
	if err := e.validate(req); err != nil {
		return Result{}, err
	}
	if err := e.verify(); err != nil {
		return Result{}, err
	}
	lock, err := e.lock()
	if err != nil {
		return Result{}, err
	}
	r := record{Request: req, PeerUID: peer, PolicyRevision: policyRevision(e.policy), StartedAt: time.Now().UTC(), Result: Result{ID: req.ID, State: "running", Phase: "intent-persisted", Message: "Root helper accepted the exact typed operation; disconnecting does not cancel it."}}
	if err := e.save(r); err != nil {
		lock.Close()
		return Result{}, err
	}
	e.active = true
	go e.finish(r, lock)
	return r.Result, nil
}

func readOnlyAction(action string) bool {
	return action == "hardware.refresh" || action == "cpu-policy.export"
}

func (e *Executor) visibleResult(r record) Result {
	if e.journalFailed && r.Result.State == "succeeded" && r.Request.Draft.Action == "profile.restore" && r.Request.Draft.RecoveryID != "" {
		return Result{ID: r.Result.ID, State: "recovery-required", Phase: "recovery-settlement-pending", Message: "Restore effects were durably recorded, but prior recovery records were not durably settled; restart the helper after persistence is restored."}
	}
	return r.Result
}

func (e *Executor) validate(r Request) error {
	if r.Draft.Target != e.policy.Target {
		return errors.New("target is not authorized by root policy")
	}
	if err := e.validateRecovery(r); err != nil {
		return err
	}
	if r.Draft.Profile != "" && r.Draft.Action != "profile.switch" {
		return errors.New("profile does not belong to this typed operation")
	}
	if r.Draft.Recipe != "" || r.Draft.Caches != nil {
		return errors.New("host executor does not accept build or cache settings")
	}
	switch r.Draft.Action {
	case "profile.switch", "profile.restore", "serving.configure", "serving.start", "serving.stop", "serving.restart", "resources.configure", "hardware.refresh", "cpu-policy.export":
	default:
		return errors.New("operation is not in the host executor allowlist")
	}
	if r.Draft.Action == "profile.switch" && r.Draft.Profile != "ai" && r.Draft.Profile != "gaming" && r.Draft.Profile != "maintenance" {
		return errors.New("unsupported profile")
	}
	if r.Draft.Action == "hardware.refresh" || r.Draft.Action == "cpu-policy.export" {
		return nil
	}
	if r.Desired.Revision != r.Desired.ContentRevision() {
		return errors.New("desired configuration content revision mismatch")
	}
	if r.Draft.SourceRevision != r.Desired.Revision {
		return errors.New("request source revision does not identify the committed desired configuration")
	}
	f, err := os.Open(e.policy.SourcePath)
	if err != nil {
		return errors.New("canonical configuration source is unavailable")
	}
	defer f.Close()
	var source domainConfiguration
	if err := strictDecode(io.LimitReader(f, 1<<20), &source); err != nil {
		return errors.New("canonical configuration source is malformed")
	}
	if source.ContentRevision() != r.Desired.ContentRevision() {
		return errors.New("canonical source changed after planning or source update")
	}
	if r.Draft.Action == "serving.configure" || r.Draft.Action == "resources.configure" || r.Draft.Action == "serving.start" || r.Draft.Action == "serving.restart" {
		s, b := r.Desired.Serving, r.Desired.Resources
		if s.Context < 128 || s.Context > 131072 || s.Concurrency < 1 || s.Concurrency > 32 || s.MemoryFraction < 0.1 || s.MemoryFraction > 0.95 || s.CPUOffloadGiB < 0 || s.CPUOffloadGiB > 32 || s.MaxRequestTokens != 0 || s.MaxOutputTokens != 0 {
			return errors.New("unsupported serving limits: global request/output caps are not enforced by this installed SGLang adapter")
		}
		if b.PhysicalGPU != "" || b.GPUCount < 1 || b.GPUCount > 2 || b.CPU < 2 || b.CPU > 48 || b.MemoryMiB < 1024 || b.MemoryMiB > 49152 || b.SharedMemoryMiB < 64 || b.SharedMemoryMiB >= b.MemoryMiB {
			return errors.New("resource budget or physical GPU selection is outside root policy")
		}
		if int64(s.CPUOffloadGiB)*1024+b.SharedMemoryMiB >= b.MemoryMiB {
			return errors.New("CPU offload and shared memory leave no engine process memory")
		}
		q, ok := e.policy.QualifiedConfigurations[ConfigurationHash(&s, &b)]
		if !ok || !q.ExpiresAt.After(time.Now()) || !strings.Contains(q.Image, "@sha256:") || len(q.ModelRevision) != 40 || !strings.HasPrefix(q.ModelPath, "/models/") || filepath.Clean(q.ModelPath) != q.ModelPath {
			return errors.New("exact model, engine image and configuration require a current owner qualification in root policy")
		}
	}
	return nil
}

func (e *Executor) finish(r record, lock *os.File) {
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.policy.TimeoutSeconds)*time.Second)
	defer cancel()
	data, err := e.run(ctx, r.Request, lock)
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() { e.active = false }()
	r.Result.Data = data
	if err != nil {
		r.Result.State = "recovery-required"
		r.Result.Phase = "external-effect-uncertain"
		r.Result.Message = fmt.Sprintf("%s; inspect root journal and legacy session state, then explicitly restore operation %s", err.Error(), r.Request.ID)
		var preflight preflightFailure
		if errors.As(err, &preflight) {
			r.Result.State = "failed"
			r.Result.Phase = "preflight-refused"
			r.Result.Message = err.Error()
		}
		if readOnlyAction(r.Request.Draft.Action) {
			r.Result.State = "failed"
			r.Result.Phase = "read-only-adapter-failed"
			r.Result.Message = err.Error()
		}
	} else {
		r.Result.State = "succeeded"
		r.Result.Phase = "effects-complete"
		r.Result.Message = "Validated operation completed; physical GPU release is a point-in-time observation."
	}
	if r.Result.State == "succeeded" && !readOnlyAction(r.Request.Draft.Action) {
		pending := r
		pending.Result.State = "running"
		pending.Result.Phase = "build-gate-disposition"
		if err := e.save(pending); err != nil {
			return
		}
		if err := e.finalize(); err != nil {
			r.Result.State = "recovery-required"
			r.Result.Phase = "build-gate-disposition"
			r.Result.Message = "External effects completed but build inhibition could not be safely finalized; owner inspection is required."
		}
	}
	if err := e.save(r); err != nil {
		return
	}
	if r.Result.State == "succeeded" && r.Request.Draft.RecoveryID != "" {
		// The successful retry is durable before any parent fence is settled. If
		// a later parent write fails, load() rolls this settlement forward without
		// re-running the already completed external restore.
		_ = e.settleRecoveryChain(r.Request.ID)
	}
}

func (e *Executor) recoveryLinks() map[string]domain.RecoveryLink {
	links := make(map[string]domain.RecoveryLink, len(e.records))
	for id, r := range e.records {
		links[id] = domain.RecoveryLink{Parent: r.Request.Draft.RecoveryID, Target: r.Request.Draft.Target, Action: r.Request.Draft.Action}
	}
	return links
}

func (e *Executor) validateRecovery(r Request) error {
	if r.Draft.RecoveryID == "" {
		for _, old := range e.records {
			if old.Result.State == "recovery-required" {
				return errors.New("root helper recovery is required; restore must identify one unresolved helper recovery chain")
			}
		}
		return nil
	}
	if r.Draft.Action != "profile.restore" || !validID(r.Draft.RecoveryID) {
		return errors.New("restore recovery ID must identify an unresolved root helper recovery chain")
	}
	old, ok := e.records[r.Draft.RecoveryID]
	if !ok || old.Result.State != "recovery-required" || !domain.HostRestoreRequired(old.Request.Draft.Action) {
		return errors.New("restore recovery ID must identify an unresolved root helper operation with host effects")
	}
	if old.Request.Draft.Target != r.Draft.Target {
		return errors.New("root helper recovery cannot change the persisted target")
	}
	chain, err := domain.RecoveryChain(e.recoveryLinks(), r.Draft.RecoveryID)
	if err != nil || !chain[r.Draft.RecoveryID] {
		return errors.New("root helper recovery chain is ambiguous; preserve the root journal for owner review")
	}
	for id, candidate := range e.records {
		if candidate.Result.State == "recovery-required" && !chain[id] {
			return errors.New("root helper recovery is required in another chain; settle it independently")
		}
	}
	return nil
}

func (e *Executor) settleRecoveryChain(recoveredID string) error {
	chain, err := domain.RecoveryChain(e.recoveryLinks(), recoveredID)
	if err != nil || !chain[recoveredID] {
		e.journalFailed = true
		return errors.New("root helper recovery chain is ambiguous; successful retry retained and mutations fenced")
	}
	recovered := e.records[recoveredID]
	for id := range chain {
		if id == recoveredID {
			continue
		}
		candidate := e.records[id]
		if candidate.Result.State == "recovery-required" && (candidate.StartedAt.IsZero() || recovered.StartedAt.IsZero() || !candidate.StartedAt.Before(recovered.StartedAt)) {
			e.journalFailed = true
			return errors.New("root helper recovery chain has a later or unordered unresolved attempt; successful retry retained and mutations fenced")
		}
	}
	ids := make([]string, 0, len(chain))
	for id := range chain {
		if id != recoveredID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		old := e.records[id]
		if old.Result.State != "recovery-required" {
			continue
		}
		old.Result.State = "failed"
		old.Result.Phase = "recovered-by-" + recoveredID
		old.Result.Message = "A later explicit restore completed; this operation did not succeed."
		if err := e.save(old); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) recoverySettlementPending() bool {
	links := e.recoveryLinks()
	for id, r := range e.records {
		if r.Result.State != "succeeded" || r.Request.Draft.Action != "profile.restore" || r.Request.Draft.RecoveryID == "" {
			continue
		}
		chain, err := domain.RecoveryChain(links, id)
		if err != nil {
			continue
		}
		for candidate := range chain {
			if candidate != id && e.records[candidate].Result.State == "recovery-required" {
				return true
			}
		}
	}
	return false
}

func (e *Executor) rollForwardRecoverySettlements() error {
	ids := make([]string, 0, len(e.records))
	for id := range e.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := e.records[id]
		if r.Result.State == "succeeded" && r.Request.Draft.Action == "profile.restore" && r.Request.Draft.RecoveryID != "" {
			if err := e.settleRecoveryChain(id); err != nil {
				return err
			}
		}
	}
	return nil
}
