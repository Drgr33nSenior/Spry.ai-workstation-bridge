//go:build linux

package hostexec

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Real Unix peer credentials and wire transport, with only a fixture executor.
// No hardware, cluster or installed service is accessed even when tests run root.
func TestUnixPeerAuthenticatedWireContract(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "configured-uid", false: "different-policy-uid"}[allowed], func(t *testing.T) {
			e := fixtureExecutor(t)
			e.policy.AllowedUID = uint32(os.Geteuid())
			if !allowed {
				e.policy.AllowedUID++
			}
			socket := filepath.Join(t.TempDir(), "helper.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- Serve(ctx, listener, e) }()
			defer func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			request := fixtureRequest("operation-peer-0001")
			client := Client{Socket: socket}
			requestContext, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			r, err := client.Execute(requestContext, request)
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				if r.ID != request.ID {
					t.Fatal("wire result identity changed")
				}
				awaitResult(t, e, request.ID)
			} else if err == nil {
				t.Fatal("unconfigured Unix UID was authorized")
			}
		})
	}
}
