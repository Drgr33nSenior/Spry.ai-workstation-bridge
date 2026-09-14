package performance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testExperimentalKV(t *testing.T, observed string, binary *string) ExperimentalKVDescriptor {
	t.Helper()
	c := ExperimentalKVConfiguration{GPUArch: "gfx1201", TensorParallel: 1, ComputeDType: "float16", Attention: "causal-full", HeadDimension: 128, KVHeads: 8, QueryHeads: 32, PageSize: 1, Rotation: "post-rope-qk"}
	hash, err := canonicalExperimentalKVConfiguration(c)
	if err != nil {
		t.Fatal(err)
	}
	return ExperimentalKVDescriptor{Schema: 1, Runtime: "sglang", SourceRepository: "sgl-project/sglang", SourceRevision: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64), PatchSHA256: strings.Repeat("c", 64), BinarySHA256: binary, CompilerRevision: "clang 20.1.0", DependenciesSHA256: strings.Repeat("d", 64), Codec: "ultraquant-rdna4-v1", ConfigurationSHA256: hash, Configuration: c, RequestedMode: "ultraquant-rdna4-v1", ObservedMode: observed, Qualification: "unqualified", RestartRequired: true}
}

func TestExperimentalKVDescriptorStrictBoundary(t *testing.T) {
	binary := strings.Repeat("e", 64)
	for _, tc := range []struct {
		name     string
		observed string
		binary   *string
		measured bool
		valid    bool
	}{
		{"source only", "not-run", nil, false, true},
		{"observed", "compressed-gpu", &binary, true, true},
		{"source cannot be measured", "not-run", nil, true, false},
		{"mapped binary required", "compressed-gpu", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExperimentalKVDescriptor(testExperimentalKV(t, tc.observed, tc.binary), tc.measured)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}

	bad := testExperimentalKV(t, "compressed-gpu", &binary)
	bad.Configuration.QueryHeads = 30
	if ValidateExperimentalKVDescriptor(bad, true) == nil {
		t.Fatal("invalid GQA ratio accepted")
	}
	bad = testExperimentalKV(t, "compressed-gpu", &binary)
	bad.Configuration.GPUArch = "gfx1100"
	if ValidateExperimentalKVDescriptor(bad, true) == nil {
		t.Fatal("non-gfx1201 descriptor accepted")
	}
	bad = testExperimentalKV(t, "compressed-gpu", &binary)
	bad.ConfigurationSHA256 = strings.Repeat("0", 64)
	if ValidateExperimentalKVDescriptor(bad, true) == nil {
		t.Fatal("configuration hash mismatch accepted")
	}
}

func TestDecodeExperimentalKVDescriptorRejectsNullAndUnknownFields(t *testing.T) {
	binary := strings.Repeat("e", 64)
	d := testExperimentalKV(t, "compressed-gpu", &binary)
	data, err := json.Marshal(map[string]any{"experimental_kv": d})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeExperimentalKVDescriptor(data, true)
	if err != nil || got == nil || got.ConfigurationSHA256 != d.ConfigurationSHA256 {
		t.Fatalf("round trip: %#v %v", got, err)
	}
	if _, err = DecodeExperimentalKVDescriptor([]byte(`{"experimental_kv":null}`), false); err == nil {
		t.Fatal("null descriptor accepted")
	}
	data = []byte(strings.Replace(string(data), `"schema":1`, `"schema":1,"extra":true`, 1))
	if _, err = DecodeExperimentalKVDescriptor(data, true); err == nil {
		t.Fatal("unknown descriptor field accepted")
	}
}

func TestVerifyBindsExperimentalKVOnlyThroughTypedComparisonSources(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	binary := strings.Repeat("e", 64)
	write("spec.json", map[string]any{"schema": 1, "kind": "comparison", "baseline_bundle": "baseline", "candidate_bundle": "candidate", "policy_json": "policy.json", "declared_variables": []string{"kv_cache"}})
	write("policy.json", map[string]any{"schema": 1})
	write("baseline/manifest.json", map[string]any{"sources": map[string]any{"runtime": "runtime.json"}})
	write("candidate/manifest.json", map[string]any{"sources": map[string]any{"runtime": "runtime.json"}})
	write("baseline/runtime.json", map[string]any{"schema": 1})
	write("candidate/runtime.json", map[string]any{"experimental_kv": testExperimentalKV(t, "compressed-gpu", &binary)})
	files := map[string]string{}
	for _, name := range []string{"spec.json", "policy.json", "baseline/manifest.json", "baseline/runtime.json", "candidate/manifest.json", "candidate/runtime.json"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = Sum(data)
	}
	manifest, err := json.Marshal(Manifest{Schema: 1, Kind: "comparison", Target: "fixture", SourceRevision: strings.Repeat("a", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(context.Background(), root, Sum(manifest)); err != nil {
		t.Fatal(err)
	}
	// A sealed but incomplete ordinary comparison spec stays available for the
	// canonical installer to classify. The optional candidate fence is not a
	// replacement comparison parser.
	write("spec.json", map[string]any{})
	data, err := os.ReadFile(filepath.Join(root, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	files["spec.json"] = Sum(data)
	manifest, err = json.Marshal(Manifest{Schema: 1, Kind: "comparison", Target: "fixture", SourceRevision: strings.Repeat("a", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(context.Background(), root, Sum(manifest)); err != nil {
		t.Fatal(err)
	}
	write("spec.json", map[string]any{"schema": 1, "kind": "comparison", "baseline_bundle": "baseline", "candidate_bundle": "candidate", "policy_json": "policy.json", "declared_variables": []string{"kv_cache"}})
	data, err = os.ReadFile(filepath.Join(root, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	files["spec.json"] = Sum(data)
	// Existing sealed but incomplete ordinary evidence remains an installer
	// refusal/report input. The optional candidate fence must not convert it
	// into a Bridge verification error merely because it cannot parse runtime.
	write("candidate/runtime.json", []any{})
	data, err = os.ReadFile(filepath.Join(root, "candidate/runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	files["candidate/runtime.json"] = Sum(data)
	manifest, err = json.Marshal(Manifest{Schema: 1, Kind: "comparison", Target: "fixture", SourceRevision: strings.Repeat("a", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(context.Background(), root, Sum(manifest)); err != nil {
		t.Fatal(err)
	}
	// A candidate backend request with no descriptor cannot become an inferred
	// compressed execution record through the sealed Bridge boundary, even when
	// the replacement file and outer manifest are internally self-consistent.
	write("candidate/runtime.json", map[string]any{"launch": []map[string]string{
		{"--attention-backend": "ordinary"}, {"--attention-backend": "ultraquant-rdna4-v1"},
	}})
	data, err = os.ReadFile(filepath.Join(root, "candidate/runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	files["candidate/runtime.json"] = Sum(data)
	manifest, err = json.Marshal(Manifest{Schema: 1, Kind: "comparison", Target: "fixture", SourceRevision: strings.Repeat("a", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(context.Background(), root, Sum(manifest)); err == nil {
		t.Fatal("requested candidate without descriptor accepted")
	}
}
