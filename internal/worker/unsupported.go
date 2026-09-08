//go:build !linux

package worker

import (
	"context"
	"errors"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"net"
)

func ConnContext(ctx context.Context, _ net.Conn) context.Context { return ctx }
func peerAuthorized(context.Context, int) bool                    { return false }
func trustedFile(string) error                                    { return errors.New("worker requires Linux") }
func restartDisposition(Policy, string) domain.Result {
	return domain.Result{State: "recovery-required", Phase: "worker-restart", Message: "Linux cgroup observation unavailable", RecoveryRequired: true}
}
func runContained(context.Context, Policy, Request) (domain.Result, error) {
	return domain.Result{}, errors.New("worker requires Linux bubblewrap and delegated cgroup v2")
}
