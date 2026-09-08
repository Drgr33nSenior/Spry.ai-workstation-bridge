// bridge-validate performs source-only structural checks. It never opens target
// credentials, calls Kubernetes, or substitutes for admission/systemd validation.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/hostexec"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/worker"
	yamljson "github.com/oasdiff/yaml"
	yaml "github.com/oasdiff/yaml3"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/bridge-validate (from the repository root)")
		os.Exit(2)
	}
	if err := validateTree("deployment"); err != nil {
		fmt.Fprintln(os.Stderr, "manifest validation:", err)
		os.Exit(1)
	}
	fmt.Println("PASS — deployment source JSON/YAML, scoped RBAC, and server/helper sandbox distinctions")
	fmt.Println("NOT RUN — Kubernetes admission/scheduling/RBAC enforcement and target systemd behavior require the qualification procedure")
}

func validateTree(root string) error {
	var objects []kubeObject
	jsonCount, yamlCount := 0, 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: source symlinks are refused", path)
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return fmt.Errorf("%s: expected a bounded regular source file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if ext == ".yaml" || ext == ".yml" {
			parsed, err := decodeKubernetes(data)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			objects = append(objects, parsed...)
			yamlCount++
			return nil
		}
		jsonCount++
		if err := validateExample(filepath.Base(path), data); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if jsonCount < 4 {
		return errors.New("required deployment JSON examples are missing")
	}
	if yamlCount == 0 {
		return errors.New("scoped Kubernetes RBAC source is missing")
	}
	if err := validateRBAC(objects); err != nil {
		return err
	}
	return validateUnits(filepath.Join(root, "systemd"))
}

func validateExample(name string, data []byte) error {
	var raw map[string]any
	if err := config.Decode(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		return errors.New("example must be a JSON object")
	}
	if err := rejectInlineCredentials(raw); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(name, "server.") && strings.HasSuffix(name, ".example.json"):
		var c config.Config
		if err := config.Decode(data, &c); err != nil {
			return err
		}
		if c.Mode != "live" || !permittedEnvironment(c.Environment) || c.OwnerUID < 0 || c.Target == "" {
			return errors.New("server example requires explicit live mode, owner, target and permitted environment")
		}
		if c.StateDir == "" || filepath.Dir(c.SourcePath) != c.StateDir || c.HostSocket == "" || c.WorkerSocket == "" {
			return errors.New("server source and executor references are incomplete")
		}
		ip, _, err := net.SplitHostPort(c.Listen)
		if err != nil || net.ParseIP(ip) == nil || net.ParseIP(ip).IsUnspecified() {
			return errors.New("server must use an explicit non-wildcard IP listener")
		}
		origin, err := url.Parse(c.ExternalURL)
		if err != nil || origin.User != nil || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			return errors.New("server origin must be a credential-free origin")
		}
		if origin.Scheme != "http" && origin.Scheme != "https" {
			return errors.New("server origin requires HTTP(S)")
		}
		if !contains(c.AllowedHosts, origin.Host) {
			return errors.New("server origin must be in the explicit Host allowlist")
		}
		if !net.ParseIP(ip).IsLoopback() && (origin.Scheme != "https" || c.TLSCertFile == "" || c.TLSKeyFile == "") {
			return errors.New("network server example requires TLS and certificate references")
		}
	case name == "management.initial.example.json":
		var c domain.Configuration
		if err := config.Decode(data, &c); err != nil {
			return err
		}
		if c.Serving.Model == "" || c.Resources.CPU == 0 || c.Caches.ModelsGiB == 0 {
			return errors.New("initial managed configuration is incomplete")
		}
		// The empty revision is intentional: initial import assigns the content hash.
		if c.Revision != "" && c.Revision != c.ContentRevision() {
			return errors.New("nonempty initial source revision does not match content")
		}
	case name == "host-policy.example.json":
		var p hostexec.Policy
		if err := config.Decode(data, &p); err != nil {
			return err
		}
		if p.Version != 1 || p.AllowedUID == 0 || !permittedEnvironment(p.Environment) || p.Target == "" || p.Context == "" || p.Namespace == "" {
			return errors.New("host example lacks explicit contract, peer, target or environment")
		}
		if !filepath.IsAbs(p.Socket) || p.Socket == "/" || p.SourcePath == "" || p.Kubeconfig == "" {
			return errors.New("host example requires fixed socket/source/credential references")
		}
		// Empty provenance/qualification maps remain intentionally unusable live.
		if p.Artifacts == nil || p.QualifiedConfigurations == nil || p.SessionQualifications == nil {
			return errors.New("host qualification map keys must be explicit, including empty placeholders")
		}
	case name == "worker-policy.example.json":
		var p worker.Policy
		if err := config.Decode(data, &p); err != nil {
			return err
		}
		if p.Version != 1 || p.Socket == "" || p.SourceRevision == "" || raw["worker_uid"] == nil || raw["api_uid"] == nil || p.ToolchainSHA256 == nil {
			return errors.New("worker example must declare identity and reviewed recipe reference fields")
		}
		// Zero UIDs and missing reviewed hashes are deliberate startup refusals.
		if p.InhibitPath != "/run/workstation/build-inhibit" {
			return errors.New("worker must use the legacy cooperative inhibition marker")
		}
	default:
		return errors.New("unrecognized deployment JSON source; add a typed validator for its contract")
	}
	return nil
}
func permittedEnvironment(e string) bool { return e == "dev" || e == "tst" || e == "int" }
func rejectInlineCredentials(value any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			switch strings.ToLower(key) {
			case "token", "password", "bearer_token", "authorization", "private_key", "client-key-data", "client-certificate-data", "api_key":
				if item != nil && item != "" {
					return errors.New("deployment examples must reference credentials, not contain credential values")
				}
			}
			if err := rejectInlineCredentials(item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := rejectInlineCredentials(item); err != nil {
				return err
			}
		}
	}
	return nil
}

type metadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
type rule struct {
	APIGroups     []string `json:"apiGroups"`
	Resources     []string `json:"resources"`
	ResourceNames []string `json:"resourceNames,omitempty"`
	Verbs         []string `json:"verbs"`
}
type roleRef struct {
	APIGroup string `json:"apiGroup"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}
type subject struct {
	Kind      string `json:"kind"`
	APIGroup  string `json:"apiGroup,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type kubeObject struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   metadata          `json:"metadata"`
	Rules      []rule            `json:"rules,omitempty"`
	RoleRef    *roleRef          `json:"roleRef,omitempty"`
	Subjects   []subject         `json:"subjects,omitempty"`
	Automount  *bool             `json:"automountServiceAccountToken,omitempty"`
	Items      []json.RawMessage `json:"items,omitempty"`
}

func decodeKubernetes(data []byte) ([]kubeObject, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var result []kubeObject
	for documents := 0; ; documents++ {
		if documents > 100 {
			return nil, errors.New("too many YAML documents")
		}
		var node yaml.Node
		if err := decoder.Decode(&node); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		b, err := yaml.Marshal(&node)
		if err != nil {
			return nil, err
		}
		b, err = yamljson.YAMLToJSON(b)
		if err != nil {
			return nil, err
		}
		if string(b) == "null" {
			continue
		}
		var object kubeObject
		if err = config.Decode(b, &object); err != nil {
			return nil, err
		}
		if object.Kind == "List" {
			if object.APIVersion != "v1" || len(object.Items) == 0 {
				return nil, errors.New("invalid Kubernetes List")
			}
			for _, item := range object.Items {
				var child kubeObject
				if err = config.Decode(item, &child); err != nil {
					return nil, err
				}
				result = append(result, child)
			}
		} else {
			result = append(result, object)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Kubernetes source has no resources")
	}
	return result, nil
}

var objectName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

func validName(name string) bool {
	return len(name) > 0 && len(name) <= 253 && objectName.MatchString(name)
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func validateRBAC(objects []kubeObject) error {
	index := map[string]kubeObject{}
	serviceAccounts := 0
	roles := 0
	bindings := 0
	for _, object := range objects {
		if len(object.Items) != 0 {
			return errors.New("items belong only to the outer Kubernetes List")
		}
		if !validName(object.Metadata.Name) || strings.Contains(object.Metadata.Name, "cluster-admin") {
			return errors.New("Kubernetes resource requires a bounded reviewed name")
		}
		if object.Metadata.Namespace != "" && !validName(object.Metadata.Namespace) {
			return errors.New("invalid Kubernetes namespace")
		}
		key := object.Kind + "/" + object.Metadata.Namespace + "/" + object.Metadata.Name
		if _, exists := index[key]; exists {
			return errors.New("duplicate Kubernetes object identity")
		}
		index[key] = object
		switch object.Kind {
		case "ServiceAccount":
			serviceAccounts++
			if object.APIVersion != "v1" || object.Metadata.Namespace == "" || object.Automount == nil || *object.Automount || len(object.Rules) != 0 || object.RoleRef != nil || len(object.Subjects) != 0 {
				return errors.New("host-resident ServiceAccount requires v1, namespace and automountServiceAccountToken: false")
			}
		case "Role", "ClusterRole":
			roles++
			if object.APIVersion != "rbac.authorization.k8s.io/v1" || len(object.Rules) == 0 || object.RoleRef != nil || len(object.Subjects) != 0 || object.Automount != nil {
				return errors.New("invalid RBAC role structure")
			}
			if (object.Kind == "Role") != (object.Metadata.Namespace != "") {
				return errors.New("Role must be namespaced; ClusterRole must not be namespaced")
			}
			for _, r := range object.Rules {
				if err := validateRule(object.Kind, r); err != nil {
					return fmt.Errorf("%s: %w", object.Metadata.Name, err)
				}
			}
		case "RoleBinding", "ClusterRoleBinding":
			bindings++
			if object.APIVersion != "rbac.authorization.k8s.io/v1" || object.RoleRef == nil || len(object.Subjects) != 1 || len(object.Rules) != 0 || object.Automount != nil {
				return errors.New("binding requires exactly one dedicated ServiceAccount subject")
			}
			if (object.Kind == "RoleBinding") != (object.Metadata.Namespace != "") {
				return errors.New("binding namespace does not match its scope")
			}
		default:
			return fmt.Errorf("unsupported Kubernetes source kind %s", object.Kind)
		}
	}
	for _, object := range objects {
		if object.RoleRef == nil {
			continue
		}
		ref := object.RoleRef
		want := "Role"
		namespace := object.Metadata.Namespace
		if object.Kind == "ClusterRoleBinding" {
			want = "ClusterRole"
			namespace = ""
		}
		if ref.APIGroup != "rbac.authorization.k8s.io" || ref.Kind != want || !validName(ref.Name) {
			return errors.New("binding must refer to a locally declared role of the matching scope")
		}
		if _, ok := index[ref.Kind+"/"+namespace+"/"+ref.Name]; !ok {
			return errors.New("binding refers to an undeclared or external role")
		}
		s := object.Subjects[0]
		if s.Kind != "ServiceAccount" || s.APIGroup != "" || s.Namespace == "" || !validName(s.Name) {
			return errors.New("binding subject must be an explicit ServiceAccount, never a user/group")
		}
		if _, ok := index["ServiceAccount/"+s.Namespace+"/"+s.Name]; !ok {
			return errors.New("binding subject is not a declared ServiceAccount")
		}
		if object.Kind == "RoleBinding" && s.Namespace != object.Metadata.Namespace {
			return errors.New("namespaced binding must use the same reviewed namespace as its subject")
		}
	}
	if serviceAccounts != 1 || roles < 2 || bindings < 2 {
		return errors.New("declare one dedicated ServiceAccount and separate cluster observation from namespaced mutation roles/bindings")
	}
	return nil
}

func validateRule(kind string, r rule) error {
	if len(r.APIGroups) != 1 || len(r.Resources) == 0 || len(r.Verbs) == 0 {
		return errors.New("RBAC rule requires one explicit API group, resources and verbs")
	}
	group := r.APIGroups[0]
	if group != "" && group != "apps" {
		return errors.New("RBAC API group is outside the reviewed core/apps contract")
	}
	for _, name := range r.ResourceNames {
		if !validName(name) {
			return errors.New("resourceNames must contain exact resource names")
		}
	}
	for _, resource := range r.Resources {
		if kind == "ClusterRole" && (group != "" || !contains([]string{"pods", "nodes", "namespaces"}, resource)) {
			return errors.New("cluster observation is restricted to Pods, nodes and the named namespace")
		}
		if group == "" && !contains([]string{"pods", "nodes", "namespaces", "configmaps"}, resource) || group == "apps" && !contains([]string{"deployments", "deployments/scale", "replicasets"}, resource) {
			return errors.New("RBAC resource is outside reviewed observation/serving scope; secrets, exec and wildcards are refused")
		}
		for _, verb := range r.Verbs {
			if contains([]string{"get", "list", "watch"}, verb) {
				continue
			}
			if kind != "Role" || group != "apps" || !contains([]string{"deployments", "deployments/scale"}, resource) || !contains([]string{"patch", "update"}, verb) || len(r.ResourceNames) == 0 {
				return errors.New("mutations require a namespaced Role, exact deployment resourceNames and patch/update only")
			}
		}
		if (resource == "configmaps" || resource == "namespaces") && (len(r.ResourceNames) == 0 || !slicesEqual(r.Verbs, []string{"get"})) {
			return errors.New("ConfigMap/namespace reads require get on exact qualification resourceNames")
		}
	}
	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parseUnit(data []byte) (map[string]string, error) {
	values := map[string]string{}
	section := ""
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			if !contains([]string{"Unit", "Service", "Socket", "Install"}, section) {
				return nil, errors.New("unsupported unit section")
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" || key == "" || strings.ContainsAny(key, " \t") || strings.HasSuffix(line, "\\") {
			return nil, errors.New("unit source requires explicit single-line assignments")
		}
		combined := section + "." + key
		if _, exists := values[combined]; exists {
			return nil, errors.New("duplicate unit assignment requires explicit validator review")
		}
		values[combined] = value
	}
	return values, nil
}
func validateUnits(root string) error {
	required := map[string]map[string]string{
		"bridged.service":       {"Service.User": "bridge", "Service.NoNewPrivileges": "yes", "Service.ProtectSystem": "strict", "Service.PrivateDevices": "yes", "Service.ProtectProc": "invisible", "Service.ProcSubset": "pid", "Service.CapabilityBoundingSet": "", "Service.AmbientCapabilities": "", "Service.UMask": "0077"},
		"bridge-hostd.service":  {"Service.User": "root", "Service.NoNewPrivileges": "yes", "Service.PrivateDevices": "no", "Service.PrivateUsers": "no", "Service.ProtectProc": "default", "Service.ProcSubset": "all", "Service.KillMode": "control-group", "Service.UMask": "0077"},
		"bridge-hostd.socket":   {"Socket.ListenStream": "/run/bridge-hostd/control.sock", "Socket.SocketUser": "bridge", "Socket.SocketGroup": "bridge", "Socket.SocketMode": "0600", "Socket.RemoveOnStop": "yes"},
		"bridge-worker.service": {"Service.User": "bridge-worker", "Service.NoNewPrivileges": "yes", "Service.CapabilityBoundingSet": "", "Service.AmbientCapabilities": "", "Service.PrivateDevices": "yes", "Service.PrivateNetwork": "yes", "Service.ProtectHome": "yes", "Service.Delegate": "cpu memory pids", "Service.KillMode": "control-group", "Service.UMask": "0077"},
		"bridge-worker.socket":  {"Socket.ListenStream": "/run/bridge-worker/control.sock", "Socket.SocketUser": "bridge", "Socket.SocketGroup": "bridge", "Socket.SocketMode": "0600", "Socket.RemoveOnStop": "yes"},
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		values, err := parseUnit(b)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		for key, want := range required[name] {
			got, exists := values[key]
			if !exists || got != want {
				return fmt.Errorf("%s: %s must explicitly equal %q", name, key, want)
			}
		}
		for key, value := range values {
			if strings.HasPrefix(key, "Socket.Listen") && !strings.HasPrefix(value, "/") {
				return errors.New("executor sockets cannot listen on a network address")
			}
			if key == "Service.ExecStart" && (strings.Contains(value, "/bin/sh") || strings.Contains(value, "/bin/bash") || strings.Contains(value, "sudo ")) {
				return errors.New("service must execute the fixed installed binary without shell/sudo")
			}
		}
	}
	return nil
}
