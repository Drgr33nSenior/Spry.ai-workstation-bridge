//go:build linux

package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
)

const (
	stagingReaderUID = 21341
	stagingReaderGID = 21342
)

// This opt-in test is an OS qualification, not a fixture claim. It uses only a
// disposable directory and synthetic numeric identity in an isolated Linux
// container. Never run it against the managed workstation model root.
func TestStagingCrossUIDReaderQualification(t *testing.T) {
	if os.Getenv("BRIDGE_STAGING_CROSS_UID") != "1" {
		t.Skip("NOT RUN — set BRIDGE_STAGING_CROSS_UID=1 as root in a disposable Linux environment")
	}
	if os.Geteuid() != 0 {
		t.Skip("NOT RUN — cross-UID qualification requires a disposable root-owned test directory")
	}
	parent, err := os.MkdirTemp("/tmp", "bridge-staging-reader-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	if err = os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "models")
	if err = os.Mkdir(root, 0750); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(root, 0, stagingReaderGID); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, os.ModeSetgid|0750); err != nil {
		t.Fatal(err)
	}
	s, err := NewStager(root, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("cross uid nested staging fixture")
	digest := sha256.Sum256(body)
	m := catalog.Model{ID: "reader-fixture", Repository: "Qwen/reader-fixture", Revision: strings.Repeat("a", 40), Files: []catalog.File{{Path: "nested/weights.safetensors", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(digest[:])}}}
	s.free = func(string) (int64, error) { return 2 << 30, nil }
	s.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	})}
	if _, err = s.Stage(context.Background(), m, nil); err != nil {
		t.Fatal(err)
	}
	partial := m
	partial.Revision = strings.Repeat("b", 40)
	if _, err = s.Stage(context.Background(), partial, func(string, int64) error { return context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	partialName := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".partial-") {
			partialName = entry.Name()
			break
		}
	}
	if partialName == "" {
		t.Fatal("missing retained private partial")
	}
	published, _ := modelPrefix(m)
	command := exec.Command(os.Args[0], "-test.run=^TestStagingCrossUIDReaderQualificationHelper$")
	command.Env = append(os.Environ(),
		"BRIDGE_STAGING_CROSS_UID_HELPER=1",
		"BRIDGE_STAGING_PUBLISHED="+filepath.Join(root, published, "nested", "weights.safetensors"),
		"BRIDGE_STAGING_PARTIAL="+filepath.Join(root, partialName, "snapshot", "nested", "weights.safetensors"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("cross-UID reader qualification: %v\n%s", err, output)
	}
}

func TestStagingCrossUIDReaderQualificationHelper(t *testing.T) {
	if os.Getenv("BRIDGE_STAGING_CROSS_UID_HELPER") != "1" {
		return
	}
	if err := syscall.Setgroups([]int{stagingReaderGID}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setgid(stagingReaderGID); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setuid(stagingReaderUID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(os.Getenv("BRIDGE_STAGING_PUBLISHED")); err != nil {
		t.Fatalf("reader group cannot read nested published model file: %v", err)
	}
	if _, err := os.ReadFile(os.Getenv("BRIDGE_STAGING_PARTIAL")); err == nil || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("reader group reached private partial: %v", err)
	}
}
