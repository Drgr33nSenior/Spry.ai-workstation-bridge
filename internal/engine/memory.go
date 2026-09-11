package engine

import (
	"context"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// InspectMemory only queries the original executor; it never redispatches work
// and cannot settle unrelated GPU recovery records.
func (e *Engine) InspectMemory(ctx context.Context, a auth.Actor, id string) (domain.Operation, error) {
	if a.Role != "owner" {
		return domain.Operation{}, domain.Fail("forbidden", "memory inspection requires owner")
	}
	o, err := e.Operation(id)
	if err != nil {
		return o, err
	}
	if !domain.MemoryAction(o.Plan.Draft.Action) {
		return o, domain.Fail("invalid", "only memory evidence operations can use read-only inspection")
	}
	if !o.Dispatched {
		return o, nil
	}
	result, err := e.inspect(ctx, o)
	if err != nil {
		return o, domain.Fail("unavailable", "independent memory executor outcome unavailable; evidence retained")
	}
	if result.State == "running" || result.State == "queued" {
		return o, domain.Fail("conflict", "helper still generating evidence; do not redispatch")
	}
	if !domain.Terminal(result.State) {
		return o, domain.Fail("unavailable", "memory executor result incomplete")
	}
	if err = e.finish(id, result); err != nil {
		return o, err
	}
	return e.Operation(id)
}
