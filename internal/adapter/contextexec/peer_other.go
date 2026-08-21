//go:build !darwin && !linux

package contextexec

import (
	"fmt"
	"net"
)

func peerCredentials(net.Conn) (int, uint32, error) {
	return 0, 0, fmt.Errorf("context helper peer credentials unavailable")
}

func platformForegroundVerifier(int, string) error {
	return fmt.Errorf("context helper foreground verification unavailable")
}
