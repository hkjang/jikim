package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Reverse proxies are configured in 서비스 관리 → 시스템 설정 rather than in an
// environment variable, so the list is cached instead of read per request.
const trustedProxyTTL = 30 * time.Second

type trustedProxyCache struct {
	value     atomic.Pointer[[]netip.Prefix]
	expiresAt atomic.Int64
	refresh   sync.Mutex
}

// clientIP resolves the address recorded in the audit log and used as the login
// rate limit key. X-Forwarded-For is only consulted when the peer is one of the
// administrator's trusted proxies, because any client can forge the header.
func (s *Server) clientIP(r *http.Request) string {
	return clientIPFrom(r, s.trustedProxyPrefixes())
}

func (s *Server) trustedProxyPrefixes() []netip.Prefix {
	cache := &s.trustedProxies
	now := time.Now()
	current := cache.value.Load()
	if current != nil && now.UnixNano() < cache.expiresAt.Load() {
		return *current
	}
	if s.store == nil {
		return nil
	}
	if !cache.refresh.TryLock() {
		// Another request is reloading; the previous list stays in effect.
		if current != nil {
			return *current
		}
		return nil
	}
	defer cache.refresh.Unlock()
	if refreshed := cache.value.Load(); refreshed != nil && time.Now().UnixNano() < cache.expiresAt.Load() {
		return *refreshed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	security, err := s.store.SecurityConfig(ctx)
	if err != nil {
		// Keep the previous list and retry soon rather than losing attribution.
		cache.expiresAt.Store(time.Now().Add(time.Second).UnixNano())
		if current != nil {
			return *current
		}
		return nil
	}
	prefixes := security.TrustedProxies
	cache.value.Store(&prefixes)
	cache.expiresAt.Store(time.Now().Add(trustedProxyTTL).UnixNano())
	return prefixes
}

func clientIPFrom(r *http.Request, trusted []netip.Prefix) string {
	peer := peerIP(r)
	if len(trusted) == 0 {
		return peer
	}
	addr, ok := parseClientAddr(peer)
	if !ok || !trustedProxy(trusted, addr) {
		return peer
	}
	hops := forwardedHops(r)
	for i := len(hops) - 1; i >= 0; i-- {
		candidate, ok := parseClientAddr(hops[i])
		if !ok {
			// A hop we cannot parse ends the chain of trust.
			return peer
		}
		if trustedProxy(trusted, candidate) {
			continue
		}
		return candidate.String()
	}
	return peer
}

func trustedProxy(trusted []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func forwardedHops(r *http.Request) []string {
	var hops []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, hop := range strings.Split(header, ",") {
			hop = strings.TrimSpace(hop)
			if hop != "" {
				hops = append(hops, hop)
			}
		}
	}
	return hops
}

func parseClientAddr(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(raw)
		if splitErr != nil {
			return netip.Addr{}, false
		}
		addr, err = netip.ParseAddr(host)
		if err != nil {
			return netip.Addr{}, false
		}
	}
	return addr.Unmap().WithZone(""), true
}
