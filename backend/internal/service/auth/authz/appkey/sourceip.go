package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/platform/clientip"
)

func ExtractRequestAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := strings.TrimSpace(r.Header.Get("X-ChatAPI-App-Key")); value != "" {
		return value
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

func RequestSourceIP(r *http.Request, trustedProxies []string) string {
	return clientip.FromRequest(r, trustedProxies)
}

func (s *Service) AllowSourceIP(principal Principal, remoteAddr string) bool {
	if len(principal.AllowedSourceIPs) == 0 {
		return true
	}
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, rawRule := range principal.AllowedSourceIPs {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			prefix, err := netip.ParsePrefix(rule)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(rule)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}
