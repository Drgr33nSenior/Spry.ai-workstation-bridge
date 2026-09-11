package contract

import (
	"bytes"
	"encoding/json"
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

func TestAdvisoryAccountingContract(t *testing.T) {
	b, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	responses := doc["paths"].(map[string]any)["/api/v1/memory/advice"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)
	for _, status := range []string{"200", "default"} {
		header := responses[status].(map[string]any)["headers"].(map[string]any)["X-Bridge-Advisory-ID"].(map[string]any)
		if header["schema"].(map[string]any)["pattern"] != "^[a-f0-9]{32}$" {
			t.Fatal("missing durable attempt identity contract")
		}
	}
	props := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["advisory"].(map[string]any)["properties"].(map[string]any)
	usage := props["usage"].(map[string]any)["anyOf"].([]any)
	if len(usage) != 2 || usage[1].(map[string]any)["type"] != "null" || props["usage_provisional"] == nil || props["creation_uncertain"] == nil {
		t.Fatal("usage/uncertainty contract lost")
	}
}
