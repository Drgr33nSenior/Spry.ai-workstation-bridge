package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/advisor"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/api"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type advisoryFunc func(context.Context, advisor.Handler) (advisor.Result, error)

func (f advisoryFunc) Advise(ctx context.Context, h advisor.Handler) (advisor.Result, error) {
	return f(ctx, h)
}

func TestAdvisoryFailureRetainsSanitizedAccounting(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown-create", true: "known-session"}[known], func(t *testing.T) {
			f := setup(t)
			// Test-only live API policy over an isolated Demo adapter; production
			// startup continues to validate live paths and cannot select this fixture.
			s := api.New(config.Config{Mode: "live", Advisor: config.Advisor{Enabled: true}, AllowedHosts: []string{"bridge.test"}}, f.eng, nil)
			calls := 0
			s.Advisor = advisoryFunc(func(ctx context.Context, h advisor.Handler) (advisor.Result, error) {
				calls++
				audit := f.db.View().Audit
				if len(audit) != 1 || audit[0].Action != "memory.advice.requested" || audit[0].Result != "outcome_not_observed" {
					t.Fatal("missing durable pre-dispatch intent")
				}
				if _, err := h(ctx, advisor.ReadMemoryEvidence); err != nil {
					t.Fatal(err)
				}
				out := advisor.Result{Summary: "SYNTHETIC_PRIVATE_PROVIDER_TEXT", CreationUncertain: !known}
				if known {
					out.SessionID = "agses_fixture"
					out.Usage = &advisor.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
					out.UsageProvisional = true
					out.UsageSource = "session"
					out.CancellationRequested, out.CancellationAccepted = true, true
				}
				return out, errors.New("SYNTHETIC_PRIVATE_PROVIDER_ERROR")
			})
			request := func(role string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://bridge.test/api/v1/memory/advice", strings.NewReader(`{"evidence_id":"demo-complete","evidence_sha256":"`+strings.Repeat("a", 64)+`","other_mib":0}`))
				r.Header.Set("Authorization", "Bearer "+f.tokens[role])
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			for _, role := range []string{"viewer", "operator"} {
				if w := request(role); w.Code != 403 {
					t.Fatal(w.Code)
				}
			}
			if calls != 0 {
				t.Fatal("non-owner reached provider")
			}
			w := request("owner")
			if w.Code != 503 || calls != 1 {
				t.Fatalf("response %d calls %d: %s", w.Code, calls, w.Body.String())
			}
			audit := f.db.View().Audit
			if len(audit) != 2 || audit[1].Action != "memory.advice.finished" || audit[0].Object != audit[1].Object || w.Header().Get("X-Bridge-Advisory-ID") != audit[0].Object {
				t.Fatal("accounting linkage lost")
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(audit[1].Result), &result); err != nil {
				t.Fatal(err)
			}
			if result["outcome"] != "failed_or_incomplete" || result["creation_uncertain"] != !known {
				t.Fatal(result)
			}
			if known {
				if result["session_id"] != "agses_fixture" || result["usage_provisional"] != true || result["cancellation_accepted"] != true || result["usage"].(map[string]any)["total_tokens"] != float64(15) {
					t.Fatal(result)
				}
			} else if result["usage"] != nil {
				t.Fatal("unknown usage became a number")
			}
			b, _ := json.Marshal(audit)
			if strings.Contains(string(b)+w.Body.String(), "SYNTHETIC_PRIVATE") {
				t.Fatal("provider text persisted or reflected")
			}
			if len(f.eng.Operations()) != 0 {
				t.Fatal("advisory created mutation/recovery operation")
			}
		})
	}
}

func TestAdvisoryAuditStorageFailureNeverRetries(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-dispatch", true: "after-dispatch"}[after], func(t *testing.T) {
			f := setup(t)
			s := api.New(config.Config{Mode: "live", Advisor: config.Advisor{Enabled: true}, AllowedHosts: []string{"bridge.test"}}, f.eng, nil)
			fault := func(string, []byte, os.FileMode) error { return errors.New("fixture persistence failure") }
			calls := 0
			s.Advisor = advisoryFunc(func(context.Context, advisor.Handler) (advisor.Result, error) {
				calls++
				f.db.Write = fault
				return advisor.Result{CreationUncertain: true}, errors.New("lost create response")
			})
			if !after {
				f.db.Write = fault
			}
			for range 2 {
				r := httptest.NewRequest("POST", "http://bridge.test/api/v1/memory/advice", strings.NewReader(`{"evidence_id":"demo-complete","evidence_sha256":"`+strings.Repeat("a", 64)+`","other_mib":0}`))
				r.Header.Set("Authorization", "Bearer "+f.tokens["owner"])
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				if w.Code != 503 || !strings.Contains(w.Body.String(), "storage_unavailable") {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			wantCalls := 0
			if after {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("calls=%d want=%d", calls, wantCalls)
			}
			audit := f.db.View().Audit
			if len(audit) != wantCalls || (after && audit[0].Result != "outcome_not_observed") {
				t.Fatal("uncertain intent lost or completion invented")
			}
			// A poisoned store cannot discard the uncertainty through another update.
			if err := f.db.Update(func(*store.State) error { return nil }); err == nil {
				t.Fatal("persistence fence bypassed")
			}
		})
	}
}
