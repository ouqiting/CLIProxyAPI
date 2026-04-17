package helps

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestNewProxyAwareHTTPClientDirectBypassesGlobalProxy(t *testing.T) {
	t.Parallel()

	client := NewProxyAwareHTTPClient(
		context.Background(),
		&config.Config{SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"}},
		&cliproxyauth.Auth{ProxyURL: "direct"},
		0,
	)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestNewProxyAwareHTTPClientUsesConfiguredUpstreamTimeout(t *testing.T) {
	t.Parallel()

	client := NewProxyAwareHTTPClient(
		context.Background(),
		&config.Config{UpstreamTimeout: 12},
		nil,
		0,
	)

	if client.Timeout != 0 {
		t.Fatalf("client.Timeout = %v, want 0", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != 12*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 12*time.Second)
	}
	if transport.TLSHandshakeTimeout != 12*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, 12*time.Second)
	}
}

func TestNewProxyAwareHTTPClientClonesContextTransportForUpstreamTimeout(t *testing.T) {
	t.Parallel()

	original := &http.Transport{}
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", original)
	client := NewProxyAwareHTTPClient(ctx, &config.Config{UpstreamTimeout: 9}, nil, 0)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport == original {
		t.Fatal("expected client transport to clone the context transport")
	}
	if original.ResponseHeaderTimeout != 0 {
		t.Fatalf("original ResponseHeaderTimeout = %v, want 0", original.ResponseHeaderTimeout)
	}
	if transport.ResponseHeaderTimeout != 9*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 9*time.Second)
	}
}
