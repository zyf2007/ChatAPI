// Package clientip resolves client addresses using an explicit proxy trust boundary.
package clientip

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// FromRequest ignores forwarding headers unless the immediate peer is trusted.
// X-Forwarded-For is walked from right to left, stopping at the first untrusted
// address so a client-supplied prefix cannot override its actual source address.
func FromRequest(r *http.Request, trustedProxies []string) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	if !trusted(peer, trustedProxies) {
		return peer.String()
	}

	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		chain := strings.Split(strings.Join(values, ","), ",")
		current := peer
		for i := len(chain) - 1; i >= 0; i-- {
			if !trusted(current, trustedProxies) {
				return current.String()
			}
			next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
			if err != nil {
				return peer.String()
			}
			current = next.Unmap()
		}
		return current.String()
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		if addr, err := netip.ParseAddr(value); err == nil {
			return addr.Unmap().String()
		}
	}
	return peer.String()
}

func trusted(addr netip.Addr, rules []string) bool {
	for _, raw := range rules {
		rule := strings.TrimSpace(raw)
		if prefix, err := netip.ParsePrefix(rule); err == nil {
			if prefix.Contains(addr) {
				return true
			}
		} else if ip, err := netip.ParseAddr(rule); err == nil && ip.Unmap() == addr {
			return true
		}
	}
	return false
}
