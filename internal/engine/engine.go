package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/source"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/telemetry"
)

type Engine struct {
	DB      *store.Store
	Source  *source.Source
	Adapter domain.Adapter
	Target  string
	Depth   int
	Timeout time.Duration
	// Telemetry is attached before Start and never controls operation admission.
	Telemetry *telemetry.Recorder
	mu        sync.Mutex
	cancel    map[string]context.CancelFunc
	wake      chan struct{}
	ctx       context.Context
	stop      context.CancelFunc
	done      chan struct{}
}

func New(db *store.Store, src *source.Source, adapter domain.Adapter, target string, depth int, timeout time.Duration) *Engine {
	ctx, stop := context.WithCancel(context.Background())
	return &Engine{DB: db, Source: src, Adapter: adapter, Target: target, Depth: depth, Timeout: timeout, cancel: map[string]context.CancelFunc{}, wake: make(chan struct{}, 1), ctx: ctx, stop: stop, done: make(chan struct{})}
}
func (e *Engine) Start() error {
	// Queued intent has no external effect and is safe to revalidate. Anything
	// dispatched is inspected, never resent. A lost response is not a retry.
	for id, o := range e.DB.View().Operations {
		if !domain.Terminal(o.State) && o.State != "queued" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			r, err := e.inspect(ctx, o)
			cancel()
			if err != nil || r.State == "" {
				r = domain.Result{State: "recovery-required", Phase: "restart-inspection", Message: "executor outcome is unknown; inspect helper/worker recovery state before restore", RecoveryRequired: true}
			}
			if r.State == "running" || r.State == "queued" {
				r = domain.Result{State: "recovery-required", Phase: "executor-still-running", Message: "executor survived controller restart; wait for its durable result before recovery", RecoveryRequired: true}
			}
			if err = e.finish(id, r); err != nil {
				return err
			}
		}
	}
	go e.loop()
	e.signal()
	return nil
}
func (e *Engine) Close(ctx context.Context) error {
	e.stop()
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
func (e *Engine) Snapshot(ctx context.Context) (domain.Inventory, error) {
	return e.Adapter.Snapshot(ctx)
}
func (e *Engine) CreatePlan(ctx context.Context, a auth.Actor, d domain.Draft) (domain.Plan, error) {
	if !domain.RoleAllows(a.Role, d.Action) {
		return domain.Plan{}, domain.Fail("forbidden", "role cannot request this operation")
	}
	if d.RecoveryID != "" {
		if a.Role != "owner" || (d.Action != "profile.restore" && d.Action != "operation.reconcile") {
			return domain.Plan{}, domain.Fail("forbidden", "only owner restore plans may reference recovery")
		}
	}
	c, err := e.Source.Read()
	if err != nil {
		return domain.Plan{}, err
	}
	inv, pv, err := e.preflight(ctx, d, c)
	if err != nil {
		return domain.Plan{}, err
	}
	if err = domain.ValidateDraft(d, c, inv); err != nil {
		return domain.Plan{}, err
	}
	desired, _ := domain.Desired(c, d)
	pv.Changes = changes(c, desired)
	if pv.Consequences == nil {
		pv.Consequences = []string{}
	}
	if pv.Warnings == nil {
		pv.Warnings = []string{}
	}
	if desired.Revision != c.Revision {
		pv.Consequences = append(pv.Consequences, "Update managed-source.json atomically; source success and live apply success are recorded separately.")
	}
	if d.Action == "serving.configure" || d.Action == "resources.configure" || d.Action == "serving.restart" || d.Action == "profile.switch" || d.Action == "profile.restore" {
		pv.Consequences = append(pv.Consequences, "GPU workloads may stop. Handover requires complete pod termination and host DRM checks. Failure can require explicit recovery.")
	}
	if d.Action == "caches.configure" {
		pv.Consequences = append(pv.Consequences, "New admission budgets apply to future work. Existing cache files are not deleted.")
	}
	if d.Action == "operation.reconcile" {
		pv.Consequences = append(pv.Consequences, "Recheck source and operation-specific durable executor evidence. No work is redispatched; previous failed attempts remain in history.")
	}
	now := time.Now().UTC()
	p := domain.Plan{ID: store.ID(), Actor: a.ID, Draft: d, Desired: desired, Preview: pv, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	p.Hash = domain.Hash(p)
	err = e.DB.Update(func(s *store.State) error {
		if len(s.Plans) >= 200 {
			return domain.Fail("capacity", "plan retention capacity reached; expired plans are removed on subsequent writes")
		}
		s.Plans[p.ID] = p
		store.Event(s, a.ID, "plan.create", p.ID, "validated")
		return nil
	})
	return p, err
}
func (e *Engine) preflight(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.Inventory, domain.Preview, error) {
	if domain.MemoryAction(d.Action) {
		pv, err := e.Adapter.Validate(ctx, d, c)
		return domain.Inventory{Target: e.Target}, pv, err
	}
	var recovery domain.Preview
	if d.RecoveryID != "" || d.Action == "operation.reconcile" {
		var err error
		recovery, err = e.recoveryPreflight(ctx, d, c)
		if err != nil || d.Action == "operation.reconcile" {
			return domain.Inventory{Target: e.Target}, recovery, err
		}
	}
	inv, err := e.Adapter.Snapshot(ctx)
	if err != nil {
		return inv, domain.Preview{}, err
	}
	pv, err := e.Adapter.Validate(ctx, d, c)
	if d.RecoveryID != "" {
		if pv.Preconditions == nil {
			pv.Preconditions = map[string]string{}
		}
		for k, v := range recovery.Preconditions {
			pv.Preconditions[k] = v
		}
	}
	return inv, pv, err
}
func changes(a, b domain.Configuration) []domain.Change {
	out := []domain.Change{}
	groups := []struct {
		name string
		a, b any
	}{{"serving", a.Serving, b.Serving}, {"resources", a.Resources, b.Resources}, {"caches", a.Caches, b.Caches}}
	for _, g := range groups {
		v, w := reflect.ValueOf(g.a), reflect.ValueOf(g.b)
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if !reflect.DeepEqual(v.Field(i).Interface(), w.Field(i).Interface()) {
				out = append(out, domain.Change{Field: g.name + "." + t.Field(i).Tag.Get("json"), Before: v.Field(i).Interface(), After: w.Field(i).Interface()})
			}
		}
	}
	return out
}
func (e *Engine) Apply(a auth.Actor, planID, target, key string) (domain.Operation, error) {
	if len(key) < 8 || len(key) > 128 {
		return domain.Operation{}, domain.Fail("invalid", "Idempotency-Key must be 8..128 printable characters")
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return domain.Operation{}, domain.Fail("invalid", "invalid Idempotency-Key")
		}
	}
	if e.ctx.Err() != nil {
		return domain.Operation{}, domain.Fail("unavailable", "controller is shutting down")
	}
	var op domain.Operation
	payload := domain.Hash(struct{ Plan, Target string }{planID, target})
	idem := a.ID + ":" + key
	err := e.DB.Update(func(s *store.State) error {
		if old, ok := s.Idempotency[idem]; ok {
			if old.PayloadHash != payload {
				return domain.Fail("idempotency_conflict", "idempotency key already belongs to another payload")
			}
			op = s.Operations[old.OperationID]
			if !domain.RoleAllows(a.Role, op.Plan.Draft.Action) {
				return domain.Fail("forbidden", "role cannot apply this operation")
			}
			return nil
		}
		p, ok := s.Plans[planID]
		if !ok {
			return domain.Fail("not_found", "plan does not exist or expired")
		}
		if p.Actor != a.ID {
			return domain.Fail("forbidden", "apply requires the identity that created this plan")
		}
		if !domain.RoleAllows(a.Role, p.Draft.Action) {
			return domain.Fail("forbidden", "role cannot apply this plan")
		}
		if p.Draft.RecoveryID != "" && a.Role != "owner" {
			return domain.Fail("forbidden", "recovery requires owner role")
		}
		if target != p.Draft.Target || target != e.Target {
			return domain.Fail("target_mismatch", "exact target confirmation does not match")
		}
		if time.Now().After(p.ExpiresAt) {
			return domain.Fail("plan_expired", "plan expired; refresh and re-plan")
		}
		check := p
		check.Hash = ""
		if domain.Hash(check) != p.Hash {
			return domain.Fail("invalid", "plan integrity check failed")
		}
		active := 0
		if err := recoveryAdmission(s.Operations, p.Draft, ""); err != nil {
			return err
		}
		for _, o := range s.Operations {
			if !domain.Terminal(o.State) {
				active++
			}
		}
		if active >= e.Depth || len(s.Operations) >= 500 {
			return domain.Fail("capacity", "operation queue or retained-record capacity is full")
		}
		now := time.Now().UTC()
		op = domain.Operation{ID: store.ID(), Actor: a.ID, Plan: p, State: "queued", Phase: "intent-persisted", Revision: 1, CreatedAt: now, UpdatedAt: now, Events: []domain.Progress{{Phase: "queued", Message: "intent durably recorded before dispatch"}}}
		s.Operations[op.ID] = op
		s.Idempotency[idem] = store.Idempotency{PayloadHash: payload, OperationID: op.ID}
		store.Event(s, a.ID, "operation.accept", op.ID, "queued")
		return nil
	})
	if err == nil {
		e.signal()
	}
	return op, err
}
func (e *Engine) Operations() []domain.Operation {
	items := []domain.Operation{}
	for _, o := range e.DB.View().Operations {
		items = append(items, o)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}
func (e *Engine) Operation(id string) (domain.Operation, error) {
	o, ok := e.DB.View().Operations[id]
	if !ok {
		return o, domain.Fail("not_found", "operation not found")
	}
	return o, nil
}
func (e *Engine) Cancel(a auth.Actor, id string, revision uint64) (domain.Operation, error) {
	err := e.DB.Update(func(s *store.State) error {
		o, ok := s.Operations[id]
		if !ok {
			return domain.Fail("not_found", "operation not found")
		}
		if !domain.RoleAllows(a.Role, o.Plan.Draft.Action) {
			return domain.Fail("forbidden", "role cannot cancel this operation")
		}
		if revision != 0 && revision != o.Revision {
			return domain.Fail("conflict", "operation changed; refresh before cancellation")
		}
		if domain.Terminal(o.State) {
			return domain.Fail("conflict", "operation already has a final or recovery result")
		}
		o.CancelRequested = true
		if o.State == "queued" {
			o.State = "cancelled"
			o.Phase = "cancelled-before-dispatch"
		} else {
			o.State = "cancel-requested"
			o.Phase = "cancellation-requested"
		}
		o.UpdatedAt = time.Now().UTC()
		o.Revision++
		s.Operations[id] = o
		store.Event(s, a.ID, "operation.cancel", id, o.State)
		return nil
	})
	if err != nil {
		return domain.Operation{}, err
	}
	e.mu.Lock()
	cancel := e.cancel[id]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return e.Operation(id)
}
func (e *Engine) Recover(ctx context.Context, a auth.Actor, id string) (domain.Plan, error) {
	if a.Role != "owner" {
		return domain.Plan{}, domain.Fail("forbidden", "recovery requires owner role")
	}
	o, err := e.Operation(id)
	if err != nil {
		return domain.Plan{}, err
	}
	if !o.RecoveryRequired {
		return domain.Plan{}, domain.Fail("conflict", "operation does not require recovery")
	}
	r, inspectErr := e.inspect(ctx, o)
	if inspectErr == nil && (r.State == "running" || r.State == "queued") {
		return domain.Plan{}, domain.Fail("conflict", "executor is still running; wait for its durable result")
	}
	if inspectErr == nil && domain.Terminal(r.State) && r.State != "recovery-required" && !r.RecoveryRequired {
		if err = e.finish(id, r); err != nil {
			return domain.Plan{}, err
		}
		return domain.Plan{}, domain.Fail("conflict", "executor result recovered; refresh operations before creating another plan")
	}
	c, err := e.Source.Read()
	if err != nil {
		return domain.Plan{}, err
	}
	d := domain.Draft{Action: "operation.reconcile", Target: e.Target, SourceRevision: c.Revision, RecoveryID: id}
	ops := e.DB.View().Operations
	chain, chainErr := domain.RecoveryChain(operationLinks(ops), id)
	if chainErr != nil {
		return domain.Plan{}, domain.Fail("recovery_required", chainErr.Error())
	}
	for member := range chain {
		previous := ops[member]
		if previous.RecoveryRequired && previous.Dispatched && domain.HostRestoreRequired(previous.Plan.Draft.Action) {
			d.Action = "profile.restore"
		}
	}
	return e.CreatePlan(ctx, a, d)
}
func (e *Engine) loop() {
	defer close(e.done)
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.wake:
		}
		for {
			if e.ctx.Err() != nil {
				return
			}
			if !e.DB.Healthy() {
				break
			}
			var next *domain.Operation
			for _, o := range e.DB.View().Operations {
				if o.State == "queued" && (next == nil || o.CreatedAt.Before(next.CreatedAt)) {
					copy := o
					next = &copy
				}
			}
			if next == nil {
				break
			}
			e.execute(*next)
		}
	}
}
func (e *Engine) progress(id string, p domain.Progress) error {
	if current, err := e.Operation(id); err == nil && len(current.Events) > 0 && current.Phase == p.Phase && current.Events[len(current.Events)-1] == p {
		return nil
	}
	return e.DB.Update(func(s *store.State) error {
		o, ok := s.Operations[id]
		if !ok {
			return errors.New("operation record missing")
		}
		o.Phase = p.Phase
		if p.Phase == "dispatch-intent" {
			o.Dispatched = true
		}
		o.Message = p.Message
		o.UpdatedAt = time.Now().UTC()
		o.Revision++
		o.Events = append(o.Events, p)
		if len(o.Events) > 128 {
			o.Events = append([]domain.Progress(nil), o.Events[len(o.Events)-128:]...)
		}
		s.Operations[id] = o
		store.Event(s, o.Actor, "operation.phase", id, p.Phase)
		return nil
	})
}
func (e *Engine) execute(op domain.Operation) {
	ctx, cancel := context.WithTimeout(e.ctx, e.Timeout)
	defer cancel()
	ctx, observed := e.Telemetry.Operation(ctx, op.Plan.Draft.Action)
	defer func() {
		state := "unknown"
		if current, err := e.Operation(op.ID); err == nil {
			state = current.State
		}
		observed(state)
	}()
	e.mu.Lock()
	e.cancel[op.ID] = cancel
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.cancel, op.ID); e.mu.Unlock() }()
	if err := e.DB.Update(func(s *store.State) error {
		o := s.Operations[op.ID]
		if o.State != "queued" {
			return domain.Fail("cancelled", "operation no longer queued")
		}
		if recoveryAdmission(s.Operations, o.Plan.Draft, o.ID) != nil {
			o.State = "failed"
			o.Phase = "blocked-by-recovery"
			o.Message = "an earlier operation requires recovery; no external effect dispatched"
			o.Revision++
			o.UpdatedAt = time.Now().UTC()
			s.Operations[o.ID] = o
			return nil
		}
		credential, ok := s.Credentials[o.Actor]
		if !ok || credential.Revoked || time.Now().After(credential.ExpiresAt) || !domain.RoleAllows(credential.Role, o.Plan.Draft.Action) || time.Now().After(o.Plan.ExpiresAt) {
			o.State = "failed"
			o.Phase = "authorization-or-plan-expired"
			o.Message = "queued authorization or plan expired; no external effect dispatched"
			o.Revision++
			o.UpdatedAt = time.Now().UTC()
			s.Operations[o.ID] = o
			return nil
		}
		o.State = "running"
		o.Phase = "preflight"
		o.Revision++
		o.UpdatedAt = time.Now().UTC()
		s.Operations[o.ID] = o
		return nil
	}); err != nil {
		return
	}
	if current, err := e.Operation(op.ID); err != nil || current.State != "running" {
		return
	}
	fail := func(err error, uncertain bool) {
		code, message := "adapter_failed", "adapter failed; inspect bounded executor diagnostics"
		var f *domain.Failure
		if errors.As(err, &f) {
			code, message = f.Code, f.Message
		}
		state := "failed"
		if uncertain {
			state = "recovery-required"
		}
		_ = e.finish(op.ID, domain.Result{State: state, Phase: code, Message: message, RecoveryRequired: uncertain})
	}
	c, err := e.Source.Read()
	if err != nil {
		fail(err, false)
		return
	}
	inv, pv, err := e.preflight(ctx, op.Plan.Draft, c)
	if err != nil {
		fail(err, false)
		return
	}
	if err = domain.ValidateDraft(op.Plan.Draft, c, inv); err != nil {
		fail(err, false)
		return
	}
	if !reflect.DeepEqual(pv.Preconditions, op.Plan.Preview.Preconditions) {
		fail(domain.Fail("precondition_drift", "target, artifact or hardware evidence changed; create a new plan"), false)
		return
	}
	if ctx.Err() != nil {
		_ = e.finish(op.ID, domain.Result{State: "cancelled", Phase: "cancelled-before-effects", Message: "no external effect dispatched"})
		return
	}
	if op.Plan.Draft.Action == "operation.reconcile" {
		_ = e.finish(op.ID, domain.Result{State: "succeeded", Phase: "operation-reconciled", Message: "source and executor evidence inspected; previous failures retained; no external dispatch"})
		return
	}
	if op.Plan.Desired.Revision != c.Revision {
		if err = e.progress(op.ID, domain.Progress{Phase: "source-update-intent", Message: "about to replace canonical managed source"}); err != nil {
			return
		}
		if err = e.Source.Update(c.Revision, op.Plan.Desired); err != nil {
			var f *domain.Failure
			uncertain := !errors.As(err, &f)
			fail(err, uncertain)
			return
		}
		if err = e.DB.Update(func(s *store.State) error {
			o := s.Operations[op.ID]
			o.SourceUpdated = true
			o.Phase = "source-updated"
			o.Revision++
			o.UpdatedAt = time.Now().UTC()
			s.Operations[o.ID] = o
			return nil
		}); err != nil {
			return
		}
	}
	if err = e.progress(op.ID, domain.Progress{Phase: "dispatch-intent", Message: "executor request authorized by validated plan; external outcome not yet known"}); err != nil {
		return
	}
	r, err := e.Adapter.Execute(ctx, domain.Execution{ID: op.ID, Actor: op.Actor, Plan: op.Plan}, func(p domain.Progress) error { return e.progress(op.ID, p) })
	if err != nil {
		if r.State != "" && domain.Terminal(r.State) {
			_ = e.finish(op.ID, r)
		} else {
			fail(err, true)
		}
		return
	}
	if !domain.Terminal(r.State) {
		fail(domain.Fail("uncertain", "executor returned without a proven terminal result"), true)
		return
	}
	_ = e.finish(op.ID, r)
}
func (e *Engine) finish(id string, r domain.Result) error {
	return e.DB.Update(func(s *store.State) error {
		o, ok := s.Operations[id]
		if !ok {
			return fmt.Errorf("operation missing")
		}
		if domain.MemoryAction(o.Plan.Draft.Action) && (r.RecoveryRequired || r.State == "recovery-required" || r.State == "running" || r.State == "queued") {
			r = domain.Result{State: "failed", Phase: "read-only-interrupted", Message: "Memory evidence/export outcome unavailable. Inspect the independent helper operation; retained evidence is not validated and no GPU recovery or workload mutation was authorized."}
		}
		o.State = r.State
		o.Phase = r.Phase
		o.Message = r.Message
		o.RecoveryRequired = r.RecoveryRequired || r.State == "recovery-required"
		o.Artifacts = r.Artifacts
		o.UpdatedAt = time.Now().UTC()
		o.Revision++
		a := o.Plan.Draft.Action
		o.LiveApplied = r.State == "succeeded" && (a == "serving.configure" || a == "resources.configure" || a == "serving.start" || a == "serving.stop" || a == "serving.restart" || a == "profile.switch" || a == "profile.restore")
		s.Operations[id] = o
		store.Event(s, o.Actor, "operation.complete", id, o.State)
		if o.State == "succeeded" && o.Plan.Draft.RecoveryID != "" {
			chain, err := domain.RecoveryChain(operationLinks(s.Operations), o.Plan.Draft.RecoveryID)
			if err != nil {
				return err
			}
			for previous := range chain {
				old := s.Operations[previous]
				if previous == id || !old.RecoveryRequired {
					continue
				}
				old.RecoveryRequired = false
				old.State = "failed"
				old.Phase = "resolved-by-explicit-restore"
				if a == "operation.reconcile" {
					old.Phase = "resolved-by-operation-inspection"
					old.SourceUpdated = o.Plan.Desired.Revision == old.Plan.Desired.Revision && old.Plan.Desired.Revision != old.Plan.Draft.SourceRevision
				}
				old.Message = "resolved by operation " + id
				old.UpdatedAt = time.Now().UTC()
				old.Revision++
				s.Operations[old.ID] = old
			}
		}
		return nil
	})
}
