// Package contract produces the checked-in OpenAPI contract from the shared Go
// wire types. The route set is checked against server registration in tests.
package contract

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/advisor"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

type Endpoint struct {
	Method, Path, Summary, Role string
	Request, Response           any
	Status                      string
	Public                      bool
}
type Apply struct {
	PlanID string `json:"plan_id"`
	Target string `json:"target"`
}
type MemoryAdvice struct {
	Advisory advisor.Result `json:"advisory"`
	Plan     *domain.Plan   `json:"plan,omitempty"`
}
type Login struct {
	Credential string `json:"credential"`
}
type Session struct {
	Actor     auth.Actor `json:"actor"`
	CSRF      string     `json:"csrf"`
	ExpiresAt time.Time  `json:"expires_at"`
	Mode      string     `json:"mode"`
}
type Profile struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
}
type Health struct {
	Alive                    bool   `json:"alive"`
	Mode                     string `json:"mode"`
	MutationStorageAvailable bool   `json:"mutation_storage_available"`
}
type Error struct {
	Error domain.Failure `json:"error"`
}

var Endpoints = []Endpoint{
	{"POST", "/api/v1/memory/inspect", "Reconcile only the original read-only helper outcome; never redispatch", "owner", domain.MemoryOperationRequest{}, domain.Operation{}, "200", false},
	{"GET", "/api/v1/memory", "Read retained unqualified memory evidence summaries", "owner", nil, []domain.MemorySummary{}, "200", false},
	{"POST", "/api/v1/memory/preview", "Inspect named sealed memory evidence without applying a workload change", "owner", domain.Draft{}, domain.MemorySummary{}, "200", false},
	{"POST", "/api/v1/memory/artifact", "Export owner-private preconditioned candidate or rollback; recheck identities", "owner", domain.MemoryArtifactRequest{}, domain.Artifact{}, "200", false},
	{"POST", "/api/v1/memory/advice", "Optional bounded Agents API advisory; no approval or application authority", "owner", domain.MemoryRequest{}, MemoryAdvice{}, "200", false},
	{"GET", "/health/live", "Controller liveness independent of K3s", "public", nil, Health{}, "200", true},
	{"POST", "/api/v1/auth/login", "Exchange a local scoped credential for a browser session", "credential", Login{}, Session{}, "200", true},
	{"GET", "/api/v1/auth/session", "Inspect authenticated browser identity", "viewer", nil, Session{}, "200", false},
	{"POST", "/api/v1/auth/logout", "Revoke browser session", "viewer", struct{}{}, map[string]string{}, "200", false},
	{"GET", "/api/v1/status", "Observe explicit demo/live inventory and dependency availability", "viewer", nil, domain.Inventory{}, "200", false},
	{"GET", "/api/v1/telemetry/summary", "Read bounded telemetry freshness and fixed aggregate queries; no query parameters accepted", "owner", nil, domain.TelemetrySummary{}, "200", false},
	{"GET", "/api/v1/models", "List selected immutable model inventories", "viewer", nil, []domain.Model{}, "200", false},
	{"GET", "/api/v1/resources", "Observe boot-bound hardware evidence", "viewer", nil, domain.Hardware{}, "200", false},
	{"GET", "/api/v1/builds", "List reviewed build recipes and explicit refusals", "viewer", nil, []domain.Recipe{}, "200", false},
	{"GET", "/api/v1/caches", "List managed cache usage and cleanup preview", "viewer", nil, []domain.Cache{}, "200", false},
	{"GET", "/api/v1/harnesses", "List supported local client modes", "viewer", nil, []domain.Harness{}, "200", false},
	{"GET", "/api/v1/profiles", "List session paths; plan checks target availability", "viewer", nil, []Profile{}, "200", false},
	{"GET", "/api/v1/config", "Read authoritative managed configuration and revision", "viewer", nil, domain.Configuration{}, "200", false},
	{"GET", "/api/v1/config/export", "Download the canonical managed source", "viewer", nil, domain.Configuration{}, "200", false},
	{"POST", "/api/v1/plans", "Validate a typed draft and return exact changes and preconditions", "action-scoped", domain.Draft{}, domain.Plan{}, "201", false},
	{"GET", "/api/v1/plans/{id}", "Inspect a retained plan", "viewer", nil, domain.Plan{}, "200", false},
	{"POST", "/api/v1/operations", "Persist intent and enqueue an approved plan", "action-scoped", Apply{}, domain.Operation{}, "202", false},
	{"GET", "/api/v1/operations", "List durable operations, including builds", "viewer", nil, []domain.Operation{}, "200", false},
	{"GET", "/api/v1/operations/{id}", "Inspect operation phases and recovery requirements", "viewer", nil, domain.Operation{}, "200", false},
	{"POST", "/api/v1/operations/{id}/cancel", "Request cancellation using observed operation revision", "action-scoped", struct{}{}, domain.Operation{}, "202", false},
	{"POST", "/api/v1/operations/{id}/recover", "Inspect the responsible executor and plan chain restoration or operation reconciliation", "owner", struct{}{}, domain.Plan{}, "201", false},
	{"GET", "/api/v1/harnesses/{id}/export", "Export verified native non-secret client files", "viewer", nil, domain.Bundle{}, "200", false},
	{"GET", "/api/v1/credentials", "List credential identifiers, expiry and revocation", "owner", nil, []auth.CredentialInfo{}, "200", false},
	{"POST", "/api/v1/credentials/{id}/revoke", "Revoke one credential and its browser sessions", "owner", struct{}{}, map[string]string{}, "200", false},
	{"GET", "/api/v1/audit", "Read bounded operation and identity audit records", "owner", nil, []store.Audit{}, "200", false},
}

func schema(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{schema(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.Interface:
		return map[string]any{}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64, reflect.Uint64, reflect.Uint32, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": []string{"array", "null"}, "items": schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": []string{"object", "null"}, "additionalProperties": schema(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			props[name] = schema(f.Type)
			if !strings.Contains(tag, "omitempty") {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	}
	panic("unsupported schema type " + t.String())
}
func Generate() ([]byte, error) {
	paths := map[string]any{}
	for _, e := range Endpoints {
		operation := map[string]any{"summary": e.Summary, "x-minimum-role": e.Role, "responses": map[string]any{e.Status: map[string]any{"description": "Successful management response", "content": map[string]any{"application/json": map[string]any{"schema": schema(reflect.TypeOf(e.Response))}}}, "default": map[string]any{"description": "Structured refusal or failure; 401 authentication, 403 authorization/CSRF, 409 drift/conflict, 413 body bound, 429 capacity/rate, 503 unavailable", "content": map[string]any{"application/json": map[string]any{"schema": schema(reflect.TypeFor[Error]())}}}}}
		if e.Public {
			operation["security"] = []any{}
		}
		parameters := []any{}
		if strings.Contains(e.Path, "{id}") {
			parameters = append(parameters, map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}})
		}
		if e.Method == "POST" {
			parameters = append(parameters, map[string]any{"name": "X-CSRF-Token", "in": "header", "required": false, "description": "Required with browser session cookies; Origin must match configured management origin. Bearer requests do not require browser headers.", "schema": map[string]any{"type": "string"}})
			if e.Path == "/api/v1/operations" {
				parameters = append(parameters, map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": map[string]any{"type": "string", "minLength": 8, "maxLength": 128}})
			}
			if strings.HasSuffix(e.Path, "/cancel") {
				parameters = append(parameters, map[string]any{"name": "If-Match", "in": "header", "required": true, "description": "Quoted decimal operation revision from its ETag.", "schema": map[string]any{"type": "string"}})
			}
		}
		if len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		if e.Request != nil {
			operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema(reflect.TypeOf(e.Request))}}}
		}
		item, ok := paths[e.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[e.Path] = item
		}
		item[strings.ToLower(e.Method)] = operation
	}
	doc := map[string]any{"openapi": "3.1.1", "info": map[string]any{"title": "Spry.ai Workstation Bridge private management API", "version": "1.0.0", "description": "No public management ingress. Explicit local owner bootstrap/recovery has no TCP endpoint. Plans expire after ten minutes; source and external effects are separate. GPU qualification remains independently enforced by the host executor."}, "paths": paths, "security": []any{map[string]any{"bearerAuth": []string{}}, map[string]any{"browserSession": []string{}}}, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "description": "Cryptographically random, named, expiring, revocable role-scoped credential"}, "browserSession": map[string]any{"type": "apiKey", "in": "cookie", "name": "__Host-bridge_session_v2", "description": "Live browser sessions require explicit browser_sessions policy and a dedicated trusted HTTPS hostname. Secure, HttpOnly, host-only, Path=/, SameSite=Strict with Origin and CSRF checks. Cookies are not port-isolated. Demo uses bridge_demo_session_v2; CLI-only live listeners refuse browser login."}}}}
	b, e := json.MarshalIndent(doc, "", "  ")
	return append(b, '\n'), e
}
