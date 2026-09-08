package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func (e *Executor) refreshHardware(ctx context.Context, id string, lock *os.File) (json.RawMessage, error) {
	if e.policy.HardwarePath != filepath.Join(e.policy.StateDir, "hardware", "hardware.json") {
		return nil, errors.New("hardware publication must use the root policy's managed hardware path")
	}
	output := filepath.Join(e.policy.StateDir, "evidence", id)
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	_, runErr := runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), []string{"--config", e.policy.WorkstationConfig, "hardware", "collect", output}, e.environment(), lock, nil, 64<<10)
	if runErr != nil {
		return nil, runErr
	}
	b, err := os.ReadFile(filepath.Join(output, "hardware.json"))
	if err != nil || len(b) > 4<<20 {
		return nil, errors.New("hardware collection did not produce bounded evidence")
	}
	boot, err := os.ReadFile(filepath.Join(output, "boot-id.txt"))
	if err != nil || len(boot) > 128 {
		return nil, errors.New("hardware collection did not record boot identity")
	}
	var raw map[string]any
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, errors.New("hardware collection evidence is invalid")
	}
	if err = os.MkdirAll(filepath.Dir(e.policy.HardwarePath), 0700); err != nil {
		return nil, err
	}
	// Publish report and boot with a generation marker last. Any interruption
	// leaves a mismatched or absent boot identity, which checkHardware refuses.
	if err = durableJSON(e.policy.HardwarePath, raw); err != nil {
		return nil, err
	}
	if err = durableBytes(filepath.Join(filepath.Dir(e.policy.HardwarePath), "boot-id.txt"), boot); err != nil {
		return nil, err
	}
	if e.policy.WorkerEvidenceDir != "" {
		dir := e.policy.WorkerEvidenceDir
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
		if err := trustedPath(dir, true); err != nil {
			return nil, err
		}
		reduced := map[string]any{}
		for _, key := range []string{"schema", "status", "reason", "collected_at", "os", "architecture", "expected", "pci_gpus", "rocm_agents", "gpu_target", "hardware_workloads_validated", "cpu_topology", "memory"} {
			reduced[key] = raw[key]
		}
		encoded, err := json.Marshal(reduced)
		if err != nil {
			return nil, err
		}
		if err = durableBytesMode(filepath.Join(dir, "hardware.json"), encoded, 0644); err != nil {
			return nil, err
		}
		if err = durableBytesMode(filepath.Join(dir, "boot-id.txt"), boot, 0644); err != nil {
			return nil, err
		}
	}
	h := SanitizeHardware(raw, strings.TrimSpace(string(boot)))
	result, err := json.Marshal(h)
	return result, err
}
func durableBytes(path string, b []byte) error {
	return durableBytesMode(path, b, 0600)
}
func durableBytesMode(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".evidence-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (e *Executor) exportCPUPolicy(ctx context.Context, id string, lock *os.File) (json.RawMessage, error) {
	output := filepath.Join(e.policy.StateDir, "evidence", id+"-cpu-policy")
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	_, err := runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), []string{"--config", e.policy.WorkstationConfig, "resources", "plan", e.policy.HardwarePath, output, filepath.Join(e.policy.RuntimeRoot, "versions.lock")}, e.environment(), lock, nil, 64<<10)
	if err != nil {
		return nil, err
	}
	files := map[string]json.RawMessage{}
	for _, name := range []string{"resource-plan.json", "ansible-vars.json"} {
		b, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || len(b) > 1<<20 || !json.Valid(b) {
			return nil, errors.New("resource planner did not produce bounded valid output")
		}
		files[name] = b
	}
	return json.Marshal(files)
}

// SanitizeHardware exports resource evidence without host serials, disk UUIDs,
// mount layout, package inventories or the collector's raw command output.
func SanitizeHardware(raw map[string]any, boot string) domain.Hardware {
	h := domain.Hardware{Status: str(raw["status"]), BootID: boot, CurrentBootID: boot, Channels: "unknown", TrainedSpeed: "unknown", CPUManagerPolicy: "unknown", HostReserveMiB: 12288, KubeReserveMiB: 6144, Warnings: []string{"Host channels and memory bandwidth are unmeasured; GPU count does not select physical cards or combine VRAM.", "CPU Manager policy and other workload budgets require a current cluster probe."}}
	h.ObservedAt, _ = time.Parse(time.RFC3339, str(raw["collected_at"]))
	h.MemoryMiB = int64(number(nested(raw, "memory", "total_bytes"))) / (1 << 20)
	for _, d := range list(nested(raw, "memory", "dimms")) {
		m := object(d)
		if str(m["size"]) != "" {
			h.DIMMs++
		}
		if speed := str(m["configured_speed"]); speed != "" {
			if h.TrainedSpeed == "unknown" {
				h.TrainedSpeed = speed
			} else if h.TrainedSpeed != speed {
				h.TrainedSpeed = "mixed or unknown"
			}
		}
	}
	for _, gpu := range list(raw["pci_gpus"]) {
		g := object(gpu)
		render := ""
		if paths := list(g["render_nodes"]); len(paths) > 0 {
			render = "/dev/dri/" + str(paths[0])
		}
		h.GPUs = append(h.GPUs, domain.GPU{ID: str(g["bdf"]), Model: "AMD PCI " + str(g["device_id"]), RenderPath: render})
	}
	online := list(nested(raw, "cpu_topology", "online_cpus"))
	cpus := list(nested(raw, "cpu_topology", "cpus"))
	h.CPUThreads = len(online)
	h.TopologyKnown = len(online) > 0 && len(cpus) == len(online)
	seen := map[float64]bool{}
	width := 0
	for _, entry := range cpus {
		cpu := object(entry)
		id, ok := cpu["cpu"].(float64)
		siblings := list(cpu["thread_siblings"])
		if !ok || seen[id] || len(siblings) == 0 || cpu["core_id"] == nil || cpu["socket_id"] == nil {
			h.TopologyKnown = false
		}
		seen[id] = true
		if width == 0 {
			width = len(siblings)
		} else if width != len(siblings) {
			h.TopologyKnown = false
		}
	}
	for _, id := range online {
		n, ok := id.(float64)
		if !ok || !seen[n] {
			h.TopologyKnown = false
		}
	}
	for _, entry := range cpus {
		cpu := object(entry)
		core, coreOK := cpu["core_id"].(float64)
		socket, socketOK := cpu["socket_id"].(float64)
		if !coreOK || !socketOK || core < 0 || socket < 0 {
			h.TopologyKnown = false
			continue
		}
		expected := map[float64]bool{}
		for _, other := range cpus {
			m := object(other)
			if m["core_id"] == core && m["socket_id"] == socket {
				expected[number(m["cpu"])] = true
			}
		}
		siblings := map[float64]bool{}
		for _, s := range list(cpu["thread_siblings"]) {
			n, ok := s.(float64)
			if !ok || !expected[n] || siblings[n] {
				h.TopologyKnown = false
			}
			siblings[n] = true
		}
		if len(siblings) != len(expected) {
			h.TopologyKnown = false
		}
	}
	if h.TopologyKnown {
		h.SMTWidth = width
		h.ReservedCPU = 3 * width
	}
	return h
}
