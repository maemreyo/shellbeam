//go:build !darwin && !linux

package contextexec

import (
	"fmt"
	"net"
)

func ListenPrivate(string, string) (net.Listener, string, error) {
	return nil, "", fmt.Errorf("context helper private socket unavailable")
}
func DialPrivate(string, string) (net.Conn, error) {
	return nil, fmt.Errorf("context helper private socket unavailable")
}
