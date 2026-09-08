package client

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func credential(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("generated-test-credential\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEndpointAndCredentialBoundaries(t *testing.T) {
	path := credential(t)
	for _, endpoint := range []string{"http://192.168.1.1", "http://metadata.google.internal", "https://owner:secret@example.com", "https://example.com/path", "https://example.com?credential=secret", "ftp://127.0.0.1"} {
		if _, err := New(Config{Endpoint: endpoint, CredentialFile: path}); err == nil {
			t.Errorf("accepted unsafe endpoint %s", endpoint)
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(path); err == nil {
		t.Error("accepted world-readable credential")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(link); err == nil {
		t.Error("accepted symlink credential")
	}
}

func TestBearerWithoutBrowserHeadersAndErrorRedaction(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer generated-test-credential" {
			t.Error("missing bearer")
		}
		if r.Header.Get("Origin") != "" {
			t.Error("CLI sent browser origin")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"refused generated-test-credential"}}`))
	}))
	defer s.Close()
	c, err := New(Config{Endpoint: s.URL, CredentialFile: credential(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Do(context.Background(), "GET", "/api/v1/status", "", nil, nil)
	if err == nil || strings.Contains(err.Error(), "generated-test-credential") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error not safely returned: %v", err)
	}
}

func TestRedirectNeverForwardsCredential(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/api/v1/status", 302)
	}))
	defer s.Close()
	c, err := New(Config{Endpoint: s.URL, CredentialFile: credential(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Do(context.Background(), "GET", "/api/v1/status", "", nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if called {
		t.Fatal("redirect destination contacted")
	}
}

func TestTLSIdentityVerification(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer s.Close()
	path := credential(t)
	c, err := New(Config{Endpoint: s.URL, CredentialFile: path})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Do(context.Background(), "GET", "/api/v1/status", "", nil, nil); err == nil {
		t.Fatal("untrusted server accepted")
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	trusted, err := New(Config{Endpoint: s.URL, CredentialFile: path, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	defer trusted.Close()
	var result map[string]bool
	if err := trusted.Do(context.Background(), "GET", "/api/v1/status", "", nil, &result); err != nil {
		t.Fatal(err)
	}
	if !result["ok"] {
		t.Fatal("missing response")
	}
}

func TestContextRejectsUnknownAndTrailingData(t *testing.T) {
	for _, body := range []string{`{"endpoint":"http://127.0.0.1","insecure":true}`, `{} {}`} {
		p := filepath.Join(t.TempDir(), "context.json")
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(p); err == nil {
			t.Fatalf("accepted context %s", body)
		}
	}
}
