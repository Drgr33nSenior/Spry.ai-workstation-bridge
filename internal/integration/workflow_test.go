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
	check := mappingValue(t, jobs, "check")
	if len(jobs.Content) != 2 || check == nil {
		t.Fatal("source workflow must call only the shared CI workflow")
	}
	assertCICaller(t, check)
	if mappingValue(t, check, "with") != nil || mappingValue(t, check, "needs") != nil {
		t.Fatal("source checks must run immediately with the empty version default")
	}
	assertPinnedActionsAndNoBypass(t, workflow)
	assertNoSecretReferences(t, workflow)
}

func TestTagBuildWorkflowValidatesBeforeSharedCI(t *testing.T) {
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
	if validate == nil || check == nil || len(jobs.Content) != 4 {
		t.Fatal("tag build must validate and then call the shared CI workflow only")
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

	assertCICaller(t, check)
	with := mappingValue(t, check, "with")
	if with == nil || len(with.Content) != 2 || scalar(t, mappingValue(t, with, "version")) != "${{ needs.validate.outputs.version }}" {
		t.Fatal("tag CI must receive only the validated version output")
	}
	assertPinnedActionsAndNoBypass(t, workflow)
	assertNoSecretReferences(t, workflow)
	assertNoReleasePublication(t, workflow)
}

func assertCICaller(t *testing.T, job *yaml.Node) {
	t.Helper()
	if got := scalar(t, mappingValue(t, job, "uses")); got != "./.github/workflows/ci.yml" {
		t.Fatalf("shared CI workflow = %q", got)
	}
	for _, forbidden := range []string{"if", "steps", "strategy", "secrets"} {
		if mappingValue(t, job, forbidden) != nil {
			t.Fatalf("CI caller must not override execution with %s", forbidden)
		}
	}
}

func TestUnifiedCheckMatrixGatesIndependentTagPackages(t *testing.T) {
	workflow := readWorkflow(t, "ci.yml")
	trigger := mappingValue(t, workflow, "on")
	if len(trigger.Content) != 2 || mappingValue(t, trigger, "workflow_call") == nil {
		t.Fatal("shared CI must be reusable only, with no automatic event trigger")
	}
	inputs := mappingValue(t, mappingValue(t, trigger, "workflow_call"), "inputs")
	version := mappingValue(t, inputs, "version")
	if got := scalar(t, mappingValue(t, version, "default")); got != "" {
		t.Fatalf("CI version default = %q, want empty", got)
	}
	if got := scalar(t, mappingValue(t, version, "required")); got != "false" {
		t.Fatalf("CI version required = %q, want false", got)
	}
	if got := scalar(t, mappingValue(t, version, "type")); got != "string" {
		t.Fatalf("CI version type = %q, want string", got)
	}

	assertReadOnlyPermissions(t, workflow)
	jobs := mappingValue(t, workflow, "jobs")
	if len(jobs.Content) != 6 {
		t.Fatal("shared CI must contain one check matrix and two packaging jobs")
	}
	check := mappingValue(t, jobs, "check")
	if mappingValue(t, check, "needs") != nil || mappingValue(t, check, "if") != nil {
		t.Fatal("all matrix rows must start without dependencies or conditional skipping")
	}
	archContainer := assertCheckMatrix(t, check)
	assertCheckSteps(t, check)
	assertNoPackageOrUpload(t, check)
	for _, name := range []string{"package", "arch-package"} {
		job := mappingValue(t, jobs, name)
		if !equalStrings(nodeStrings(t, mappingValue(t, job, "needs")), []string{"check"}) {
			t.Fatalf("%s must depend only on the complete check matrix, not the other package job", name)
		}
		// No status function overrides implicit success(): any failed or cancelled
		// matrix row blocks both jobs. Main/PRs cannot package, even with an input.
		const gate = "${{ inputs.version != '' && github.event_name == 'push' && github.ref == format('refs/tags/{0}', inputs.version) && github.event.created == true && github.event.deleted == false }}"
		if got := scalar(t, mappingValue(t, job, "if")); got != gate {
			t.Fatalf("%s new-tag gate = %q", name, got)
		}
		if scalar(t, mappingValue(t, job, "runs-on")) != "ubuntu-24.04" {
			t.Fatalf("%s must run on Ubuntu 24.04", name)
		}
	}
	packageJob := mappingValue(t, jobs, "package")
	if mappingValue(t, packageJob, "container") != nil {
		t.Fatal("cross-platform packaging must stay on its native runner")
	}
	assertPackageUpload(t, packageJob)
	arch := mappingValue(t, jobs, "arch-package")
	alias := mappingValue(t, arch, "container")
	if alias == nil || alias.Kind != yaml.AliasNode || alias.Alias != archContainer {
		t.Fatal("Arch packaging must reuse the matrix's exact image and resource pin")
	}
	assertArchSteps(t, arch)
	assertPinnedActionsAndNoBypass(t, workflow)
	assertNoSecretReferences(t, workflow)
	assertNoReleasePublication(t, workflow)
	assertArchSetupScript(t)
	assertArchPackageScript(t)
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

func assertCheckMatrix(t *testing.T, job *yaml.Node) *yaml.Node {
	t.Helper()
	strategy := mappingValue(t, job, "strategy")
	if scalar(t, mappingValue(t, strategy, "fail-fast")) != "false" || mappingValue(t, strategy, "max-parallel") != nil {
		t.Fatal("matrix must permit parallel diagnostics across every platform after a failure")
	}
	matrix := mappingValue(t, strategy, "matrix")
	rows := mappingValue(t, matrix, "include")
	if matrix == nil || len(matrix.Content) != 2 || rows == nil || rows.Kind != yaml.SequenceNode || len(rows.Content) != 3 {
		t.Fatal("matrix must contain exactly three explicit platform rows")
	}
	want := map[string]string{"ubuntu": "ubuntu-24.04", "macos": "macos-15", "arch": "ubuntu-24.04"}
	var archContainer *yaml.Node
	for _, row := range rows.Content {
		platform := scalar(t, mappingValue(t, row, "platform"))
		runner, ok := want[platform]
		if !ok || scalar(t, mappingValue(t, row, "runner")) != runner {
			t.Fatalf("unknown, duplicate or incorrect platform row %q", platform)
		}
		delete(want, platform)
		container := mappingValue(t, row, "container")
		if platform == "arch" {
			assertArchContainer(t, container)
			archContainer = container
		} else if scalar(t, container) != "" {
			t.Fatalf("%s must run natively without a container", platform)
		}
	}
	if scalar(t, mappingValue(t, job, "runs-on")) != "${{ matrix.runner }}" || scalar(t, mappingValue(t, job, "container")) != "${{ matrix.container }}" {
		t.Fatal("check execution must select the runner and container from the matrix")
	}
	return archContainer
}

func assertCheckSteps(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(t, job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("check job lacks steps")
	}
	checkoutFound, gatesFound, setupFound, archSetupFound := false, false, false, false
	if scalar(t, mappingValue(t, mappingValue(t, mappingValue(t, job, "defaults"), "run"), "shell")) != "bash" {
		t.Fatal("matrix script needs Bash on native and container rows")
	}
	for _, step := range steps.Content {
		if uses := mappingValue(t, step, "uses"); uses != nil && strings.HasPrefix(scalar(t, uses), "actions/checkout@") {
			checkoutFound = true
			if got := scalar(t, mappingValue(t, mappingValue(t, step, "with"), "persist-credentials")); got != "false" {
				t.Fatalf("checkout persist-credentials = %q, want false", got)
			}
		}
		if uses := mappingValue(t, step, "uses"); uses != nil && strings.HasPrefix(scalar(t, uses), "actions/setup-go@") {
			setupFound = true
			with := mappingValue(t, step, "with")
			if scalar(t, mappingValue(t, with, "cache")) != "${{ matrix.platform != 'arch' }}" || scalar(t, mappingValue(t, with, "go-version-file")) != ".go-version" {
				t.Fatal("matrix must retain the exact Go version and disable Arch's root-user cache")
			}
		}
		if run := mappingValue(t, step, "run"); run != nil && scalar(t, run) == "bash scripts/ci-arch-setup.sh" {
			archSetupFound = true
			if scalar(t, mappingValue(t, step, "if")) != "${{ matrix.platform == 'arch' }}" {
				t.Fatal("pacman/container setup must only run in the Arch row")
			}
		}
		if id := mappingValue(t, step, "id"); id != nil && scalar(t, id) == "checks" {
			gatesFound = true
			if mappingValue(t, step, "if") != nil || scalar(t, mappingValue(t, mappingValue(t, step, "env"), "BRIDGE_CHECK_PLATFORM")) != "${{ matrix.platform }}" {
				t.Fatal("unconditional checks must receive the platform as environment data")
			}
			script := scalar(t, mappingValue(t, step, "run"))
			assertShellSyntax(t, script)
			for _, platform := range []string{"ubuntu", "macos", "arch"} {
				for _, exitCode := range []string{"0", "17"} {
					assertMatrixCheckScript(t, script, platform, exitCode)
				}
			}
		}
	}
	if !checkoutFound {
		t.Fatal("check job must use checkout with persisted credentials disabled")
	}
	if !gatesFound || !setupFound || !archSetupFound {
		t.Fatal("check job must set up each environment and run all source gates")
	}
}

func assertMatrixCheckScript(t *testing.T, script, platform, exitCode string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	stubs := map[string]string{
		"make":    "printf 'make' >> \"$BRIDGE_TEST_CALLS\"\nprintf ' %s' \"$@\" >> \"$BRIDGE_TEST_CALLS\"\nprintf '\\n' >> \"$BRIDGE_TEST_CALLS\"\nexit \"$BRIDGE_TEST_EXIT\"\n",
		"chown":   "[[ $# == 3 && $1 == -R && $2 == bridge-ci:bridge-ci && $3 == \"$GITHUB_WORKSPACE\" ]] || exit 98\nprintf 'chown\\n' >> \"$BRIDGE_TEST_CALLS\"\n",
		"runuser": "[[ $1 == -u && $2 == bridge-ci && $3 == -- ]] || exit 99\nprintf 'runuser\\n' >> \"$BRIDGE_TEST_CALLS\"\nshift 3\nexec \"$@\"\n",
	}
	for name, contents := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\nset -euo pipefail\n"+contents), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-e", "-o", "pipefail", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "BRIDGE_CHECK_PLATFORM=" + platform, "GITHUB_WORKSPACE=" + filepath.Join(dir, "checkout with spaces"), "BRIDGE_TEST_CALLS=" + log, "BRIDGE_TEST_EXIT=" + exitCode}
	output, err := cmd.CombinedOutput()
	if (exitCode == "0" && err != nil) || (exitCode == "17" && (cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17)) {
		t.Fatalf("%s checks did not propagate exit %s: %v: %s", platform, exitCode, err, output)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "make toolchain fmt vet test race generated openapi manifests security\n"
	if platform == "arch" {
		want = "chown\nrunuser\n" + want
	}
	if string(got) != want {
		t.Fatalf("%s check calls = %q, want %q", platform, got, want)
	}
}

func assertArchContainer(t *testing.T, container *yaml.Node) {
	t.Helper()
	if container == nil || container.Kind != yaml.MappingNode {
		t.Fatal("Arch job must use a pinned container")
	}
	image := scalar(t, mappingValue(t, container, "image"))
	if !regexp.MustCompile(`^quay\.io/archlinux/archlinux@sha256:[0-9a-f]{64}$`).MatchString(image) {
		t.Fatalf("Arch container image is not an official digest pin: %q", image)
	}
	options := scalar(t, mappingValue(t, container, "options"))
	if strings.Contains(options, "--privileged") || !sameStrings(strings.Fields(options), []string{"--cpus", "2", "--memory", "6g", "--pids-limit", "1024"}) {
		t.Fatalf("Arch container options = %q, want unprivileged 2 CPU, 6 GiB, 1024 PID bounds", options)
	}
}

func assertArchSteps(t *testing.T, job *yaml.Node) {
	t.Helper()
	steps := mappingValue(t, job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		t.Fatal("Arch job lacks steps")
	}
	checkout, setup, workspace, packageStep := false, false, false, false
	uploads := 0
	for _, step := range steps.Content {
		if mappingValue(t, step, "if") != nil {
			t.Fatal("Arch packaging steps must not skip checks inside the gated job")
		}
		if uses := mappingValue(t, step, "uses"); uses != nil {
			switch {
			case strings.HasPrefix(scalar(t, uses), "actions/checkout@"):
				checkout = true
				if got := scalar(t, mappingValue(t, mappingValue(t, step, "with"), "persist-credentials")); got != "false" {
					t.Fatalf("Arch checkout persist-credentials = %q, want false", got)
				}
			case strings.HasPrefix(scalar(t, uses), "actions/setup-go@"):
				setup = true
				if got := scalar(t, mappingValue(t, mappingValue(t, step, "with"), "cache")); got != "false" {
					t.Fatalf("Arch setup-go cache = %q, want false", got)
				}
			case strings.HasPrefix(scalar(t, uses), "actions/upload-artifact@"):
				uploads++
				if !packageStep {
					t.Fatal("Arch upload must follow the package script and its checksum verification")
				}
				assertArchPackageUpload(t, mappingValue(t, step, "with"))
			}
		}
		if run := mappingValue(t, step, "run"); run != nil {
			script := scalar(t, run)
			if strings.Contains(script, "chown -R bridge-ci:bridge-ci \"$GITHUB_WORKSPACE\"") {
				workspace = true
			}
			if strings.Contains(script, "bash scripts/ci-arch-package.sh") {
				packageStep = true
				if !workspace || script != "runuser -u bridge-ci -- env PATH=\"$PATH\" BRIDGE_VERSION=\"$BRIDGE_VERSION\" bash scripts/ci-arch-package.sh" {
					t.Fatal("Arch package build must use the prepared unprivileged workspace")
				}
				if scalar(t, mappingValue(t, mappingValue(t, step, "env"), "BRIDGE_VERSION")) != "${{ inputs.version }}" {
					t.Fatal("Arch package version must be passed as environment data")
				}
			}
		}
	}
	if !checkout || !setup || !workspace || !packageStep || uploads != 1 {
		t.Fatalf("Arch steps incomplete: checkout=%t setup=%t workspace=%t package=%t uploads=%d", checkout, setup, workspace, packageStep, uploads)
	}
}

func assertArchPackageUpload(t *testing.T, with *yaml.Node) {
	t.Helper()
	if scalar(t, mappingValue(t, with, "name")) != "spry-bridge-arch-${{ inputs.version }}" {
		t.Fatal("Arch artifact name must bind the validated version")
	}
	const expected = "dist/arch/PKGBUILD\ndist/arch/spry-bridge-*-src.tar.gz\ndist/arch/spry-ai-workstation-bridge-*.pkg.tar.zst\ndist/arch/SHA256SUMS"
	if got := strings.TrimSpace(scalar(t, mappingValue(t, with, "path"))); got != expected {
		t.Fatalf("Arch artifact paths = %q", got)
	}
	if got := scalar(t, mappingValue(t, with, "if-no-files-found")); got != "error" {
		t.Fatalf("Arch artifact missing-file policy = %q, want error", got)
	}
	if got := scalar(t, mappingValue(t, with, "retention-days")); got != "7" {
		t.Fatalf("Arch artifact retention = %q, want 7 days", got)
	}
}

func assertArchSetupScript(t *testing.T) {
	t.Helper()
	script := readScript(t, "ci-arch-setup.sh")
	assertShellSyntax(t, script)
	for _, required := range []string{
		"archive.archlinux.org/repos/2026/09/07/\\$repo/os/\\$arch",
		"pacman-key --init",
		"pacman-key --populate archlinux",
		"pacman -Syyu --noconfirm --needed",
		"useradd --create-home --shell /usr/bin/bash bridge-ci",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("Arch setup script missing %q", required)
		}
	}
	if strings.Contains(script, "SigLevel = Never") || strings.Contains(script, "--noconfirm --needed --nodeps") {
		t.Fatal("Arch setup must retain signature verification and dependency checks")
	}
}

func assertArchPackageScript(t *testing.T) {
	t.Helper()
	script := readScript(t, "ci-arch-package.sh")
	assertShellSyntax(t, script)
	for _, required := range []string{
		"id -u) == 0",
		"BRIDGE_VERSION:-} =~ ^v(0|[1-9][0-9]*)",
		"make arch-source VERSION=\"$BRIDGE_VERSION\"",
		"sha256sum --check SHA256SUMS",
		"makepkg --verifysource",
		"makepkg --cleanbuild --noconfirm",
		"pacman -Qlp \"$bridge_pkg\"",
		"bsdtar -tf \"$bridge_pkg\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("Arch package script missing %q", required)
		}
	}
	if strings.Contains(script, "--syncdeps") || strings.Contains(script, "--install") || strings.Contains(script, "makepkg -i") {
		t.Fatal("Arch CI package build must not install or synchronise package dependencies")
	}
	firstChecksum := strings.Index(script, "sha256sum --check SHA256SUMS")
	verifySource := strings.Index(script, "makepkg --verifysource")
	if firstChecksum < 0 || firstChecksum > verifySource {
		t.Fatal("Arch package script must verify source checksums before makepkg")
	}
	if strings.LastIndex(script, "sha256sum --check SHA256SUMS") < strings.Index(script, "test -f \"$bridge_pkg\"") {
		t.Fatal("Arch package script must verify final package checksums before upload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", scriptPath(t, "ci-arch-package.sh"))
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "BRIDGE_VERSION=v01.2.3"}
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("Arch package script accepted an invalid version: %s", output)
	}
}

func readScript(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(scriptPath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "scripts", name)
}

func assertShellSyntax(t *testing.T, script string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(file, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/bash", "-n", file).CombinedOutput(); err != nil {
		t.Fatalf("shell syntax: %v: %s", err, output)
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
			if uses := mappingValue(t, node, "uses"); uses != nil {
				value := scalar(t, uses)
				if value != "./.github/workflows/ci.yml" && !pinned.MatchString(value) {
					t.Fatalf("external action must be pinned to a full commit SHA: %q", value)
				}
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

func assertNoSecretReferences(t *testing.T, root *yaml.Node) {
	t.Helper()
	for _, value := range allScalarValues(root) {
		if strings.Contains(strings.ToLower(value), "secrets.") {
			t.Fatalf("CI must not consume repository secrets: %q", value)
		}
	}
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
		if scalar(t, mappingValue(t, with, "name")) != "spry-bridge-${{ inputs.version }}" {
			t.Fatal("archive artifact name must bind the validated version")
		}
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
