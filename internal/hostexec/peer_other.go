//go:build !linux

package hostexec

import (
	"errors"
	"net"
)

func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("live host executor peer authentication requires Linux")
}
