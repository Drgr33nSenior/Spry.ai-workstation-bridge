package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestImmutableInventory(t *testing.T) {
	models := Selected()
	if len(models) != 3 {
		t.Fatal(len(models))
	}
	sizes := []int64{30890049597, 19329393661, 1207489041}
	for i, m := range models {
		if m.Size != sizes[i] || m.License != "apache-2.0" || len(m.Revision) != 40 {
			t.Fatalf("bad inventory %s", m.ID)
		}
		seen := map[string]bool{}
		for _, f := range m.Files {
			if seen[f.Path] || f.Size < 0 {
				t.Fatal(f)
			}
			seen[f.Path] = true
		}
		if !seen["README.md"] || (m.GPUs > 0 && !seen["LICENSE"]) {
			t.Fatal("upstream licensing material missing")
		}
	}
}
func TestLiteralParserRefusesExecution(t *testing.T) {
	for _, text := range []string{"A=$(id)", "A=`id`", "A=x\nA=y", "export A=value", "source file"} {
		if _, e := ParseAssignments(strings.NewReader(text)); e == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	m, e := ParseAssignments(strings.NewReader("# comment\nA=https://example.invalid/path\nB=literal\n"))
	if e != nil || m["B"] != "literal" {
		t.Fatal(m, e)
	}
}
func TestNativeBundleIntegrityAndRefusals(t *testing.T) {
	p := ClientProfile{Harness: "qwen", BaseURL: "http://127.0.0.1:18000/v1", Model: "Qwen3.8-27B-FP8", Context: 32768, MaxOutput: 4096}
	files, e := NativeBundle(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range []string{"qwen", "dsh", "hermes"} {
		mode := "cli"
		if h == "dsh" {
			mode = "acp"
		}
		if _, e = VerifyNative(files, h, mode); e != nil {
			t.Fatal(h, e)
		}
	}
	if _, e = VerifyNative(files, "dsh", "cli"); e == nil {
		t.Fatal("accepted DSH cli")
	}
	for path, b := range files {
		if strings.Contains(string(b), "Bearer ") || strings.Contains(string(b), "kubeconfig") || strings.Contains(string(b), "management_token") {
			t.Fatal("unexpected credential", path)
		}
	}
	var metadata BundleMetadata
	json.Unmarshal(files["bundle.json"], &metadata)
	metadata.Sources["qwen"]["commit"] = strings.Repeat("a", 40)
	files["bundle.json"], _ = json.Marshal(metadata)
	if _, e = VerifyNative(files, "qwen", "cli"); e == nil {
		t.Fatal("accepted altered source pin")
	}
	files, _ = NativeBundle(p)
	var q map[string]any
	json.Unmarshal(files["qwen/settings.json"], &q)
	q["tools"] = map[string]any{"approvalMode": "yolo"}
	files["qwen/settings.json"], _ = json.Marshal(q)
	json.Unmarshal(files["bundle.json"], &metadata)
	hash := sha256.Sum256(files["qwen/settings.json"])
	metadata.ConfigSHA256["qwen"] = hex.EncodeToString(hash[:])
	files["bundle.json"], _ = json.Marshal(metadata)
	if _, e = VerifyNative(files, "qwen", "cli"); e == nil {
		t.Fatal("accepted checksum-adjusted unsafe policy")
	}
}
func TestClientEndpointValidation(t *testing.T) {
	p := ClientProfile{Harness: "qwen", Model: "Qwen3.5-9B", Context: 4096, MaxOutput: 2048}
	for _, url := range []string{"http://192.168.1.10/v1", "https://user:secret@example.invalid/v1", "https://example.invalid/v1?token=x", "https://example.invalid/v1#token", "https://example.invalid:65536/v1", "https://example.invalid/../../v1"} {
		p.BaseURL = url
		if ValidateClient(p) == nil {
			t.Fatal("accepted", url)
		}
	}
	for _, url := range []string{"https://inference.example.invalid/v1", "http://localhost:18000/v1"} {
		p.BaseURL = url
		if e := ValidateClient(p); e != nil {
			t.Fatal(url, e)
		}
	}
}
