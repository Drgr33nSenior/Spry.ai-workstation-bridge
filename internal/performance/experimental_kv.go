package performance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
)

// ExperimentalKVDescriptor is optional retained runtime evidence for the
// isolated gfx1201 candidate. It is distinct from model weight quantization.
// Its intentionally closed schema prevents a sealed performance bundle from
// becoming a generic runtime configuration channel.
type ExperimentalKVDescriptor struct {
	Schema              int                         `json:"schema"`
	Runtime             string                      `json:"runtime"`
	SourceRepository    string                      `json:"source_repository"`
	SourceRevision      string                      `json:"source_revision"`
	SourceTreeSHA256    string                      `json:"source_tree_sha256"`
	PatchSHA256         string                      `json:"patch_sha256"`
	BinarySHA256        *string                     `json:"binary_sha256"`
	CompilerRevision    string                      `json:"compiler_revision"`
	DependenciesSHA256  string                      `json:"dependencies_sha256"`
	Codec               string                      `json:"codec"`
	ConfigurationSHA256 string                      `json:"configuration_sha256"`
	Configuration       ExperimentalKVConfiguration `json:"configuration"`
	RequestedMode       string                      `json:"requested_mode"`
	ObservedMode        string                      `json:"observed_mode"`
	Qualification       string                      `json:"qualification"`
	RestartRequired     bool                        `json:"restart_required"`
}

type ExperimentalKVConfiguration struct {
	GPUArch        string `json:"gpu_arch"`
	TensorParallel int    `json:"tensor_parallel"`
	ComputeDType   string `json:"compute_dtype"`
	Attention      string `json:"attention"`
	HeadDimension  int    `json:"head_dimension"`
	KVHeads        int    `json:"kv_heads"`
	QueryHeads     int    `json:"query_heads"`
	PageSize       int    `json:"page_size"`
	Rotation       string `json:"rotation"`
}

var compilerRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._+:/-]{0,239}$`)

func canonicalExperimentalKVConfiguration(c ExperimentalKVConfiguration) (string, error) {
	// Map encoding is key-sorted by encoding/json. This matches the Python
	// producer's json.dumps(sort_keys=True, separators=(",", ":")) contract.
	v := map[string]any{
		"attention": c.Attention, "compute_dtype": c.ComputeDType,
		"gpu_arch": c.GPUArch, "head_dimension": c.HeadDimension,
		"kv_heads": c.KVHeads, "page_size": c.PageSize,
		"query_heads": c.QueryHeads, "rotation": c.Rotation,
		"tensor_parallel": c.TensorParallel,
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func validExperimentalKVHeadDimension(v int) bool {
	return v == 32 || v == 64 || v == 128 || v == 256
}

// ValidateExperimentalKVDescriptor validates one exact stable descriptor.
// measured requires a verified compressed cache writer/reader observation and
// a mapped candidate library digest. Source-only descriptors remain explicit
// but cannot satisfy that stronger condition.
func ValidateExperimentalKVDescriptor(d ExperimentalKVDescriptor, measured bool) error {
	if d.Schema != 1 || d.Runtime != "sglang" || d.SourceRepository != "sgl-project/sglang" ||
		!regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(d.SourceRevision) ||
		!Digest.MatchString(d.SourceTreeSHA256) || !Digest.MatchString(d.PatchSHA256) ||
		!Digest.MatchString(d.DependenciesSHA256) || !compilerRevision.MatchString(d.CompilerRevision) ||
		d.Codec != "ultraquant-rdna4-v1" || d.RequestedMode != "ultraquant-rdna4-v1" ||
		d.Qualification != "unqualified" || !d.RestartRequired {
		return errors.New("invalid experimental KV descriptor identity")
	}
	if d.BinarySHA256 != nil && !Digest.MatchString(*d.BinarySHA256) {
		return errors.New("invalid experimental KV binary digest")
	}
	c := d.Configuration
	if c.GPUArch != "gfx1201" || c.TensorParallel != 1 || c.ComputeDType != "float16" ||
		c.Attention != "causal-full" || !validExperimentalKVHeadDimension(c.HeadDimension) ||
		c.KVHeads < 1 || c.KVHeads > 64 || c.QueryHeads < c.KVHeads || c.QueryHeads > 128 ||
		c.QueryHeads%c.KVHeads != 0 || c.PageSize != 1 || c.Rotation != "post-rope-qk" {
		return errors.New("unsupported experimental KV configuration")
	}
	want, err := canonicalExperimentalKVConfiguration(c)
	if err != nil || d.ConfigurationSHA256 != want {
		return errors.New("experimental KV configuration digest changed")
	}
	if d.ObservedMode != "not-run" && d.ObservedMode != "compressed-gpu" {
		return errors.New("invalid experimental KV observed mode")
	}
	if d.ObservedMode == "compressed-gpu" && d.BinarySHA256 == nil {
		return errors.New("compressed experimental KV observation lacks binary digest")
	}
	if measured && (d.ObservedMode != "compressed-gpu" || d.BinarySHA256 == nil) {
		return errors.New("experimental KV measured evidence requires compressed GPU execution")
	}
	return nil
}

func DecodeExperimentalKVDescriptor(data []byte, measured bool) (*ExperimentalKVDescriptor, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("experimental KV runtime evidence is malformed")
	}
	v, ok := raw["experimental_kv"]
	if !ok {
		return nil, nil
	}
	if string(v) == "null" {
		return nil, errors.New("experimental KV descriptor cannot be null")
	}
	var d ExperimentalKVDescriptor
	decoder := json.NewDecoder(bytes.NewReader(v))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil || decoder.More() {
		return nil, errors.New("experimental KV descriptor is malformed")
	}
	if err := ValidateExperimentalKVDescriptor(d, measured); err != nil {
		return nil, err
	}
	return &d, nil
}

func nestedEvidencePath(prefix, name string) (string, bool) {
	if !Relative(prefix) || !Relative(name) {
		return "", false
	}
	joined := filepath.ToSlash(filepath.Join(prefix, name))
	return joined, Relative(joined)
}

func runtimeRequestedExperimentalKV(data []byte) bool {
	var runtime struct {
		Launch []map[string]json.RawMessage `json:"launch"`
	}
	if json.Unmarshal(data, &runtime) != nil {
		// Existing incomplete evidence remains analyzable by the installer. Only
		// a concrete, recognized candidate request activates this extra fence.
		return false
	}
	for _, launch := range runtime.Launch {
		var backend string
		if json.Unmarshal(launch["--attention-backend"], &backend) == nil && backend == "ultraquant-rdna4-v1" {
			return true
		}
	}
	return false
}

func runtimeHasExperimentalKVDescriptor(data []byte) bool {
	var runtime map[string]json.RawMessage
	if json.Unmarshal(data, &runtime) != nil {
		return false
	}
	_, ok := runtime["experimental_kv"]
	return ok
}

// validateExperimentalKVBundle follows only the typed comparison spec and the
// typed profile manifest's runtime source. It intentionally does not scan JSON
// values for paths. Ordinary comparisons without this optional descriptor keep
// their established evidence contract.
func validateExperimentalKVBundle(root string, m Manifest) error {
	if m.Kind != "comparison" {
		return nil
	}
	data, err := safefile.Read(filepath.Join(root, "spec.json"), MaxFile)
	if err != nil {
		return nil
	}
	var spec struct {
		BaselineBundle  string `json:"baseline_bundle"`
		CandidateBundle string `json:"candidate_bundle"`
	}
	if json.Unmarshal(data, &spec) != nil || !Relative(spec.BaselineBundle) || !Relative(spec.CandidateBundle) {
		return nil
	}
	profileDescriptor := func(bundle string) (*ExperimentalKVDescriptor, error) {
		profileManifest, ok := nestedEvidencePath(bundle, "manifest.json")
		if !ok || !Digest.MatchString(m.Files[profileManifest]) {
			return nil, nil
		}
		profileData, readErr := safefile.Read(filepath.Join(root, profileManifest), MaxFile)
		if readErr != nil {
			return nil, nil
		}
		var profile struct {
			Sources map[string]json.RawMessage `json:"sources"`
		}
		if json.Unmarshal(profileData, &profile) != nil {
			return nil, nil
		}
		rawRuntime, ok := profile.Sources["runtime"]
		if !ok {
			return nil, nil
		}
		var runtimeName string
		if json.Unmarshal(rawRuntime, &runtimeName) != nil {
			return nil, nil
		}
		runtimePath, ok := nestedEvidencePath(bundle, runtimeName)
		if !ok || !Digest.MatchString(m.Files[runtimePath]) {
			return nil, nil
		}
		runtime, readErr := safefile.Read(filepath.Join(root, runtimePath), MaxFile)
		if readErr != nil {
			return nil, nil
		}
		requested, hasDescriptor := runtimeRequestedExperimentalKV(runtime), runtimeHasExperimentalKVDescriptor(runtime)
		if !requested && !hasDescriptor {
			return nil, nil
		}
		descriptor, decodeErr := DecodeExperimentalKVDescriptor(runtime, false)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if requested && descriptor == nil {
			return nil, errors.New("requested experimental KV runtime lacks a sealed descriptor")
		}
		return descriptor, nil
	}
	baseline, err := profileDescriptor(spec.BaselineBundle)
	if err != nil {
		return err
	}
	candidate, err := profileDescriptor(spec.CandidateBundle)
	if err != nil {
		return err
	}
	if baseline != nil {
		return errors.New("experimental KV comparison baseline must remain unchanged")
	}
	if candidate != nil && candidate.ObservedMode == "compressed-gpu" && candidate.BinarySHA256 == nil {
		return errors.New("experimental KV comparison candidate lacks mapped binary digest")
	}
	return nil
}
