// Package worker implements the separate unprivileged compilation executor.
// Requests select a named recipe; installed policy supplies every path.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

const SourceCommit = "427291b5b34cd914a31b3fd3b61a68f6184f4b9f"
const SunshineCommit = "cb72dffa3233c5815cd5ba88f09f049dd679ba75"

type Policy struct {
	Version                int               `json:"version"`
	WorkerUID              int               `json:"worker_uid"`
	APIUID                 int               `json:"api_uid"`
	Socket                 string            `json:"socket"`
	StateDir               string            `json:"state_dir"`
	CgroupRoot             string            `json:"cgroup_root"`
	ScratchRoot            string            `json:"scratch_root"`
	CacheRoot              string            `json:"cache_root"`
	InhibitPath            string            `json:"inhibit_path"`
	SourceRoot             string            `json:"source_root"`
	SourceManifest         string            `json:"source_manifest"`
	SourceManifestSHA256   string            `json:"source_manifest_sha256"`
	SourceRevision         string            `json:"source_revision"`
	ROCmRoot               string            `json:"rocm_root"`
	GPUTarget              string            `json:"gpu_target"`
	HardwarePath           string            `json:"hardware_path"`
	ToolchainSHA256        map[string]string `json:"toolchain_sha256"`
	SunshineSourceRoot     string            `json:"sunshine_source_root"`
	SunshineManifest       string            `json:"sunshine_manifest"`
	SunshineManifestSHA256 string            `json:"sunshine_manifest_sha256"`
	Jobs                   int               `json:"jobs"`
	MemoryMiB              int64             `json:"memory_mib"`
	ScratchGiB             int64             `json:"scratch_gib"`
	CacheGiB               int64             `json:"cache_gib"`
	MaxLogBytes            int64             `json:"max_log_bytes"`
	MaxMinutes             int               `json:"max_minutes"`
}
type Request struct {
	ID             string              `json:"id"`
	Actor          string              `json:"actor"`
	Recipe         string              `json:"recipe"`
	Hash           string              `json:"hash"`
	Budgets        domain.CacheBudgets `json:"budgets"`
	SourceRevision string              `json:"source_revision"`
}
type Record struct {
	Request   Request       `json:"request"`
	Result    domain.Result `json:"result"`
	UpdatedAt time.Time     `json:"updated_at"`
}
type Service struct {
	policy            Policy
	mu                sync.Mutex
	running           bool
	persistenceFailed bool
	recovery          map[string]bool
	cancel            map[string]context.CancelFunc
}

func Recipes(configured bool) []domain.Recipe {
	status := "unavailable"
	reason := "reviewed dedicated worker policy and staged source are required"
	if configured {
		status = "available"
		reason = ""
	}
	return []domain.Recipe{{ID: "llama-hip", Revision: SourceCommit, Status: status, Reason: reason, Output: "managed build scratch/<operation>/build/bin/llama-{cli,bench}", Qualification: "candidate-not-installed-not-qualified"}, {ID: "llama-vulkan", Revision: SourceCommit, Status: status, Reason: reason, Output: "managed build scratch/<operation>/build/bin/llama-{cli,bench}", Qualification: "candidate-not-installed-not-qualified"}, {ID: "sunshine-image", Revision: SunshineCommit, Status: "unavailable", Reason: "reviewed offline OCI base, complete signed apt inputs and rootless Buildah toolchain staging required", Output: "managed build scratch/<operation>/sunshine.oci", Qualification: "not-qualified"}, {ID: "therock", Revision: "b927c1865f37fa7bbecf5c7e35dee41b02afbb4f", Status: "unsupported", Reason: "reference implements planning, not a complete supported TheRock build recipe", Output: "none", Qualification: "not-qualified"}}
}
func RequestHash(r Request) string { r.Hash = ""; return domain.Hash(r) }
func LoadPolicy(path string) (Policy, error) {
	var p Policy
	if e := trustedFile(path); e != nil {
		return p, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return p, e
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&p); e != nil {
		return p, e
	}
	if p.Version != 1 || p.WorkerUID == 0 || p.WorkerUID != os.Geteuid() || p.APIUID == p.WorkerUID {
		return p, errors.New("worker requires its separate configured unprivileged UID")
	}
	if p.Jobs < 1 || p.Jobs > 16 || p.MemoryMiB < 4096 || p.MemoryMiB > 49152 || p.ScratchGiB < 1 || p.CacheGiB < 1 || p.MaxLogBytes < 1024 || p.MaxLogBytes > 16<<20 || p.MaxMinutes < 1 || p.MaxMinutes > 720 {
		return p, errors.New("invalid worker bounds")
	}
	if p.SourceRevision != SourceCommit || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.SourceManifestSHA256) {
		return p, errors.New("worker source does not match reviewed recipe")
	}
	for _, v := range []string{p.Socket, p.StateDir, p.CgroupRoot, p.ScratchRoot, p.CacheRoot, p.InhibitPath, p.SourceRoot, p.SourceManifest, p.HardwarePath} {
		if !filepath.IsAbs(v) || v == "/" || strings.ContainsAny(v, "\x00\r\n") {
			return p, errors.New("worker policy requires explicit bounded absolute paths")
		}
	}
	for _, name := range []string{"cmake", "ninja", "ccache", "cc", "c++", "bwrap"} {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.ToolchainSHA256[name]) {
			return p, errors.New("reviewed compiler/toolchain digests are required")
		}
	}
	if p.InhibitPath != "/run/workstation/build-inhibit" {
		return p, errors.New("worker must honor the legacy build inhibition marker")
	}
	return p, nil
}
func New(p Policy) (*Service, error) {
	if e := verifySource(p); e != nil {
		return nil, e
	}
	s := &Service{policy: p, cancel: map[string]context.CancelFunc{}, recovery: map[string]bool{}}
	entries, e := os.ReadDir(p.StateDir)
	if e != nil {
		return nil, e
	}
	for _, f := range entries {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(p.StateDir, f.Name()))
		if e != nil {
			return nil, e
		}
		var r Record
		if json.Unmarshal(b, &r) != nil {
			return nil, errors.New("worker state is corrupt")
		}
		if !validID(r.Request.ID) {
			return nil, errors.New("worker record identity is invalid")
		}
		if !domain.Terminal(r.Result.State) {
			r.Result = restartDisposition(p, r.Request.ID)
			if e = s.save(r); e != nil {
				return nil, e
			}
		}
		if r.Result.RecoveryRequired {
			s.recovery[r.Request.ID] = true
		}
	}
	return s, nil
}
func (s *Service) save(r Record) error {
	r.UpdatedAt = time.Now().UTC()
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(s.policy.StateDir, ".record-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(b)
	}
	if e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(name, filepath.Join(s.policy.StateDir, r.Request.ID+".json")); e != nil {
		return e
	}
	d, e := os.Open(s.policy.StateDir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (s *Service) read(id string) (Record, error) {
	var r Record
	if !validID(id) {
		return r, errors.New("invalid operation ID")
	}
	b, e := os.ReadFile(filepath.Join(s.policy.StateDir, id+".json"))
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(b, &r)
	return r, e
}
func validID(id string) bool { return regexp.MustCompile(`^[a-zA-Z0-9_-]{8,100}$`).MatchString(id) }
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !peerAuthorized(r.Context(), s.policy.APIUID) {
			http.Error(w, "peer not authorized", 403)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/recipes" {
			recipes := Recipes(true)
			if s.policy.SunshineSourceRoot != "" {
				recipes[2].Status = "available"
				recipes[2].Reason = ""
			}
			json.NewEncoder(w).Encode(recipes)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/validate" {
			var request Request
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Hash != RequestHash(request) || (request.Recipe != "llama-hip" && request.Recipe != "llama-vulkan" && request.Recipe != "sunshine-image") {
				http.Error(w, "invalid named build plan", 400)
				return
			}
			if _, e := executionPolicy(s.policy, request); e != nil {
				http.Error(w, e.Error(), 409)
				return
			}
			if _, e := os.Lstat(s.policy.InhibitPath); e == nil || !errors.Is(e, fs.ErrNotExist) {
				http.Error(w, "new builds are inhibited", 409)
				return
			}
			json.NewEncoder(w).Encode(domain.Result{State: "succeeded", Phase: "validated", Message: "build budgets fit installed worker ceilings; source and hardware will be rechecked before execution"})
			return
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/operations/") {
			s.mu.Lock()
			record, e := s.read(strings.TrimPrefix(r.URL.Path, "/operations/"))
			if e == nil && record.Result.RecoveryRequired {
				next := restartDisposition(s.policy, record.Request.ID)
				if !next.RecoveryRequired {
					record.Result = next
					e = s.save(record)
					if e == nil {
						delete(s.recovery, record.Request.ID)
					}
				}
			}
			s.mu.Unlock()
			if e != nil {
				http.Error(w, "operation unavailable", 404)
				return
			}
			json.NewEncoder(w).Encode(record.Result)
			return
		}
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/cancel/") {
			id := strings.TrimPrefix(r.URL.Path, "/cancel/")
			s.mu.Lock()
			cancel := s.cancel[id]
			s.mu.Unlock()
			if cancel == nil {
				http.Error(w, "operation is not running", 409)
				return
			}
			cancel()
			w.WriteHeader(202)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/execute" {
			http.NotFound(w, r)
			return
		}
		var req Request
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF || !validID(req.ID) || req.Hash != RequestHash(req) || (req.Recipe != "llama-hip" && req.Recipe != "llama-vulkan" && req.Recipe != "sunshine-image") || req.Actor == "" {
			http.Error(w, "invalid recipe request", 400)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, e := s.read(req.ID); e == nil {
			if existing.Request.Hash != req.Hash {
				http.Error(w, "operation payload conflict", 409)
				return
			}
			json.NewEncoder(w).Encode(existing.Result)
			return
		}
		if s.running {
			http.Error(w, "worker queue is full", 429)
			return
		}
		if s.persistenceFailed || len(s.recovery) != 0 {
			http.Error(w, "worker persistence or prior descendant recovery is unresolved", 503)
			return
		}
		selected, err := executionPolicy(s.policy, req)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		entries, e := os.ReadDir(s.policy.StateDir)
		if e != nil || len(entries) >= 1024 {
			http.Error(w, "worker record retention is full; owner archive required", 503)
			return
		}
		if _, e := os.Lstat(s.policy.InhibitPath); e == nil || !errors.Is(e, fs.ErrNotExist) {
			http.Error(w, "managed builds inhibited", 409)
			return
		}
		if e := verifySource(selected); e != nil {
			http.Error(w, "reviewed build source validation failed", 409)
			return
		}
		record := Record{Request: req, Result: domain.Result{State: "queued", Phase: "accepted", Message: "named offline build accepted"}}
		if e := s.save(record); e != nil {
			http.Error(w, "worker persistence unavailable", 503)
			return
		}
		s.running = true
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.policy.MaxMinutes)*time.Minute)
		s.cancel[req.ID] = cancel
		go s.run(ctx, record)
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(record.Result)
	})
}
func (s *Service) run(ctx context.Context, r Record) {
	s.mu.Lock()
	r.Result = domain.Result{State: "running", Phase: "compiling", Message: "isolated named build in progress"}
	e := s.save(r)
	s.mu.Unlock()
	if e == nil {
		var selected Policy
		selected, e = executionPolicy(s.policy, r.Request)
		if e == nil {
			r.Result, e = runContained(ctx, selected, r.Request)
		}
	}
	if e != nil {
		r.Result = domain.Result{State: "failed", Phase: "build-failed", Message: e.Error()}
		if ctx.Err() != nil {
			r.Result.State = "cancelled"
			r.Result.Message = "build cgroup stopped; candidate artifacts retained"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = s.save(r); e != nil {
		s.persistenceFailed = true /* Leave running intent durable; restart requires recovery. */
	}
	if r.Result.RecoveryRequired {
		s.recovery[r.Request.ID] = true
	}
	s.cancel[r.Request.ID]()
	delete(s.cancel, r.Request.ID)
	s.running = false
}
func executionPolicy(p Policy, r Request) (Policy, error) {
	b := r.Budgets
	if len(r.SourceRevision) != 64 || b.BuildJobs < 1 || b.BuildJobs > p.Jobs || b.BuildMemoryMiB < 4096 || b.BuildMemoryMiB > p.MemoryMiB || b.ScratchGiB < 1 || b.ScratchGiB > p.ScratchGiB || b.CompilerGiB < 1 || b.CompilerGiB > p.CacheGiB {
		return p, errors.New("requested build/source budgets exceed installed worker ceilings")
	}
	p.Jobs = b.BuildJobs
	p.MemoryMiB = b.BuildMemoryMiB
	p.ScratchGiB = b.ScratchGiB
	p.CacheGiB = b.CompilerGiB
	if r.Recipe == "sunshine-image" {
		if p.SunshineSourceRoot == "" || p.SunshineManifest == "" || len(p.SunshineManifestSHA256) != 64 {
			return p, errors.New("offline Sunshine source/base/package closure not staged")
		}
		p.SourceRoot = p.SunshineSourceRoot
		p.SourceManifest = p.SunshineManifest
		p.SourceManifestSHA256 = p.SunshineManifestSHA256
		p.SourceRevision = SunshineCommit
	}
	return p, nil
}

// Preserve the reference memory-heavy 4 GiB/job and separate 16 GiB host plus
// 8 GiB link reserves. User job count is a ceiling, never an affinity request.
func buildJobs(available, memoryLimit int64, ceiling int) (int, error) {
	budget := available - 16384 - 8192
	if limit := memoryLimit - 8192; limit < budget {
		budget = limit
	}
	jobs := int(budget / 4096)
	if jobs < 1 {
		return 0, errors.New("insufficient available RAM after host and link reserves")
	}
	if jobs > ceiling {
		jobs = ceiling
	}
	return jobs, nil
}
func verifySource(p Policy) error {
	if e := trustedFile(p.SourceManifest); e != nil {
		return e
	}
	b, e := os.ReadFile(p.SourceManifest)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != p.SourceManifestSHA256 {
		return errors.New("source manifest identity mismatch")
	}
	var files map[string]string
	if json.Unmarshal(b, &files) != nil || len(files) == 0 || len(files) > 30000 {
		return errors.New("invalid source manifest")
	}
	root, e := os.OpenRoot(p.SourceRoot)
	if e != nil {
		return e
	}
	defer root.Close()
	count := 0
	err := fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("recipe source must not contain symlinks")
		}
		if e := trustedFile(filepath.Join(p.SourceRoot, path)); e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		count++
		expected, ok := files[path]
		if !ok {
			return errors.New("unreviewed source file")
		}
		f, e := root.Open(path)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, io.LimitReader(f, 64<<30))
		f.Close()
		if e != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return errors.New("source digest mismatch")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != len(files) {
		return errors.New("source manifest contains missing files")
	}
	return nil
}

type Client struct{ Socket string }

func (c Client) call(ctx context.Context, method, path string, body any) (domain.Result, error) {
	var out domain.Result
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://worker"+path, reader)
	if e != nil {
		return out, e
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	}}}
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	if e != nil {
		return out, errors.New("dedicated build worker unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		err := fmt.Errorf("build worker refused request (HTTP %d)", resp.StatusCode)
		if path == "/execute" || path == "/validate" {
			out = domain.Result{State: "failed", Phase: "executor-refused", Message: err.Error()}
		}
		return out, err
	}
	if path != "" && strings.HasPrefix(path, "/cancel/") {
		return domain.Result{State: "cancel-requested"}, nil
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return out, e
}
func (c Client) Execute(ctx context.Context, r Request) (domain.Result, error) {
	r.Hash = RequestHash(r)
	return c.call(ctx, "POST", "/execute", r)
}
func (c Client) Validate(ctx context.Context, r Request) error {
	r.Hash = RequestHash(r)
	_, e := c.call(ctx, "POST", "/validate", r)
	return e
}
func (c Client) Status(ctx context.Context, id string) (domain.Result, error) {
	if !validID(id) {
		return domain.Result{}, errors.New("invalid operation ID")
	}
	return c.call(ctx, "GET", "/operations/"+id, nil)
}
func (c Client) Cancel(ctx context.Context, id string) error {
	if !validID(id) {
		return errors.New("invalid operation ID")
	}
	_, e := c.call(ctx, "POST", "/cancel/"+id, nil)
	return e
}
func (c Client) Recipes(ctx context.Context) ([]domain.Recipe, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	}}
	defer transport.CloseIdleConnections()
	r, e := http.NewRequestWithContext(ctx, "GET", "http://worker/recipes", nil)
	if e != nil {
		return nil, e
	}
	response, e := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Do(r)
	if e != nil {
		return nil, errors.New("dedicated worker unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("dedicated worker refused inventory")
	}
	var recipes []domain.Recipe
	e = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&recipes)
	return recipes, e
}
