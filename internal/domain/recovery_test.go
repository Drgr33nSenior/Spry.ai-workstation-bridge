package domain

import "testing"

func TestRecoveryChainsRejectAmbiguity(t *testing.T) {
	links := map[string]RecoveryLink{"A": {Target: "node", Action: "profile.switch"}, "B": {Parent: "A", Target: "node", Action: "profile.restore"}, "C": {Parent: "B", Target: "node", Action: "profile.restore"}, "unrelated": {Target: "node", Action: "profile.switch"}}
	chain, err := RecoveryChain(links, "B")
	if err != nil || len(chain) != 3 || chain["unrelated"] {
		t.Fatalf("wrong chain: %v %v", chain, err)
	}
	for _, bad := range []RecoveryLink{{Parent: "missing", Target: "node", Action: "profile.restore"}, {Parent: "B", Target: "other", Action: "profile.restore"}, {Parent: "C", Target: "node", Action: "profile.restore"}, {Parent: "A", Target: "node", Action: "build.start"}} {
		links["B"] = bad
		if _, err := RecoveryRoot(links, "B"); err == nil {
			t.Fatalf("unsafe link accepted: %+v", bad)
		}
	}
	links["A"] = RecoveryLink{Target: "node", Action: "model.stage"}
	links["B"] = RecoveryLink{Parent: "A", Target: "node", Action: "profile.restore"}
	if _, err := RecoveryRoot(links, "B"); err == nil {
		t.Fatal("model operation routed into host chain")
	}
}
