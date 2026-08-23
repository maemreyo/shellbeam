//go:build darwin || linux

package contextexec

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSocketIsOwnedMode0600AndDerivedOnlyFromOpaqueLaunch(t *testing.T) {
	runtime, err := os.MkdirTemp("/tmp", "sb-cx-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtime)
	listener, path, err := ListenPrivate(runtime, "launch_socket_01")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v", info.Mode())
	}
	if filepath.Base(path) == "launch_socket_01.sock" {
		t.Fatal("public launch id used verbatim as socket filename")
	}
	conn, err := DialPrivate(runtime, "launch_socket_01")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	if _, ok := conn.(*net.UnixConn); !ok {
		t.Fatalf("conn=%T", conn)
	}
}
