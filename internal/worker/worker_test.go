package worker

import (
	"context"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecipesAndBudgetCeilings(t *testing.T) {
	recipes := Recipes(true)
	if recipes[0].Revision != SourceCommit || recipes[2].Status != "unavailable" || recipes[3].Status != "unsupported" {
		t.Fatal(recipes)
	}
	p := Policy{Jobs: 8, MemoryMiB: 32768, ScratchGiB: 50, CacheGiB: 100}
	r := Request{SourceRevision: strings.Repeat("a", 64), Budgets: domain.CacheBudgets{BuildJobs: 4, BuildMemoryMiB: 16384, ScratchGiB: 20, CompilerGiB: 50}}
	next, e := executionPolicy(p, r)
	if e != nil || next.Jobs != 4 || next.MemoryMiB != 16384 {
		t.Fatal(next, e)
	}
	r.Budgets.BuildJobs = 9
	if _, e = executionPolicy(p, r); e == nil {
		t.Fatal("accepted job escalation")
	}
	if _, e = buildJobs(24000, 32768, 8); e == nil {
		t.Fatal("accepted insufficient memory")
	}
	if n, e := buildJobs(65536, 32768, 8); e != nil || n != 6 {
		t.Fatal(n, e)
	}
}
func TestRequestHashBindsBudgetActorSource(t *testing.T) {
	r := Request{ID: "test-operation", Actor: "owner", Recipe: "llama-vulkan", SourceRevision: strings.Repeat("a", 64)}
	h := RequestHash(r)
	r.Budgets.BuildJobs = 4
	if RequestHash(r) == h {
		t.Fatal("hash omitted budget")
	}
	h = RequestHash(r)
	r.Actor = "another"
	if RequestHash(r) == h {
		t.Fatal("hash omitted actor")
	}
	if validID("../../escape") || validID("short") {
		t.Fatal("unsafe ID accepted")
	}
}
func TestSocketPeerRequired(t *testing.T) {
	s := &Service{policy: Policy{APIUID: 1234}, cancel: map[string]context.CancelFunc{}}
	r := httptest.NewRequest("POST", "http://worker/execute", strings.NewReader(`{"recipe":"arbitrary"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
