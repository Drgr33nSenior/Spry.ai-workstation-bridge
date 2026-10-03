// Package catalog holds the reviewed immutable upstream inventory. It does not
// qualify images or execute model code.
package catalog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed models.json
var modelData []byte

type File struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}
type Model struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	Revision     string `json:"revision"`
	License      string `json:"license"`
	Quantization string `json:"quantization"`
	Files        []File `json:"files"`
	Size         int64  `json:"size_bytes"`
	GPUs         int    `json:"gpus"`
	Context      int    `json:"context"`
	Status       string `json:"status"`
}
type Inventory struct {
	Models         []Model           `json:"models"`
	LockHash       string            `json:"lock_sha256"`
	SourceRevision string            `json:"source_revision"`
	Locks          map[string]string `json:"locks"`
}

func Selected() []Model {
	var models []Model
	if err := json.Unmarshal(modelData, &models); err != nil {
		panic(err)
	}
	for i := range models {
		m := &models[i]
		m.ID = strings.TrimPrefix(m.Repository, "Qwen/")
		m.Status = "not-staged-not-qualified"
		for _, f := range m.Files {
			m.Size += f.Size
		}
		switch m.ID {
		case "Qwen3.8-27B-FP8":
			m.Quantization = "serialized-blockwise-FP8"
			m.GPUs = 2
			m.Context = 32768
		case "Qwen3.5-9B":
			m.Quantization = "BF16"
			m.GPUs = 1
			m.Context = 4096
		default:
			m.Quantization = "BF16"
		}
	}
	return models
}

func Find(id string) (Model, error) {
	for _, m := range Selected() {
		if id == m.ID {
			return m, nil
		}
	}
	return Model{}, errors.New("model is outside the reviewed catalog")
}

// ParseAssignments deliberately does not evaluate shell quoting or expansion.
// The reference lock uses literal KEY=value assignments. Unknown keys remain
// inert inventory fields; only explicit consumers may use them.
func ParseAssignments(r io.Reader) (map[string]string, error) {
	values := map[string]string{}
	s := bufio.NewScanner(io.LimitReader(r, 1<<20))
	s.Buffer(make([]byte, 4096), 64<<10)
	keyPattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !keyPattern.MatchString(k) || strings.ContainsAny(v, "\x00\r\n`$") {
			return nil, errors.New("unsupported configuration assignment")
		}
		if _, exists := values[k]; exists {
			return nil, fmt.Errorf("duplicate configuration key %s", k)
		}
		values[k] = v
	}
	return values, s.Err()
}

func Import(root string) (Inventory, error) {
	path := filepath.Join(root, "versions.lock")
	info, err := os.Lstat(path)
	if err != nil {
		return Inventory{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Inventory{}, errors.New("versions.lock must be a bounded regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Inventory{}, err
	}
	locks, err := ParseAssignments(bytes.NewReader(b))
	if err != nil {
		return Inventory{}, err
	}
	models := Selected()
	keys := []string{"SGLANG_DUAL_MODEL", "SGLANG_SINGLE_MODEL", "RAG_EMBEDDING"}
	for i, k := range keys {
		if locks[k+"_REPOSITORY"] != models[i].Repository || locks[k+"_REVISION"] != models[i].Revision {
			return Inventory{}, errors.New("selected model lock changed; review and regenerate Bridge catalog before import")
		}
	}
	for k, v := range HarnessPins() {
		if locks[k] != v {
			return Inventory{}, fmt.Errorf("harness contract drift: %s", k)
		}
	}
	hash := sha256.Sum256(b)
	rev := "sha256:" + hex.EncodeToString(hash[:])
	return Inventory{Models: models, LockHash: hex.EncodeToString(hash[:]), SourceRevision: rev, Locks: locks}, nil
}

func HarnessPins() map[string]string {
	return map[string]string{"QWEN_CODE_VERSION": "0.23.2", "QWEN_CODE_COMMIT": "f56de980b316cd5410f067fbb62357481ebd66b8", "DSH_COMMIT": "c389f96bf3a9b6807cb71ed6bdad5849be0df6d8", "HERMES_AGENT_COMMIT": "13fb5e1eceba51fc45a48b5d95a357e144d42689"}
}
