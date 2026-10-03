package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type ClientProfile struct {
	Harness   string `json:"harness"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	Context   int    `json:"context_tokens"`
	MaxOutput int    `json:"max_output_tokens"`
}
type BundleMetadata struct {
	Schema             int                          `json:"schema"`
	Status             string                       `json:"status"`
	DefaultHarness     string                       `json:"default_harness"`
	PreferenceOrder    []string                     `json:"preference_order"`
	AutomaticFallback  bool                         `json:"automatic_fallback"`
	BaseURL            string                       `json:"base_url"`
	Model              string                       `json:"model"`
	Context            int                          `json:"context_tokens"`
	MaxOutput          int                          `json:"max_output_tokens"`
	OutputLimitSupport map[string]string            `json:"output_limit_support"`
	ConfigSHA256       map[string]string            `json:"config_sha256"`
	Sources            map[string]map[string]string `json:"sources"`
}

func ValidateClient(p ClientProfile) error {
	if p.Harness != "qwen" && p.Harness != "dsh" && p.Harness != "hermes" {
		return errors.New("harness must be qwen, dsh or hermes")
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1" || u.Opaque != "" {
		return errors.New("endpoint must be a credential-free /v1 URL")
	}
	if u.Scheme == "http" {
		if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
			return errors.New("HTTP client endpoints require a loopback tunnel")
		}
	} else if u.Scheme != "https" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`).MatchString(u.Hostname()) {
		return errors.New("endpoint must use HTTPS")
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return errors.New("endpoint port is invalid")
		}
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:-]{0,199}$`).MatchString(p.Model) || strings.Contains(p.Model, "..") {
		return errors.New("invalid served model identifier")
	}
	if p.Context < 2048 || p.Context > 262144 || p.MaxOutput < 128 || p.MaxOutput > p.Context/2 {
		return errors.New("context must be 2048..262144; output must be 128..half the context")
	}
	return nil
}

// NativeBundle preserves the installer's schema-1 bundle and three pinned client
// schemas. JSON files with .yaml extensions are native JSON-compatible YAML.
func NativeBundle(p ClientProfile) (map[string][]byte, error) {
	if err := ValidateClient(p); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	put := func(path string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		files[path] = append(b, '\n')
		return e
	}
	qwen := map[string]any{"general": map[string]any{"enableAutoUpdate": false}, "privacy": map[string]any{"usageStatisticsEnabled": false}, "telemetry": map[string]any{"enabled": false}, "security": map[string]any{"auth": map[string]any{"selectedType": "openai"}, "folderTrust": map[string]any{"enabled": true}}, "tools": map[string]any{"approvalMode": "default"}, "context": map[string]any{"fileName": []string{"AGENTS.md", "QWEN.md"}}, "model": map[string]any{"name": p.Model}, "modelProviders": map[string]any{"openai": []any{map[string]any{"id": p.Model, "name": "Workstation local Qwen", "baseUrl": p.BaseURL, "envKey": "WORKSTATION_AGENT_API_KEY", "generationConfig": map[string]any{"contextWindowSize": p.Context, "timeout": 120000, "streamIdleTimeoutMs": 180000, "maxRetries": 1, "samplingParams": map[string]any{"max_tokens": p.MaxOutput}}}}}}
	dsh := map[string]any{"agent-default-model": map[string]any{"provider": "workstation", "model": p.Model}, "llm-pi-ai": map[string]any{"providers": map[string]any{"workstation": map[string]any{"api": "openai-completions", "baseURL": p.BaseURL, "apiKeyEnv": "WORKSTATION_AGENT_API_KEY", "models": []any{map[string]any{"id": p.Model, "contextWindow": p.Context, "maxTokens": p.MaxOutput, "input": []string{"text"}}}}}}}
	hermes := map[string]any{"model": map[string]any{"provider": "custom", "default": p.Model, "base_url": p.BaseURL, "api_key": "${WORKSTATION_AGENT_API_KEY}", "context_length": p.Context}, "approvals": map[string]any{"mode": "manual"}, "terminal": map[string]any{"backend": "local"}, "fallback_providers": []string{}, "compression": map[string]any{"enabled": true, "threshold": 0.5, "target_ratio": 0.2, "protect_last_n": 20}, "auxiliary": map[string]any{"compression": map[string]any{"provider": "main"}, "title_generation": map[string]any{"enabled": false}, "background_review": map[string]any{"enabled": false}}, "mcp_servers": map[string]any{}}
	for name, v := range map[string]any{"qwen/settings.json": qwen, "dsh/settings.yaml": dsh, "hermes/config.yaml": hermes} {
		if e := put(name, v); e != nil {
			return nil, e
		}
	}
	hashes := map[string]string{}
	for h, f := range map[string]string{"qwen": "qwen/settings.json", "dsh": "dsh/settings.yaml", "hermes": "hermes/config.yaml"} {
		sum := sha256.Sum256(files[f])
		hashes[h] = hex.EncodeToString(sum[:])
	}
	pins := HarnessPins()
	meta := BundleMetadata{Schema: 1, Status: "configured-not-qualified", DefaultHarness: p.Harness, PreferenceOrder: []string{"qwen", "dsh", "hermes"}, BaseURL: p.BaseURL, Model: p.Model, Context: p.Context, MaxOutput: p.MaxOutput, OutputLimitSupport: map[string]string{"qwen": "request", "dsh": "request-default", "hermes": "unsupported-provider-owned"}, ConfigSHA256: hashes, Sources: map[string]map[string]string{"qwen": {"repository": "https://github.com/QwenLM/qwen-code", "commit": pins["QWEN_CODE_COMMIT"], "version": pins["QWEN_CODE_VERSION"]}, "dsh": {"repository": "https://github.com/deepseek-ai/deepseek-harness", "commit": pins["DSH_COMMIT"]}, "hermes": {"repository": "https://github.com/NousResearch/hermes-agent", "commit": pins["HERMES_AGENT_COMMIT"]}}}
	if e := put("bundle.json", meta); e != nil {
		return nil, e
	}
	return files, nil
}

// VerifyNative compares complete regenerated native configurations as well as
// their hashes. Altering bundle route metadata and a checksum cannot smuggle
// another tool policy or secondary service into the local client configuration.
func VerifyNative(files map[string][]byte, selected, mode string) (ClientProfile, error) {
	var m BundleMetadata
	if err := json.Unmarshal(files["bundle.json"], &m); err != nil {
		return ClientProfile{}, errors.New("invalid bundle metadata")
	}
	if selected == "" {
		selected = m.DefaultHarness
	}
	p := ClientProfile{selected, m.BaseURL, m.Model, m.Context, m.MaxOutput}
	if m.Schema != 1 || m.AutomaticFallback || strings.Join(m.PreferenceOrder, ",") != "qwen,dsh,hermes" {
		return p, errors.New("unsupported bundle contract")
	}
	if mode != "cli" && mode != "acp" {
		return p, errors.New("mode must be cli or acp")
	}
	if selected == "dsh" && mode != "acp" {
		return p, errors.New("pinned DSH supports ACP only, no interactive CLI profile")
	}
	expected, err := NativeBundle(p)
	if err != nil {
		return p, err
	}
	var canonicalMetadata BundleMetadata
	if json.Unmarshal(expected["bundle.json"], &canonicalMetadata) != nil || m.Status != canonicalMetadata.Status || !reflect.DeepEqual(m.Sources, canonicalMetadata.Sources) || !reflect.DeepEqual(m.OutputLimitSupport, canonicalMetadata.OutputLimitSupport) {
		return p, errors.New("bundle source pins or capability contract differ from reviewed revisions")
	}
	path := map[string]string{"qwen": "qwen/settings.json", "dsh": "dsh/settings.yaml", "hermes": "hermes/config.yaml"}[selected]
	sum := sha256.Sum256(files[path])
	if hex.EncodeToString(sum[:]) != m.ConfigSHA256[selected] {
		return p, errors.New("native bundle checksum mismatch")
	}
	var actual, canonical any
	if json.Unmarshal(files[path], &actual) != nil || json.Unmarshal(expected[path], &canonical) != nil {
		return p, errors.New("native bundle is malformed")
	}
	a, _ := json.Marshal(actual)
	c, _ := json.Marshal(canonical)
	if string(a) != string(c) {
		return p, fmt.Errorf("%s native route, budget or policy differs from pinned schema", selected)
	}
	return p, nil
}
