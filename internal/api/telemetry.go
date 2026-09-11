package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseStatus) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseStatus) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) observe(pattern string, next http.HandlerFunc) http.HandlerFunc {
	// pattern is the fixed route registration, never URL.Path, query or PathValue.
	_, route, _ := strings.Cut(pattern, " ")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, finish := s.Engine.Telemetry.HTTP(r.Context(), r.Method, route)
		status := &responseStatus{ResponseWriter: w}
		defer func() {
			if status.status == 0 {
				status.status = 200
			}
			finish(status.status)
		}()
		next(status, r.WithContext(ctx))
	}
}

func (s *Server) telemetrySummary(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.fail(w, domain.Fail("invalid", "telemetry summary accepts no query parameters"))
		return
	}
	metrics, traces := s.Engine.Telemetry.Status()
	operations, healthy := s.Engine.DB.TelemetrySnapshot()
	out := domain.TelemetrySummary{ObservedAt: time.Now().UTC(), Mode: s.Config.Mode, MutationStorageAvailable: healthy, Operations: operations, MetricsExport: metrics, TracesExport: traces}
	out.Backend, out.Values = s.prometheus.Summary(r.Context())
	s.json(w, http.StatusOK, out)
}
