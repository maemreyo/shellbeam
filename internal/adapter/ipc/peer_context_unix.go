//go:build linux || darwin

package ipc

import (
	"context"
	"net"
)

type authenticatedConn struct {
	net.Conn
	uid uint32
}

func (c *authenticatedConn) trustedPeerUID() uint32 { return c.uid }

type trustedPeerUIDContextKey struct{}

func trustedPeerConnContext(ctx context.Context, conn net.Conn) context.Context {
	provider, ok := conn.(interface{ trustedPeerUID() uint32 })
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, trustedPeerUIDContextKey{}, provider.trustedPeerUID())
}

// TrustedPeerUID returns the UID authenticated by the Unix socket listener for
// the connection carrying this request. Callers must not substitute request
// payload fields for this transport-owned identity.
func TrustedPeerUID(ctx context.Context) (uint32, bool) {
	if ctx == nil {
		return 0, false
	}
	uid, ok := ctx.Value(trustedPeerUIDContextKey{}).(uint32)
	return uid, ok
}
