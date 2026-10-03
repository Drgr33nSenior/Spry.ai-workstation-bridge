package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSource(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deployment", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRepositoryDeploymentSources(t *testing.T) {
	if err := validateTree(filepath.Join("..", "..", "deployment")); err != nil {
		t.Fatal(err)
	}
}

func TestRejectUnsafeRules(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		rule       rule
	}{
		{"wildcard-group", "ClusterRole", rule{APIGroups: []string{"*"}, Resources: []string{"pods"}, Verbs: []string{"get"}}},
		{"wildcard-resource", "ClusterRole", rule{APIGroups: []string{""}, Resources: []string{"*"}, Verbs: []string{"get"}}},
		{"secrets", "Role", rule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}}},
		{"pod-exec", "Role", rule{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}}},
		{"pod-delete", "Role", rule{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"delete"}}},
		{"cluster-mutation", "ClusterRole", rule{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, ResourceNames: []string{"sglang"}, Verbs: []string{"update"}}},
		{"unnamed-mutation", "Role", rule{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"update"}}},
		{"escalation", "Role", rule{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, ResourceNames: []string{"sglang"}, Verbs: []string{"escalate"}}},
		{"all-configmaps", "Role", rule{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"list"}}},
		{"cluster-configmaps", "ClusterRole", rule{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{"sglang-profile"}, Verbs: []string{"get"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validateRule(test.kind, test.rule) == nil {
				t.Fatal("accepted unsafe RBAC")
			}
		})
	}
	if err := validateRule("Role", rule{APIGroups: []string{"apps"}, Resources: []string{"deployments/scale"}, ResourceNames: []string{"sglang", "parent-steam-headless"}, Verbs: []string{"get", "patch", "update"}}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectExternalRolesAndHumanSubjects(t *testing.T) {
	for _, mutation := range []func([]kubeObject){
		func(objects []kubeObject) {
			for i := range objects {
				if objects[i].RoleRef != nil {
					objects[i].RoleRef.Name = "cluster-admin"
					return
				}
			}
		},
		func(objects []kubeObject) {
			for i := range objects {
				if len(objects[i].Subjects) > 0 {
					objects[i].Subjects[0].Kind = "Group"
					objects[i].Subjects[0].Name = "system:masters"
					return
				}
			}
		},
		func(objects []kubeObject) {
			for i := range objects {
				if objects[i].Kind == "ServiceAccount" {
					value := true
					objects[i].Automount = &value
					return
				}
			}
		},
	} {
		objects, err := decodeKubernetes(readSource(t, "kubernetes-rbac.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		mutation(objects)
		if err := validateRBAC(objects); err == nil {
			t.Fatal("accepted unsafe binding or service account")
		}
	}
}

func TestYAMLIsStrictAndReadsAllDocuments(t *testing.T) {
	objects, err := decodeKubernetes(readSource(t, "kubernetes-rbac.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 5 {
		t.Fatalf("read %d resources, expected all 5 documents", len(objects))
	}
	for _, content := range []string{
		"apiVersion: v1\nkind: ServiceAccount\nkind: Secret\nmetadata:\n  name: bad\n",
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: bad\ndata:\n  token: forbidden-example-value\n",
		"apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: safe\n  unexpected: refused\n",
	} {
		if _, err := decodeKubernetes([]byte(content)); err == nil {
			t.Fatal("accepted duplicate or unknown YAML field")
		}
	}
}

func TestIncompleteExamplesRemainUnqualified(t *testing.T) {
	for _, name := range []string{"host-policy.example.json", "worker-policy.example.json"} {
		if err := validateExample(name, readSource(t, name)); err != nil {
			t.Fatalf("source checker attempted target qualification: %v", err)
		}
	}
	var worker map[string]any
	if err := json.Unmarshal(readSource(t, "worker-policy.example.json"), &worker); err != nil {
		t.Fatal(err)
	}
	if worker["worker_uid"] != float64(0) || worker["source_manifest_sha256"] != "OWNER_REVIEW_REQUIRED" {
		t.Fatal("intentionally invalid worker qualification placeholders changed")
	}
	if err := validateExample("server.local.example.json", []byte(`{"mode":"live","mode":"demo"}`)); err == nil {
		t.Fatal("accepted duplicate JSON field")
	}
	if err := validateExample("server.local.example.json", []byte(`{"token":"forbidden-example-value"}`)); err == nil {
		t.Fatal("accepted inline credential example")
	}
}

func TestServerAndHelperNeedDifferentHostVisibility(t *testing.T) {
	root := t.TempDir()
	entries, err := os.ReadDir(filepath.Join("..", "..", "deployment", "systemd"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		b := readSource(t, filepath.Join("systemd", entry.Name()))
		if entry.Name() == "bridge-hostd.service" {
			b = []byte(strings.ReplaceAll(string(b), "PrivateDevices=no", "PrivateDevices=yes"))
		}
		if err := os.WriteFile(filepath.Join(root, entry.Name()), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateUnits(root); err == nil {
		t.Fatal("accepted incomplete helper device visibility")
	}
	if _, err := parseUnit([]byte("[Service]\nUser=root\nUser=bridge\n")); err == nil {
		t.Fatal("accepted ambiguous repeated service directive")
	}
}
