package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureStager(t *testing.T, body []byte) (*Stager, catalog.Model) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, os.ModeSetgid|0750); e != nil {
		t.Fatal(e)
	}
	s, e := NewStager(root, 1<<30)
	if e != nil {
		t.Fatal(e)
	}
	s.free = func(string) (int64, error) { return 2 << 30, nil }
	sum := sha256.Sum256(body)
	m := catalog.Model{ID: "test-model", Repository: "Qwen/test-model", Revision: strings.Repeat("a", 40), Files: []catalog.File{{Path: "weights.safetensors", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(sum[:])}}}
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credential sent")
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	return s, m
}
func TestStageVerifyAtomicAndCorrupt(t *testing.T) {
	s, m := fixtureStager(t, bytes.Repeat([]byte("fixture"), 8192))
	progress := int64(0)
	out, e := s.Stage(context.Background(), m, func(_ string, n int64) error { progress = n; return nil })
	if e != nil || len(out) != 1 || progress == 0 {
		t.Fatal(out, e)
	}
	if _, e = s.Verify(context.Background(), m); e != nil {
		t.Fatal(e)
	}
	published, _ := modelPrefix(m)
	info, e := os.Stat(filepath.Join(s.root, published))
	if e != nil || info.Mode().Perm() != 0750 {
		t.Fatal("published traversal mode", info, e)
	}
	fileInfo, e := os.Stat(filepath.Join(s.root, published, "weights.safetensors"))
	if e != nil || fileInfo.Mode().Perm() != 0640 {
		t.Fatal("published reader mode", fileInfo, e)
	}
	if repaired, repairErr := s.Stage(context.Background(), m, nil); repairErr != nil || len(repaired) != 1 {
		t.Fatal("repeat stage did not use bounded verified repair", repaired, repairErr)
	}
	prefix, _ := modelPrefix(m)
	if e = os.WriteFile(filepath.Join(s.root, prefix, "weights.safetensors"), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Verify(context.Background(), m); e == nil {
		t.Fatal("accepted corruption")
	}
}
func TestNoPublicationOnFault(t *testing.T) {
	for _, kind := range []string{"digest", "disconnect", "full", "path", "symlink", "budget", "short"} {
		t.Run(kind, func(t *testing.T) {
			s, m := fixtureStager(t, []byte("test data"))
			switch kind {
			case "digest":
				m.Files[0].Digest = strings.Repeat("0", 64)
			case "full":
				s.free = func(string) (int64, error) { return 1, nil }
			case "budget":
				s.budget = 1
			case "path":
				m.Files[0].Path = "../escape"
			case "symlink":
				os.Symlink(t.TempDir(), filepath.Join(s.root, m.ID))
			case "short":
				m.Files[0].Size++
			case "disconnect":
				s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection interrupted") })
			}
			if _, e := s.Stage(context.Background(), m, nil); e == nil {
				t.Fatal("expected refusal")
			}
			p, _ := modelPrefix(m)
			if _, e := os.Stat(filepath.Join(s.root, p, ".bridge-receipt.json")); e == nil {
				t.Fatal("published failed transfer")
			}
		})
	}
}
func TestCancellationRetainsPartialWithoutReady(t *testing.T) {
	s, m := fixtureStager(t, []byte("fixture"))
	if _, e := s.Stage(context.Background(), m, func(string, int64) error { return context.Canceled }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	p, _ := modelPrefix(m)
	if _, e := os.Stat(filepath.Join(s.root, p)); !os.IsNotExist(e) {
		t.Fatal("published cancelled output")
	}
	usage := CacheUsage("models", s.root, 1<<30)
	if len(usage.CleanupPreview) != 1 || usage.UsedBytes == 0 {
		t.Fatal(usage)
	}
}

func TestPublicationFailureRetainsVerifiedPrivateSnapshot(t *testing.T) {
	s, m := fixtureStager(t, []byte("fixture"))
	s.rename = func(*os.Root, string, string) error { return errors.New("injected atomic publication failure") }
	if _, err := s.Stage(context.Background(), m, nil); err == nil {
		t.Fatal("published despite injected rename failure")
	}
	prefix, _ := modelPrefix(m)
	if _, err := os.Lstat(filepath.Join(s.root, prefix)); err == nil {
		t.Fatal("published revision exists after failed rename")
	}
	assertPrivatePartial(t, s.root)
	entries, err := os.ReadDir(s.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".partial-") {
			if _, err = os.Lstat(filepath.Join(s.root, entry.Name(), "snapshot", ".bridge-receipt.json")); err != nil {
				t.Fatalf("verified receipt was not retained below private partial: %v", err)
			}
			return
		}
	}
	t.Fatal("missing partial snapshot")
}

// The bridge service uses UMask=0077. Run these cases in children so changing
// the process-wide umask cannot race unrelated package tests.
func TestStagingPublishedPermissionsUnderServiceUmask(t *testing.T) {
	for _, testUmask := range []string{"0022", "0077"} {
		t.Run(testUmask, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestStagingPublishedPermissionsUnderServiceUmaskHelper$")
			command.Env = append(os.Environ(), "BRIDGE_STAGING_TEST_UMASK="+testUmask)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("subprocess umask %s: %v\n%s", testUmask, err, output)
			}
		})
	}
}

func TestStagingPublishedPermissionsUnderServiceUmaskHelper(t *testing.T) {
	raw := os.Getenv("BRIDGE_STAGING_TEST_UMASK")
	if raw == "" {
		return
	}
	value, err := strconv.ParseInt(raw, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Umask(int(value))

	body := []byte("nested fixture model")
	s, m := fixtureStager(t, body)
	digest := sha256.Sum256(body)
	m.Files = []catalog.File{
		{Path: "nested/one/weights.safetensors", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(digest[:])},
		{Path: "tokenizer.json", Size: int64(len(body)), Algorithm: "sha256", Digest: hex.EncodeToString(digest[:])},
	}
	// A previous service version can have created the model-ID parent under the
	// restrictive service umask. Staging must repair this one canonical parent.
	if err = os.Mkdir(filepath.Join(s.root, m.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(s.root, m.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Stage(context.Background(), m, nil); err != nil {
		t.Fatal(err)
	}
	assertPublishedModes(t, s.root, m)
	if _, err = s.Verify(context.Background(), m); err != nil {
		t.Fatal(err)
	}

	// A failed transfer retains data below an owner-only .partial-* parent even
	// though successful snapshots are group-readable after publication.
	partial := m
	partial.Revision = strings.Repeat("b", 40)
	if _, err = s.Stage(context.Background(), partial, func(string, int64) error { return context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertPrivatePartial(t, s.root)

	// A conflict after download/receipt generation cannot publish a partial
	// snapshot. The test creates only the exact model-ID path during the staged
	// progress callback; it does not use an arbitrary target path.
	conflict := m
	conflict.ID = "conflict-model"
	conflict.Revision = strings.Repeat("c", 40)
	created := false
	if _, err = s.Stage(context.Background(), conflict, func(string, int64) error {
		if created {
			return nil
		}
		created = true
		return os.WriteFile(filepath.Join(s.root, conflict.ID), []byte("block publication"), 0600)
	}); err == nil {
		t.Fatal("published despite final model-ID conflict")
	}
	prefix, _ := modelPrefix(conflict)
	if _, err = os.Lstat(filepath.Join(s.root, prefix)); err == nil {
		t.Fatal("conflicting model revision published")
	}
	assertPrivatePartial(t, s.root)

	// Repair is deliberately bounded to the verified model-ID/revision snapshot.
	// It repairs the exact manifest tree and receipt, then a repeat stage remains
	// a refusal rather than an overwrite.
	published, _ := modelPrefix(m)
	for _, rel := range []string{"", "nested", "nested/one"} {
		if err = os.Chmod(filepath.Join(s.root, published, rel), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Chmod(filepath.Join(s.root, m.ID), 0700); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"nested/one/weights.safetensors", "tokenizer.json", ".bridge-receipt.json"} {
		if err = os.Chmod(filepath.Join(s.root, published, rel), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Verify(context.Background(), m); err == nil {
		t.Fatal("verified unreadable published snapshot")
	}
	if _, err = s.RepairPublishedPermissions(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	assertPublishedModes(t, s.root, m)
	if repaired, repairErr := s.Stage(context.Background(), m, nil); repairErr != nil || len(repaired) != len(m.Files) {
		t.Fatal("repeat staging did not use bounded verified repair", repaired, repairErr)
	}
	if err = os.WriteFile(filepath.Join(s.root, published, ".bridge-receipt.json"), []byte("not a receipt"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepairPublishedPermissions(context.Background(), m); err == nil {
		t.Fatal("repaired a snapshot without a verified receipt")
	}
}

func assertPublishedModes(t *testing.T, root string, m catalog.Model) {
	t.Helper()
	prefix, err := modelPrefix(m)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	rootOwner, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("cannot inspect model root group")
	}
	for _, rel := range []string{m.ID, prefix, prefix + "/nested", prefix + "/nested/one"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0750 || info.Mode()&os.ModeSetgid == 0 {
			t.Fatalf("published directory %s has mode %v: %v", rel, info, err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Gid != rootOwner.Gid {
			t.Fatalf("published directory %s lost the reviewed reader group", rel)
		}
	}
	for _, rel := range []string{prefix + "/nested/one/weights.safetensors", prefix + "/tokenizer.json", prefix + "/.bridge-receipt.json"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0640 {
			t.Fatalf("published file %s has mode %v: %v", rel, info, err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Gid != rootOwner.Gid {
			t.Fatalf("published file %s lost the reviewed reader group", rel)
		}
	}
}

func assertPrivatePartial(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".partial-") {
			continue
		}
		found = true
		info, err := entry.Info()
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("partial %s is not owner-only: %v %v", entry.Name(), info, err)
		}
	}
	if !found {
		t.Fatal("expected retained private partial")
	}
}
func TestDownloadDestinationAndCredentials(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "100.100.100.200", "192.0.2.1", "::1", "fc00::1"} {
		if publicAddress(net.ParseIP(addr)) {
			t.Fatal("allowed reserved IP", addr)
		}
	}
	if !publicAddress(net.ParseIP("1.1.1.1")) {
		t.Fatal("public address refused")
	}
	client := downloadClient()
	for _, raw := range []string{"http://huggingface.co/model", "https://169.254.169.254/model", "https://evil.huggingface.co/model", "https://user:pass@huggingface.co/model"} {
		u, _ := url.Parse(raw)
		if client.CheckRedirect(&http.Request{URL: u, Header: http.Header{}}, nil) == nil {
			t.Fatal("allowed redirect", raw)
		}
	}
	u, _ := url.Parse("https://cas-bridge.xethub.hf.co/model?reviewed-signed-redirect=opaque")
	r := &http.Request{URL: u, Header: http.Header{"Authorization": []string{"never-forward"}, "Cookie": []string{"never-forward"}}}
	if e := client.CheckRedirect(r, nil); e != nil {
		t.Fatal(e)
	}
	if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
		t.Fatal("forwarded credentials")
	}
}
