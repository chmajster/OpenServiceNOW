package requestinfo

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolverIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	resolver, err := NewResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")

	if got := resolver.Resolve(request); got != "192.0.2.10" {
		t.Fatalf("Resolve() = %q", got)
	}
}

func TestResolverUsesFirstUntrustedAddressBehindTrustedProxy(t *testing.T) {
	resolver, err := NewResolver([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.25, 192.168.1.10")

	if got := resolver.Resolve(request); got != "198.51.100.25" {
		t.Fatalf("Resolve() = %q", got)
	}
}

func TestResolverFallsBackOnMalformedForwardedFor(t *testing.T) {
	resolver, err := NewResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:443"
	request.Header.Set("X-Forwarded-For", "not-an-ip")

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("Resolve() = %q", got)
	}
}

func TestMiddlewareStoresResolvedClientIP(t *testing.T) {
	resolver, err := NewResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	var got string
	handler := resolver.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		got = ClientIP(request)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.50")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if got != "203.0.113.50" {
		t.Fatalf("ClientIP() = %q", got)
	}
}

func TestResolverFallsBackToDirectPeerWhenAllForwardedHopsAreTrusted(t *testing.T) {
	resolver, err := NewResolver([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.5:443"
	request.Header.Set("X-Forwarded-For", "10.9.9.9, 192.168.1.10")

	if got := resolver.Resolve(request); got != "10.0.0.5" {
		t.Fatalf("Resolve() = %q", got)
	}
}
