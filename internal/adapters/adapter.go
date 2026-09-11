package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/catalog"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/hostexec"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/worker"
)

func models(stager *Stager) []domain.Model {
	out := []domain.Model{}
	for _, m := range catalog.Selected() {
		model := domain.Model{ID: m.ID, Repository: m.Repository, Revision: m.Revision, Quantization: m.Quantization, License: m.License, Size: m.Size, Status: m.Status, Qualification: "target-qualification-required", GPUCount: m.GPUs}
		if stager != nil {
			model.Status = stager.Status(m)
		}
		for _, f := range m.Files {
			df := domain.ModelFile{Path: f.Path, Size: f.Size}
			if f.Algorithm == "sha256" {
				df.SHA256 = f.Digest
			} else {
				df.GitSHA1 = f.Digest
			}
			model.Files = append(model.Files, df)
		}
		out = append(out, model)
	}
	return out
}
func harnesses() []domain.Harness {
	return []domain.Harness{{ID: "qwen", Version: catalog.HarnessPins()["QWEN_CODE_VERSION"], Modes: []string{"cli", "acp"}, Limitations: []string{"existing system policy requires reviewed merge", "local tools require user approval"}}, {ID: "dsh", Version: catalog.HarnessPins()["DSH_COMMIT"], Modes: []string{"acp"}, Limitations: []string{"no interactive CLI profile in pinned revision"}}, {ID: "hermes", Version: catalog.HarnessPins()["HERMES_AGENT_COMMIT"], Modes: []string{"cli", "acp"}, Limitations: []string{"output token cap is provider-owned and cannot be enforced by this client"}}}
}
func exportBundle(harness, endpoint string, c domain.Configuration) (domain.Bundle, error) {
	files, e := catalog.NativeBundle(catalog.ClientProfile{Harness: harness, BaseURL: endpoint, Model: c.Serving.Model, Context: c.Serving.Context, MaxOutput: min(4096, c.Serving.Context/2)})
	if e != nil {
		return domain.Bundle{}, e
	}
	b := domain.Bundle{Harness: harness, Revision: domain.Hash(catalog.HarnessPins()), Limitations: []string{"configured-not-qualified; client launch is explicit and local", "no automatic fallback, management credentials or RAG inheritance", "Hermes output token cap is provider-owned"}}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := sha256.Sum256(files[name])
		b.Files = append(b.Files, domain.BundleFile{Path: name, Content: string(files[name]), SHA256: hex.EncodeToString(h[:])})
	}
	return b, nil
}
func defaultConfig() domain.Configuration {
	c := domain.Configuration{Serving: domain.Serving{Model: "Qwen3.5-9B", Context: 4096, Concurrency: 2, MemoryFraction: .8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 16384, GPUCount: 1}, Caches: domain.CacheBudgets{ModelsGiB: 100, CompilerGiB: 100, ShaderGiB: 20, BuildJobs: 8, BuildMemoryMiB: 32768, ScratchGiB: 50}}
	c.Revision = c.ContentRevision()
	return c
}
func preview(d domain.Draft, c domain.Configuration, revision string) domain.Preview {
	p := domain.Preview{Changes: []domain.Change{}, Consequences: []string{}, Warnings: []string{}, Preconditions: map[string]string{"reference_revision": revision}}
	n, _ := domain.Desired(c, d)
	if c.Serving != n.Serving {
		p.Changes = append(p.Changes, domain.Change{Field: "serving", Before: c.Serving, After: n.Serving})
	}
	if c.Resources != n.Resources {
		p.Changes = append(p.Changes, domain.Change{Field: "resources", Before: c.Resources, After: n.Resources})
	}
	if c.Caches != n.Caches {
		p.Changes = append(p.Changes, domain.Change{Field: "caches", Before: c.Caches, After: n.Caches})
	}
	switch d.Action {
	case "profile.switch", "profile.restore":
		p.Consequences = append(p.Consequences, "all conflicting AI workloads are unloaded before GPU handover; incoming requests fail during transition", "build inhibition prevents new managed compilations only; it does not suspend existing builds")
	case "serving.configure", "resources.configure", "serving.restart":
		p.Consequences = append(p.Consequences, "persist managed source separately, gracefully stop serving, wait for pods and DRM holders, then apply and verify readiness")
	case "model.stage":
		p.Consequences = append(p.Consequences, "download the complete pinned snapshot into managed persistent storage; retained partial files consume budget; staging does not qualify a model")
	case "build.start":
		p.Consequences = append(p.Consequences, "execute reviewed source as an isolated unprivileged worker; output remains an unqualified candidate")
	case "cpu-policy.export":
		p.Consequences = append(p.Consequences, "export a host maintenance plan; CPU Manager state and host policy are unchanged")
	}
	return p
}
func saveJSON(dir, name string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".pending-")
	if e != nil {
		return e
	}
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), filepath.Join(dir, name)); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func readConfig(path string) (domain.Configuration, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return domain.Configuration{}, e
	}
	var c domain.Configuration
	e = json.Unmarshal(b, &c)
	return c, e
}

type demoState struct {
	Profile       string                   `json:"profile"`
	Previous      string                   `json:"previous"`
	Staged        map[string]bool          `json:"staged"`
	Results       map[string]domain.Result `json:"results"`
	Configuration domain.Configuration     `json:"configuration"`
}
type Demo struct {
	dir, target string
	mu          sync.Mutex
	state       demoState
	Fault       string
	Hardware    *domain.Hardware
}

func NewDemo(stateDir, target string) (*Demo, error) {
	dir := filepath.Join(stateDir, "demo-adapter")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	d := &Demo{dir: dir, target: target, state: demoState{Profile: "maintenance", Previous: "ai", Staged: map[string]bool{}, Results: map[string]domain.Result{}, Configuration: defaultConfig()}}
	b, e := os.ReadFile(filepath.Join(dir, "state.json"))
	if e == nil {
		if e = json.Unmarshal(b, &d.state); e != nil {
			return nil, e
		}
		for id, r := range d.state.Results {
			if !domain.Terminal(r.State) {
				d.state.Results[id] = domain.Result{State: "recovery-required", Phase: "demo-restart", Message: "DEMO: interrupted fixture effect requires explicit recovery", RecoveryRequired: true}
			}
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return d, saveJSON(dir, "state.json", d.state)
}
func demoHardware() domain.Hardware {
	return domain.Hardware{Status: "DEMO fixture, not observed target hardware", ObservedAt: time.Now().UTC(), BootID: "demo-boot-1", CurrentBootID: "demo-boot-1", MemoryMiB: 65536, DIMMs: 2, TrainedSpeed: "unknown — expected DDR5-5600", Channels: "unknown — DIMM count is not bandwidth evidence", CPUThreads: 48, SMTWidth: 2, TopologyKnown: true, CPUManagerPolicy: "static", FullPCPUsOnly: true, HostReserveMiB: 12288, KubeReserveMiB: 6144, ReservedCPU: 6, GPUs: []domain.GPU{{ID: "0000:41:00.0", Model: "expected Radeon AI PRO R9700 (fixture)", RenderPath: "/dev/dri/renderD129", MemoryMiB: 32768}, {ID: "0000:81:00.0", Model: "expected Radeon AI PRO R9700 (fixture)", RenderPath: "/dev/dri/renderD128", MemoryMiB: 32768}}, Warnings: []string{"DEMO: expected hardware represented by a generated fixture", "two separate VRAM pools; no physical-device placement support"}}
}
func (d *Demo) Snapshot(context.Context) (domain.Inventory, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := demoHardware()
	if d.Hardware != nil {
		h = *d.Hardware
	}
	ms := models(nil)
	for i := range ms {
		ms[i].Qualification = "DEMO simulated qualification only"
		if d.state.Staged[ms[i].ID] {
			ms[i].Status = "DEMO simulated staging; no model weights downloaded"
		}
	}
	return domain.Inventory{Mode: "demo", Target: d.target, Environment: "dev", SourceRevision: "demo-reference-v1", ClusterAvailable: true, ClusterMessage: "DEMO fixture: no real cluster contacted", Profile: d.state.Profile, Hardware: h, Models: ms, Recipes: worker.Recipes(true), Harnesses: harnesses(), Caches: []domain.Cache{{Name: "models", Status: "DEMO fixture", BudgetBytes: d.state.Configuration.Caches.ModelsGiB << 30}, {Name: "compiler", Status: "DEMO fixture", BudgetBytes: d.state.Configuration.Caches.CompilerGiB << 30}, {Name: "shader", Status: "DEMO fixture", BudgetBytes: d.state.Configuration.Caches.ShaderGiB << 30}}}, nil
}
func (d *Demo) Validate(ctx context.Context, draft domain.Draft, c domain.Configuration) (domain.Preview, error) {
	inv, e := d.Snapshot(ctx)
	if e != nil {
		return domain.Preview{}, e
	}
	if e = domain.ValidateDraft(draft, c, inv); e != nil {
		return domain.Preview{}, e
	}
	p := preview(draft, c, inv.SourceRevision)
	if domain.MemoryAction(draft.Action) {
		s, err := d.MemoryPreview(ctx, draft, c)
		if err != nil {
			return p, err
		}
		for k, v := range s.Preconditions {
			p.Preconditions[k] = v
		}
		p.Consequences = append(p.Consequences, "DEMO evidence export only; no resource change or qualification.")
	}
	p.Warnings = append(p.Warnings, "DEMO: effects are simulated and do not qualify hardware")
	return p, nil
}
func (d *Demo) Execute(ctx context.Context, x domain.Execution, progress func(domain.Progress) error) (domain.Result, error) {
	if domain.MemoryAction(x.Plan.Draft.Action) {
		return d.executeMemory(ctx, x)
	}
	d.mu.Lock()
	if r, ok := d.state.Results[x.ID]; ok {
		d.mu.Unlock()
		return r, nil
	}
	r := domain.Result{State: "running", Phase: "preflight", Message: "DEMO fixture preflight"}
	d.state.Results[x.ID] = r
	if e := saveJSON(d.dir, "state.json", d.state); e != nil {
		d.mu.Unlock()
		return r, e
	}
	fault := d.Fault
	d.Fault = ""
	d.mu.Unlock()
	for _, phase := range []string{"preflight", "source-observed", "external-effect", "readiness"} {
		if progress != nil {
			if e := progress(domain.Progress{Phase: phase, Message: "DEMO: " + phase}); e != nil {
				return domain.Result{}, e
			}
		}
		if fault == phase || fault == "occupied-gpu" || fault == "incomplete-proc" {
			d.mu.Lock()
			r = domain.Result{State: "recovery-required", Phase: phase, Message: "DEMO fault: " + fault + "; previous snapshot retained, AI restart refused", RecoveryRequired: true}
			d.state.Results[x.ID] = r
			e := saveJSON(d.dir, "state.json", d.state)
			d.mu.Unlock()
			return r, e
		}
		select {
		case <-ctx.Done():
			d.mu.Lock()
			r = domain.Result{State: "cancelled", Phase: phase, Message: "DEMO fixture cancellation observed"}
			d.state.Results[x.ID] = r
			e := saveJSON(d.dir, "state.json", d.state)
			d.mu.Unlock()
			return r, e
		case <-time.After(15 * time.Millisecond):
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	a := x.Plan.Draft
	switch a.Action {
	case "profile.switch":
		if d.state.Profile != a.Profile {
			d.state.Previous = d.state.Profile
			d.state.Profile = a.Profile
		}
	case "profile.restore":
		d.state.Profile, d.state.Previous = d.state.Previous, d.state.Profile
	case "model.stage":
		d.state.Staged[a.Model] = true
	case "model.verify":
		if !d.state.Staged[a.Model] {
			r = domain.Result{State: "failed", Phase: "verify", Message: "DEMO: stage model fixture before verification"}
			d.state.Results[x.ID] = r
			return r, saveJSON(d.dir, "state.json", d.state)
		}
	case "serving.start", "serving.restart":
		d.state.Profile = "ai"
	case "serving.stop":
		d.state.Profile = "maintenance"
	}
	d.state.Configuration = x.Plan.Desired
	r = domain.Result{State: "succeeded", Phase: "completed", Message: "DEMO fixture operation completed; no real external effect"}
	if a.Action == "build.start" {
		r.Artifacts = []domain.Artifact{{Name: "demo-build-result", SHA256: domain.Hash(a.Recipe), Size: 0, SourceRevision: worker.SourceCommit, Qualification: "DEMO-only-not-compiled"}}
	}
	d.state.Results[x.ID] = r
	return r, saveJSON(d.dir, "state.json", d.state)
}
func (d *Demo) Inspect(_ context.Context, id string) (domain.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.state.Results[id]
	if !ok {
		return domain.Result{}, errors.New("demo executor has no dispatch record")
	}
	return r, nil
}
func (d *Demo) Export(_ context.Context, h string) (domain.Bundle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return exportBundle(h, "http://127.0.0.1:18000/v1", d.state.Configuration)
}

type LiveOptions struct {
	StateDir          string
	Target            string
	Environment       string
	ReferenceRoot     string
	ModelRoot         string
	HardwarePath      string
	SourcePath        string
	HostSocket        string
	WorkerSocket      string
	ClientBaseURL     string
	CompilerCacheRoot string
	ShaderCacheRoot   string
	ModelBudgetBytes  int64
}
type Live struct {
	opts      LiveOptions
	stager    *Stager
	host      hostexec.Client
	worker    worker.Client
	findModel func(string) (catalog.Model, error)
	mu        sync.Mutex
	active    map[string]bool
}
type dispatch struct {
	Kind           string        `json:"kind"`
	Hash           string        `json:"hash"`
	Result         domain.Result `json:"result"`
	Action         string        `json:"action"`
	SourceRevision string        `json:"source_revision"`
}

func executionKind(action string) string {
	switch action {
	case "model.stage", "model.verify", "caches.configure":
		return "local"
	case "build.start":
		return "worker"
	default:
		return "host"
	}
}

func NewLive(o LiveOptions) (*Live, error) {
	if o.Environment != "dev" && o.Environment != "tst" && o.Environment != "int" {
		return nil, errors.New("live target requires explicit dev, tst or int classification")
	}
	if o.Target == "" || o.ReferenceRoot == "" || o.StateDir == "" || o.SourcePath == "" {
		return nil, errors.New("explicit live adapter paths and target are required")
	}
	if _, e := catalog.Import(o.ReferenceRoot); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Join(o.StateDir, "live-dispatch"), 0700); e != nil {
		return nil, e
	}
	s, _ := NewStager(o.ModelRoot, o.ModelBudgetBytes)
	return &Live{opts: o, stager: s, host: hostexec.Client{Socket: o.HostSocket}, worker: worker.Client{Socket: o.WorkerSocket}, findModel: catalog.Find, active: map[string]bool{}}, nil
}
func (l *Live) Snapshot(ctx context.Context) (domain.Inventory, error) {
	ref, e := catalog.Import(l.opts.ReferenceRoot)
	if e != nil {
		return domain.Inventory{}, e
	}
	h := domain.Hardware{Status: "unknown", TrainedSpeed: "unknown", Channels: "unknown"}
	inv := domain.Inventory{Mode: "live", Target: l.opts.Target, Environment: l.opts.Environment, SourceRevision: ref.SourceRevision, Profile: "unknown", Hardware: h, Models: models(l.stager), Recipes: worker.Recipes(l.opts.WorkerSocket != ""), Harnesses: harnesses(), ClusterMessage: "host executor inventory probe required"}
	if l.stager == nil {
		for i := range inv.Models {
			inv.Models[i].Status = "unavailable — managed model storage requires owner setup"
		}
	}
	inv.Recipes = worker.Recipes(false)
	if l.opts.WorkerSocket != "" {
		if recipes, e := l.worker.Recipes(ctx); e == nil {
			inv.Recipes = recipes
		}
	}
	if l.opts.HostSocket != "" {
		hostInv, e := l.host.Snapshot(ctx)
		if e == nil {
			inv.ClusterAvailable = hostInv.ClusterAvailable
			inv.ClusterMessage = hostInv.ClusterMessage
			inv.Profile = hostInv.Profile
			inv.Hardware = hostInv.Hardware
		} else {
			inv.ClusterMessage = "host executor or scoped cluster inventory unavailable"
		}
	}
	c, e := readConfig(l.opts.SourcePath)
	if e == nil {
		inv.Caches = []domain.Cache{CacheUsage("models", l.opts.ModelRoot, c.Caches.ModelsGiB<<30), CacheUsage("compiler", l.opts.CompilerCacheRoot, c.Caches.CompilerGiB<<30), CacheUsage("shader", l.opts.ShaderCacheRoot, c.Caches.ShaderGiB<<30)}
	}
	return inv, nil
}
func (l *Live) Validate(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.Preview, error) {
	if domain.MemoryAction(d.Action) {
		// Intake must remain usable during a cluster outage. Export independently
		// checks live capacity in the helper; generic inventory would probe twice.
		ref, err := catalog.Import(l.opts.ReferenceRoot)
		if err != nil {
			return domain.Preview{}, err
		}
		if err = domain.ValidateDraft(d, c, domain.Inventory{Target: l.opts.Target}); err != nil {
			return domain.Preview{}, err
		}
		s, err := l.MemoryPreview(ctx, d, c)
		if err != nil {
			return domain.Preview{}, err
		}
		p := preview(d, c, ref.SourceRevision)
		for k, v := range s.Preconditions {
			p.Preconditions[k] = v
		}
		p.Consequences = append(p.Consequences, "Export evidence/candidate/rollback only. No source update, Pod resize, restart or qualification.")
		p.Warnings = append(p.Warnings, s.Limitations...)
		return p, nil
	}
	inv, e := l.Snapshot(ctx)
	if e != nil {
		return domain.Preview{}, e
	}
	if e = domain.ValidateDraft(d, c, inv); e != nil {
		return domain.Preview{}, e
	}
	if (d.Action == "model.stage" || d.Action == "model.verify") && l.stager == nil {
		return domain.Preview{}, domain.Fail("unavailable", "managed model storage is unavailable; restore its reviewed persistent mount")
	}
	if d.Action == "build.start" {
		next, _ := domain.Desired(c, d)
		if e := l.worker.Validate(ctx, worker.Request{ID: "validate-build", Actor: "validated-plan", Recipe: d.Recipe, Budgets: next.Caches, SourceRevision: next.Revision}); e != nil {
			return domain.Preview{}, domain.Fail("unavailable", e.Error())
		}
	}
	switch d.Action {
	case "profile.switch", "profile.restore", "serving.start", "serving.stop", "serving.restart", "serving.configure", "resources.configure":
		if !inv.ClusterAvailable {
			return domain.Preview{}, domain.Fail("unavailable", inv.ClusterMessage)
		}
	}
	p := preview(d, c, inv.SourceRevision)
	p.Preconditions["hardware_boot_id"] = inv.Hardware.BootID
	p.Warnings = append(p.Warnings, "hardware and exact workload qualification are independently enforced by root-owned executor policy")
	return p, nil
}
func (l *Live) Execute(ctx context.Context, x domain.Execution, progress func(domain.Progress) error) (domain.Result, error) {
	ref, e := catalog.Import(l.opts.ReferenceRoot)
	if e != nil {
		return domain.Result{State: "failed", Phase: "reference-preflight", Message: e.Error()}, e
	}
	if expected := x.Plan.Preview.Preconditions["reference_revision"]; expected != "" && expected != ref.SourceRevision {
		return domain.Result{State: "failed", Phase: "source-drift", Message: "reference source changed after planning; nothing was dispatched"}, domain.Fail("source_drift", "reference source changed after planning")
	}
	kind := executionKind(x.Plan.Draft.Action)
	l.mu.Lock()
	recordPath := filepath.Join(l.opts.StateDir, "live-dispatch", x.ID+".json")
	if b, e := os.ReadFile(recordPath); e == nil {
		var r dispatch
		if json.Unmarshal(b, &r) != nil {
			l.mu.Unlock()
			return domain.Result{}, errors.New("adapter dispatch record corrupt")
		}
		if r.Hash != domain.Hash(x) {
			l.mu.Unlock()
			return domain.Result{}, errors.New("adapter operation payload conflict")
		}
		l.mu.Unlock()
		return l.Inspect(ctx, x.ID)
	} else if !os.IsNotExist(e) {
		l.mu.Unlock()
		return domain.Result{}, errors.New("adapter dispatch record cannot be read safely")
	}
	rec := dispatch{Kind: kind, Hash: domain.Hash(x), Action: x.Plan.Draft.Action, SourceRevision: x.Plan.Desired.Revision, Result: domain.Result{State: "running", Phase: "dispatch-intent", Message: "durable adapter intent recorded"}}
	e = saveJSON(filepath.Dir(recordPath), filepath.Base(recordPath), rec)
	if e == nil {
		// The intent and active marker share the same mutex so a concurrent
		// recovery probe cannot settle a just-dispatched local operation before
		// this goroutine reaches the adapter.
		l.active[x.ID] = true
	}
	l.mu.Unlock()
	if e != nil {
		return domain.Result{}, e
	}
	defer func() {
		l.mu.Lock()
		delete(l.active, x.ID)
		l.mu.Unlock()
	}()
	var result domain.Result
	switch kind {
	case "worker":
		req := worker.Request{ID: x.ID, Actor: x.Actor, Recipe: x.Plan.Draft.Recipe, Budgets: x.Plan.Desired.Caches, SourceRevision: x.Plan.Desired.Revision}
		result, e = l.worker.Execute(ctx, req)
		if e == nil {
			result, e = l.waitWorker(ctx, x.ID, progress)
		}
	case "host":
		req := hostexec.Request{Version: hostexec.ContractVersion, ID: x.ID, ExecutionIdentity: x.Actor, Draft: x.Plan.Draft, Desired: x.Plan.Desired}
		req.Draft.SourceRevision = x.Plan.Desired.Revision
		req.PayloadHash = hostexec.RequestHash(req)
		var hr hostexec.Result
		hr, e = l.host.Execute(ctx, req)
		if e != nil && hr.State == "failed" {
			result = domain.Result{State: "failed", Phase: "executor-refused", Message: hr.Message}
		}
		if e == nil {
			for !domain.Terminal(hr.State) {
				if progress != nil {
					if pe := progress(domain.Progress{Phase: hr.Phase, Message: hr.Message}); pe != nil {
						return domain.Result{}, pe
					}
				}
				select {
				case <-ctx.Done():
					return domain.Result{State: "recovery-required", Phase: "executor-detached", Message: "helper continues independently; inspect its durable operation", RecoveryRequired: true}, nil
				case <-time.After(250 * time.Millisecond):
				}
				hr, e = l.host.Status(ctx, x.ID)
				if e != nil {
					break
				}
			}
			result = domain.Result{State: hr.State, Phase: hr.Phase, Message: hr.Message, RecoveryRequired: hr.State == "recovery-required"}
			if x.Plan.Draft.Action == "cpu-policy.export" && hr.State == "succeeded" {
				result.Artifacts, e = cpuArtifacts(hr.Data, x.Plan.Desired.Revision)
			}
			if domain.MemoryAction(x.Plan.Draft.Action) && hr.State == "succeeded" {
				result.Artifacts, e = memoryArtifacts(hr.Data, x.Plan.Desired.Revision)
			}
		}
	default:
		switch x.Plan.Draft.Action {
		case "model.stage", "model.verify":
			if l.stager == nil {
				e = domain.Fail("unavailable", "managed model storage is unavailable")
				break
			}
			m, fe := l.findModel(x.Plan.Draft.Model)
			if fe != nil {
				e = fe
				break
			}
			if x.Plan.Draft.Action == "model.stage" {
				result.Artifacts, e = l.stager.Stage(ctx, m, func(file string, n int64) error {
					if progress == nil {
						return nil
					}
					return progress(domain.Progress{Phase: "download-verified", Message: file, Bytes: n})
				})
			} else {
				result.Artifacts, e = l.stager.Verify(ctx, m)
			}
		case "caches.configure":
			if l.stager != nil {
				l.stager.mu.Lock()
				l.stager.budget = x.Plan.Desired.Caches.ModelsGiB << 30
				l.stager.mu.Unlock()
			}
		}
		if e == nil {
			result.State = "succeeded"
			result.Phase = "completed"
			result.Message = "managed asset/source operation completed; workload qualification unchanged"
		}
	}
	if e != nil {
		proven := domain.Terminal(result.State) && !result.RecoveryRequired
		if !proven {
			result = domain.Result{State: "failed", Phase: "adapter-failed", Message: e.Error()}
		}
		if kind != "local" && !proven {
			result.State = "recovery-required"
			result.RecoveryRequired = true
			result.Message = "executor outcome uncertain; inspect its durable journal"
		}
	}
	l.mu.Lock()
	rec.Result = result
	saveErr := saveJSON(filepath.Dir(recordPath), filepath.Base(recordPath), rec)
	l.mu.Unlock()
	if saveErr != nil {
		return domain.Result{}, saveErr
	}
	return result, e
}
func cpuArtifacts(data []byte, revision string) ([]domain.Artifact, error) {
	if len(data) > 1<<20 {
		return nil, errors.New("CPU export exceeds bounded artifact size")
	}
	var files map[string]json.RawMessage
	if json.Unmarshal(data, &files) != nil || len(files) != 2 {
		return nil, errors.New("invalid CPU export contract")
	}
	var out []domain.Artifact
	for _, name := range []string{"resource-plan.json", "ansible-vars.json"} {
		b, ok := files[name]
		if !ok || !json.Valid(b) {
			return nil, errors.New("CPU export missing expected file")
		}
		sum := sha256.Sum256(b)
		out = append(out, domain.Artifact{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b)), SourceRevision: revision, Qualification: "offline-plan-not-applied", Content: string(b)})
	}
	return out, nil
}
func (l *Live) waitWorker(ctx context.Context, id string, p func(domain.Progress) error) (domain.Result, error) {
	for {
		r, e := l.worker.Status(ctx, id)
		if e != nil {
			return r, e
		}
		if domain.Terminal(r.State) {
			return r, nil
		}
		if p != nil {
			if e = p(domain.Progress{Phase: r.Phase, Message: r.Message}); e != nil {
				return r, e
			}
		}
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = l.worker.Cancel(cancelCtx, id)
			cancel()
			return domain.Result{State: "recovery-required", Phase: "cancel-requested", Message: "worker cancellation requested; inspect descendant/cgroup disposition", RecoveryRequired: true}, nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}
func (l *Live) Inspect(ctx context.Context, id string) (domain.Result, error) {
	if strings.ContainsAny(id, "/\\.") || len(id) < 8 {
		return domain.Result{}, errors.New("invalid operation identity")
	}
	b, e := os.ReadFile(filepath.Join(l.opts.StateDir, "live-dispatch", id+".json"))
	if e != nil {
		return domain.Result{}, e
	}
	var r dispatch
	if json.Unmarshal(b, &r) != nil {
		return domain.Result{}, errors.New("corrupt adapter record")
	}
	if r.Kind == "host" {
		if r.Result.State == "failed" && r.Result.Phase == "executor-refused" {
			return r.Result, nil
		}
		h, e := l.host.Status(ctx, id)
		result := domain.Result{State: h.State, Phase: h.Phase, Message: h.Message, RecoveryRequired: h.State == "recovery-required"}
		if e == nil && r.Action == "cpu-policy.export" && h.State == "succeeded" {
			result.Artifacts, e = cpuArtifacts(h.Data, r.SourceRevision)
		}
		if e == nil && domain.MemoryAction(r.Action) && h.State == "succeeded" {
			result.Artifacts, e = memoryArtifacts(h.Data, r.SourceRevision)
		}
		return result, e
	}
	if r.Kind == "worker" {
		return l.worker.Status(ctx, id)
	}
	if !domain.Terminal(r.Result.State) {
		return domain.Result{State: "recovery-required", Phase: "local-effect-uncertain", Message: "inspect managed partial/publication receipt and source; external effects are not retried", RecoveryRequired: true}, nil
	}
	return r.Result, nil
}

// InspectExecution binds recovery to the original durable execution. It never
// accepts a caller-selected executor, model path or replacement plan.
func (l *Live) InspectExecution(ctx context.Context, x domain.Execution) (domain.Result, error) {
	if strings.ContainsAny(x.ID, "/\\.") || len(x.ID) < 8 {
		return domain.Result{}, errors.New("invalid operation identity")
	}
	kind := executionKind(x.Plan.Draft.Action)
	l.mu.Lock()
	active := l.active[x.ID]
	recordPath := filepath.Join(l.opts.StateDir, "live-dispatch", x.ID+".json")
	b, e := os.ReadFile(recordPath)
	l.mu.Unlock()
	if active && kind == "local" {
		return domain.Result{State: "running", Phase: "local-execution-active", Message: "local staging/verification is still active; no recovery probe was run"}, nil
	}
	if os.IsNotExist(e) {
		if kind == "local" {
			return l.inspectLocalExecution(ctx, x)
		}
		return domain.Result{}, errors.New("executor dispatch record is absent; outcome is not proven")
	}
	if e != nil {
		return domain.Result{}, e
	}
	var r dispatch
	if json.Unmarshal(b, &r) != nil {
		return domain.Result{}, errors.New("adapter dispatch record corrupt")
	}
	if r.Hash == "" || r.Hash != domain.Hash(x) || r.Kind != kind {
		return domain.Result{}, errors.New("adapter dispatch record does not bind the original execution")
	}
	// Action and source revision were added as defense-in-depth fields. Older
	// records can omit them only because their immutable execution hash already
	// binds the original persisted plan.
	if r.Action != "" && r.Action != x.Plan.Draft.Action {
		return domain.Result{}, errors.New("adapter dispatch action does not match the original execution")
	}
	if r.SourceRevision != "" && r.SourceRevision != x.Plan.Desired.Revision {
		return domain.Result{}, errors.New("adapter dispatch source revision does not match the original execution")
	}
	switch kind {
	case "local":
		return l.inspectLocalExecution(ctx, x)
	case "worker":
		return l.worker.Status(ctx, x.ID)
	default:
		if r.Result.State == "failed" && r.Result.Phase == "executor-refused" {
			return r.Result, nil
		}
		h, statusErr := l.host.Status(ctx, x.ID)
		result := domain.Result{State: h.State, Phase: h.Phase, Message: h.Message, RecoveryRequired: h.State == "recovery-required"}
		if statusErr == nil && x.Plan.Draft.Action == "cpu-policy.export" && h.State == "succeeded" {
			result.Artifacts, statusErr = cpuArtifacts(h.Data, x.Plan.Desired.Revision)
		}
		if statusErr == nil && domain.MemoryAction(x.Plan.Draft.Action) && h.State == "succeeded" {
			result.Artifacts, statusErr = memoryArtifacts(h.Data, x.Plan.Desired.Revision)
		}
		return result, statusErr
	}
}

func (l *Live) inspectLocalExecution(ctx context.Context, x domain.Execution) (domain.Result, error) {
	if x.Plan.Draft.Action == "caches.configure" {
		c, e := readConfig(l.opts.SourcePath)
		if e == nil && c.Revision == x.Plan.Desired.Revision && c.Caches == x.Plan.Desired.Caches {
			return domain.Result{State: "succeeded", Phase: "local-cache-budget-observed", Message: "durable managed cache budget matches the original local execution"}, nil
		}
		return domain.Result{State: "recovery-required", Phase: "local-source-inspection-required", Message: "cache budget source differs or is unavailable; no host or worker recovery was selected", RecoveryRequired: true}, nil
	}
	if x.Plan.Draft.Action != "model.stage" && x.Plan.Draft.Action != "model.verify" {
		return domain.Result{}, errors.New("unsupported local recovery action")
	}
	if l.stager == nil {
		return domain.Result{State: "recovery-required", Phase: "model-storage-unavailable", Message: "managed model storage is unavailable; local publication evidence cannot be inspected", RecoveryRequired: true}, nil
	}
	m, e := l.findModel(x.Plan.Draft.Model)
	if e != nil {
		return domain.Result{State: "recovery-required", Phase: "model-catalog-drift", Message: "the original selected model is no longer in the reviewed catalog", RecoveryRequired: true}, nil
	}
	if expected := x.Plan.Preview.Preconditions["reference_revision"]; expected != "" {
		ref, importErr := catalog.Import(l.opts.ReferenceRoot)
		if importErr != nil || ref.SourceRevision != expected {
			return domain.Result{State: "recovery-required", Phase: "source-drift", Message: "reviewed reference source changed or is unavailable; local publication remains fenced", RecoveryRequired: true}, nil
		}
	}
	artifacts, verifyErr := l.stager.Verify(ctx, m)
	if verifyErr == nil {
		return domain.Result{State: "succeeded", Phase: "local-publication-verified", Message: "verified staged model receipt, hashes and reader permissions", Artifacts: artifacts}, nil
	}
	// Recovery inspection is read-only. A restrictive legacy mode can be proved
	// separately from content corruption, but only an explicit repeat model.stage
	// repairs it through the bounded stager workflow.
	if _, contentErr := l.stager.VerifyContent(ctx, m); contentErr == nil {
		return domain.Result{State: "failed", Phase: "local-publication-permissions-invalid", Message: "published receipt and hashes verify, but reader permissions are invalid; explicitly rerun model.stage to repair this managed snapshot"}, nil
	}
	present, presentErr := l.stager.snapshotPresent(m)
	if presentErr != nil {
		return domain.Result{State: "recovery-required", Phase: "local-publication-unknown", Message: "managed model publication path cannot be inspected safely", RecoveryRequired: true}, nil
	}
	if !present {
		return domain.Result{State: "failed", Phase: "local-interrupted-no-publication", Message: "no published model snapshot exists; the interrupted local operation did not dispatch host or worker effects"}, nil
	}
	return domain.Result{State: "recovery-required", Phase: "local-publication-uncertain", Message: "published model receipt, hashes or reader permissions are invalid; preserve the local recovery fence", RecoveryRequired: true}, nil
}
func (l *Live) Export(_ context.Context, h string) (domain.Bundle, error) {
	c, e := readConfig(l.opts.SourcePath)
	if e != nil {
		return domain.Bundle{}, e
	}
	return exportBundle(h, l.opts.ClientBaseURL, c)
}

var _ domain.Adapter = (*Demo)(nil)
var _ domain.Adapter = (*Live)(nil)
