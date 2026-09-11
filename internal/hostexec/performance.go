package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	perf "github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/performance"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

// profileBootID is deliberately limited to the canonical dispatcher's boot
// token syntax. It is only a boundary fence for a claimed current identity;
// the canonical dispatcher remains authoritative for identity completeness and
// freshness.
var profileBootID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (p Policy) validatePerformancePolicy() error {
	if len(p.PerformanceSources) > 20 {
		return errors.New("at most twenty reviewed performance bundles")
	}
	for id, s := range p.PerformanceSources {
		if !validID(id) || !perf.Digest.MatchString(s.SHA256) || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || s.Path == "/" {
			return errors.New("performance sources require reviewed IDs, canonical private paths and manifest SHA-256")
		}
	}
	if len(p.PerformanceSources) > 0 {
		for _, name := range perf.Tools {
			if !perf.Digest.MatchString(p.Artifacts["lib/workstation/"+name]) {
				return errors.New("performance analysis requires independently approved installed tool hashes")
			}
		}
	}
	return nil
}
func (e *Executor) validatePerformanceRequest(r Request) error {
	if r.Desired.Revision != r.Desired.ContentRevision() || r.Draft.SourceRevision != r.Desired.Revision {
		return errors.New("performance source revision mismatch")
	}
	b, err := safefile.Read(e.policy.SourcePath, 1<<20)
	if err != nil {
		return err
	}
	var current domain.Configuration
	if config.Decode(b, &current) != nil || current.ContentRevision() != r.Desired.Revision {
		return errors.New("performance canonical configuration changed")
	}
	s, ok := e.policy.PerformanceSources[r.Draft.Performance.EvidenceID]
	if !ok || s.SHA256 != r.Draft.Performance.EvidenceSHA256 {
		return errors.New("performance source is not approved by independent root policy")
	}
	return nil
}
func (e *Executor) performanceInput(ctx context.Context, d domain.Draft) (string, perf.Manifest, error) {
	s := e.policy.PerformanceSources[d.Performance.EvidenceID]
	var manifest perf.Manifest
	if err := e.pathTrust(s.Path, true); err != nil {
		return "", manifest, err
	}
	entries := 0
	if err := filepath.WalkDir(s.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > 40000 {
			return errors.New("performance managed tree entry bound exceeded")
		}
		return e.pathTrust(path, entry.IsDir())
	}); err != nil {
		return "", manifest, err
	}
	m, err := perf.Verify(ctx, s.Path, s.SHA256)
	if err == nil && (m.Kind != d.Performance.Kind || m.SourceRevision != d.SourceRevision || m.Target != e.policy.Target) {
		err = errors.New("performance bundle target, source or kind changed")
	}
	if err == nil && m.Kind == "profile-status" {
		// Missing or malformed observed identity is a canonical read-only
		// unknown, not a reason to infer a current profile. A syntactically
		// usable observed boot ID is different: it must still be tied to the
		// current root-qualified hardware report and kernel boot.
		var spec struct {
			Schema          int    `json:"schema"`
			Kind            string `json:"kind"`
			Selection       string `json:"selection"`
			CurrentIdentity string `json:"current_identity"`
		}
		data, readErr := safefile.Read(filepath.Join(s.Path, "spec.json"), 64<<10)
		if readErr != nil || config.Decode(data, &spec) != nil || !perf.Relative(spec.CurrentIdentity) || !perf.Digest.MatchString(m.Files[spec.CurrentIdentity]) {
			return s.Path, m, errors.New("profile status requires sealed current identity evidence")
		}
		data, readErr = safefile.Read(filepath.Join(s.Path, spec.CurrentIdentity), 64<<10)
		var observed map[string]json.RawMessage
		if readErr != nil || config.Decode(data, &observed) != nil {
			return s.Path, m, errors.New("profile identity evidence unavailable")
		}
		var boot string
		if json.Unmarshal(observed["boot_id"], &boot) == nil && profileBootID.MatchString(boot) {
			current, currentErr := os.ReadFile("/proc/sys/kernel/random/boot_id")
			if currentErr != nil || boot != strings.TrimSpace(string(current)) {
				return s.Path, m, errors.New("profile identity boot changed or unavailable; recollect, do not treat selection as current")
			}
			if err = e.checkHardware(); err != nil {
				return s.Path, m, err
			}
		}
	}
	return s.Path, m, err
}
func (e *Executor) PerformancePreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.PerformanceSummary, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.performancePreview(ctx, d, c)
}
func (e *Executor) performancePreview(ctx context.Context, d domain.Draft, c domain.Configuration) (domain.PerformanceSummary, error) {
	var s domain.PerformanceSummary
	if !domain.PerformanceAction(d.Action) || domain.ValidatePerformanceRequest(d) != nil {
		return s, errors.New("invalid typed performance request")
	}
	if err := e.validate(Request{Draft: d, Desired: c}); err != nil {
		return s, err
	}
	if err := e.verify(); err != nil {
		return s, err
	}
	root, _, err := e.performanceInput(ctx, d)
	if err != nil {
		return s, err
	}
	args := []string{"performance", "inspect", root, d.Performance.EvidenceSHA256}
	data, err := runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), args, append(e.environment(), "PYTHONDONTWRITEBYTECODE=1"), nil, nil, 32<<10)
	if err != nil {
		return s, errors.New("installed performance inspector refused the sealed bundle; inspect private offline validation")
	}
	if err = config.Decode(data, &s); err != nil {
		return s, err
	}
	if err = perf.ValidateSummary(s); err != nil {
		return s, err
	}
	if s.Kind != d.Performance.Kind || s.SHA256 != d.Performance.EvidenceSHA256 {
		return s, errors.New("performance inspector identity mismatch")
	}
	s.EvidenceID = d.Performance.EvidenceID
	s.Preconditions = map[string]string{"performance_manifest": s.SHA256, "performance_source": c.Revision, "performance_policy": policyRevision(e.policy)}
	return s, nil
}
func (e *Executor) executePerformance(ctx context.Context, r Request, lock *os.File) (json.RawMessage, error) {
	s, err := e.performancePreview(ctx, r.Draft, r.Desired)
	if err != nil {
		return nil, err
	}
	root, _, err := e.performanceInput(ctx, r.Draft)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(e.policy.StateDir, "performance")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	used, err := memoryTreeBytes(parent)
	if err != nil {
		return nil, err
	}
	var space syscall.Statfs_t
	if err = syscall.Statfs(parent, &space); err != nil || used+perf.MaxBundle+(64<<20) > 4<<30 || uint64(space.Bavail)*uint64(space.Bsize) < perf.MaxBundle+(96<<20) {
		return nil, errors.New("performance evidence admission budget/free-space reserve unavailable; retain existing evidence")
	}
	dir := filepath.Join(parent, r.ID)
	if err = os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	if err = safefile.SyncDir(parent); err != nil {
		return nil, err
	}
	input := filepath.Join(dir, "inputs")
	if err = perf.Copy(ctx, root, input, s.SHA256); err != nil {
		return nil, err
	}
	output := filepath.Join(dir, "output")
	args := []string{"--config", e.policy.WorkstationConfig, "performance", "export", input, s.SHA256, output, "--target", e.policy.Target, "--source-revision", r.Desired.Revision, "--owner", r.ExecutionIdentity}
	if _, err = runFixed(ctx, filepath.Join(e.policy.RuntimeRoot, "bin/workstationctl"), args, append(e.environment(), "PYTHONDONTWRITEBYTECODE=1"), lock, nil, 32<<10); err != nil {
		return nil, errors.New("performance analysis refused or interrupted; retained private inputs/output are not qualified; no workload, cache deletion or code execution was requested")
	}
	checked, err := e.performancePreview(ctx, r.Draft, r.Desired)
	if err != nil || !reflect.DeepEqual(checked.Preconditions, s.Preconditions) {
		return nil, errors.New("performance preconditions changed during analysis; output retained, not exportable")
	}
	b, err := safefile.Read(filepath.Join(output, "summary.json"), 32<<10)
	if err != nil {
		return nil, err
	}
	var result domain.PerformanceSummary
	if err = config.Decode(b, &result); err != nil {
		return nil, err
	}
	if err = perf.ValidateSummary(result); err != nil {
		return nil, err
	}
	if result.Kind != s.Kind || result.SHA256 != s.SHA256 {
		return nil, errors.New("performance result identity mismatch")
	}
	result.EvidenceID = r.Draft.Performance.EvidenceID
	result.Preconditions = checked.Preconditions
	result.ObservedAt = time.Now().UTC()
	for _, a := range result.Artifacts {
		b, err := safefile.Read(filepath.Join(output, a.Name), perf.MaxArtifact)
		if err != nil || perf.Sum(b) != a.SHA256 || int64(len(b)) != a.Size || a.SourceRevision != r.Desired.Revision {
			return nil, errors.New("performance output hash or source mismatch")
		}
	}
	if err = durableJSON(filepath.Join(dir, "summary.json"), result); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func (e *Executor) PerformanceArtifact(ctx context.Context, id, name string) (domain.Artifact, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !validID(id) || !perf.Relative(name) {
		return domain.Artifact{}, errors.New("invalid performance artifact identity")
	}
	r, ok := e.records[id]
	if !ok || !domain.PerformanceAction(r.Request.Draft.Action) || r.Result.State != "succeeded" {
		return domain.Artifact{}, errors.New("performance artifact needs independent completed helper record")
	}
	var s domain.PerformanceSummary
	if config.Decode(r.Result.Data, &s) != nil || perf.ValidateSummary(s) != nil {
		return domain.Artifact{}, errors.New("invalid retained performance summary")
	}
	if name == "performance-summary.json" {
		// Reconstruct exactly the adapter's bounded synthetic summary from the
		// independent root journal, never from an API-supplied artifact record.
		b, err := json.Marshal(s)
		if err != nil {
			return domain.Artifact{}, err
		}
		return domain.Artifact{Name: name, SHA256: perf.Sum(b), Size: int64(len(b)), SourceRevision: r.Request.Desired.Revision, Qualification: s.Status, Content: string(b)}, nil
	}
	// Historical reports remain inspectable after configuration drift. They are
	// never executable authority; new selection/export must pass fresh planning.
	for _, a := range s.Artifacts {
		if a.Name == name {
			path := filepath.Join(e.policy.StateDir, "performance", id, "output", name)
			if err := e.pathTrust(path, false); err != nil {
				return a, err
			}
			if err := ctx.Err(); err != nil {
				return a, err
			}
			b, err := safefile.Read(path, perf.MaxArtifact)
			if err != nil || perf.Sum(b) != a.SHA256 || int64(len(b)) != a.Size {
				return a, errors.New("performance artifact changed; preserve evidence")
			}
			a.Content = string(b)
			return a, nil
		}
	}
	return domain.Artifact{}, errors.New("performance artifact not in independent result")
}
