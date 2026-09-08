package engine

import (
	"context"
	"sort"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// An inspector receives the original immutable execution, not an API-selected
// executor or path. Live adapters bind it to their dispatch hash and journal.
type executionInspector interface {
	InspectExecution(context.Context, domain.Execution) (domain.Result, error)
}

func (e *Engine) inspect(ctx context.Context, o domain.Operation) (domain.Result, error) {
	if !o.Dispatched {
		return domain.Result{State: "recovery-required", RecoveryRequired: true, Phase: "source-inspection-required"}, nil
	}
	if a, ok := e.Adapter.(executionInspector); ok {
		return a.InspectExecution(ctx, domain.Execution{ID: o.ID, Actor: o.Actor, Plan: o.Plan})
	}
	return e.Adapter.Inspect(ctx, o.ID)
}

func operationLinks(ops map[string]domain.Operation) map[string]domain.RecoveryLink {
	links := map[string]domain.RecoveryLink{}
	for id, o := range ops {
		links[id] = domain.RecoveryLink{Parent: o.Plan.Draft.RecoveryID, Target: o.Plan.Draft.Target, Action: o.Plan.Draft.Action}
	}
	return links
}

func recoveryAdmission(ops map[string]domain.Operation, d domain.Draft, executing string) error {
	chain := map[string]bool{}
	if d.RecoveryID != "" {
		old, ok := ops[d.RecoveryID]
		if !ok || !old.RecoveryRequired || (d.Action != "profile.restore" && d.Action != "operation.reconcile") {
			return domain.Fail("conflict", "reference must identify an unresolved recovery operation")
		}
		if old.Plan.Draft.Target != d.Target {
			return domain.Fail("target_mismatch", "recovery target differs from the persisted operation")
		}
		var err error
		chain, err = domain.RecoveryChain(operationLinks(ops), old.ID)
		if err != nil {
			return domain.Fail("recovery_required", err.Error())
		}
	}
	for id, o := range ops {
		if id == executing {
			continue
		}
		if o.RecoveryRequired && !chain[id] && d.Action != "hardware.refresh" {
			return domain.Fail("recovery_required", "inspect and resolve the unrelated outstanding operation before new mutations")
		}
		if d.RecoveryID != "" && !domain.Terminal(o.State) && o.Plan.Draft.RecoveryID != "" && chain[id] {
			return domain.Fail("conflict", "another recovery attempt for this chain is queued or running")
		}
	}
	return nil
}

func (e *Engine) recoveryPreflight(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.Preview, error) {
	ops := e.DB.View().Operations
	old, ok := ops[d.RecoveryID]
	if !ok || !old.RecoveryRequired {
		return domain.Preview{}, domain.Fail("conflict", "reference must identify an unresolved recovery operation")
	}
	links := operationLinks(ops)
	chain, err := domain.RecoveryChain(links, old.ID)
	if err != nil {
		return domain.Preview{}, domain.Fail("recovery_required", err.Error())
	}
	rootID, _ := domain.RecoveryRoot(links, old.ID)
	root := ops[rootID]
	if root.Plan.Draft.Target != d.Target || d.Target != e.Target {
		return domain.Preview{}, domain.Fail("target_mismatch", "recovery cannot change the persisted target")
	}
	if c.Revision != root.Plan.Desired.Revision && c.Revision != root.Plan.Draft.SourceRevision {
		return domain.Preview{}, domain.Fail("source_drift", "source differs from both sides of the interrupted update; owner review required")
	}
	hostEffects := false
	evidence := map[string]string{}
	ids := []string{}
	for id := range chain {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := ops[id]
		if !o.RecoveryRequired {
			continue
		}
		evidence[id] = o.Plan.Hash
		if !o.Dispatched {
			continue
		}
		if domain.HostRestoreRequired(o.Plan.Draft.Action) {
			hostEffects = true
		}
		r, inspectErr := e.inspect(ctx, o)
		if inspectErr == nil && (r.State == "running" || r.State == "queued" || r.State == "cancel-requested") {
			return domain.Preview{}, domain.Fail("conflict", "executor still running; recovery cannot race it")
		}
		if d.Action == "operation.reconcile" {
			if inspectErr != nil || !domain.Terminal(r.State) || r.State == "recovery-required" || r.RecoveryRequired {
				return domain.Preview{}, domain.Fail("recovery_required", "executor-specific evidence is unavailable or uncertain; inspect the model receipt or worker/helper journal; no restore or redispatch was attempted")
			}
			evidence[id] = domain.Hash(r)
		}
	}
	if d.Action == "profile.restore" && !hostEffects {
		return domain.Preview{}, domain.Fail("conflict", "this chain has no dispatched host effects; use operation-specific reconciliation")
	}
	return domain.Preview{Preconditions: map[string]string{"recovery_id": old.ID, "recovery_root": rootID, "recovery_evidence": domain.Hash(evidence), "source_revision": c.Revision}}, nil
}
