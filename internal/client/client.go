// Package client implements the private management transport shared by CLI commands.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

const maxResponseBytes = 8 << 20

type Config struct {
	Endpoint       string `json:"endpoint"`
	CredentialFile string `json:"credential_file"`
	CAFile         string `json:"ca_file,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type Client struct {
	endpoint   string
	credential string
	http       *http.Client
}

// APIError contains the server's bounded, public error. It never includes request headers.
type APIError struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("open context: %w", err)
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("read context: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return cfg, errors.New("context must contain one JSON object")
	}
	return cfg, nil
}

// ReadCredential refuses links and files readable by another account.
func ReadCredential(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("cannot read credential file")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return "", errors.New("credential must be an owner-only regular file of at most 4096 bytes")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return "", errors.New("credential file must belong to the current account")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", errors.New("cannot open credential file")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("credential file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 {
		return "", errors.New("cannot read credential file")
	}
	token := strings.TrimSpace(string(b))
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return "", errors.New("credential file must contain one token")
	}
	return token, nil
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("endpoint must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("non-loopback management endpoints require HTTPS")
	}
	credential, err := ReadCredential(cfg.CredentialFile)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errors.New("cannot read management CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("management CA file has no valid certificates")
		}
		tlsConfig.RootCAs = roots
	}
	seconds := cfg.TimeoutSeconds
	if seconds == 0 {
		seconds = 30
	}
	if seconds < 1 || seconds > 3600 {
		return nil, errors.New("timeout_seconds must be between 1 and 3600")
	}
	transport := &http.Transport{
		// Management credentials must not enter an environment-selected proxy.
		TLSClientConfig:       tlsConfig,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: time.Duration(seconds) * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	if u.Scheme == "http" && u.Hostname() == "localhost" {
		// Resolve localhost ourselves: a local name-service override cannot send a bearer token onto the LAN.
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}
	}
	return &Client{endpoint: strings.TrimSuffix(cfg.Endpoint, "/"), credential: credential, http: &http.Client{Transport: transport, Timeout: time.Duration(seconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("management redirects are refused") }}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

// Do sends one versioned API request. Callers may omit body and result.
func (c *Client) Do(ctx context.Context, method, path, idempotency string, body, result any) error {
	return c.do(ctx, method, path, idempotency, "", body, result)
}

func (c *Client) DoRevision(ctx context.Context, method, path string, revision uint64, body, result any) error {
	if revision == 0 {
		return errors.New("observed operation revision is required")
	}
	return c.do(ctx, method, path, "", fmt.Sprintf("\"%d\"", revision), body, result)
}

func (c *Client) do(ctx context.Context, method, path, idempotency, ifMatch string, body, result any) error {
	if !strings.HasPrefix(path, "/api/v1/") || strings.Contains(path, "..") || strings.ContainsAny(path, "?#\\") {
		return errors.New("invalid management API path")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reader)
	if err != nil {
		return errors.New("invalid management request")
	}
	req.Header.Set("Authorization", "Bearer "+c.credential)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotency != "" {
		req.Header.Set("Idempotency-Key", idempotency)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("management request deadline or cancellation: %w", ctx.Err())
		}
		return errors.New("management request failed: check endpoint, TLS identity and deadline")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return errors.New("cannot read management response")
	}
	if len(b) > maxResponseBytes {
		return errors.New("management response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Code: "request_failed", Message: "management request was refused"}
		var envelope struct {
			Error   json.RawMessage `json:"error"`
			Code    string          `json:"code"`
			Message string          `json:"message"`
		}
		if json.Unmarshal(b, &envelope) == nil {
			if envelope.Code != "" {
				apiErr.Code = envelope.Code
			}
			if envelope.Message != "" {
				apiErr.Message = envelope.Message
			}
			if len(envelope.Error) > 0 {
				var nested struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}
				if json.Unmarshal(envelope.Error, &nested) == nil {
					if nested.Code != "" {
						apiErr.Code = nested.Code
					}
					if nested.Message != "" {
						apiErr.Message = nested.Message
					}
				}
			}
		}
		// A server error must not accidentally echo the submitted credential.
		apiErr.Code = strings.ReplaceAll(apiErr.Code, c.credential, "[redacted]")
		apiErr.Message = strings.ReplaceAll(apiErr.Message, c.credential, "[redacted]")
		return apiErr
	}
	if result == nil || len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, result); err != nil {
		return errors.New("management response is not valid JSON")
	}
	return nil
}
