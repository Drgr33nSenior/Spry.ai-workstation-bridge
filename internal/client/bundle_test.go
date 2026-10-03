package client

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func fixtureBundle(t *testing.T) domain.Bundle {
	t.Helper()
	files, err := catalog.NativeBundle(catalog.ClientProfile{Harness: "qwen", BaseURL: "http://127.0.0.1:8000/v1", Model: "Qwen/Qwen3.5-9B", Context: 4096, MaxOutput: 1024})
	if err != nil {
		t.Fatal(err)
	}
	b := domain.Bundle{Harness: "qwen", Revision: "fixture"}
	for path, content := range files {
		sum := sha256.Sum256(content)
		b.Files = append(b.Files, domain.BundleFile{Path: path, Content: string(content), SHA256: hex.EncodeToString(sum[:])})
	}
	return b
}
func TestBundleIntegrityAndNewDirectory(t *testing.T) {
	b := fixtureBundle(t)
	dir := filepath.Join(t.TempDir(), "native")
	if err := ConfigureBundle(b, dir); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureBundle(b, dir); err == nil {
		t.Fatal("overwrote existing native directory")
	}
	for _, f := range b.Files {
		info, err := os.Stat(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("file permissions %o", info.Mode().Perm())
		}
	}
	files, err := readNativeDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.VerifyNative(files, "dsh", "cli"); err == nil {
		t.Fatal("DSH CLI refusal lost")
	}
	for _, path := range []string{"../../escaped", "qwen/other.json"} {
		bad := fixtureBundle(t)
		bad.Files[0].Path = path
		if _, err := VerifyBundle(bad); err == nil {
			t.Fatalf("accepted path %s", path)
		}
	}
	bad := fixtureBundle(t)
	bad.Files[0].Content += "tamper"
	if _, err := VerifyBundle(bad); err == nil {
		t.Fatal("accepted corrupt digest")
	}
}
func TestBundleSemanticTamperAndSymlink(t *testing.T) {
	b := fixtureBundle(t)
	for i := range b.Files {
		if b.Files[i].Path == "qwen/settings.json" {
			b.Files[i].Content = strings.ReplaceAll(b.Files[i].Content, `"approvalMode": "default"`, `"approvalMode": "yolo"`)
			sum := sha256.Sum256([]byte(b.Files[i].Content))
			b.Files[i].SHA256 = hex.EncodeToString(sum[:])
		}
	}
	if _, err := VerifyBundle(b); err == nil {
		t.Fatal("accepted semantic tamper")
	}
	dir := filepath.Join(t.TempDir(), "native")
	if err := ConfigureBundle(fixtureBundle(t), dir); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "qwen", "settings.json")
	if err := os.Rename(f, f+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f+".old", f); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeDirectory(dir); err == nil {
		t.Fatal("accepted native file symlink")
	}
}
func TestLaunchHasFixedArgsAndNoManagementEnvironment(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("launch intentionally requires non-root")
	}
	dir := filepath.Join(t.TempDir(), "native")
	if err := ConfigureBundle(fixtureBundle(t), dir); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("BRIDGE_TOKEN", "test-value-must-not-inherit")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-value-must-not-inherit")
	spec, err := PrepareLaunch(dir, "hermes", "acp")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Arguments) != 1 || spec.Arguments[0] != "acp" {
		t.Fatalf("unexpected launch args: %v", spec.Arguments)
	}
	for _, e := range spec.Environment {
		if strings.Contains(e, "must-not-inherit") {
			t.Fatal("unrelated credential inherited")
		}
	}
	if _, err := PrepareLaunch(dir, "dsh", "cli"); err == nil {
		t.Fatal("unsupported DSH CLI accepted")
	}
}

func TestArtifactDownloadIntegrity(t *testing.T) {
	content := "reviewed maintenance export\n"
	sum := sha256.Sum256([]byte(content))
	artifact := domain.Artifact{Name: "cpu-maintenance.json", Content: content, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
	path := filepath.Join(t.TempDir(), "maintenance.json")
	if err := WriteArtifact(artifact, path); err != nil {
		t.Fatal(err)
	}
	if err := WriteArtifact(artifact, path); err == nil {
		t.Fatal("overwrote existing export")
	}
	artifact.Content += "tamper"
	if err := WriteArtifact(artifact, filepath.Join(t.TempDir(), "bad.json")); err == nil {
		t.Fatal("accepted corrupt artifact")
	}
}
