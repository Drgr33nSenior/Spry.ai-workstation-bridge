package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

func (e *Executor) Snapshot(ctx context.Context) (domain.Inventory, error) {
	inv := domain.Inventory{Mode: "live", Target: e.policy.Target, Environment: e.policy.Environment, Profile: "unknown", ClusterMessage: "Kubernetes evidence unavailable"}
	if err := e.verify(); err != nil {
		return inv, err
	}
	b, err := os.ReadFile(e.policy.HardwarePath)
	if err == nil && len(b) <= 4<<20 {
		var raw map[string]any
		if json.Unmarshal(b, &raw) == nil {
			boot, _ := os.ReadFile(filepath.Join(filepath.Dir(e.policy.HardwarePath), "boot-id.txt"))
			inv.Hardware = SanitizeHardware(raw, strings.TrimSpace(string(boot)))
		}
	}
	current, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	inv.Hardware.CurrentBootID = strings.TrimSpace(string(current))
	if err := e.checkHardware(); err != nil {
		inv.Hardware.Status = "stale-or-unknown"
		inv.Hardware.Warnings = append(inv.Hardware.Warnings, err.Error())
	}
	state, _ := os.ReadFile(filepath.Join(e.policy.SessionDir, "state.json"))
	var session struct {
		Mode  string `json:"mode"`
		Phase string `json:"phase"`
	}
	if json.Unmarshal(state, &session) == nil {
		inv.Profile = session.Mode
		if session.Phase != "ready" {
			inv.Profile = "recovery-required"
		}
	}
	manager, _ := os.ReadFile("/var/lib/kubelet/cpu_manager_state")
	var policy struct {
		PolicyName string `json:"policyName"`
	}
	if json.Unmarshal(manager, &policy) == nil {
		inv.Hardware.CPUManagerPolicy = policy.PolicyName
	}
	cpuConfig := "/var/lib/rancher/k3s/agent/etc/kubelet.conf.d/90-workstation-cpu.conf"
	if trustedPath(cpuConfig, false) == nil {
		b, _ := os.ReadFile(cpuConfig)
		s := string(b)
		inv.Hardware.FullPCPUsOnly = strings.Contains(s, "\n  full-pcpus-only: \"true\"\n") && strings.Contains(s, "\n  strict-cpu-reservation: \"true\"\n") && strings.Contains(s, "topologyManagerPolicy: restricted") && strings.Contains(s, "topologyManagerScope: pod")
	}
	node, err := e.kube(ctx, nil, "get", "node", e.policy.Target, "-o", "json")
	if err != nil {
		return inv, nil
	}
	ready := false
	for _, condition := range list(nested(node, "status", "conditions")) {
		m := object(condition)
		if str(m["type"]) == "Ready" && str(m["status"]) == "True" {
			ready = true
		}
	}
	if !ready || str(nested(node, "status", "nodeInfo", "bootID")) != inv.Hardware.CurrentBootID {
		inv.ClusterMessage = "Node is not Ready or its boot identity differs from this workstation"
		return inv, nil
	}
	pods, err := e.kube(ctx, nil, "get", "pods", "-A", "-o", "json")
	if err != nil {
		return inv, nil
	}
	rs, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "replicasets", "-o", "json")
	if err != nil {
		return inv, nil
	}
	managed := map[string]bool{}
	for _, entry := range list(rs["items"]) {
		m := object(entry)
		for _, o := range list(nested(m, "metadata", "ownerReferences")) {
			uid := str(object(o)["uid"])
			if uid == e.policy.AIDeploymentUID || uid == e.policy.GameDeploymentUID {
				managed[str(nested(m, "metadata", "uid"))] = true
			}
		}
	}
	for _, entry := range list(pods["items"]) {
		pod := object(entry)
		phase := str(nested(pod, "status", "phase"))
		if phase == "Succeeded" || phase == "Failed" {
			continue
		}
		skip := false
		for _, o := range list(nested(pod, "metadata", "ownerReferences")) {
			if managed[str(object(o)["uid"])] {
				skip = true
			}
		}
		if skip {
			continue
		}
		cpu, memory, gpu, err := podBudget(pod)
		if err != nil {
			inv.ClusterMessage = err.Error()
			return inv, nil
		}
		if gpu > 0 {
			inv.Hardware.OtherGPUs += int(gpu)
		} // Pending consumers anywhere also fence handover.
		if str(nested(pod, "spec", "nodeName")) == e.policy.Target {
			inv.Hardware.OtherCPU += int((cpu + 999) / 1000)
			inv.Hardware.OtherMemoryMiB += (memory + (1 << 20) - 1) / (1 << 20)
		}
	}
	allocCPU, err := quantity(nested(node, "status", "allocatable", "cpu"), true)
	if err != nil {
		return inv, nil
	}
	allocMem, err := quantity(nested(node, "status", "allocatable", "memory"), false)
	if err != nil {
		return inv, nil
	}
	inv.Hardware.ReservedCPU = inv.Hardware.CPUThreads - int(allocCPU/1000)
	inv.Hardware.HostReserveMiB = 0
	inv.Hardware.KubeReserveMiB = inv.Hardware.MemoryMiB - allocMem/(1<<20)
	if inv.Hardware.ReservedCPU < 0 || inv.Hardware.KubeReserveMiB < 0 {
		inv.ClusterMessage = "Node allocatable capacity exceeds observed host inventory"
		return inv, nil
	}
	inv.ClusterAvailable = true
	inv.ClusterMessage = "Explicit dedicated identity read current node, all Pods and managed ReplicaSet ownership; mutation RBAC remains checked on apply"
	return inv, nil
}
func podBudget(p map[string]any) (int64, int64, int64, error) {
	if nested(p, "spec", "resources") != nil {
		return 0, 0, 0, errors.New("pod-level resource accounting requires qualification")
	}
	result := map[string]int64{}
	for _, key := range []string{"cpu", "memory", "amd.com/gpu"} {
		var sum, maxInit int64
		for _, c := range list(nested(p, "spec", "containers")) {
			v := nested(object(c), "resources", "requests", key)
			if v == nil {
				continue
			}
			q, err := quantity(v, key == "cpu")
			if err != nil {
				return 0, 0, 0, err
			}
			sum += q
		}
		for _, c := range list(nested(p, "spec", "initContainers")) {
			if str(object(c)["restartPolicy"]) == "Always" {
				return 0, 0, 0, errors.New("restartable init resource accounting requires qualification")
			}
			if v := nested(object(c), "resources", "requests", key); v != nil {
				q, err := quantity(v, key == "cpu")
				if err != nil {
					return 0, 0, 0, err
				}
				if q > maxInit {
					maxInit = q
				}
			}
		}
		if maxInit > sum {
			sum = maxInit
		}
		if v := nested(p, "spec", "overhead", key); v != nil {
			q, err := quantity(v, key == "cpu")
			if err != nil {
				return 0, 0, 0, err
			}
			sum += q
		}
		result[key] = sum
	}
	return result["cpu"], result["memory"], result["amd.com/gpu"], nil
}
