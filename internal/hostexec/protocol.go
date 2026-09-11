// Package hostexec implements the fixed local workstation executor contract.
// The helper's root-owned policy and journal are independent of the API store.
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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

const ContractVersion = 1

type Request struct {
	Version           int                  `json:"version"`
	ID                string               `json:"id"`
	ExecutionIdentity string               `json:"execution_identity"`
	PayloadHash       string               `json:"payload_hash"`
	Draft             domain.Draft         `json:"draft"`
	Desired           domain.Configuration `json:"desired"`
}

type Result struct {
	ID      string          `json:"id"`
	State   string          `json:"state"`
	Phase   string          `json:"phase"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func PayloadHash(d domain.Draft) string {
	b, _ := json.Marshal(d)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func RequestHash(r Request) string { r.PayloadHash = ""; return domain.Hash(r) }

// ConfigurationHash identifies an exact owner-qualified engine configuration.
func ConfigurationHash(s *domain.Serving, r *domain.Resources) string {
	b, _ := json.Marshal(struct {
		Serving   *domain.Serving   `json:"serving"`
		Resources *domain.Resources `json:"resources"`
	}{s, r})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Client struct{ Socket string }

func (c Client) MemoryPreview(ctx context.Context, d domain.Draft, cfg domain.Configuration) (domain.MemorySummary, error) {
	r, e := c.call(ctx, http.MethodPost, "/v1/memory/preview", Request{Draft: d, Desired: cfg})
	if e != nil {
		return domain.MemorySummary{}, e
	}
	var s domain.MemorySummary
	e = strictDecode(bytes.NewReader(r.Data), &s)
	return s, e
}
func (c Client) MemoryArtifact(ctx context.Context, id, name string) (domain.Artifact, error) {
	r, e := c.call(ctx, http.MethodPost, "/v1/memory/artifact", domain.MemoryArtifactRequest{OperationID: id, Name: name})
	if e != nil {
		return domain.Artifact{}, e
	}
	var a domain.Artifact
	e = strictDecode(bytes.NewReader(r.Data), &a)
	return a, e
}

func (c Client) call(ctx context.Context, method, path string, body any) (Result, error) {
	var result Result
	if c.Socket == "" {
		return result, errors.New("host executor socket is not configured")
	}
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return result, err
		}
		input = bytes.NewReader(b)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", c.Socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, "http://host-executor"+path, input)
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return result, errors.New("host executor is unavailable; inspect its local service and recovery journal")
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return result, errors.New("invalid host executor response")
	}
	if response.StatusCode >= 400 {
		return result, fmt.Errorf("host executor: %s", result.Message)
	}
	return result, nil
}
func (c Client) Execute(ctx context.Context, r Request) (Result, error) {
	return c.call(ctx, http.MethodPost, "/v1/operations", r)
}
func (c Client) Status(ctx context.Context, id string) (Result, error) {
	if !validID(id) {
		return Result{}, errors.New("invalid operation ID")
	}
	return c.call(ctx, http.MethodGet, "/v1/operations/"+id, nil)
}
func (c Client) Snapshot(ctx context.Context) (domain.Inventory, error) {
	r, err := c.call(ctx, http.MethodGet, "/v1/inventory", nil)
	if err != nil {
		return domain.Inventory{}, err
	}
	var inventory domain.Inventory
	if err = json.Unmarshal(r.Data, &inventory); err != nil {
		return inventory, errors.New("invalid host inventory response")
	}
	return inventory, nil
}
func validID(v string) bool {
	if len(v) < 8 || len(v) > 80 {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
func strictDecode(r io.Reader, out any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func safeIdentity(s string) bool {
	return len(s) > 0 && len(s) <= 128 && !strings.ContainsAny(s, "\n\r\x00")
}
