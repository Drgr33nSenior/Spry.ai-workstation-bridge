package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type peerKey struct{}

func Listen(p Policy) (net.Listener, error) {
	if os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) && os.Getenv("LISTEN_FDS") == "1" {
		f := os.NewFile(3, "systemd-host-socket")
		l, err := net.FileListener(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		if l.Addr().Network() != "unix" {
			l.Close()
			return nil, errors.New("host executor requires a Unix socket")
		}
		return l, nil
	}
	if err := trustedPath(filepath.Dir(p.Socket), true); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", p.Socket)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(p.Socket, 0600); err != nil {
		l.Close()
		return nil, err
	}
	if err = os.Chown(p.Socket, int(p.AllowedUID), 0); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
func Serve(ctx context.Context, l net.Listener, e *Executor) error {
	if l.Addr().Network() != "unix" {
		return errors.New("host executor has no network listener")
	}
	s := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			uid, err := peerUID(c)
			if err != nil {
				return ctx
			}
			return context.WithValue(ctx, peerKey{}, uid)
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			fail := func(code int, message string) {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(Result{State: "failed", Message: message})
			}
			uid, ok := r.Context().Value(peerKey{}).(uint32)
			if !ok || uid != e.policy.AllowedUID {
				fail(403, "Unix peer is not authorized")
				return
			}
			if r.Host != "host-executor" {
				fail(400, "invalid host executor Host")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			if r.Method == http.MethodGet && r.URL.Path == "/v1/inventory" {
				inventory, err := e.Snapshot(r.Context())
				if err != nil {
					fail(503, err.Error())
					return
				}
				data, _ := json.Marshal(inventory)
				_ = json.NewEncoder(w).Encode(Result{State: "succeeded", Data: data})
				return
			}
			if r.Method == http.MethodPost && r.URL.Path == "/v1/operations" {
				var request Request
				if err := strictDecode(r.Body, &request); err != nil {
					fail(400, "invalid typed helper request")
					return
				}
				result, err := e.Submit(uid, request)
				if err != nil {
					fail(409, err.Error())
					return
				}
				w.WriteHeader(202)
				_ = json.NewEncoder(w).Encode(result)
				return
			}
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/operations/") {
				id := strings.TrimPrefix(r.URL.Path, "/v1/operations/")
				if !validID(id) {
					fail(400, "invalid operation ID")
					return
				}
				result, err := e.Status(id)
				if err != nil {
					fail(404, err.Error())
					return
				}
				_ = json.NewEncoder(w).Encode(result)
				return
			}
			fail(404, "fixed executor operation not found")
		})}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdown)
	}()
	err := s.Serve(l)
	// A helper stop drains its bounded action. API disconnects never reach this
	// lifecycle, and the inherited legacy lock remains held until final commit.
	for {
		e.mu.Lock()
		active := e.active
		e.mu.Unlock()
		if !active {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
