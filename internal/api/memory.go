package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/advisor"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func (s *Server) memoryEvidence(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.fail(w, domain.Fail("invalid", "memory evidence accepts no query"))
		return
	}
	out := []domain.MemorySummary{}
	for _, o := range s.Engine.Operations() {
		if !domain.MemoryAction(o.Plan.Draft.Action) {
			continue
		}
		found := false
		for _, a := range o.Artifacts {
			if a.Name == "memory-summary.json" {
				var v domain.MemorySummary
				if config.Decode([]byte(a.Content), &v) == nil {
					out = append(out, v)
					found = true
				}
			}
		}
		if !found {
			digest := ""
			if o.Plan.Draft.Memory != nil {
				digest = o.Plan.Draft.Memory.EvidenceSHA256
			}
			out = append(out, domain.MemorySummary{EvidenceID: o.ID, Status: "incomplete", Reason: "operation_" + o.State, SHA256: digest})
		}
	}
	s.json(w, 200, out)
}
func (s *Server) memoryPreview(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var d domain.Draft
	if !s.read(w, r, &d) {
		return
	}
	c, e := s.Engine.Source.Read()
	if e != nil {
		s.fail(w, e)
		return
	}
	if !domain.MemoryAction(d.Action) {
		s.fail(w, domain.Fail("invalid", "memory preview requires a memory operation"))
		return
	}
	if e = domain.ValidateDraft(d, c, domain.Inventory{Target: s.Engine.Target}); e != nil {
		s.fail(w, e)
		return
	}
	a, ok := s.Engine.Adapter.(domain.MemoryAdapter)
	if !ok {
		s.fail(w, domain.Fail("unavailable", "memory adapter unavailable"))
		return
	}
	v, e := a.MemoryPreview(r.Context(), d, c)
	if e != nil {
		s.fail(w, domain.Fail("invalid", e.Error()))
		return
	}
	s.json(w, 200, v)
}
func (s *Server) memoryArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var request domain.MemoryArtifactRequest
	if !s.read(w, r, &request) {
		return
	}
	o, e := s.Engine.Operation(request.OperationID)
	if e != nil {
		s.fail(w, e)
		return
	}
	if o.Plan.Draft.Action != "memory.plan.export" || o.State != "succeeded" {
		s.fail(w, domain.Fail("conflict", "private memory export requires a completed plan operation"))
		return
	}
	a, ok := s.Engine.Adapter.(domain.MemoryAdapter)
	if !ok {
		s.fail(w, domain.Fail("unavailable", "memory artifact adapter unavailable"))
		return
	}
	artifact, e := a.MemoryArtifact(r.Context(), request.OperationID, request.Name)
	if e != nil {
		s.fail(w, domain.Fail("conflict", e.Error()))
		return
	}
	s.json(w, 200, artifact)
}

type MemoryAdvice struct {
	Advisory advisor.Result `json:"advisory"`
	Plan     *domain.Plan   `json:"plan,omitempty"`
}

func (s *Server) memoryInspect(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var req domain.MemoryOperationRequest
	if !s.read(w, r, &req) {
		return
	}
	o, e := s.Engine.InspectMemory(r.Context(), actor(r), req.OperationID)
	if e != nil {
		s.fail(w, e)
		return
	}
	s.json(w, 200, o)
}

// Cloud data is a new allowlisted numeric view, not a serialization of evidence,
// operations, policy, raw errors or even the complete sanitized UI response.
func cloudMemory(s domain.MemorySummary) any {
	status := "unavailable"
	switch s.Status {
	case "incomplete", "imported-unqualified", "ready-for-plan", "plan-only-unqualified", "demo-only":
		status = s.Status
	}
	type window struct {
		Phase   string              `json:"phase"`
		Startup domain.MemoryWindow `json:"startup"`
		Steady  domain.MemoryWindow `json:"steady"`
	}
	windows := []window{}
	for _, o := range s.Observations {
		if o.Phase == "cold" || o.Phase == "warm" {
			windows = append(windows, window{o.Phase, o.Startup, o.Steady})
		}
		if len(windows) == 20 {
			break
		}
	}
	return struct {
		Status    string   `json:"status"`
		Requested int64    `json:"requested_mib"`
		Limited   int64    `json:"limited_mib"`
		SHM       int64    `json:"shm_ceiling_mib"`
		Other     int64    `json:"other_mib"`
		Candidate int64    `json:"candidate_mib"`
		Envelope  int64    `json:"envelope_bytes"`
		Headroom  int64    `json:"headroom_bytes"`
		Cold      int      `json:"cold_observations"`
		Warm      int      `json:"warm_observations"`
		Windows   []window `json:"windows"`
	}{status, s.BaselineMiB, s.LimitedMiB, s.SharedMemoryMiB, s.OtherMiB, s.CandidateMiB, s.EnvelopeBytes, s.HeadroomBytes, s.Cold, s.Warm, windows}
}
func (s *Server) memoryAdvice(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var request domain.MemoryRequest
	if !s.read(w, r, &request) {
		return
	}
	if s.Advisor == nil || !s.Config.Advisor.Enabled || s.Config.Mode != "live" {
		s.fail(w, domain.Fail("unavailable", "optional Agents API advisor is disabled; deterministic memory planning remains available"))
		return
	}
	c, e := s.Engine.Source.Read()
	if e != nil {
		s.fail(w, e)
		return
	}
	d := domain.Draft{Action: "memory.plan.export", Target: s.Engine.Target, SourceRevision: c.Revision, Memory: &request}
	if e = domain.ValidateDraft(d, c, domain.Inventory{Target: s.Engine.Target}); e != nil {
		s.fail(w, e)
		return
	}
	a, ok := s.Engine.Adapter.(domain.MemoryAdapter)
	if !ok {
		s.fail(w, domain.Fail("unavailable", "memory adapter unavailable"))
		return
	}
	summary, e := a.MemoryPreview(r.Context(), d, c)
	if e != nil {
		s.fail(w, domain.Fail("invalid", "selected evidence is unavailable, incomplete or stale; use deterministic preview"))
		return
	}
	var plan *domain.Plan
	// Record intent before any potentially paid request. A crash or failed final
	// journal write leaves an explicit unknown outcome, not an invitation to retry.
	attemptID := store.ID()
	if e = s.Engine.DB.Update(func(v *store.State) error {
		store.Event(v, actor(r).ID, "memory.advice.requested", attemptID, "outcome_not_observed")
		return nil
	}); e != nil {
		s.fail(w, e)
		return
	}
	w.Header().Set("X-Bridge-Advisory-ID", attemptID)
	result, e := s.Advisor.Advise(r.Context(), func(ctx context.Context, name string) (json.RawMessage, error) {
		identity := actor(r)
		state := s.Engine.DB.AuthState()
		var current auth.Actor
		var authErr error
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			current, authErr = auth.Bearer(state, strings.TrimPrefix(header, "Bearer "))
		} else if cookie, err := r.Cookie(s.Config.BrowserSessionCookieName()); err == nil {
			current, _, authErr = auth.Session(state, cookie.Value, s.Config.BrowserSessionPurpose())
		} else {
			authErr = err
		}
		if authErr != nil || current.ID != identity.ID || current.Role != "owner" {
			return nil, domain.Fail("forbidden", "owner session expired or revoked")
		}
		switch name {
		case advisor.ReadMemoryEvidence:
			return json.Marshal(cloudMemory(summary))
		case advisor.ExplainMemoryCapacity:
			inv, err := s.Engine.Snapshot(ctx)
			if err != nil {
				return nil, domain.Fail("unavailable", "capacity evidence unavailable")
			}
			h := inv.Hardware
			return json.Marshal(struct {
				TotalMiB, HostReserveMiB, KubeReserveMiB, OtherMiB, RequestedMiB, SHMCeilingMiB int64
				DIMMs                                                                           int
				TopologyKnown                                                                   bool
			}{h.MemoryMiB, h.HostReserveMiB, h.KubeReserveMiB, request.OtherMiB, c.Resources.MemoryMiB, c.Resources.SharedMemoryMiB, h.DIMMs, h.TopologyKnown})
		case advisor.RequestMemoryPlan:
			if plan == nil {
				p, err := s.Engine.CreatePlan(ctx, actor(r), d)
				if err != nil {
					return nil, domain.Fail("invalid", "deterministic planning refused; owner must inspect evidence")
				}
				plan = &p
			}
			return json.Marshal(map[string]string{"plan_id": plan.ID, "status": "owner_approval_required_export_only"})
		default:
			return nil, domain.Fail("forbidden", "tool is not allowed")
		}
	})
	// Audit only fixed accounting metadata. In particular, do not persist a
	// provider's explanation or error text, even when an advisory fails.
	outcome := "completed"
	if e != nil {
		outcome = "failed_or_incomplete"
	}
	accounting, marshalErr := json.Marshal(struct {
		Outcome               string         `json:"outcome"`
		SessionID             string         `json:"session_id,omitempty"`
		Usage                 *advisor.Usage `json:"usage"`
		UsageProvisional      bool           `json:"usage_provisional"`
		UsageSource           string         `json:"usage_source,omitempty"`
		CreationUncertain     bool           `json:"creation_uncertain"`
		CancellationRequested bool           `json:"cancellation_requested"`
		CancellationAccepted  bool           `json:"cancellation_accepted"`
	}{outcome, result.SessionID, result.Usage, result.UsageProvisional, result.UsageSource, result.CreationUncertain, result.CancellationRequested, result.CancellationAccepted})
	if marshalErr != nil {
		s.fail(w, domain.Fail("internal", "advisory accounting unavailable; inspect the requested audit record before any new advisory"))
		return
	}
	if recordErr := s.Engine.DB.Update(func(v *store.State) error {
		store.Event(v, actor(r).ID, "memory.advice.finished", attemptID, string(accounting))
		return nil
	}); recordErr != nil {
		s.fail(w, recordErr)
		return
	}
	if e != nil {
		s.fail(w, domain.Fail("unavailable", "Agents API advisory unavailable or bounded run ended; inspect audit attempt "+attemptID+" and retained plans before a new paid request. Unknown creation may already have started inference; cancellation acceptance is not proof of completion. Deterministic workflow remains available"))
		return
	}
	s.json(w, 200, MemoryAdvice{Advisory: result, Plan: plan})
}
