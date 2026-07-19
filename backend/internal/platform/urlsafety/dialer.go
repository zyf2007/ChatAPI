package urlsafety

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ErrRestrictedAddress is returned when resolution yields any restricted address
// (fail-closed mixed DNS) or when dial-time resolution drifts into a restricted target.
var ErrRestrictedAddress = errors.New("ntfy destination address is restricted")

// ErrNoSafeAddress is returned when resolution yields no dialable public address.
var ErrNoSafeAddress = errors.New("ntfy destination has no safe address")

// DialFunc dials a concrete network address (typically "ip:port").
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// SafeDialer resolves a hostname on every dial and enforces the same fail-closed
// restricted-address policy used at save time. If any resolved address is
// restricted, the dial is rejected entirely — we do not skip private records
// and dial a sibling public one, because that opens DNS round-robin / rebinding
// ambiguity between validation and connection.
//
// Proxy is intentionally unsupported: requests always dial the request host
// through this dialer. Callers should leave Transport.Proxy unset/nil.
type SafeDialer struct {
	Lookup       HostLookup
	Dial         DialFunc
	AllowPrivate bool
	Timeout      time.Duration
}

func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d == nil {
		return nil, errors.New("safe dialer is nil")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split dial address: %w", err)
	}
	normalizedHost := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if zoneIdx := strings.LastIndex(normalizedHost, "%"); zoneIdx >= 0 {
		normalizedHost = normalizedHost[:zoneIdx]
	}
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		if !d.AllowPrivate {
			return nil, ErrRestrictedAddress
		}
	}

	addresses, err := ResolveHostAddresses(ctx, normalizedHost, d.Lookup)
	if err != nil {
		return nil, err
	}

	// Fail-closed: any restricted record rejects the whole destination unless
	// AllowPrivate is explicitly set (test / lab only).
	if !d.AllowPrivate {
		for _, addr := range addresses {
			if IsRestrictedAddr(addr) {
				return nil, ErrRestrictedAddress
			}
		}
	}

	dial := d.Dial
	if dial == nil {
		timeout := d.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		base := &net.Dialer{Timeout: timeout}
		dial = base.DialContext
	}

	var firstDialErr error
	tried := 0
	for _, addr := range addresses {
		if IsRestrictedAddr(addr) && !d.AllowPrivate {
			// Defensive: already rejected above when !AllowPrivate.
			if firstDialErr == nil {
				firstDialErr = ErrRestrictedAddress
			}
			continue
		}
		tried++
		target := net.JoinHostPort(addr.String(), port)
		conn, dialErr := dial(ctx, network, target)
		if dialErr == nil {
			return conn, nil
		}
		if firstDialErr == nil {
			firstDialErr = dialErr
		}
	}
	if tried == 0 {
		if firstDialErr != nil {
			return nil, firstDialErr
		}
		return nil, ErrNoSafeAddress
	}
	return nil, firstDialErr
}

// NewSafeHTTPClient builds an HTTP client that re-validates destinations at dial time.
//
// Redirect policy: redirects are not followed. Following a Location would require
// equal host/IP safety checks on every hop; ntfy push endpoints should respond
// in-place, so blocking redirects is the simpler closed boundary.
func NewSafeHTTPClient(timeout time.Duration, dialer *SafeDialer) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if dialer == nil {
		dialer = &SafeDialer{Timeout: timeout}
	} else if dialer.Timeout <= 0 {
		dialer.Timeout = timeout
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
		// Keep the original hostname for SNI / cert verification even though
		// DialContext connects to a concrete validated IP.
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// FilterPublicAddresses returns only non-restricted addresses from a resolved set.
// Prefer fail-closed AssessNtfyHost / SafeDialer for policy decisions; this helper
// is for callers that explicitly want the public subset after a separate check.
func FilterPublicAddresses(addresses []netip.Addr, allowPrivate bool) []netip.Addr {
	out := make([]netip.Addr, 0, len(addresses))
	for _, addr := range addresses {
		if IsRestrictedAddr(addr) && !allowPrivate {
			continue
		}
		out = append(out, addr)
	}
	return out
}
