package engine

import (
	"context"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// InspectMemory only queries the original executor; it never redispatches work
// and cannot settle unrelated GPU recovery records.
func (e *Engine) InspectMemory(ctx context.Context, a auth.Actor, id string) (domain.Operation, error) {
	return e.inspectEvidence(ctx, a, id, false)
}

// InspectPerformance reconciles only the original read-only analysis executor.
func (e *Engine) InspectPerformance(ctx context.Context, a auth.Actor, id string) (domain.Operation, error) {
	return e.inspectEvidence(ctx, a, id, true)
}
func (e *Engine) inspectEvidence(ctx context.Context, a auth.Actor, id string, performance bool) (domain.Operation, error) {
	if a.Role != "owner" {
		return domain.Operation{}, domain.Fail("forbidden", "evidence inspection requires owner")
	}
	o, err := e.Operation(id)
	if err != nil {
		return o, err
	}
	if (!performance && !domain.MemoryAction(o.Plan.Draft.Action)) || (performance && !domain.PerformanceAction(o.Plan.Draft.Action)) {
		return o, domain.Fail("invalid", "only the matching evidence operation can use read-only inspection")
	}
	if !o.Dispatched {
		return o, nil
	}
	result, err := e.inspect(ctx, o)
	if err != nil {
		return o, domain.Fail("unavailable", "independent evidence executor outcome unavailable; evidence retained")
	}
	if result.State == "running" || result.State == "queued" {
		return o, domain.Fail("conflict", "helper still generating evidence; do not redispatch")
	}
	if !domain.Terminal(result.State) {
		return o, domain.Fail("unavailable", "evidence executor result incomplete")
	}
	if err = e.finish(id, result); err != nil {
		return o, err
	}
	return e.Operation(id)
}
