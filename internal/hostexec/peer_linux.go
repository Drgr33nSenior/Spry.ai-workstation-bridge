//go:build linux

package hostexec

import (
	"errors"
	"net"
	"syscall"
)

func peerUID(c net.Conn) (uint32, error) {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("Unix peer required")
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credential *syscall.Ucred
	var socketErr error
	err = raw.Control(func(fd uintptr) {
		credential, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if socketErr != nil {
		return 0, socketErr
	}
	return credential.Uid, nil
}
