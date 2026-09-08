package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/adapters"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/api"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/source"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type fixture struct {
	db     *store.Store
	eng    *engine.Engine
	server *httptest.Server
	tokens map[string]string
	conf   domain.Configuration
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir, pathErr := filepath.EvalSymlinks(t.TempDir())
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "state"), "demo")
	if err != nil {
		t.Fatal(err)
	}
	src := source.New(filepath.Join(dir, "state", "source.json"))
	c := domain.Configuration{Serving: domain.Serving{Model: "Qwen3.5-9B", Context: 4096, Concurrency: 2, MemoryFraction: .8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 16384, GPUCount: 1}, Caches: domain.CacheBudgets{ModelsGiB: 128, CompilerGiB: 100, ShaderGiB: 16, BuildJobs: 8, BuildMemoryMiB: 32768, ScratchGiB: 64}}
	if err = src.Initialize(c); err != nil {
		t.Fatal(err)
	}
	c, _ = src.Read()
	demo, err := adapters.NewDemo(filepath.Join(dir, "executor"), "demo-workstation")
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(db, src, demo, "demo-workstation", 4, 10*time.Second)
	if err = eng.Start(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, eng: eng, tokens: map[string]string{}, conf: c}
	for _, role := range []string{"owner", "operator", "viewer"} {
		cr, token, e := auth.NewCredential(role, role, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Update(func(v *store.State) error { v.Credentials[cr.ID] = cr; return nil }); e != nil {
			t.Fatal(e)
		}
		f.tokens[role] = token
	}
	ts := httptest.NewUnstartedServer(nil)
	origin := "http://" + ts.Listener.Addr().String()
	ts.Config.Handler = api.New(config.Config{Mode: "demo", Target: "demo-workstation", AllowedHosts: []string{ts.Listener.Addr().String()}, ExternalURL: origin}, eng, nil).Handler()
	ts.Start()
	f.server = ts
	t.Cleanup(func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = eng.Close(ctx)
		_ = db.Close()
	})
	return f
}
func (f *fixture) request(t *testing.T, method, path, role string, body any, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	var b []byte
	switch v := body.(type) {
	case string:
		b = []byte(v)
	case nil:
	default:
		b, _ = json.Marshal(v)
	}
	r, e := http.NewRequest(method, f.server.URL+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	if role != "" {
		r.Header.Set("Authorization", "Bearer "+f.tokens[role])
	}
	for k, v := range headers {
		if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	resp, e := f.server.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	out, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, out, resp.Header
}
func TestAuthenticationOriginRolesAndBounds(t *testing.T) {
	f := setup(t)
	draft := domain.Draft{Action: "serving.restart", Target: "demo-workstation", SourceRevision: f.conf.Revision}
	for _, tc := range []struct {
		name, method, path, role string
		body                     any
		headers                  map[string]string
		status                   int
	}{
		{"unauthenticated", "GET", "/api/v1/config", "", nil, nil, 401},
		{"viewer denied", "POST", "/api/v1/plans", "viewer", draft, nil, 403},
		{"operator admin denied", "POST", "/api/v1/plans", "operator", draft, nil, 403},
		{"bad host", "GET", "/health/live", "", nil, map[string]string{"Host": "attacker.example"}, 400},
		{"bad origin", "GET", "/api/v1/config", "owner", nil, map[string]string{"Origin": "https://attacker.example"}, 403},
		{"bad fetch", "GET", "/api/v1/config", "owner", nil, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"no browser headers needed", "GET", "/api/v1/config", "owner", nil, nil, 200},
		{"unknown field", "POST", "/api/v1/plans", "owner", `{"action":"profile.switch","shell":"echo unsafe"}`, nil, 400},
		{"duplicate key", "POST", "/api/v1/plans", "owner", `{"action":"profile.switch","action":"build.start"}`, nil, 400},
		{"model stage cannot edit serving", "POST", "/api/v1/plans", "owner", domain.Draft{Action: "model.stage", Target: "demo-workstation", SourceRevision: f.conf.Revision, Model: "Qwen3.5-9B", Serving: &domain.Serving{Model: "arbitrary", Context: -1}}, nil, 400},
		{"model verify cannot edit serving", "POST", "/api/v1/plans", "owner", domain.Draft{Action: "model.verify", Target: "demo-workstation", SourceRevision: f.conf.Revision, Model: "Qwen3.5-9B", Serving: &domain.Serving{Model: "arbitrary", Context: -1}}, nil, 400},
		{"large body", "POST", "/api/v1/plans", "owner", strings.Repeat("x", (1<<20)+1), nil, 413},
		{"bootstrap absent", "POST", "/api/v1/admin/bootstrap", "owner", map[string]string{}, nil, 405},
		{"policy edit absent", "POST", "/api/v1/config", "owner", map[string]string{"mode": "live"}, nil, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, b, _ := f.request(t, tc.method, tc.path, tc.role, tc.body, tc.headers)
			if status != tc.status {
				t.Fatalf("status %d want %d: %s", status, tc.status, b)
			}
			for _, token := range f.tokens {
				if bytes.Contains(b, []byte(token)) {
					t.Fatal("response leaked credential")
				}
			}
		})
	}
}
func TestBrowserSessionCSRFRevocationExpiry(t *testing.T) {
	f := setup(t)
	status, b, _ := f.request(t, "POST", "/api/v1/auth/login", "", map[string]string{"credential": f.tokens["owner"]}, nil)
	if status != 403 {
		t.Fatalf("login missing Origin: %d %s", status, b)
	}
	status, b, h := f.request(t, "POST", "/api/v1/auth/login", "", map[string]string{"credential": f.tokens["owner"]}, map[string]string{"Origin": f.server.URL})
	if status != 200 {
		t.Fatalf("login %d %s", status, b)
	}
	var login struct {
		CSRF  string     `json:"csrf"`
		Actor auth.Actor `json:"actor"`
	}
	if e := json.Unmarshal(b, &login); e != nil {
		t.Fatal(e)
	}
	cookie := h.Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatal("unsafe browser cookie")
	}
	cookie = strings.Split(cookie, ";")[0]
	status, _, _ = f.request(t, "POST", "/api/v1/auth/logout", "", map[string]string{}, map[string]string{"Cookie": cookie, "Origin": f.server.URL})
	if status != 403 {
		t.Fatal("logout without CSRF accepted")
	}
	status, _, _ = f.request(t, "POST", "/api/v1/auth/logout", "", map[string]string{}, map[string]string{"Cookie": cookie, "Origin": f.server.URL, "X-CSRF-Token": login.CSRF})
	if status != 200 {
		t.Fatal("valid logout refused")
	}
	status, _, _ = f.request(t, "GET", "/api/v1/auth/session", "", nil, map[string]string{"Cookie": cookie})
	if status != 401 {
		t.Fatal("logout did not revoke session")
	}
	status, _, _ = f.request(t, "POST", "/api/v1/credentials/"+login.Actor.ID+"/revoke", "owner", map[string]string{}, nil)
	if status != 200 {
		t.Fatal("revoke failed")
	}
	status, _, _ = f.request(t, "GET", "/api/v1/config", "owner", nil, nil)
	if status != 401 {
		t.Fatal("revoked bearer accepted")
	}
	err := f.db.Update(func(v *store.State) error {
		for k, c := range v.Credentials {
			if c.Role == "viewer" {
				c.ExpiresAt = time.Now().Add(-time.Second)
				v.Credentials[k] = c
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ = f.request(t, "GET", "/api/v1/config", "viewer", nil, nil)
	if status != 401 {
		t.Fatal("expired bearer accepted")
	}
}

func TestBrowserSessionExpiry(t *testing.T) {
	f := setup(t)
	status, _, header := f.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"credential": f.tokens["owner"]}, map[string]string{"Origin": f.server.URL})
	if status != http.StatusOK {
		t.Fatalf("login %d", status)
	}
	cookie := strings.Split(header.Get("Set-Cookie"), ";")[0]
	if err := f.db.Update(func(v *store.State) error {
		for id, session := range v.Sessions {
			session.ExpiresAt = time.Now().Add(-time.Second)
			v.Sessions[id] = session
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, _, _ = f.request(t, http.MethodGet, "/api/v1/auth/session", "", nil, map[string]string{"Cookie": cookie})
	if status != http.StatusUnauthorized {
		t.Fatalf("expired browser session accepted: %d", status)
	}
}

// TestLiveBrowserSessionDoesNotCrossToPlaintextLoopback reproduces the prior
// host-scoped cookie leak and proves the replacement does not send a live
// management session to an unrelated plaintext loopback service. This does not
// claim HTTPS ports isolate cookies: every HTTPS service on the dedicated DNS
// management identity remains part of the same trust boundary.
func TestLiveBrowserSessionDoesNotCrossToPlaintextLoopback(t *testing.T) {
	f := setup(t)
	live := httptest.NewUnstartedServer(nil)
	_, port, err := net.SplitHostPort(live.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	managementHost := "bridge.test:" + port
	origin := "https://" + managementHost
	// The test TLS listener is addressed by loopback but the configured Host and
	// Origin model a separately named trusted management endpoint.
	live.Config.Handler = api.New(config.Config{Mode: "live", BrowserSessions: true, Target: "demo-workstation", AllowedHosts: []string{managementHost}, ExternalURL: origin}, f.eng, nil).Handler()
	live.StartTLS()
	defer live.Close()
	var ownerID string
	for id, credential := range f.db.View().Credentials {
		if credential.Role == "owner" {
			ownerID = id
		}
	}
	if ownerID == "" {
		t.Fatal("owner fixture credential missing")
	}
	legacyToken := auth.Secret()
	if err := f.db.Update(func(v *store.State) error {
		v.Sessions[auth.Verifier(legacyToken)] = store.Session{Verifier: auth.Verifier(legacyToken), CredentialID: ownerID, CSRF: auth.Secret(), ExpiresAt: time.Now().Add(time.Hour)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// An on-disk pre-v2 session record must not become valid merely because an
	// attacker rewraps its token in the new cookie name.
	rewrapped, err := http.NewRequest(http.MethodGet, live.URL+"/api/v1/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	rewrapped.Host = managementHost
	rewrapped.Header.Set("Cookie", config.LiveBrowserSessionCookie+"="+legacyToken)
	if response, err := (&http.Client{Transport: live.Client().Transport}).Do(rewrapped); err != nil || response.StatusCode != http.StatusUnauthorized {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("pre-v2 session token was accepted in the new cookie: response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}
	demoToken := auth.Secret()
	if err := f.db.Update(func(v *store.State) error {
		v.Sessions[auth.Verifier(demoToken)] = store.Session{Verifier: auth.Verifier(demoToken), CredentialID: ownerID, CSRF: auth.Secret(), ExpiresAt: time.Now().Add(time.Hour), Purpose: config.DemoBrowserSessionPurpose}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rewrapped.Header.Set("Cookie", config.LiveBrowserSessionCookie+"="+demoToken)
	if response, err := (&http.Client{Transport: live.Client().Transport}).Do(rewrapped); err != nil || response.StatusCode != http.StatusUnauthorized {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("demo session token was accepted by the live cookie policy: response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := live.Client()
	client.Jar = jar
	body, _ := json.Marshal(map[string]string{"credential": f.tokens["owner"]})
	req, err := http.NewRequest(http.MethodPost, live.URL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Host = managementHost
	response, err := client.Do(req)
	if err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("login: response=%v error=%v", response, err)
	}
	var cookie *http.Cookie
	for _, candidate := range response.Cookies() {
		if candidate.Name == config.LiveBrowserSessionCookie {
			cookie = candidate
		}
	}
	response.Body.Close()
	if cookie == nil || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("live cookie is not a host-only secure strict session: %#v", cookie)
	}
	// Pre-remediation, this request received bridge_session. The current server
	// neither accepts that name nor uses it for new session state.
	legacy, err := http.NewRequest(http.MethodGet, live.URL+"/api/v1/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Host = managementHost
	legacy.Header.Set("Cookie", config.LegacyBrowserSessionCookie+"="+cookie.Value)
	legacyClient := &http.Client{Transport: live.Client().Transport}
	if response, err := legacyClient.Do(legacy); err != nil || response.StatusCode != http.StatusUnauthorized {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("legacy session name accepted: response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}
	seen := make(chan bool, 1)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := r.Cookie(config.LiveBrowserSessionCookie)
		seen <- err == nil
		w.WriteHeader(http.StatusNoContent)
	}))
	defer other.Close()
	response, err = client.Get(other.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if <-seen {
		t.Fatal("live Secure session crossed to unrelated plaintext loopback service")
	}
}

func TestLiveLoopbackIsBearerCLIOnly(t *testing.T) {
	f := setup(t)
	server := httptest.NewUnstartedServer(nil)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	host := "127.0.0.1:" + port
	// The exact server origin is intentionally loopback HTTP: it remains valid
	// only for the bearer CLI, never for cookie-backed browser management.
	origin := "http://" + host
	server.Config.Handler = api.New(config.Config{Mode: "live", Target: "demo-workstation", AllowedHosts: []string{host}, ExternalURL: origin}, f.eng, nil).Handler()
	server.Start()
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, origin+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := server.Client().Do(request); err != nil || response.StatusCode != http.StatusForbidden {
		if response != nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("live HTTP browser UI was available: status=%d error=%v body=%s", response.StatusCode, err, body)
		}
		t.Fatalf("live HTTP browser UI was unavailable: error=%v", err)
	} else {
		response.Body.Close()
	}
	status, _, _ := f.request(t, http.MethodGet, "/api/v1/config", "owner", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("fixture bearer precondition failed: %d", status)
	}
	request, err = http.NewRequest(http.MethodGet, origin+"/api/v1/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.tokens["owner"])
	if response, err := server.Client().Do(request); err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("live loopback bearer CLI refused: response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}
	request, err = http.NewRequest(http.MethodPost, origin+"/api/v1/auth/login", strings.NewReader(`{"credential":"ignored"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	if response, err := server.Client().Do(request); err != nil || response.StatusCode != http.StatusForbidden {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("live HTTP browser login was available: response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}
}
func TestPlanApplyIdempotencySourceDrift(t *testing.T) {
	f := setup(t)
	serving := f.conf.Serving
	serving.Context = 8192
	d := domain.Draft{Action: "serving.configure", Target: "demo-workstation", SourceRevision: f.conf.Revision, Serving: &serving}
	status, b, _ := f.request(t, "POST", "/api/v1/plans", "owner", d, nil)
	if status != 201 {
		t.Fatalf("plan %d %s", status, b)
	}
	var p domain.Plan
	_ = json.Unmarshal(b, &p)
	if len(p.Preview.Changes) != 1 || p.Preview.Changes[0].Field != "serving.context" {
		t.Fatalf("preview not exact: %+v", p.Preview.Changes)
	}
	status, b, _ = f.request(t, "POST", "/api/v1/operations", "owner", map[string]string{"plan_id": p.ID, "target": "demo-workstation"}, map[string]string{"Idempotency-Key": "test-apply-1"})
	if status != 202 {
		t.Fatalf("apply %d %s", status, b)
	}
	var op domain.Operation
	_ = json.Unmarshal(b, &op)
	id := op.ID
	for deadline := time.Now().Add(5 * time.Second); !domain.Terminal(op.State) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		op, _ = f.eng.Operation(id)
	}
	if op.State != "succeeded" || !op.SourceUpdated || !op.LiveApplied {
		t.Fatalf("apply result %+v", op)
	}
	status, b, _ = f.request(t, "POST", "/api/v1/operations", "owner", map[string]string{"plan_id": p.ID, "target": "demo-workstation"}, map[string]string{"Idempotency-Key": "test-apply-1"})
	if status != 202 {
		t.Fatalf("replay %d %s", status, b)
	}
	var replay domain.Operation
	_ = json.Unmarshal(b, &replay)
	if replay.ID != id {
		t.Fatal("idempotency created duplicate")
	}
	status, _, _ = f.request(t, "POST", "/api/v1/operations", "owner", map[string]string{"plan_id": "different", "target": "demo-workstation"}, map[string]string{"Idempotency-Key": "test-apply-1"})
	if status != 409 {
		t.Fatal("idempotency conflict not detected")
	}
	status, b, _ = f.request(t, "POST", "/api/v1/plans", "owner", d, nil)
	if status != 409 {
		t.Fatalf("source drift %d %s", status, b)
	}
	stateBytes, e := os.ReadFile(filepath.Join(filepath.Dir(f.eng.Source.Path), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, token := range f.tokens {
		if bytes.Contains(stateBytes, []byte(token)) {
			t.Fatal("store contains reusable credential")
		}
	}
}
func TestRateLimit(t *testing.T) {
	f := setup(t)
	for i := 0; i < 13; i++ {
		status, b, _ := f.request(t, "POST", "/api/v1/auth/login", "", map[string]string{"credential": "bad"}, map[string]string{"Origin": f.server.URL})
		want := 401
		if i == 12 {
			want = 429
		}
		if status != want {
			t.Fatal(fmt.Sprintf("attempt %d status %d %s", i, status, b))
		}
	}
}
