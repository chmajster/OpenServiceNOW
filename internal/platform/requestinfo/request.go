package requestinfo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const clientIPKey contextKey = "client_ip"

type Resolver struct {
	trusted []*net.IPNet
}

func NewResolver(cidrs []string) (*Resolver, error) {
	resolver := &Resolver{}
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", raw, err)
		}
		resolver.trusted = append(resolver.trusted, network)
	}
	return resolver, nil
}

func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		clientIP := r.Resolve(request)
		ctx := context.WithValue(request.Context(), clientIPKey, clientIP)
		next.ServeHTTP(w, request.WithContext(ctx))
	})
}

func (r *Resolver) Resolve(request *http.Request) string {
	direct := DirectPeerIP(request)
	peer := net.ParseIP(direct)
	if peer == nil || !r.isTrusted(peer) {
		return direct
	}

	values := strings.Join(request.Header.Values("X-Forwarded-For"), ",")
	if strings.TrimSpace(values) == "" {
		return direct
	}

	parts := strings.Split(values, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(parts[i])
		ip := net.ParseIP(raw)
		if ip == nil {
			return direct
		}
		if !r.isTrusted(ip) {
			return ip.String()
		}
	}
	return direct
}

func (r *Resolver) isTrusted(ip net.IP) bool {
	for _, network := range r.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func ClientIP(request *http.Request) string {
	if value, ok := request.Context().Value(clientIPKey).(string); ok && value != "" {
		return value
	}
	return DirectPeerIP(request)
}

func DirectPeerIP(request *http.Request) string {
	raw := strings.TrimSpace(request.RemoteAddr)
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
		return ""
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String()
	}
	return ""
}
