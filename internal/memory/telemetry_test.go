package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTelemetryEvidenceSchemasAndBounds(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "telemetry"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(v any) Manifest {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, "telemetry", "evidence.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		return Manifest{Files: map[string]string{"telemetry/evidence.json": Sum(b)}}
	}
	valid := map[string]any{
		"schema": 2, "status": "generated-not-deployed", "hardware_qualification": "NOT RUN", "enabled": true,
		"profile": "metrics", "gpu_exporter": false, "sglang_trace": false, "kubelet": false,
		"node_name": "fixture", "api_address": "10.0.0.1", "workstation_address": "10.0.0.2",
		"reserve_mib": 6144, "stack_limit_mib": 2944,
		"component_limits_mib": map[string]int64{"cluster_stack_mib": 2944, "host_alloy_mib": 512, "hardware_sampler_mib": 128},
		"margin_mib":           768, "calculated_allowance_mib": 4352, "planned_workloads": []string{"sglang"},
		"workload_overlay": "apps/overlays/dual-gpu", "stack_images": []string{}, "workload_images": []string{},
		"source_identity": map[string]any{}, "source_sha256": map[string]string{},
	}
	m := write(valid)
	a, err := Telemetry(root, m)
	if err != nil || a.Profile != "metrics" || a.ReserveMiB != 6144 || a.CalculatedAllowanceMiB != 4352 || a.ComponentLimitsMiB["host_alloy_mib"] != 512 {
		t.Fatalf("valid schema 2 telemetry refused: %#v %v", a, err)
	}
	legacy := map[string]any{"schema": 1, "status": "generated-not-deployed", "enabled": true, "reserve_mib": 6144, "stack_limit_mib": 4736}
	a, err = Telemetry(root, write(legacy))
	if err != nil || a.Profile != "full" || a.ReserveMiB != 6144 || a.CalculatedAllowanceMiB != 0 {
		t.Fatalf("legacy full telemetry refused: %#v %v", a, err)
	}
	for name, edit := range map[string]func(map[string]any){
		"unknown_field":              func(v map[string]any) { v["unreviewed"] = true },
		"profile_component_mismatch": func(v map[string]any) { v["component_limits_mib"].(map[string]any)["host_alloy_mib"] = -1 },
		"calculated_mismatch":        func(v map[string]any) { v["calculated_allowance_mib"] = 1 },
		"legacy_metrics":             func(v map[string]any) { v["schema"] = 1; v["profile"] = "metrics" },
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(valid)
			var mutated map[string]any
			_ = json.Unmarshal(b, &mutated)
			edit(mutated)
			if _, err := Telemetry(root, write(mutated)); err == nil {
				t.Fatal("unsafe telemetry evidence accepted")
			}
		})
	}
	if _, err := Telemetry(root, Manifest{}); err != nil {
		t.Fatalf("omitted optional telemetry should remain explicit owner-budget responsibility: %v", err)
	}
}
