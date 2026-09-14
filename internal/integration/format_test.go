package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the actual Make target with isolated tools, not a second copy of its
// shell logic. Discovery/formatter errors must not become an empty success.
func TestFormatGateFailsClosed(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"clean", true}, {"unformatted", false}, {"formatter-error", false},
		{"discovery-error", false}, {"partial-discovery", false}, {"empty", false},
		{"missing-rg", false}, {"missing-gofmt", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "tools")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(name, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(dir, "Makefile"), string(makefile))
			// The clean case also catches a missing script when the target uses it.
			if script, err := os.ReadFile("../../scripts/check-format.sh"); err == nil {
				if err := os.Mkdir(filepath.Join(dir, "scripts"), 0700); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(dir, "scripts/check-format.sh"), string(script))
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, tool := range []string{"mktemp", "rm", "xargs"} {
				resolved, err := exec.LookPath(tool)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(resolved, filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name != "missing-rg" {
				write(filepath.Join(bin, "rg"), `#!/bin/sh
case "$FORMAT_MODE" in
  discovery-error) exit 17 ;;
  empty) exit 0 ;;
esac
case " $* " in
  *" -0 "*) printf 'ordinary.go\0nested space/file name.go\0' ;;
  *) printf 'ordinary.go\nnested space/file name.go\n' ;;
esac
[ "$FORMAT_MODE" != partial-discovery ] || exit 17
`)
			}
			if tc.name != "missing-gofmt" {
				write(filepath.Join(bin, "gofmt"), `#!/bin/sh
printf 'called\n' >> "$FORMAT_CALLS"
[ "$FORMAT_MODE" != formatter-error ] || exit 19
[ "$FORMAT_MODE" != unformatted ] || { printf 'ordinary.go\n'; exit 0; }
[ "$#" = 3 ] && [ "$1" = -l ] && [ "$2" = ordinary.go ] && [ "$3" = 'nested space/file name.go' ] || exit 21
`)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, makePath, "fmt")
			cmd.Dir = dir
			calls := filepath.Join(dir, "calls")
			cmd.Env = []string{"PATH=" + bin, "TMPDIR=" + dir, "FORMAT_MODE=" + tc.name, "FORMAT_CALLS=" + calls}
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("format gate timed out: %s", out)
			}
			if (err == nil) != tc.ok {
				t.Fatalf("fmt error = %v, want success=%v: %s", err, tc.ok, out)
			}
			if tc.name == "clean" {
				data, err := os.ReadFile(calls)
				if err != nil || string(data) != "called\n" {
					t.Fatalf("format all filenames once: %q, %v", data, err)
				}
			}
			if strings.Contains(tc.name, "discovery") || tc.name == "empty" || tc.name == "missing-rg" {
				if _, err := os.Stat(calls); !os.IsNotExist(err) {
					t.Fatal("formatter ran without a complete nonempty source inventory")
				}
			}
		})
	}
}

func TestNativeFormattingDependencies(t *testing.T) {
	workflow := readWorkflow(t, "ci.yml")
	steps := mappingValue(t, mappingValue(t, mappingValue(t, workflow, "jobs"), "check"), "steps")
	var script string
	for _, step := range steps.Content {
		if id := mappingValue(t, step, "id"); id != nil && scalar(t, id) == "checks" && script == "" {
			t.Fatal("native formatting dependencies must be prepared before checks")
		}
		if name := mappingValue(t, step, "name"); name != nil && scalar(t, name) == "Prepare native source tools" {
			if scalar(t, mappingValue(t, step, "if")) != "${{ matrix.platform != 'arch' }}" || scalar(t, mappingValue(t, mappingValue(t, step, "env"), "BRIDGE_CHECK_PLATFORM")) != "${{ matrix.platform }}" {
				t.Fatal("native dependency setup must use the native matrix identity")
			}
			script = scalar(t, mappingValue(t, step, "run"))
		}
	}
	if script == "" {
		t.Fatal("native source tool setup missing")
	}
	for _, tc := range []struct {
		platform, mode, calls string
		ok                    bool
	}{
		{"ubuntu", "", "apt-get update\napt-get install --no-install-recommends -y ripgrep\n", true},
		{"ubuntu", "update-error", "apt-get update\n", false},
		{"ubuntu", "install-error", "apt-get update\napt-get install --no-install-recommends -y ripgrep\n", false},
		{"macos", "installed", "list --versions ripgrep\n", true},
		{"macos", "", "list --versions ripgrep\ninstall ripgrep\n", true},
		{"macos", "install-error", "list --versions ripgrep\ninstall ripgrep\n", false},
		{"arch", "", "", false},
		{"invalid", "", "", false},
	} {
		t.Run(tc.platform+"/"+tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			// Only fake package tools are on PATH. No installation or privilege
			// escalation occurs; execute the actual workflow fragment unchanged.
			for name, body := range map[string]string{
				"sudo": `printf '%s\n' "$*" >> "$BRIDGE_TEST_CALLS"
case "$*" in
  'apt-get update') [ "$BRIDGE_TEST_MODE" != update-error ] ;;
  'apt-get install --no-install-recommends -y ripgrep') [ "$BRIDGE_TEST_MODE" != install-error ] ;;
  *) exit 99 ;;
esac
`,
				"brew": `printf '%s\n' "$*" >> "$BRIDGE_TEST_CALLS"
case "$*" in
  'list --versions ripgrep') [ "$BRIDGE_TEST_MODE" = installed ] ;;
  'install ripgrep') [ "$BRIDGE_TEST_MODE" != install-error ] ;;
  *) exit 99 ;;
esac
`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nset -eu\n"+body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/bash", "-e", "-o", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = []string{"PATH=" + dir, "BRIDGE_CHECK_PLATFORM=" + tc.platform, "BRIDGE_TEST_MODE=" + tc.mode, "BRIDGE_TEST_CALLS=" + log}
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err == nil) != tc.ok {
				t.Fatalf("native dependency setup: %v, want success=%v: %s", err, tc.ok, out)
			}
			calls, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(calls) != tc.calls {
				t.Fatalf("package tool calls = %q, want %q", calls, tc.calls)
			}
		})
	}
}
