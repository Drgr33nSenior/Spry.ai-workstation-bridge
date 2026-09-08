package domain

import "fmt"

// RecoveryLink is derived from one authority's persisted journal. Never combine
// API and executor links: only the executor journal can authorize its effects.
type RecoveryLink struct {
	Parent string
	Target string
	Action string
}

func HostRestoreRequired(action string) bool {
	switch action {
	case "profile.switch", "profile.restore", "serving.configure", "resources.configure", "serving.start", "serving.stop", "serving.restart":
		return true
	}
	return false
}

// RecoveryRoot fails closed for legacy records with missing/ambiguous linkage.
// Existing unlinked operations are separate roots, not implicitly related.
func RecoveryRoot(links map[string]RecoveryLink, id string) (string, error) {
	seen := map[string]bool{}
	target := ""
	for {
		link, ok := links[id]
		if !ok || seen[id] || link.Target == "" {
			return "", fmt.Errorf("recovery linkage is missing or cyclic; preserve journals for owner review")
		}
		seen[id] = true
		if target != "" && target != link.Target {
			return "", fmt.Errorf("recovery linkage crosses targets")
		}
		target = link.Target
		if link.Parent == "" {
			return id, nil
		}
		if link.Action != "profile.restore" && link.Action != "operation.reconcile" {
			return "", fmt.Errorf("recovery linkage belongs to a non-recovery action")
		}
		parent, ok := links[link.Parent]
		if !ok || (link.Action == "profile.restore" && !HostRestoreRequired(parent.Action)) {
			return "", fmt.Errorf("recovery linkage crosses executors or has a missing parent")
		}
		id = link.Parent
	}
}

func RecoveryChain(links map[string]RecoveryLink, id string) (map[string]bool, error) {
	root, err := RecoveryRoot(links, id)
	if err != nil {
		return nil, err
	}
	chain := map[string]bool{}
	for candidate := range links {
		r, e := RecoveryRoot(links, candidate)
		if e == nil && r == root {
			chain[candidate] = true
		}
	}
	return chain, nil
}
