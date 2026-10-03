package hostexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("executor output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func runFixed(ctx context.Context, path string, args []string, env []string, lock *os.File, input []byte, outputLimit int) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, errors.New("runtime deadline expired before dispatch")
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if lock != nil {
		cmd.ExtraFiles = []*os.File{lock}
	}
	cmd.Stdin = bytes.NewReader(input)
	var out boundedBuffer
	out.limit = outputLimit
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, errors.New("installed runtime executable could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return nil, errors.New("reviewed runtime refused or failed the operation; inspect target qualification, RBAC and legacy recovery state")
		}
		return out.Bytes(), nil
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		return nil, errors.New("bounded runtime deadline expired; external outcome requires recovery")
	}
}
func (e *Executor) environment() []string {
	return []string{"PATH=/usr/bin:/bin:/opt/rocm/bin", "LANG=C", "LC_ALL=C", "HOME=/var/empty", "KUBECONFIG=" + e.policy.Kubeconfig, "WORKSTATION_SESSION_LOCK_FD=3"}
}
func (e *Executor) session(ctx context.Context, profile string, lock *os.File, effectStarted *bool) error {
	environment := e.environment()
	desired := profile
	if desired == "restore" {
		b, err := os.ReadFile(filepath.Join(e.policy.SessionDir, "state.json"))
		if err != nil {
			return err
		}
		var saved struct {
			Previous struct {
				AI   int `json:"ai"`
				Game int `json:"game"`
			} `json:"previous"`
		}
		if json.Unmarshal(b, &saved) != nil {
			return errors.New("invalid legacy restoration snapshot")
		}
		desired = "maintenance"
		if saved.Previous.AI == 1 {
			desired = "ai"
		}
		if saved.Previous.Game == 1 {
			desired = "gaming"
		}
	}
	if desired == "ai" || desired == "gaming" {
		deployment := e.policy.AIDeployment
		key := "WORKSTATION_SESSION_AI_TEMPLATE_SHA256="
		if desired == "gaming" {
			deployment = e.policy.GameDeployment
			key = "WORKSTATION_SESSION_GAME_TEMPLATE_SHA256="
		}
		document, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "deployment", deployment, "-o", "json")
		if err != nil {
			return err
		}
		if err = e.qualifySession(ctx, desired, document, document); err != nil {
			return err
		}
		environment = append(environment, key+canonicalTemplateHash(nested(document, "spec", "template")))
	}
	args := []string{"--config", e.policy.WorkstationConfig, "session", "switch", profile, e.policy.HardwarePath, e.policy.SessionDir, "--execute"}
	if profile == "restore" {
		args = []string{"--config", e.policy.WorkstationConfig, "session", "restore", e.policy.HardwarePath, e.policy.SessionDir, "--execute"}
	}
	*effectStarted = true
	_, err := runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), args, environment, lock, nil, 64<<10)
	return err
}

func canonicalTemplateHash(v any) string {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(v)
	h := sha256.Sum256(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	return hex.EncodeToString(h[:])
}
func (e *Executor) kube(ctx context.Context, input []byte, args ...string) (map[string]any, error) {
	fixed := []string{"--kubeconfig", e.policy.Kubeconfig, "--context", e.policy.Context, "--request-timeout=15s"}
	fixed = append(fixed, args...)
	b, err := runFixed(ctx, e.policy.Kubectl, fixed, e.environment(), nil, input, 4<<20)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err = json.Unmarshal(b, &value); err != nil {
		return nil, errors.New("bounded Kubernetes response is invalid")
	}
	return value, nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func list(v any) []any            { a, _ := v.([]any); return a }
func str(v any) string            { s, _ := v.(string); return s }
func nested(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		v = object(v)[k]
	}
	return v
}
func number(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	if s, ok := v.(string); ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return 0
}

type preflightFailure struct{ error }

func (e *Executor) execute(ctx context.Context, r Request, lock *os.File) (data json.RawMessage, err error) {
	effectStarted := false
	defer func() {
		if err != nil && !effectStarted {
			err = preflightFailure{err}
		}
	}()
	// Recheck mutable source and all artifact provenance while holding the same
	// lock that the legacy CLI uses. API store edits cannot broaden this policy.
	if err := e.verify(); err != nil {
		return nil, err
	}
	if err := e.validate(r); err != nil {
		return nil, err
	}
	if r.Draft.Action == "hardware.refresh" {
		return e.refreshHardware(ctx, r.ID, lock)
	}
	if domain.MemoryAction(r.Draft.Action) {
		return e.executeMemory(ctx, r, lock)
	}
	if domain.PerformanceAction(r.Draft.Action) {
		return e.executePerformance(ctx, r, lock)
	}
	if err := e.checkHardware(); err != nil {
		return nil, err
	}
	if r.Draft.Action == "cpu-policy.export" {
		return e.exportCPUPolicy(ctx, r.ID, lock)
	}
	ai, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "deployment", e.policy.AIDeployment, "-o", "json")
	if err != nil {
		return nil, err
	}
	game, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "deployment", e.policy.GameDeployment, "-o", "json")
	if err != nil {
		return nil, err
	}
	if str(nested(ai, "metadata", "uid")) != e.policy.AIDeploymentUID || str(nested(game, "metadata", "uid")) != e.policy.GameDeploymentUID {
		return nil, errors.New("Deployment UID changed from independent root policy; re-review target identity")
	}
	if r.Draft.Action == "serving.configure" || r.Draft.Action == "resources.configure" || r.Draft.Action == "serving.start" || r.Draft.Action == "serving.restart" || (r.Draft.Action == "profile.switch" && r.Draft.Profile == "ai") {
		if err := e.verifyModel(ctx, r.Desired); err != nil {
			return nil, err
		}
	}
	switch r.Draft.Action {
	case "profile.switch":
		if r.Draft.Profile != "maintenance" {
			if err := e.qualifySession(ctx, r.Draft.Profile, ai, game); err != nil {
				return nil, err
			}
		}
		if err = e.validate(r); err != nil {
			return nil, err
		}
		return nil, e.session(ctx, r.Draft.Profile, lock, &effectStarted)
	case "profile.restore":
		b, readErr := os.ReadFile(filepath.Join(e.policy.SessionDir, "state.json"))
		if readErr != nil {
			return nil, errors.New("saved legacy session snapshot unavailable")
		}
		var saved struct {
			Previous struct {
				AI   int `json:"ai"`
				Game int `json:"game"`
			} `json:"previous"`
		}
		if json.Unmarshal(b, &saved) != nil {
			return nil, errors.New("saved legacy snapshot is malformed")
		}
		if saved.Previous.AI == 1 {
			if err := e.verifyModel(ctx, r.Desired); err != nil {
				return nil, err
			}
			if err := e.qualifySession(ctx, "ai", ai, game); err != nil {
				return nil, err
			}
		}
		if saved.Previous.Game == 1 {
			if err := e.qualifySession(ctx, "gaming", ai, game); err != nil {
				return nil, err
			}
		}
		if err = e.validate(r); err != nil {
			return nil, err
		}
		return nil, e.session(ctx, "restore", lock, &effectStarted)
	case "serving.stop":
		if err = e.validate(r); err != nil {
			return nil, err
		}
		return nil, e.session(ctx, "maintenance", lock, &effectStarted)
	case "serving.start":
		if err := e.qualifySession(ctx, "ai", ai, game); err != nil {
			return nil, err
		}
		if err = e.validate(r); err != nil {
			return nil, err
		}
		return nil, e.session(ctx, "ai", lock, &effectStarted)
	case "serving.restart":
		if err := e.qualifySession(ctx, "ai", ai, game); err != nil {
			return nil, err
		}
		if err = e.validate(r); err != nil {
			return nil, err
		}
		if err = e.session(ctx, "maintenance", lock, &effectStarted); err != nil {
			return nil, err
		}
		return nil, e.session(ctx, "ai", lock, &effectStarted)
	}
	if number(nested(game, "spec", "replicas")) != 0 {
		return nil, errors.New("gaming is active; switch explicitly to AI or maintenance before serving configuration")
	}
	wasRunning := number(nested(ai, "spec", "replicas")) == 1
	candidate, err := e.servingCandidate(ai, r.Desired)
	if err != nil {
		return nil, err
	}
	{
		if err := e.qualifySession(ctx, "ai", candidate, game); err != nil {
			return nil, err
		}
	}
	if err = e.capacity(ctx, candidate); err != nil {
		return nil, err
	}
	if err = e.validate(r); err != nil {
		return nil, err
	}
	if err = e.session(ctx, "maintenance", lock, &effectStarted); err != nil {
		return nil, err
	}
	// Refetch resourceVersion after scale-to-zero; compare immutable identity and
	// the full old pod template before submitting an optimistic replace.
	stopped, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "deployment", e.policy.AIDeployment, "-o", "json")
	if err != nil {
		return nil, err
	}
	if str(nested(stopped, "metadata", "uid")) != e.policy.AIDeploymentUID || number(nested(stopped, "spec", "replicas")) != 0 || domain.Hash(nested(stopped, "spec", "template")) != domain.Hash(nested(ai, "spec", "template")) {
		return nil, errors.New("deployment changed during stop; source is saved but live apply is refused")
	}
	object(candidate["metadata"])["resourceVersion"] = nested(stopped, "metadata", "resourceVersion")
	object(candidate["spec"])["replicas"] = 0
	delete(candidate, "status")
	b, _ := json.Marshal(candidate)
	if _, err = e.kube(ctx, b, "-n", e.policy.Namespace, "replace", "-f", "-", "-o", "json"); err != nil {
		return nil, err
	}
	if wasRunning {
		return nil, e.session(ctx, "ai", lock, &effectStarted)
	}
	return nil, nil
}

func (e *Executor) qualifySession(ctx context.Context, profile string, ai, game map[string]any) error {
	target := ai
	if profile == "gaming" {
		target = game
	}
	for key, q := range e.policy.SessionQualifications {
		if key != profile && !strings.HasPrefix(key, profile+"/") {
			continue
		}
		if !q.ExpiresAt.After(time.Now()) || q.TemplateHash != domain.Hash(nested(target, "spec", "template")) {
			continue
		}
		return e.checkConfigMaps(ctx, target, q.ConfigMaps)
	}
	return errors.New("session Pod template qualification is absent, changed or expired in independent root policy")
}

func (e *Executor) checkConfigMaps(ctx context.Context, target map[string]any, hashes map[string]string) error {
	references := map[string]bool{}
	for _, container := range append(list(nested(target, "spec", "template", "spec", "containers")), list(nested(target, "spec", "template", "spec", "initContainers"))...) {
		c := object(container)
		for _, from := range list(c["envFrom"]) {
			name := str(nested(object(from), "configMapRef", "name"))
			if name != "" {
				references[name] = true
			}
		}
		for _, env := range list(c["env"]) {
			name := str(nested(object(env), "valueFrom", "configMapKeyRef", "name"))
			if name != "" {
				references[name] = true
			}
		}
	}
	for _, volume := range list(nested(target, "spec", "template", "spec", "volumes")) {
		name := str(nested(object(volume), "configMap", "name"))
		if name != "" {
			references[name] = true
		}
	}
	for name := range references {
		want := hashes[name]
		if len(want) != 64 {
			return errors.New("session configuration reference is not independently qualified")
		}
		m, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "configmap", name, "-o", "json")
		if err != nil {
			return err
		}
		value := map[string]any{"data": m["data"], "binaryData": m["binaryData"]}
		if domain.Hash(value) != want {
			return errors.New("session ConfigMap changed from root qualification")
		}
	}
	return nil
}

func (e *Executor) servingCandidate(current map[string]any, c domain.Configuration) (map[string]any, error) {
	b, _ := json.Marshal(current)
	var candidate map[string]any
	_ = json.Unmarshal(b, &candidate)
	containers := list(nested(candidate, "spec", "template", "spec", "containers"))
	if len(containers) != 1 || len(list(nested(candidate, "spec", "template", "spec", "initContainers"))) != 0 {
		return nil, errors.New("serving adapter requires the existing single-container SGLang deployment")
	}
	container := object(containers[0])
	q := e.policy.QualifiedConfigurations[ConfigurationHash(&c.Serving, &c.Resources)]
	if str(container["name"]) != "sglang" || str(container["image"]) != q.Image || str(nested(candidate, "metadata", "annotations", "workstation.ai/qualification")) != "qualified" || str(nested(candidate, "spec", "strategy", "type")) != "Recreate" {
		return nil, errors.New("engine image, SGLang container or deployment qualification does not match root policy")
	}
	if domain.Hash(container["command"]) != domain.Hash([]string{"python3", "-m", "sglang.launch_server"}) {
		return nil, errors.New("SGLang command changed from reviewed deployment contract")
	}
	// v0.5.15.post1 0b3bb0cbe31873994c9f989fddfe2f87ca839fdd uses
	// cpu_offload_gb * 1024**3. The exact configuration still needs target
	// qualification; presence of the upstream flag is not GPU support evidence.
	args := list(container["args"])
	withoutOffload := make([]any, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		arg := str(args[i])
		if arg == "--cpu-offload-gb" {
			if i+1 >= len(args) {
				return nil, errors.New("malformed existing CPU offload argument")
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "--cpu-offload-gb=") {
			continue
		}
		withoutOffload = append(withoutOffload, args[i])
	}
	container["args"] = append(withoutOffload, "--cpu-offload-gb", strconv.Itoa(c.Serving.CPUOffloadGiB))
	// Preserve parsers/quantization selected in the existing ConfigMap. Only
	// reviewed tuning variables are overridden with the exact typed values.
	values := map[string]string{"MODEL_PATH": q.ModelPath, "MODEL_REVISION": q.ModelRevision, "SERVED_MODEL_NAME": c.Serving.Model, "CONTEXT_LENGTH": strconv.Itoa(c.Serving.Context), "MEM_FRACTION_STATIC": strconv.FormatFloat(c.Serving.MemoryFraction, 'f', -1, 64), "MAX_RUNNING_REQUESTS": strconv.Itoa(c.Serving.Concurrency), "TENSOR_PARALLEL": strconv.Itoa(c.Resources.GPUCount)}
	env := list(container["env"])
	filtered := make([]any, 0, len(env)+len(values))
	for _, entry := range env {
		if _, ok := values[str(object(entry)["name"])]; !ok {
			filtered = append(filtered, entry)
		}
	}
	for _, k := range []string{"MODEL_PATH", "MODEL_REVISION", "SERVED_MODEL_NAME", "CONTEXT_LENGTH", "MEM_FRACTION_STATIC", "MAX_RUNNING_REQUESTS", "TENSOR_PARALLEL"} {
		filtered = append(filtered, map[string]any{"name": k, "value": values[k]})
	}
	container["env"] = filtered
	budget := map[string]any{"cpu": strconv.Itoa(c.Resources.CPU), "memory": fmt.Sprintf("%dMi", c.Resources.MemoryMiB), "amd.com/gpu": strconv.Itoa(c.Resources.GPUCount)}
	container["resources"] = map[string]any{"requests": budget, "limits": budget}
	found := false
	for _, v := range list(nested(candidate, "spec", "template", "spec", "volumes")) {
		volume := object(v)
		if str(volume["name"]) == "shm" {
			empty := object(volume["emptyDir"])
			if str(empty["medium"]) != "Memory" {
				return nil, errors.New("shared-memory volume changed from reviewed abstraction")
			}
			empty["sizeLimit"] = fmt.Sprintf("%dMi", c.Resources.SharedMemoryMiB)
			found = true
		}
	}
	if !found {
		return nil, errors.New("reviewed shared-memory volume is missing")
	}
	return candidate, nil
}

// PreviewServing renders the same fixed candidate as the live adapter without
// reading credentials, opening a socket or qualifying/installing the output.
func PreviewServing(p Policy, current map[string]any, c domain.Configuration) (map[string]any, error) {
	return (&Executor{policy: p}).servingCandidate(current, c)
}

func quantity(v any, cpu bool) (int64, error) {
	s := fmt.Sprint(v)
	factor := float64(1)
	if cpu {
		factor = 1000
		if strings.HasSuffix(s, "m") {
			factor = 1
			s = strings.TrimSuffix(s, "m")
		}
	} else {
		for _, u := range []struct {
			s string
			n float64
		}{{"Ki", 1024}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}} {
			if strings.HasSuffix(s, u.s) {
				factor = u.n
				s = strings.TrimSuffix(s, u.s)
				break
			}
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 || n*factor > float64(1<<60) {
		return 0, errors.New("unsupported Kubernetes resource quantity")
	}
	return int64(n * factor), nil
}
func (e *Executor) capacity(ctx context.Context, candidate map[string]any) error {
	hardware, err := os.ReadFile(e.policy.HardwarePath)
	if err != nil {
		return errors.New("current hardware topology unavailable")
	}
	var raw map[string]any
	if json.Unmarshal(hardware, &raw) != nil {
		return errors.New("invalid hardware topology")
	}
	h := SanitizeHardware(raw, "")
	requested := number(nested(object(list(nested(candidate, "spec", "template", "spec", "containers"))[0]), "resources", "requests", "cpu"))
	if !h.TopologyKnown || h.SMTWidth < 1 || int(requested)%h.SMTWidth != 0 {
		return errors.New("desired CPU budget must contain complete discovered SMT cores")
	}
	node, err := e.kube(ctx, nil, "get", "node", e.policy.Target, "-o", "json")
	if err != nil {
		return err
	}
	pods, err := e.kube(ctx, nil, "get", "pods", "-A", "-o", "json")
	if err != nil {
		return err
	}
	rs, err := e.kube(ctx, nil, "-n", e.policy.Namespace, "get", "replicasets", "-o", "json")
	if err != nil {
		return err
	}
	managed := map[string]bool{}
	for _, entry := range list(rs["items"]) {
		m := object(entry)
		for _, owner := range list(nested(m, "metadata", "ownerReferences")) {
			uid := str(object(owner)["uid"])
			if uid == e.policy.AIDeploymentUID || uid == e.policy.GameDeploymentUID {
				managed[str(nested(m, "metadata", "uid"))] = true
			}
		}
	}
	used := map[string]int64{}
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
		if str(nested(pod, "spec", "nodeName")) != e.policy.Target {
			continue
		}
		if nested(pod, "spec", "resources") != nil {
			return errors.New("pod-level resource accounting requires separate qualification")
		}
		for _, key := range []string{"cpu", "memory", "amd.com/gpu"} {
			var sum, maxInit int64
			for _, c := range list(nested(pod, "spec", "containers")) {
				v := nested(object(c), "resources", "requests", key)
				if v == nil {
					continue
				}
				q, err := quantity(v, key == "cpu")
				if err != nil {
					return err
				}
				sum += q
			}
			for _, c := range list(nested(pod, "spec", "initContainers")) {
				if str(object(c)["restartPolicy"]) == "Always" {
					return errors.New("restartable init resource accounting is not qualified")
				}
				v := nested(object(c), "resources", "requests", key)
				if v != nil {
					q, err := quantity(v, key == "cpu")
					if err != nil {
						return err
					}
					if q > maxInit {
						maxInit = q
					}
				}
			}
			if maxInit > sum {
				sum = maxInit
			}
			if v := nested(pod, "spec", "overhead", key); v != nil {
				q, err := quantity(v, key == "cpu")
				if err != nil {
					return err
				}
				sum += q
			}
			used[key] += sum
		}
	}
	container := object(list(nested(candidate, "spec", "template", "spec", "containers"))[0])
	for _, key := range []string{"cpu", "memory", "amd.com/gpu"} {
		available, err := quantity(nested(node, "status", "allocatable", key), key == "cpu")
		if err != nil {
			return err
		}
		wanted, err := quantity(nested(container, "resources", "requests", key), key == "cpu")
		if err != nil {
			return err
		}
		if used[key]+wanted > available {
			return errors.New("desired budget does not fit node allocatable capacity after other workloads")
		}
	}
	return nil
}

func (e *Executor) checkHardware() error {
	if err := trustedPath(e.policy.HardwarePath, false); err != nil {
		return errors.New("root-owned hardware report is unavailable")
	}
	f, err := os.Open(e.policy.HardwarePath)
	if err != nil {
		return err
	}
	defer f.Close()
	var h struct {
		CollectedAt time.Time `json:"collected_at"`
		Status      string    `json:"status"`
	}
	if err = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&h); err != nil {
		return errors.New("hardware report is malformed")
	}
	if h.Status != "observed" || time.Since(h.CollectedAt) > 24*time.Hour || time.Until(h.CollectedAt) > time.Minute {
		return errors.New("hardware report is stale or unobserved; collect current target evidence")
	}
	boot, err := os.ReadFile(filepath.Join(filepath.Dir(e.policy.HardwarePath), "boot-id.txt"))
	if err != nil {
		return errors.New("hardware boot identity unavailable")
	}
	now, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != strings.TrimSpace(string(now)) {
		return errors.New("hardware boot identity is stale")
	}
	return nil
}
func (e *Executor) finishBuildGate() error {
	path := "/run/workstation/build-inhibit"
	if err := trustedPath("/run/workstation", true); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(e.policy.SessionDir, "state.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	var state struct {
		Mode     string `json:"mode"`
		Phase    string `json:"phase"`
		Previous struct {
			AI   int `json:"ai"`
			Game int `json:"game"`
		} `json:"previous"`
	}
	if err = json.NewDecoder(f).Decode(&state); err != nil {
		return err
	}
	if state.Phase != "ready" {
		return errors.New("legacy recovery remains incomplete")
	}
	if state.Mode == "ai" || (state.Mode == "restore" && state.Previous.AI == 1) {
		if err := trustedPath(path, false); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil // Gaming and maintenance inhibit new managed builds.
}
