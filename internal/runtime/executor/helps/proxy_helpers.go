package helps

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

// NewProxyAwareHTTPClient creates an HTTP client with proper proxy configuration priority:
// 1. Use auth.ProxyURL if configured (highest priority)
// 2. Use cfg.ProxyURL if auth proxy is not configured
// 3. Use RoundTripper from context if neither are configured
//
// Parameters:
//   - ctx: The context containing optional RoundTripper
//   - cfg: The application configuration
//   - auth: The authentication information
//   - timeout: Upstream connect/first-byte timeout (0 means no timeout)
//
// Returns:
//   - *http.Client: An HTTP client with configured proxy or transport
func NewProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
	httpClient := &http.Client{}
	resolvedTimeout := resolveUpstreamTimeout(cfg, timeout)

	// Priority 1: Use auth.ProxyURL if configured
	var proxyURL string
	if auth != nil {
		proxyURL = strings.TrimSpace(auth.ProxyURL)
	}

	// Priority 2: Use cfg.ProxyURL if auth proxy is not configured
	if proxyURL == "" && cfg != nil {
		proxyURL = strings.TrimSpace(cfg.ProxyURL)
	}

	// If we have a proxy URL configured, set up the transport
	if proxyURL != "" {
		transport := transportWithUpstreamTimeouts(buildProxyTransport(proxyURL), resolvedTimeout)
		if transport != nil {
			httpClient.Transport = transport
			return httpClient
		}
		// If proxy setup failed, log and fall through to context RoundTripper
		log.Debugf("failed to setup proxy from URL: %s, falling back to context transport", proxyURL)
	}

	// Priority 3: Use RoundTripper from context (typically from RoundTripperFor)
	if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
		httpClient.Transport = roundTripperWithUpstreamTimeout(rt, resolvedTimeout)
		return httpClient
	}

	if resolvedTimeout > 0 {
		httpClient.Transport = transportWithUpstreamTimeouts(nil, resolvedTimeout)
	}

	return httpClient
}

func resolveUpstreamTimeout(cfg *config.Config, timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	if cfg == nil || cfg.UpstreamTimeout <= 0 {
		return 0
	}
	return time.Duration(cfg.UpstreamTimeout) * time.Second
}

func roundTripperWithUpstreamTimeout(rt http.RoundTripper, timeout time.Duration) http.RoundTripper {
	if rt == nil || timeout <= 0 {
		return rt
	}
	transport, ok := rt.(*http.Transport)
	if !ok {
		return rt
	}
	return transportWithUpstreamTimeouts(transport, timeout)
}

func transportWithUpstreamTimeouts(transport *http.Transport, timeout time.Duration) *http.Transport {
	if timeout <= 0 {
		return transport
	}

	var clone *http.Transport
	if transport != nil {
		clone = transport.Clone()
	} else if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok && defaultTransport != nil {
		clone = defaultTransport.Clone()
	} else {
		clone = &http.Transport{}
	}

	originalDialContext := clone.DialContext
	if originalDialContext != nil {
		clone.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if ctx == nil {
				ctx = context.Background()
			}
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return originalDialContext(timeoutCtx, network, addr)
		}
	} else {
		clone.DialContext = (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	clone.TLSHandshakeTimeout = timeout
	clone.ResponseHeaderTimeout = timeout
	return clone
}

// buildProxyTransport creates an HTTP transport configured for the given proxy URL.
// It supports SOCKS5, HTTP, and HTTPS proxy protocols.
//
// Parameters:
//   - proxyURL: The proxy URL string (e.g., "socks5://user:pass@host:port", "http://host:port")
//
// Returns:
//   - *http.Transport: A configured transport, or nil if the proxy URL is invalid
func buildProxyTransport(proxyURL string) *http.Transport {
	transport, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
	if errBuild != nil {
		log.Errorf("%v", errBuild)
		return nil
	}
	return transport
}
