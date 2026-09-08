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
	"path/filepath"
	"strings"
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
	if _, e = s.Stage(context.Background(), m, nil); e == nil {
		t.Fatal("overwrote ready revision")
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
