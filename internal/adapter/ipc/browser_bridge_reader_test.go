package ipc

import (
	"context"
	"testing"
)

func TestBrowserBridgeReaderSurfacesTransportFailure(t *testing.T) {
	reader := NewBrowserBridgeReader("/nonexistent/shellbeam-browser-bridge-test.sock")
	if _, _, err := reader.Activity(context.Background(), "wt"); err == nil {
		t.Fatal("expected transport error against missing socket")
	}
}
