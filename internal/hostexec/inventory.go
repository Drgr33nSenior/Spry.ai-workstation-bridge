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

func (e *Executor) Snapshot(ctx context.Context) (domain.Inventory, error) {
	inv := domain.Inventory{Mode: "live", Target: e.policy.Target, Environment: e.policy.Environment, Profile: "unknown", ClusterMessage: "Kubernetes evidence unavailable"}
	inv.ServingStatus = domain.ServingStatus{State: "unknown", RepresentativeWarmup: "unknown", Reason: "Current scoped Pod/process evidence unavailable; retained reports are not current readiness.", ObservedAt: time.Now().UTC()}
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
	aiReplicas := map[string]bool{}
	for _, entry := range list(rs["items"]) {
		m := object(entry)
		for _, o := range list(nested(m, "metadata", "ownerReferences")) {
			uid := str(object(o)["uid"])
			if uid == e.policy.AIDeploymentUID || uid == e.policy.GameDeploymentUID {
				managed[str(nested(m, "metadata", "uid"))] = true
			}
			if uid == e.policy.AIDeploymentUID {
				aiReplicas[str(nested(m, "metadata", "uid"))] = true
			}
		}
	}
	inv.ServingStatus = observedServingStatus(pods, aiReplicas, e.policy.Target, e.policy.Namespace, inv.Profile)
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

// Readiness remains Kubernetes' observation. A successful HTTP health probe is
// not proof of representative warmup, nor may a historical report prove that a
// replacement process is warm. The non-root installer status command separately
// validates fresh in-Pod model/runtime/device evidence for an explicit warmup.
func observedServingStatus(pods map[string]any, aiReplicas map[string]bool, target, namespace, profile string) domain.ServingStatus {
	s := domain.ServingStatus{State: "unknown", RepresentativeWarmup: "unknown", Reason: "No unique current AI Pod/process; inspect the explicit target and retry status, not prior inference/tool work.", ObservedAt: time.Now().UTC()}
	if profile == "gaming" || profile == "maintenance" || profile == "recovery-required" {
		s.State = "not-applicable"
		s.RepresentativeWarmup = "not-applicable"
		s.Reason = "AI is not in a settled AI profile. Complete the explicit transition/recovery before submitting new inference."
		return s
	}
	var selected map[string]any
	for _, entry := range list(pods["items"]) {
		p := object(entry)
		if str(nested(p, "metadata", "namespace")) != namespace || str(nested(p, "spec", "nodeName")) != target {
			continue
		}
		owned := false
		for _, o := range list(nested(p, "metadata", "ownerReferences")) {
			if aiReplicas[str(object(o)["uid"])] {
				owned = true
			}
		}
		if !owned || nested(p, "metadata", "deletionTimestamp") != nil {
			continue
		}
		phase := str(nested(p, "status", "phase"))
		if phase == "Succeeded" || phase == "Failed" {
			continue
		}
		if selected != nil {
			s.Reason = "Multiple active AI Pods; rollout identity is not settled."
			return s
		}
		selected = p
	}
	if selected == nil {
		return s
	}
	for _, entry := range list(nested(selected, "status", "containerStatuses")) {
		c := object(entry)
		if str(c["name"]) != "sglang" {
			continue
		}
		ready, ok := c["ready"].(bool)
		if !ok || str(nested(selected, "metadata", "uid")) == "" {
			return s
		}
		if str(c["containerID"]) == "" || str(c["imageID"]) == "" {
			// Image pulls can fail before a process or resolved image exists. The
			// unique scoped Pod supports that diagnostic, but not process identity,
			// loading progress or representative warmth.
			waiting := str(nested(c, "state", "waiting", "reason"))
			if !ready && len(object(c["state"])) == 1 && (waiting == "ImagePullBackOff" || waiting == "ErrImagePull") {
				s.KubernetesReady = &ready
				s.State, s.Reason = servingNotReadyState(c)
			}
			return s
		}
		s.KubernetesReady = &ready
		s.Identity = domain.Hash(map[string]any{"pod": nested(selected, "metadata", "uid"), "container": c["containerID"], "image": c["imageID"], "restart": c["restartCount"], "started": nested(c, "state", "running", "startedAt")})
		if !ready {
			s.State, s.Reason = servingNotReadyState(c)
		} else {
			s.State = "healthy"
			s.Reason = "Kubernetes Ready; representative warmth is unknown. Run the explicit non-root serving-warm-status command with fresh evidence; Bridge cannot launch that harness through its root helper."
		}
		return s
	}
	return s
}

// servingNotReadyState maps only current, documented container-state reasons to
// fixed operator guidance. Kubernetes messages are never returned. A previous
// OOM reason explains a current crash loop, but cannot mark a recovered Ready
// process unhealthy.
func servingNotReadyState(container map[string]any) (string, string) {
	state := object(container["state"])
	waitingState, waitingOK := state["waiting"]
	terminatedState, terminatedOK := state["terminated"]
	_, runningOK := state["running"]
	waiting := str(nested(state, "waiting", "reason"))
	terminated := str(nested(state, "terminated", "reason"))
	switch {
	case waiting == "CrashLoopBackOff":
		if str(nested(object(container["lastState"]), "terminated", "reason")) == "OOMKilled" {
			return "unavailable", "SGLang is restarting repeatedly after a memory termination. Inspect the current bounded resource and memory evidence before retrying; do not treat it as model loading."
		}
		return "unavailable", "SGLang is restarting repeatedly. Inspect the bounded Kubernetes status and OOM/resource evidence before retrying; do not treat it as model loading."
	case terminated == "OOMKilled":
		return "unavailable", "SGLang was terminated for memory use. Inspect the current bounded resource and memory evidence before retrying; do not treat it as model loading."
	case waiting == "ImagePullBackOff" || waiting == "ErrImagePull":
		return "unavailable", "SGLang image retrieval is unavailable. Inspect the qualified image reference and private registry access before retrying."
	case terminatedOK && object(terminatedState) != nil:
		return "unavailable", "SGLang current container terminated. Inspect the bounded Kubernetes status and qualified runtime before retrying."
	case waitingOK && object(waitingState) != nil && (waiting == "ContainerCreating" || waiting == "PodInitializing"):
		return "model-loading", "Kubernetes readiness is false. Loading, compilation and warmup phases are not independently observed by this probe."
	case runningOK && object(state["running"]) != nil:
		return "model-loading", "SGLang is running but Kubernetes readiness is false. Loading, compilation and warmup phases are not independently observed by this probe."
	default:
		return "unknown", "SGLang has no recognized current container state. Inspect the bounded Kubernetes status and retry lifecycle observation."
	}
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
