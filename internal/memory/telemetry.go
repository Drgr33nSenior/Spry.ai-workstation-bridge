package memory

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
)

// TelemetryAllowance is a sealed, nonsecret capacity declaration. It is not
// observed RSS and cannot lower the owner-supplied other-workload budget.
type TelemetryAllowance struct {
	Profile                string           `json:"profile"`
	ReserveMiB             int64            `json:"reserve_mib"`
	ComponentLimitsMiB     map[string]int64 `json:"component_limits_mib,omitempty"`
	MarginMiB              int64            `json:"margin_mib,omitempty"`
	CalculatedAllowanceMiB int64            `json:"calculated_allowance_mib,omitempty"`
}

type telemetryEvidence struct {
	Schema                 int               `json:"schema"`
	Status                 string            `json:"status"`
	HardwareQualification  string            `json:"hardware_qualification"`
	Enabled                bool              `json:"enabled"`
	Profile                string            `json:"profile"`
	GPUExporter            bool              `json:"gpu_exporter"`
	SGLangTrace            bool              `json:"sglang_trace"`
	Kubelet                bool              `json:"kubelet"`
	NodeName               string            `json:"node_name"`
	APIAddress             string            `json:"api_address"`
	WorkstationAddress     string            `json:"workstation_address"`
	ReserveMiB             int64             `json:"reserve_mib"`
	StackLimitMiB          int64             `json:"stack_limit_mib"`
	ComponentLimitsMiB     map[string]int64  `json:"component_limits_mib"`
	MarginMiB              int64             `json:"margin_mib"`
	CalculatedAllowanceMiB int64             `json:"calculated_allowance_mib"`
	PlannedWorkloads       []string          `json:"planned_workloads"`
	WorkloadOverlay        string            `json:"workload_overlay"`
	StackImages            []string          `json:"stack_images"`
	WorkloadImages         []string          `json:"workload_images"`
	SourceIdentity         json.RawMessage   `json:"source_identity"`
	SourceSHA256           map[string]string `json:"source_sha256"`
}

// Telemetry reads the optional installer telemetry evidence included in a
// sealed memory bundle. Legacy schema 1 is accepted only as full-profile
// evidence; it does not claim a calculated component allowance.
func Telemetry(root string, m Manifest) (*TelemetryAllowance, error) {
	if _, ok := m.Files["telemetry/evidence.json"]; !ok {
		return nil, nil
	}
	want := m.Files["telemetry/evidence.json"]
	b, err := Read(filepath.Join(root, "telemetry", "evidence.json"), 2<<20)
	if err != nil {
		return nil, err
	}
	if !Digest.MatchString(want) || Sum(b) != want {
		return nil, errors.New("telemetry evidence hash changed")
	}
	var e telemetryEvidence
	if err = config.Decode(b, &e); err != nil {
		return nil, errors.New("invalid telemetry evidence JSON")
	}
	if (e.Schema != 1 && e.Schema != 2) || (e.Status != "generated-not-deployed" && e.Status != "disabled-not-deployed") || e.ReserveMiB <= 0 || e.ReserveMiB > 1<<30 || e.StackLimitMiB < 0 || e.StackLimitMiB > 1<<30 {
		return nil, errors.New("invalid telemetry evidence status or bounded reserve")
	}
	if e.Schema == 1 {
		if e.Profile != "" && e.Profile != "full" {
			return nil, errors.New("legacy telemetry evidence supports full profile only")
		}
		return &TelemetryAllowance{Profile: "full", ReserveMiB: e.ReserveMiB}, nil
	}
	if e.Profile != "full" && e.Profile != "metrics" || e.MarginMiB <= 0 || e.MarginMiB > 1<<30 || len(e.ComponentLimitsMiB) != 3 || e.ComponentLimitsMiB["cluster_stack_mib"] != e.StackLimitMiB {
		return nil, errors.New("invalid telemetry profile allowance")
	}
	var sum int64
	components := map[string]int64{}
	for _, name := range []string{"cluster_stack_mib", "host_alloy_mib", "hardware_sampler_mib"} {
		value, ok := e.ComponentLimitsMiB[name]
		if !ok || value < 0 || value > 1<<30 {
			return nil, errors.New("invalid telemetry profile allowance")
		}
		components[name] = value
		sum += value
	}
	if sum > 1<<30-e.MarginMiB || e.CalculatedAllowanceMiB != sum+e.MarginMiB || e.ReserveMiB < e.CalculatedAllowanceMiB {
		return nil, errors.New("invalid telemetry profile allowance")
	}
	return &TelemetryAllowance{Profile: e.Profile, ReserveMiB: e.ReserveMiB, ComponentLimitsMiB: components, MarginMiB: e.MarginMiB, CalculatedAllowanceMiB: e.CalculatedAllowanceMiB}, nil
}
