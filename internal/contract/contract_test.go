package contract

import (
	"bytes"
	"os"
	"regexp"
	"testing"
)

func TestServerRouteCoverage(t *testing.T) {
	b, e := os.ReadFile("../api/server.go")
	if e != nil {
		t.Fatal(e)
	}
	routes := regexp.MustCompile(`(?:secure\(|HandleFunc\()"(GET|POST) ([^"]+)"`).FindAllSubmatch(b, -1)
	found := map[string]bool{}
	for _, r := range routes {
		found[string(r[1])+" "+string(r[2])] = true
	}
	for _, e := range Endpoints {
		key := e.Method + " " + e.Path
		if !found[key] {
			t.Errorf("contract route not registered: %s", key)
		}
		delete(found, key)
	}
	for key := range found {
		t.Errorf("server route missing contract: %s", key)
	}
}
func TestGeneratedContract(t *testing.T) {
	b, e := Generate()
	if e != nil {
		t.Fatal(e)
	}
	old, e := os.ReadFile("../../api/openapi.json")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(old, b) {
		t.Fatal("contract drift; run make generate")
	}
}
