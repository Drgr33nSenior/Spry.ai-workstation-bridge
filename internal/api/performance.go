package api

import (
	"net/http"
	"regexp"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// PerformanceArtifactRequest keeps private report retrieval distinct from the
// public operation metadata. The client can never supply a filesystem path.
type PerformanceArtifactRequest struct {
	OperationID string `json:"operation_id"`
	Name        string `json:"name"`
}
type PerformanceOperationRequest struct {
	OperationID string `json:"operation_id"`
}

var performanceOperationID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Server) performancePreview(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var d domain.Draft
	if !s.read(w, r, &d) {
		return
	}
	c, err := s.Engine.Source.Read()
	if err != nil {
		s.fail(w, err)
		return
	}
	if !domain.PerformanceAction(d.Action) {
		s.fail(w, domain.Fail("invalid", "performance preview requires performance.export or performance.profile.select"))
		return
	}
	if err = domain.ValidateDraft(d, c, domain.Inventory{Target: s.Engine.Target}); err != nil {
		s.fail(w, err)
		return
	}
	a, ok := s.Engine.Adapter.(domain.PerformanceAdapter)
	if !ok {
		s.fail(w, domain.Fail("unavailable", "performance adapter unavailable"))
		return
	}
	summary, err := a.PerformancePreview(r.Context(), d, c)
	if err != nil {
		s.fail(w, domain.Fail("invalid", err.Error()))
		return
	}
	// The adapter verifies the sealed manifest; bind the request identity again
	// before returning a summary to an API client.
	if summary.Kind != d.Performance.Kind || summary.SHA256 != d.Performance.EvidenceSHA256 {
		s.fail(w, domain.Fail("conflict", "performance preview identity changed"))
		return
	}
	summary.EvidenceID = d.Performance.EvidenceID
	s.json(w, http.StatusOK, summary)
}

func (s *Server) performanceArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var request PerformanceArtifactRequest
	if !s.read(w, r, &request) {
		return
	}
	if !performanceOperationID.MatchString(request.OperationID) || request.Name == "" || len(request.Name) > 240 {
		s.fail(w, domain.Fail("invalid", "performance artifact requires an operation ID and exact artifact name"))
		return
	}
	o, err := s.Engine.Operation(request.OperationID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !domain.PerformanceAction(o.Plan.Draft.Action) || o.State != "succeeded" {
		s.fail(w, domain.Fail("conflict", "private performance artifact requires a completed performance operation"))
		return
	}
	var expected *domain.Artifact
	for i := range o.Artifacts {
		if o.Artifacts[i].Name == request.Name {
			expected = &o.Artifacts[i]
			break
		}
	}
	if expected == nil {
		s.fail(w, domain.Fail("not_found", "performance artifact is not recorded by this operation"))
		return
	}
	a, ok := s.Engine.Adapter.(domain.PerformanceAdapter)
	if !ok {
		s.fail(w, domain.Fail("unavailable", "performance adapter unavailable"))
		return
	}
	artifact, err := a.PerformanceArtifact(r.Context(), request.OperationID, request.Name)
	if err != nil {
		s.fail(w, domain.Fail("conflict", err.Error()))
		return
	}
	if artifact.Name != expected.Name || artifact.SHA256 != expected.SHA256 || artifact.Size != expected.Size || artifact.SourceRevision != expected.SourceRevision || artifact.Qualification != expected.Qualification {
		s.fail(w, domain.Fail("conflict", "private performance artifact differs from the recorded operation"))
		return
	}
	s.json(w, http.StatusOK, artifact)
}

func (s *Server) performanceInspect(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var request PerformanceOperationRequest
	if !s.read(w, r, &request) {
		return
	}
	if !performanceOperationID.MatchString(request.OperationID) {
		s.fail(w, domain.Fail("invalid", "performance inspection requires an operation ID"))
		return
	}
	operation, err := s.Engine.InspectPerformance(r.Context(), actor(r), request.OperationID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.json(w, http.StatusOK, operation)
}
