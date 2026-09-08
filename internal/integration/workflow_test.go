package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/oasdiff/yaml3"
)

// These tests deliberately assert workflow invariants instead of a rendered
// YAML snapshot. GitHub Actions has no local runner in this repository, so the
// tag-validation shell fragment is also exercised in an isolated bash process.
func TestSourceCheckWorkflowOnlyChecksMainChanges(t *testing.T) {
	workflow := readWorkflow(t, "check.yml")
	trigger := mappingValue(t, workflow, "on")
	assertBranchTrigger(t, mappingValue(t, trigger, "push"), "push")
	assertBranchTrigger(t, mappingValue(t, trigger, "pull_request"), "pull_request")

	assertReadOnlyPermissions(t, workflow)
	jobs := mappingValue(t, workflow, "jobs")
	if len(jobs.Content) != 2 || mappingValue(t, jobs, "check") == nil {
		t.Fatal("source workflow must contain only the check job")
	}
	check := mappingValue(t, jobs, "check")
	assertCheckMatrix(t, check)
	assertCheckSteps(t, check)
	assertPinnedActionsAndNoBypass(t, workflow)
	assertNoPackageOrUpload(t, check)
}

func TestTagBuildWorkflowValidatesExactVersionAndPublishesOnlyArchives(t *testing.T) {
	workflow := readWorkflow(t, "build.yml")
	trigger := mappingValue(t, workflow, "on")
	if len(trigger.Content) != 2 {
		t.Fatalf("tag build must have exactly one trigger, got %d entries", len(trigger.Content)/2)
	}
	push := mappingValue(t, trigger, "push")
	if push == nil {
		t.Fatal("tag build must be triggered by push")
	}
	if mappingValue(t, push, "branches") != nil || !equalStrings(nodeStrings(t, mappingValue(t, push, "tags")), []string{"v*"}) {
		t.Fatal("tag build must use only push tags: [v*]")
	}

	assertReadOnlyPermissions(t, workflow)
	jobs := mappingValue(t, workflow, "jobs")
	validate := mappingValue(t, jobs, "validate")
	check := mappingValue(t, jobs, "check")
	packageJob := mappingValue(t, jobs, "package")
	if validate == nil || check == nil || packageJob == nil || len(jobs.Content) != 6 {
		t.Fatal("tag build must contain validate, check, and package jobs only")
	}
	if !equalStrings(nodeStrings(t, mappingValue(t, check, "needs")), []string{"validate"}) {
		t.Fatalf("tag check dependencies = %v, want validate", nodeStrings(t, mappingValue(t, check, "needs")))
	}
	if got := scalar(t, mappingValue(t, validate, "if")); got != "${{ github.event.created == true && github.event.deleted == false }}" {
		t.Fatalf("validate job creation gate = %q", got)
	}
	if got := scalar(t, mappingValue(t, mappingValue(t, validate, "outputs"), "version")); got != "${{ steps.tag.outputs.version }}" {
		t.Fatalf("validate version output = %q", got)
	}
	script := tagValidationScript(t, validate)
	for _, version := range []string{"v0.1.0", "v1.2.3", "v10.20.30", "v0.0.0"} {
		assertTagScript(t, script, version, true)
	}
	for _, version := range []string{
		"v01.2.3", "v1.02.3", "v1.2.03", "1.2.3", "v1.2", "v1.2.3.4",
		"v1.2.3-rc.1", "v1.2.3+build.1", "v1.2.3 ", "../v1.2.3",
		"v1.2.3; touch injected", "v1.2.3\nversion=evil",
	} {
		assertTagScript(t, script, version, false)
	}

	assertCheckMatrix(t, check)
	assertCheckSteps(t, check)
	if !sameStrings(nodeStrings(t, mappingValue(t, packageJob, "needs")), []string{"validate", "check"}) {
		t.Fatalf("package dependencies = %v, want validate and check", nodeStrings(t, mappingValue(t, packageJob, "needs")))
	}
	if mappingValue(t, packageJob, "if") != nil {
		t.Fatal("package job must use GitHub's default successful-needs condition")
	}
	assertPinnedActionsAndNoBypass(t, workflow)
	assertNoReleasePublication(t, workflow)
	assertPackageUpload(t, packageJob)
}

func readWorkflow(t *testing.T, name string) *yaml.Node {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(b, &document); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("%s is not a YAML mapping document", name)
	}
	return document.Content[0]
}

func mappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping == nil {
		return nil
	}
	if mapping.Kind != yaml.MappingNode {
		t.Fatalf("expected mapping while looking up %q, got kind %d", key, mapping.Kind)
	}
	var result *yaml.Node
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		if result != nil {
			t.Fatalf("duplicate %q key", key)
		}
		result = mapping.Content[i+1]
	}
	return result
}

func scalar(t *testing.T, node *yaml.Node) string {
	t.Helper()
	if node == nil || node.Kind != yaml.ScalarNode {
		t.Fatalf("expected scalar, got %#v", node)
	}
	return node.Value
}

func nodeStrings(t *testing.T, node *yaml.Node) []string {
	t.Helper()
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	if node.Kind != yaml.SequenceNode {
		t.Fatalf("expected sequence or scalar, got kind %d", node.Kind)
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		values = append(values, scalar(t, item))
	}
	return values
}

func assertBranchTrigger(t *testing.T, trigger *yaml.Node, event string) {
	t.Helper()
	if trigger == nil || !equalStrings(nodeStrings(t, mappingValue(t, trigger, "branches")), []string{"main"}) || mappingValue(t, trigger, "tags") != nil {
		t.Fatalf("%s trigger must be branches: [main] with no tag filter", event)
	}
}

func assertReadOnlyPermissions(t *testing.T, workflow *yaml.Node) {
	t.Helper()
	permissions := mappingValue(t, workflow, "permissions")
	if permissions == nil || len(permissions.Content) != 2 || scalar(t, mappingValue(t, permissions, "contents")) != "read" {
		t.Fatal("workflow must grant only contents: read")
	}
}

func assertCheckMatrix(t *testing.T, job *yaml.Node) {
	t.Helper()
	matrix := mappingValue(t, mappingValue(t, job, "strategy"), "matrix")
	if !sameStrings(nodeStrings(t, mappingValue(t, matrix, "os")), []string{"ubuntu-24.04", "macos-15"}) {
		t.Fatalf("check matrix = %v, want Ubuntu 24.04 and macOS 15", nodeStrings(t, mappingValue(t, matrix, "os")))
	}
}

func assertCheckSteps(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(t, job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("check job lacks steps")
	}
	checkoutFound, gatesFound := false, false
	for _, step := range steps.Content {
		if uses := mappingValue(t, step, "uses"); uses != nil && strings.HasPrefix(scalar(t, uses), "actions/checkout@") {
			checkoutFound = true
			if got := scalar(t, mappingValue(t, mappingValue(t, step, "with"), "persist-credentials")); got != "false" {
				t.Fatalf("checkout persist-credentials = %q, want false", got)
			}
		}
		if run := mappingValue(t, step, "run"); run != nil && strings.HasPrefix(scalar(t, run), "make ") {
			fields := strings.Fields(scalar(t, run))
			want := []string{"toolchain", "fmt", "vet", "test", "race", "generated", "openapi", "manifests", "security"}
			if sameStrings(fields[1:], want) {
				gatesFound = true
			}
		}
	}
	if !checkoutFound {
		t.Fatal("check job must use checkout with persisted credentials disabled")
	}
	if !gatesFound {
		t.Fatal("check job must run all source gates, including test and race")
	}
}

func assertPinnedActionsAndNoBypass(t *testing.T, root *yaml.Node) {
	t.Helper()
	pinned := regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			if uses := mappingValue(t, node, "uses"); uses != nil && !pinned.MatchString(scalar(t, uses)) {
				t.Fatalf("action must be pinned to a full commit SHA: %q", scalar(t, uses))
			}
			if mappingValue(t, node, "continue-on-error") != nil {
				t.Fatal("workflow must not use continue-on-error")
			}
			if condition := mappingValue(t, node, "if"); condition != nil && strings.Contains(strings.ToLower(scalar(t, condition)), "always()") {
				t.Fatal("workflow must not bypass failed dependencies with always()")
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(root)
}

func assertNoReleasePublication(t *testing.T, root *yaml.Node) {
	t.Helper()
	for _, scalar := range allScalarValues(root) {
		lower := strings.ToLower(scalar)
		for _, forbidden := range []string{
			"gh release", "softprops/action-gh-release", "ncipollo/release-action",
			"actions/create-release", "github-release", "release-action",
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("build pipeline must upload a CI artifact, not publish a release: %q", scalar)
			}
		}
	}
}

func allScalarValues(root *yaml.Node) []string {
	var values []string
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.ScalarNode {
			values = append(values, node.Value)
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(root)
	return values
}

func assertNoPackageOrUpload(t *testing.T, job *yaml.Node) {
	t.Helper()
	for _, step := range mappingValue(t, job, "steps").Content {
		if run := mappingValue(t, step, "run"); run != nil && strings.Contains(scalar(t, run), "package") {
			t.Fatal("main source-check workflow must not package artifacts")
		}
		if uses := mappingValue(t, step, "uses"); uses != nil && strings.Contains(scalar(t, uses), "upload-artifact") {
			t.Fatal("main source-check workflow must not upload artifacts")
		}
	}
}

func tagValidationScript(t *testing.T, validate *yaml.Node) string {
	t.Helper()
	steps := mappingValue(t, validate, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("validate job lacks steps")
	}
	for _, step := range steps.Content {
		if id := mappingValue(t, step, "id"); id == nil || scalar(t, id) != "tag" {
			continue
		}
		env := mappingValue(t, step, "env")
		if got := scalar(t, mappingValue(t, env, "BRIDGE_TAG")); got != "${{ github.ref_name }}" {
			t.Fatalf("tag validation BRIDGE_TAG = %q", got)
		}
		if got := scalar(t, mappingValue(t, step, "shell")); got != "bash" {
			t.Fatalf("tag validation shell = %q", got)
		}
		return scalar(t, mappingValue(t, step, "run"))
	}
	t.Fatal("validate job lacks tag step")
	return ""
}

func assertTagScript(t *testing.T, script, tag string, valid bool) {
	t.Helper()
	dir := t.TempDir()
	output := filepath.Join(dir, "github-output")
	if err := os.WriteFile(output, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "BRIDGE_TAG=" + tag, "GITHUB_OUTPUT=" + output}
	err := cmd.Run()
	b, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if valid {
		if err != nil {
			t.Fatalf("valid tag %q rejected: %v", tag, err)
		}
		if got, want := string(b), fmt.Sprintf("version=%s\n", tag); got != want {
			t.Fatalf("valid tag %q output %q, want %q", tag, got, want)
		}
		return
	}
	if err == nil {
		t.Fatalf("invalid tag %q accepted", tag)
	}
	if len(bytes.TrimSpace(b)) != 0 {
		t.Fatalf("invalid tag %q wrote output %q", tag, b)
	}
}

func assertPackageUpload(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(t, job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("package job lacks steps")
	}
	const expected = "dist/spry-bridge-linux-amd64.tar.gz\ndist/spry-bridge-darwin-arm64.tar.gz\ndist/spry-bridge-darwin-amd64.tar.gz\ndist/SHA256SUMS"
	uploads := 0
	checksumBeforeUpload := false
	for index, step := range steps.Content {
		if run := mappingValue(t, step, "run"); run != nil && strings.Contains(scalar(t, run), "sha256sum --check SHA256SUMS") {
			checksumBeforeUpload = true
		}
		uses := mappingValue(t, step, "uses")
		if uses == nil || !strings.Contains(scalar(t, uses), "actions/upload-artifact@") {
			continue
		}
		uploads++
		if !checksumBeforeUpload {
			t.Fatalf("artifact upload step %d must follow checksum verification", index)
		}
		with := mappingValue(t, step, "with")
		if got := strings.TrimSpace(scalar(t, mappingValue(t, with, "path"))); got != expected {
			t.Fatalf("artifact paths = %q, want only release archives and SHA256SUMS", got)
		}
		if got := scalar(t, mappingValue(t, with, "retention-days")); got != "7" {
			t.Fatalf("artifact retention = %q, want 7 days", got)
		}
		if got := scalar(t, mappingValue(t, with, "if-no-files-found")); got != "error" {
			t.Fatalf("artifact missing-file policy = %q, want error", got)
		}
	}
	if uploads != 1 {
		t.Fatalf("package job must upload exactly one artifact set, got %d", uploads)
	}
}

func equalStrings(got, want []string) bool {
	return len(got) == len(want) && sameStrings(got, want)
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		seen[value]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}
