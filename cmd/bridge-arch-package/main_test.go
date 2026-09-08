package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCreatesDeterministicVerifiedSourceInputs(t *testing.T) {
	root := fixtureRoot(t)
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	for _, output := range []string{first, second} {
		if err := run(config{root: root, output: output, version: "v1.2.3"}); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	archiveName := "spry-bridge-1.2.3-src.tar.gz"
	firstArchive := mustRead(t, filepath.Join(first, archiveName))
	if got, want := sha256Bytes(firstArchive), sha256Bytes(mustRead(t, filepath.Join(second, archiveName))); got != want {
		t.Fatalf("source archive is not deterministic: %s != %s", got, want)
	}
	firstPackage := string(mustRead(t, filepath.Join(first, "PKGBUILD")))
	if got, want := firstPackage, string(mustRead(t, filepath.Join(second, "PKGBUILD"))); got != want {
		t.Fatal("PKGBUILD is not deterministic")
	}
	if !strings.Contains(firstPackage, "pkgver=1.2.3") || !strings.Contains(firstPackage, "sha256sums=('"+sha256Bytes(firstArchive)+"')") {
		t.Fatalf("PKGBUILD does not bind its version and source checksum:\n%s", firstPackage)
	}
	if strings.Contains(firstPackage, "SKIP") || strings.Contains(firstPackage, "systemctl") || strings.Contains(firstPackage, "license=(") {
		t.Fatalf("PKGBUILD has unsafe package metadata or activation behavior:\n%s", firstPackage)
	}
	if !strings.Contains(firstPackage, "Licence decision pending") {
		t.Fatalf("PKGBUILD must retain the pending owner licence decision:\n%s", firstPackage)
	}
	assertSums(t, first, archiveName)
	entries := archiveEntries(t, firstArchive)
	want := []string{
		packageName + "-1.2.3/.go-version",
		packageName + "-1.2.3/Makefile",
		packageName + "-1.2.3/README.md",
		packageName + "-1.2.3/api/openapi.json",
		packageName + "-1.2.3/cmd/bridged/main.go",
		packageName + "-1.2.3/deployment/systemd/bridged.service",
		packageName + "-1.2.3/docs/OPERATIONS.md",
		packageName + "-1.2.3/internal/api/server.go",
		packageName + "-1.2.3/packaging/arch/PKGBUILD.in",
		packageName + "-1.2.3/scripts/browser-test.mjs",
		packageName + "-1.2.3/.github/workflows/check.yml",
	}
	for _, name := range want {
		if _, ok := entries[name]; !ok {
			t.Errorf("archive is missing %s", name)
		}
	}
	if entries[packageName+"-1.2.3/scripts/browser-test.mjs"].Mode != 0755 {
		t.Fatalf("archive did not preserve executable mode: %#o", entries[packageName+"-1.2.3/scripts/browser-test.mjs"].Mode)
	}
	if _, ok := entries[packageName+"-1.2.3/.idea/workspace.xml"]; ok {
		t.Fatal("archive contains a non-allowlisted path")
	}
	if _, ok := entries[packageName+"-1.2.3/dist/secret"]; ok {
		t.Fatal("archive contains a non-allowlisted path")
	}
}

func TestRunRefusesSymlinkedSource(t *testing.T) {
	root := fixtureRoot(t)
	if err := os.Symlink("main.go", filepath.Join(root, "cmd", "bridged", "linked.go")); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	err := run(config{root: root, output: filepath.Join(t.TempDir(), "out"), version: "v1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "symlink refused") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}

func TestParseVersionRejectsNonStableTag(t *testing.T) {
	for _, version := range []string{"", "1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc1", "v1.2.3\n$(bad)"} {
		if _, err := parseVersion(version); err == nil {
			t.Errorf("parseVersion(%q) unexpectedly succeeded", version)
		}
	}
	if got, err := parseVersion("v0.0.0"); err != nil || got != "0.0.0" {
		t.Fatalf("parseVersion valid version = %q, %v", got, err)
	}
}

func TestRenderTemplateRequiresExactlyOneOfEachRequiredPlaceholder(t *testing.T) {
	valid := "pkgver=@PKGVER@\nsource=('@SOURCE_ARCHIVE@')\nsha256sums=('@SOURCE_SHA256@')\n"
	for _, template := range []string{
		strings.Replace(valid, "@PKGVER@", "", 1),
		strings.Replace(valid, "@SOURCE_ARCHIVE@", "@SOURCE_ARCHIVE@ @SOURCE_ARCHIVE@", 1),
		strings.Replace(valid, "@SOURCE_SHA256@", "tampered", 1),
	} {
		if _, err := renderTemplate(template, "1.2.3", "spry-bridge-1.2.3-src.tar.gz", strings.Repeat("a", 64)); err == nil {
			t.Fatalf("renderTemplate accepted a missing or duplicated required placeholder: %q", template)
		}
	}
}

func TestPackageTemplateHasReviewedPayloadOnly(t *testing.T) {
	template := string(mustRead(t, filepath.Join("..", "..", "packaging", "arch", "PKGBUILD.in")))
	for _, expected := range []string{
		"arch=('x86_64')",
		"GOTOOLCHAIN=local",
		"go mod verify",
		"-mod=readonly",
		"$pkgdir/usr/lib/bridge/bridged",
		"$pkgdir/usr/bin/bridgectl",
		"$pkgdir/usr/lib/systemd/system/bridged.service",
		"$pkgdir/usr/lib/sysusers.d/bridge.conf",
		"$pkgdir/usr/lib/tmpfiles.d/bridge.conf",
		"$pkgdir/usr/share/doc/$pkgname/docs/${document##*/}",
		"$pkgdir/usr/share/$pkgname/examples/",
		"Licence decision pending",
	} {
		if !strings.Contains(template, expected) {
			t.Errorf("PKGBUILD template is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"SKIP", "systemctl", ".INSTALL", "$pkgdir/etc/", "$pkgdir/var/", "license=(", "workstation-runtime", "workstation-reference"} {
		if strings.Contains(template, forbidden) {
			t.Errorf("PKGBUILD template contains forbidden %q", forbidden)
		}
	}
}

func TestPackageFunctionInstallsOnlyReviewedPayloadOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("NOT RUN — package fixture requires GNU install on Linux")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("NOT RUN — Bash unavailable")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	srcdir := filepath.Join(work, "src")
	sourceDir := filepath.Join(srcdir, packageName+"-1.2.3")
	pkgdir := filepath.Join(work, "pkg")
	for _, binary := range []string{"bridged", "bridgectl", "bridge-hostd", "bridge-worker"} {
		writeFixture(t, sourceDir, binary, "fixture binary\n")
		if err := os.Chmod(filepath.Join(sourceDir, binary), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range packageFixtureFiles(t, repoRoot) {
		copyFixtureFile(t, repoRoot, sourceDir, relative)
	}
	template := string(mustRead(t, filepath.Join(repoRoot, "packaging", "arch", "PKGBUILD.in")))
	packageScript, err := renderTemplate(template, "1.2.3", "spry-bridge-1.2.3-src.tar.gz", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(work, "PKGBUILD")
	if err = os.WriteFile(scriptPath, []byte(packageScript), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bash, "-c", `set -euo pipefail; source "$1"; srcdir="$2"; pkgdir="$3"; package`, "bash", scriptPath, srcdir, pkgdir)
	if output, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("fixture package() failed: %v\n%s", runErr, output)
	}
	expected := expectedPackageFiles(t, repoRoot)
	actual := packageFiles(t, pkgdir)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("package payload mismatch\nactual: %#v\nexpected: %#v", actual, expected)
	}
	link := filepath.Join(pkgdir, "usr", "bin", "bridgectl")
	if destination, err := os.Readlink(link); err != nil || destination != "../lib/bridge/bridgectl" {
		t.Fatalf("bridgectl link = %q, %v", destination, err)
	}
	for _, forbidden := range []string{"etc", "var", ".INSTALL", "usr/lib/bridge/workstation-runtime", "usr/lib/bridge/workstation-reference"} {
		if _, err := os.Lstat(filepath.Join(pkgdir, forbidden)); !os.IsNotExist(err) {
			t.Errorf("package created forbidden path %q: %v", forbidden, err)
		}
	}
}

func packageFixtureFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	files := []string{
		"README.md",
		"api/openapi.json",
		"deployment/systemd/bridged.service",
		"deployment/systemd/bridge-hostd.service",
		"deployment/systemd/bridge-hostd.socket",
		"deployment/systemd/bridge-worker.service",
		"deployment/systemd/bridge-worker.socket",
		"deployment/systemd/bridge.sysusers",
		"deployment/systemd/bridge.tmpfiles",
		"deployment/server.local.example.json",
		"deployment/server.vpn.example.json",
		"deployment/management.initial.example.json",
		"deployment/host-policy.example.json",
		"deployment/worker-policy.example.json",
		"deployment/session-policy.conf.example",
		"deployment/kubernetes-rbac.yaml",
	}
	docs, err := filepath.Glob(filepath.Join(repoRoot, "docs", "*.md"))
	if err != nil || len(docs) == 0 {
		t.Fatalf("find package documentation: %v", err)
	}
	for _, document := range docs {
		files = append(files, filepath.ToSlash(filepath.Join("docs", filepath.Base(document))))
	}
	return files
}

func copyFixtureFile(t *testing.T, sourceRoot, destinationRoot, relative string) {
	t.Helper()
	contents := mustRead(t, filepath.Join(sourceRoot, relative))
	writeFixture(t, destinationRoot, relative, string(contents))
}

func expectedPackageFiles(t *testing.T, repoRoot string) map[string]fs.FileMode {
	t.Helper()
	expected := map[string]fs.FileMode{}
	for _, binary := range []string{"bridged", "bridgectl", "bridge-hostd", "bridge-worker"} {
		expected["usr/lib/bridge/"+binary] = 0755
	}
	expected["usr/bin/bridgectl"] = os.ModeSymlink
	for _, service := range []string{"bridged.service", "bridge-hostd.service", "bridge-hostd.socket", "bridge-worker.service", "bridge-worker.socket"} {
		expected["usr/lib/systemd/system/"+service] = 0644
	}
	expected["usr/lib/sysusers.d/bridge.conf"] = 0644
	expected["usr/lib/tmpfiles.d/bridge.conf"] = 0644
	expected["usr/share/doc/"+packageName+"/README.md"] = 0644
	expected["usr/share/doc/"+packageName+"/api/openapi.json"] = 0644
	for _, relative := range packageFixtureFiles(t, repoRoot) {
		if strings.HasPrefix(relative, "docs/") {
			expected["usr/share/doc/"+packageName+"/docs/"+filepath.Base(relative)] = 0644
		}
	}
	for _, example := range []string{
		"server.local.example.json",
		"server.vpn.example.json",
		"management.initial.example.json",
		"host-policy.example.json",
		"worker-policy.example.json",
		"session-policy.conf.example",
		"kubernetes-rbac.yaml",
	} {
		expected["usr/share/"+packageName+"/examples/"+example] = 0644
	}
	return expected
}

func packageFiles(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()
	files := map[string]fs.FileMode{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		mode := info.Mode() & (os.ModeType | 0777)
		if mode&os.ModeSymlink != 0 {
			mode = os.ModeSymlink
		}
		files[filepath.ToSlash(relative)] = mode
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range sourceFiles {
		writeFixture(t, root, file, "fixture\n")
	}
	for _, path := range []string{
		"cmd/bridged/main.go",
		"internal/api/server.go",
		"api/openapi.json",
		"deployment/systemd/bridged.service",
		"docs/OPERATIONS.md",
		"scripts/browser-test.mjs",
		"packaging/arch/PKGBUILD.in",
		".github/workflows/check.yml",
		".idea/workspace.xml",
		"dist/secret",
	} {
		contents := "fixture\n"
		if path == "packaging/arch/PKGBUILD.in" {
			contents = "pkgver=@PKGVER@\nsource=('@SOURCE_ARCHIVE@')\nsha256sums=('@SOURCE_SHA256@')\n# Licence decision pending\n"
		}
		writeFixture(t, root, path, contents)
	}
	if err := os.Chmod(filepath.Join(root, "scripts", "browser-test.mjs"), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func sha256Bytes(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func assertSums(t *testing.T, output, archiveName string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(string(mustRead(t, filepath.Join(output, "SHA256SUMS")))), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("invalid SHA256SUMS entry %q", line)
		}
		if got, want := sha256Bytes(mustRead(t, filepath.Join(output, fields[1]))), fields[0]; got != want {
			t.Fatalf("checksum for %s = %s, want %s", fields[1], got, want)
		}
	}
	if !strings.Contains(string(mustRead(t, filepath.Join(output, "SHA256SUMS"))), archiveName) {
		t.Fatalf("SHA256SUMS does not include %s", archiveName)
	}
}

func archiveEntries(t *testing.T, contents []byte) map[string]tar.Header {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	entries := map[string]tar.Header{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		if !header.ModTime.Equal(time.Unix(0, 0)) || header.Typeflag != tar.TypeReg {
			t.Fatalf("archive header %s is not deterministic regular data", header.Name)
		}
		entries[header.Name] = *header
	}
}
