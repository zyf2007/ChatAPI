package urlsafety

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const MaxNtfyURLLength = 2048

// restrictedNetworks lists destinations that must never be used as public ntfy
// endpoints (RFC 6890 special-purpose / non-global ranges and documentation nets).
// Ordinary global unicast addresses are intentionally not covered.
//
// 192.0.0.0/24 is blocked in full: it is the IETF Protocol Assignments block.
// Callers must not rely on the few special anycast hosts inside it as ntfy sinks.
var restrictedNetworks = mustParseRestrictedNetworks([]string{
	// IPv4 special-purpose / non-public
	"0.0.0.0/8",       // "this" network
	"10.0.0.0/8",      // RFC1918
	"100.64.0.0/10",   // shared address space (CGNAT)
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local / cloud metadata
	"172.16.0.0/12",   // RFC1918
	"192.0.0.0/24",    // IETF Protocol Assignments
	"192.0.2.0/24",    // TEST-NET-1
	"192.88.99.0/24",  // 6to4 relay anycast (deprecated)
	"192.168.0.0/16",  // RFC1918
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved for future use (includes broadcast)

	// IPv6 special-purpose / non-public
	"::/128",        // unspecified
	"::1/128",       // loopback
	"100::/64",      // discard-only
	"2001:db8::/32", // documentation
	"2002::/16",     // 6to4
	"fc00::/7",      // unique local
	"fe80::/10",     // link-local
	"ff00::/8",      // multicast
})

// HostLookup resolves a hostname to unmapped IP addresses.
// Tests inject deterministic resolvers; production uses the system resolver.
type HostLookup func(ctx context.Context, host string) ([]netip.Addr, error)

type URLSafetyResult struct {
	OK        bool
	IsPrivate bool
	Reason    string
}

// ParsedNtfyURL is the syntax-validated form of an ntfy endpoint.
// Address safety is evaluated separately at save time and again at dial time.
type ParsedNtfyURL struct {
	Raw      string
	URL      *url.URL
	Scheme   string
	Host     string
	Port     string
	Hostname string
}

func ValidateNtfyURL(raw string, allowPrivate bool) URLSafetyResult {
	return ValidateNtfyURLContext(context.Background(), raw, allowPrivate, nil)
}

func ValidateNtfyURLContext(ctx context.Context, raw string, allowPrivate bool, lookup HostLookup) URLSafetyResult {
	parsed, result := ParseNtfyURL(raw)
	if !result.OK || parsed == nil {
		return result
	}
	return AssessNtfyHost(ctx, parsed.Hostname, allowPrivate, lookup)
}

// ParseNtfyURL validates ntfy URL syntax without DNS resolution.
// Empty input is treated as valid so callers can model "unset".
//
// Rejected forms (shared by save-time and send-time callers):
//   - non-http(s) schemes, missing host, control characters, oversize
//   - userinfo (credential leakage / Host ambiguity)
//   - fragments (never sent to the server; would only confuse configuration)
//   - opaque URLs (e.g. "https:example.com/topic") that skip normal host parsing
//
// Empty paths and non-default ports are allowed: ntfy servers may use either.
// Zone-indexed IPv6 literals are accepted syntactically; link-local zones fail
// later in AssessNtfyHost / IsRestrictedAddr.
func ParseNtfyURL(raw string) (*ParsedNtfyURL, URLSafetyResult) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, URLSafetyResult{OK: true}
	}
	if len(value) > MaxNtfyURLLength {
		return nil, URLSafetyResult{Reason: "ntfy 地址过长，最多 2048 个字符"}
	}
	for _, ch := range value {
		if ch < 32 || ch == 127 {
			return nil, URLSafetyResult{Reason: "ntfy 地址不能包含控制字符"}
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, URLSafetyResult{Reason: "ntfy 地址格式无效"}
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, URLSafetyResult{Reason: "ntfy 地址只支持 http 或 https"}
	}
	if parsed.Opaque != "" {
		return nil, URLSafetyResult{Reason: "ntfy 地址格式无效"}
	}
	if parsed.User != nil {
		return nil, URLSafetyResult{Reason: "ntfy 地址不能包含用户名或密码"}
	}
	if parsed.Fragment != "" {
		return nil, URLSafetyResult{Reason: "ntfy 地址不能包含 fragment"}
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return nil, URLSafetyResult{Reason: "ntfy 地址必须包含主机名"}
	}
	// Strip IPv6 zone identifier for normalization; zone forms still parse as
	// the base address for restriction checks (link-local stays blocked).
	normalizedHost := strings.TrimSuffix(strings.ToLower(hostname), ".")
	if zoneIdx := strings.LastIndex(normalizedHost, "%"); zoneIdx >= 0 {
		normalizedHost = normalizedHost[:zoneIdx]
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return &ParsedNtfyURL{
		Raw:      value,
		URL:      parsed,
		Scheme:   scheme,
		Host:     parsed.Host,
		Port:     port,
		Hostname: normalizedHost,
	}, URLSafetyResult{OK: true}
}

// AssessNtfyHost resolves hostname (or parses literal IP) and rejects restricted destinations.
// Policy is fail-closed on mixed DNS: if any resolved address is restricted, the whole
// host is rejected. This avoids DNS round-robin / rebinding ambiguity where save-time
// or dial-time might otherwise pick a public A/AAAA while another record is private.
func AssessNtfyHost(ctx context.Context, host string, allowPrivate bool, lookup HostLookup) URLSafetyResult {
	normalizedHost := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if zoneIdx := strings.LastIndex(normalizedHost, "%"); zoneIdx >= 0 {
		normalizedHost = normalizedHost[:zoneIdx]
	}
	if normalizedHost == "" {
		return URLSafetyResult{Reason: "ntfy 地址必须包含主机名"}
	}
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		if allowPrivate {
			return URLSafetyResult{OK: true, IsPrivate: true}
		}
		return URLSafetyResult{IsPrivate: true, Reason: "ntfy 地址不能指向本机或内网地址"}
	}
	addresses, err := ResolveHostAddresses(ctx, normalizedHost, lookup)
	if err != nil {
		return URLSafetyResult{Reason: err.Error()}
	}
	restricted := false
	for _, address := range addresses {
		if IsRestrictedAddr(address) {
			restricted = true
			break
		}
	}
	if restricted && !allowPrivate {
		return URLSafetyResult{IsPrivate: true, Reason: "ntfy 地址不能指向本机或内网地址"}
	}
	return URLSafetyResult{OK: true, IsPrivate: restricted}
}

func ResolveHostAddresses(ctx context.Context, host string, lookup HostLookup) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{literal.Unmap()}, nil
	}
	if lookup == nil {
		lookup = defaultHostLookup
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return nil, errors.New("ntfy 地址域名解析失败")
	}
	out := make([]netip.Addr, 0, len(addresses))
	seen := map[netip.Addr]struct{}{}
	for _, address := range addresses {
		if !address.IsValid() {
			continue
		}
		address = address.Unmap()
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	if len(out) == 0 {
		return nil, errors.New("ntfy 地址域名没有可用的 IP 解析结果")
	}
	return out, nil
}

func IsRestrictedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	if !addr.IsValid() {
		return true
	}
	// Drop zone for prefix matching (link-local zones still match fe80::/10).
	if addr.Is6() && addr.Zone() != "" {
		addr = addr.WithZone("")
	}
	for _, prefix := range restrictedNetworks {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func defaultHostLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	records, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(records))
	for _, record := range records {
		addr, ok := netip.AddrFromSlice(record.IP)
		if !ok {
			continue
		}
		addresses = append(addresses, addr.Unmap())
	}
	return addresses, nil
}

func mustParseRestrictedNetworks(values []string) []netip.Prefix {
	items := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		items = append(items, prefix)
	}
	return items
}
