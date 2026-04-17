package helps

import (
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestNewUtlsHTTPClientUsesConfiguredUpstreamTimeout(t *testing.T) {
	t.Parallel()

	client := NewUtlsHTTPClient(&config.Config{UpstreamTimeout: 7}, nil, 0)
	if client.Timeout != 0 {
		t.Fatalf("client.Timeout = %v, want 0", client.Timeout)
	}

	fallback, ok := client.Transport.(*fallbackRoundTripper)
	if !ok {
		t.Fatalf("transport type = %T, want *fallbackRoundTripper", client.Transport)
	}
	if fallback.utls.connectTimeout != 7*time.Second {
		t.Fatalf("utls connectTimeout = %v, want %v", fallback.utls.connectTimeout, 7*time.Second)
	}

	transport, ok := fallback.fallback.(*http.Transport)
	if !ok {
		t.Fatalf("fallback transport type = %T, want *http.Transport", fallback.fallback)
	}
	if transport.ResponseHeaderTimeout != 7*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 7*time.Second)
	}
	if transport.TLSHandshakeTimeout != 7*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, 7*time.Second)
	}
}
