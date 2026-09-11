package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/advisor"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/telemetry"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/web"
)

type actorKey struct{}
type attempt struct {
	n     int
	start time.Time
}
type Server struct {
	Advisor    *advisor.Client
	Config     config.Config
	Engine     *engine.Engine
	Logger     *slog.Logger
	mu         sync.Mutex
	attempts   map[string]attempt
	global     attempt
	handler    http.Handler
	prometheus *telemetry.Prometheus
}

func New(c config.Config, e *engine.Engine, logger *slog.Logger) *Server {
	s := &Server{Config: c, Engine: e, Logger: logger, attempts: map[string]attempt{}, prometheus: telemetry.NewPrometheus(c.Telemetry.PrometheusURL)}
	mux := http.NewServeMux()
	mux.Handle("/", s.browser(web.Handler()))
	mux.HandleFunc("GET /health/live", s.observe("GET /health/live", s.live))
	mux.HandleFunc("POST /api/v1/auth/login", s.observe("POST /api/v1/auth/login", s.login))
	secure := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.observe(pattern, s.authenticate(h).ServeHTTP))
	}
	secure("GET /api/v1/auth/session", s.session)
	secure("POST /api/v1/auth/logout", s.logout)
	secure("GET /api/v1/status", s.inventory)
	secure("GET /api/v1/telemetry/summary", s.telemetrySummary)
	secure("GET /api/v1/memory", s.memoryEvidence)
	secure("POST /api/v1/memory/preview", s.memoryPreview)
	secure("POST /api/v1/memory/artifact", s.memoryArtifact)
	secure("POST /api/v1/memory/advice", s.memoryAdvice)
	secure("POST /api/v1/memory/inspect", s.memoryInspect)
	secure("GET /api/v1/models", s.inventory)
	secure("GET /api/v1/resources", s.inventory)
	secure("GET /api/v1/builds", s.inventory)
	secure("GET /api/v1/caches", s.inventory)
	secure("GET /api/v1/harnesses", s.inventory)
	secure("GET /api/v1/profiles", s.profiles)
	secure("GET /api/v1/config", s.configuration)
	secure("GET /api/v1/config/export", s.configuration)
	secure("POST /api/v1/plans", s.plan)
	secure("GET /api/v1/plans/{id}", s.getPlan)
	secure("POST /api/v1/operations", s.apply)
	secure("GET /api/v1/operations", s.operations)
	secure("GET /api/v1/operations/{id}", s.operations)
	secure("POST /api/v1/operations/{id}/cancel", s.cancel)
	secure("POST /api/v1/operations/{id}/recover", s.recover)
	secure("GET /api/v1/harnesses/{id}/export", s.export)
	secure("GET /api/v1/credentials", s.credentials)
	secure("POST /api/v1/credentials/{id}/revoke", s.revoke)
	secure("GET /api/v1/audit", s.audit)
	s.handler = s.guard(mux)
	return s
}
func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) browser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Config.BrowserSessionsEnabled() {
			s.fail(w, domain.Fail("browser_disabled", "live browser management is disabled; use a scoped CLI credential or configure a dedicated trusted HTTPS management identity"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.Config.Mode == "live" && strings.HasPrefix(s.Config.ExternalURL, "https:") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		allowed := false
		for _, h := range s.Config.AllowedHosts {
			if strings.EqualFold(h, r.Host) {
				allowed = true
			}
		}
		if !allowed {
			s.fail(w, domain.Fail("invalid_host", "Host is not in administrator policy"))
			return
		}
		if r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.Config.ExternalURL {
			s.fail(w, domain.Fail("forbidden", "cross-origin requests are forbidden"))
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
			s.fail(w, domain.Fail("forbidden", "cross-origin browser requests are forbidden"))
			return
		}
		if r.Method == "OPTIONS" {
			s.fail(w, domain.Fail("forbidden", "cross-origin access is disabled"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) limited(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if now.Sub(s.global.start) > time.Minute {
		s.global = attempt{start: now}
	}
	s.global.n++
	if s.global.n > 120 {
		return true
	}
	a := s.attempts[ip]
	if now.Sub(a.start) > time.Minute {
		a = attempt{start: now}
	}
	a.n++
	if len(s.attempts) >= 256 {
		for k, v := range s.attempts {
			if now.Sub(v.start) > time.Minute {
				delete(s.attempts, k)
			}
		}
		if _, ok := s.attempts[ip]; !ok && len(s.attempts) >= 256 {
			return true
		}
	}
	s.attempts[ip] = a
	return a.n > 12
}
func (s *Server) authenticate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a auth.Actor
		var err error
		v := s.Engine.DB.AuthState()
		header := r.Header.Get("Authorization")
		if header != "" {
			if !strings.HasPrefix(header, "Bearer ") {
				err = domain.Fail("unauthorized", "Bearer authentication required")
			} else {
				a, err = auth.Bearer(v, strings.TrimPrefix(header, "Bearer "))
			}
		}
		if header == "" {
			if !s.Config.BrowserSessionsEnabled() {
				err = domain.Fail("unauthorized", "sign in with a scoped CLI credential")
			} else {
				c, e := r.Cookie(s.Config.BrowserSessionCookieName())
				if e != nil {
					err = domain.Fail("unauthorized", "sign in or use a scoped credential")
				} else {
					var session store.Session
					a, session, err = auth.Session(v, c.Value, s.Config.BrowserSessionPurpose())
					if err == nil && r.Method != "GET" && r.Method != "HEAD" {
						csrf := r.Header.Get("X-CSRF-Token")
						if r.Header.Get("Origin") != s.Config.ExternalURL || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRF)) != 1 {
							err = domain.Fail("forbidden", "valid same-origin CSRF token required")
						}
					}
				}
			}
		}
		if err != nil {
			if s.limited(r) {
				w.Header().Set("Retry-After", "60")
				s.fail(w, domain.Fail("rate_limited", "authentication attempts limited; retry after one minute"))
				return
			}
			s.fail(w, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func actor(r *http.Request) auth.Actor { return r.Context().Value(actorKey{}).(auth.Actor) }
func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	s.json(w, 200, map[string]any{"alive": true, "mode": s.Config.Mode, "mutation_storage_available": s.Engine.DB.Healthy()})
}
func (s *Server) read(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		s.fail(w, domain.Fail("invalid", "Content-Type application/json required"))
		return false
	}
	b, e := io.ReadAll(r.Body)
	if e != nil {
		s.fail(w, domain.Fail("too_large", "request body exceeds the allowed bound"))
		return false
	}
	if e = config.Decode(b, v); e != nil {
		s.fail(w, domain.Fail("invalid", e.Error()))
		return false
	}
	return true
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.Config.BrowserSessionsEnabled() {
		s.fail(w, domain.Fail("browser_disabled", "live browser management requires an explicitly configured dedicated trusted HTTPS management identity"))
		return
	}
	if r.Header.Get("Origin") != s.Config.ExternalURL {
		s.fail(w, domain.Fail("forbidden", "browser login requires the configured same origin"))
		return
	}
	if s.limited(r) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, domain.Fail("rate_limited", "authentication attempts limited; retry after one minute"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Credential string `json:"credential"`
	}
	if !s.read(w, r, &input) {
		return
	}
	a, token, session, e := auth.Login(s.Engine.DB, input.Credential, s.Config.BrowserSessionPurpose())
	input.Credential = ""
	if e != nil {
		s.fail(w, e)
		return
	}
	http.SetCookie(w, s.sessionCookie(token, session.ExpiresAt))
	s.json(w, 200, map[string]any{"actor": a, "csrf": session.CSRF, "expires_at": session.ExpiresAt, "mode": s.Config.Mode})
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	var csrf string
	var expires time.Time
	if c, e := r.Cookie(s.Config.BrowserSessionCookieName()); e == nil {
		_, session, err := auth.Session(s.Engine.DB.AuthState(), c.Value, s.Config.BrowserSessionPurpose())
		if err == nil {
			csrf = session.CSRF
			expires = session.ExpiresAt
		}
	}
	s.json(w, 200, map[string]any{"actor": actor(r), "csrf": csrf, "expires_at": expires, "mode": s.Config.Mode})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie(s.Config.BrowserSessionCookieName()); e == nil {
		if err := s.Engine.DB.Update(func(v *store.State) error {
			delete(v.Sessions, auth.Verifier(c.Value))
			store.Event(v, actor(r).ID, "logout", actor(r).ID, "succeeded")
			return nil
		}); err != nil {
			s.fail(w, err)
			return
		}
	}
	http.SetCookie(w, s.expireSessionCookie(s.Config.BrowserSessionCookieName()))
	// The old name was used before live browser sessions were restricted to a
	// dedicated HTTPS identity. It is not accepted, and this best-effort expiry
	// removes it from a client that still presents it.
	http.SetCookie(w, s.expireSessionCookie(config.LegacyBrowserSessionCookie))
	s.json(w, 200, map[string]string{"state": "logged-out"})
}

func (s *Server) sessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: s.Config.BrowserSessionCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.ExternalURL, "https:"), SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())}
}
func (s *Server) expireSessionCookie(name string) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(s.Config.ExternalURL, "https:"), SameSite: http.SameSiteStrictMode}
}
func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	inv, e := s.Engine.Snapshot(r.Context())
	if e != nil {
		s.fail(w, e)
		return
	}
	var out any = inv
	switch r.URL.Path {
	case "/api/v1/models":
		out = inv.Models
	case "/api/v1/resources":
		out = inv.Hardware
	case "/api/v1/builds":
		out = inv.Recipes
	case "/api/v1/caches":
		out = inv.Caches
	case "/api/v1/harnesses":
		out = inv.Harnesses
	}
	s.json(w, 200, out)
}
func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	inv, err := s.Engine.Snapshot(r.Context())
	if err != nil {
		s.fail(w, domain.Fail("unavailable", "profile inventory is unavailable; inspect reference and helper dependencies"))
		return
	}
	reason := "dependency checks passed; exact qualification and mutable handover gates are checked during execution"
	if !inv.ClusterAvailable {
		reason = inv.ClusterMessage
	}
	s.json(w, 200, []map[string]any{{"id": "ai", "description": "Restore the qualified AI deployment; gaming must release its GPUs.", "available": inv.ClusterAvailable, "reason": reason}, {"id": "gaming", "description": "Unload all AI before the configured Sunshine or Steam Remote Play session.", "available": inv.ClusterAvailable, "reason": reason}, {"id": "maintenance", "description": "Stop managed GPU workloads and inhibit new cooperative builds.", "available": inv.ClusterAvailable, "reason": reason}})
}
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	c, e := s.Engine.Source.Read()
	if e != nil {
		s.fail(w, e)
		return
	}
	w.Header().Set("ETag", `"`+c.Revision+`"`)
	if strings.HasSuffix(r.URL.Path, "/export") {
		w.Header().Set("Content-Disposition", `attachment; filename="managed-source.json"`)
	}
	s.json(w, 200, c)
}
func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var d domain.Draft
	if !s.read(w, r, &d) {
		return
	}
	p, e := s.Engine.CreatePlan(r.Context(), actor(r), d)
	if e != nil {
		s.fail(w, e)
		return
	}
	s.json(w, 201, p)
}
func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	p, ok := s.Engine.DB.View().Plans[r.PathValue("id")]
	if !ok {
		s.fail(w, domain.Fail("not_found", "plan not found"))
		return
	}
	if domain.MemoryAction(p.Draft.Action) && !s.owner(w, r) {
		return
	}
	s.json(w, 200, p)
}
func (s *Server) apply(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlanID string `json:"plan_id"`
		Target string `json:"target"`
	}
	if !s.read(w, r, &input) {
		return
	}
	op, e := s.Engine.Apply(actor(r), input.PlanID, input.Target, r.Header.Get("Idempotency-Key"))
	if e != nil {
		s.fail(w, e)
		return
	}
	w.Header().Set("Location", "/api/v1/operations/"+op.ID)
	s.json(w, 202, op)
}
func (s *Server) operations(w http.ResponseWriter, r *http.Request) {
	if id := r.PathValue("id"); id != "" {
		o, e := s.Engine.Operation(id)
		if e != nil {
			s.fail(w, e)
			return
		}
		w.Header().Set("ETag", `"`+strconv.FormatUint(o.Revision, 10)+`"`)
		if domain.MemoryAction(o.Plan.Draft.Action) && !s.owner(w, r) {
			return
		}
		s.json(w, 200, o)
		return
	}
	operations := s.Engine.Operations()
	if actor(r).Role != "owner" {
		filtered := []domain.Operation{}
		for _, o := range operations {
			if !domain.MemoryAction(o.Plan.Draft.Action) {
				filtered = append(filtered, o)
			}
		}
		operations = filtered
	}
	s.json(w, 200, operations)
}
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	var empty struct{}
	if !s.read(w, r, &empty) {
		return
	}
	var rev uint64
	if match := r.Header.Get("If-Match"); match != "" {
		var err error
		rev, err = strconv.ParseUint(strings.Trim(match, `"`), 10, 64)
		if err != nil {
			s.fail(w, domain.Fail("invalid", "invalid operation revision"))
			return
		}
	}
	if rev == 0 {
		s.fail(w, domain.Fail("conflict", "If-Match with the observed operation revision is required"))
		return
	}
	o, e := s.Engine.Cancel(actor(r), r.PathValue("id"), rev)
	if e != nil {
		s.fail(w, e)
		return
	}
	s.json(w, 202, o)
}
func (s *Server) recover(w http.ResponseWriter, r *http.Request) {
	var empty struct{}
	if !s.read(w, r, &empty) {
		return
	}
	p, e := s.Engine.Recover(r.Context(), actor(r), r.PathValue("id"))
	if e != nil {
		s.fail(w, e)
		return
	}
	s.json(w, 201, p)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	b, e := s.Engine.Adapter.Export(r.Context(), r.PathValue("id"))
	if e != nil {
		s.fail(w, e)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="client-bundle.json"`)
	s.json(w, 200, b)
}
func (s *Server) owner(w http.ResponseWriter, r *http.Request) bool {
	if actor(r).Role != "owner" {
		s.fail(w, domain.Fail("forbidden", "owner role required"))
		return false
	}
	return true
}
func (s *Server) credentials(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	items := []auth.CredentialInfo{}
	for _, c := range s.Engine.DB.AuthState().Credentials {
		items = append(items, auth.Info(c))
	}
	s.json(w, 200, items)
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	var empty struct{}
	if !s.read(w, r, &empty) {
		return
	}
	id := r.PathValue("id")
	if e := s.Engine.DB.Update(func(v *store.State) error {
		c, ok := v.Credentials[id]
		if !ok {
			return domain.Fail("not_found", "credential not found")
		}
		c.Revoked = true
		v.Credentials[id] = c
		for k, se := range v.Sessions {
			if se.CredentialID == id {
				delete(v.Sessions, k)
			}
		}
		store.Event(v, actor(r).ID, "credential.revoke", id, "succeeded")
		return nil
	}); e != nil {
		s.fail(w, e)
		return
	}
	s.json(w, 200, map[string]string{"id": id, "state": "revoked"})
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	if !s.owner(w, r) {
		return
	}
	s.json(w, 200, s.Engine.DB.View().Audit)
}
func (s *Server) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) fail(w http.ResponseWriter, err error) {
	f := &domain.Failure{Code: "internal", Message: "operation failed; inspect local redacted diagnostics"}
	var known *domain.Failure
	if errors.As(err, &known) {
		f = known
	}
	status := http.StatusBadRequest
	switch f.Code {
	case "unauthorized":
		status = 401
	case "forbidden", "browser_disabled":
		status = 403
	case "not_found":
		status = 404
	case "conflict", "source_drift", "precondition_drift", "idempotency_conflict", "recovery_required", "plan_expired":
		status = 409
	case "too_large":
		status = 413
	case "capacity", "rate_limited":
		status = 429
	case "unavailable", "storage_unavailable", "source_unavailable", "stale_evidence":
		status = 503
	case "internal":
		status = 500
	}
	if s.Logger != nil {
		s.Logger.Info("request refused", "code", f.Code, "status", status)
	}
	s.json(w, status, map[string]any{"error": f})
}
