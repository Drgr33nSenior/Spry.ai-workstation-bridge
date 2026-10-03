package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedAssetsAndPolicy(t *testing.T) {
	s := httptest.NewServer(Handler())
	defer s.Close()
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		resp, err := s.Client().Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(b) == 0 {
			t.Fatalf("missing asset %s", path)
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing CSP")
		}
		if strings.Contains(string(b), "localStorage") {
			t.Fatal("browser token persistence added")
		}
	}
	for _, path := range []string{"/assets/", "/index.html", "/unknown"} {
		resp, err := s.Client().Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("unexpected route %s", path)
		}
	}
}
