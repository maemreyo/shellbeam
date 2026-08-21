//go:build !darwin && !linux

package contextexec

import (
	"fmt"
	"net"
)

func peerCredentials(net.Conn) (int, uint32, error) {
	return 0, 0, fmt.Errorf("context helper peer credentials unavailable")
}
