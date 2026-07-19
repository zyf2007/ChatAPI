package urlsafety

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeDialer_PublicIPAllowed(t *testing.T) {
	var dialed atomic.Value
	dialer := &SafeDialer{
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			if host != "ntfy.example" {
				t.Fatalf("unexpected host: %s", host)
			}
			// Use a real public IP (not TEST-NET); TEST-NET is restricted.
			return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed.Store(address)
			return nil, errors.New("stop after dial target recorded")
		},
	}
	_, err := dialer.DialContext(context.Background(), "tcp", "ntfy.example:443")
	if err == nil {
		t.Fatal("expected dial error from stub")
	}
	got, _ := dialed.Load().(string)
	if got != "1.2.3.4:443" {
		t.Fatalf("expected dial to validated public IP, got %q", got)
	}
}

func TestSafeDialer_PrivateAndLoopbackRejected(t *testing.T) {
	cases := []struct {
		name string
		host string
		addr string
	}{
		{name: "loopback", host: "127.0.0.1", addr: "127.0.0.1"},
		{name: "rfc1918", host: "10.0.0.8", addr: "10.0.0.8"},
		{name: "mapped_loopback", host: "::ffff:127.0.0.1", addr: "::ffff:127.0.0.1"},
		{name: "localhost_name", host: "localhost", addr: "127.0.0.1"},
		{name: "test_net_3", host: "203.0.113.10", addr: "203.0.113.10"},
		{name: "doc_v6", host: "2001:db8::1", addr: "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialer := &SafeDialer{
				Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr(tc.addr)}, nil
				},
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					t.Fatalf("must not dial restricted address %s", address)
					return nil, nil
				},
			}
			_, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(tc.host, "80"))
			if !errors.Is(err, ErrRestrictedAddress) {
				t.Fatalf("expected restricted address error, got %v", err)
			}
		})
	}
}

func TestSafeDialer_RejectsResolutionDriftToPrivate(t *testing.T) {
	// Save-time might have seen a public A record; dial-time re-resolve sees private.
	dialer := &SafeDialer{
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			t.Fatalf("must not dial after rebinding to private: %s", address)
			return nil, nil
		},
	}
	_, err := dialer.DialContext(context.Background(), "tcp", "ntfy.example:80")
	if !errors.Is(err, ErrRestrictedAddress) {
		t.Fatalf("expected restricted after drift, got %v", err)
	}
}

func TestSafeDialer_MixedDNSFailClosed(t *testing.T) {
	// Previously skipped private and dialed public; policy is now fail-closed.
	dialer := &SafeDialer{
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("1.2.3.4"),
			}, nil
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			t.Fatalf("must not dial when mixed DNS includes restricted: %s", address)
			return nil, nil
		},
	}
	_, err := dialer.DialContext(context.Background(), "tcp", "dual.example:443")
	if !errors.Is(err, ErrRestrictedAddress) {
		t.Fatalf("expected fail-closed mixed DNS, got %v", err)
	}
}

func TestSafeDialer_AllPublicMixedTriesUntilSuccess(t *testing.T) {
	var dialed []string
	dialer := &SafeDialer{
		Lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{
				netip.MustParseAddr("1.2.3.4"),
				netip.MustParseAddr("5.6.7.8"),
			}, nil
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if address == "1.2.3.4:443" {
				return nil, errors.New("first failed")
			}
			return nil, errors.New("recorded second")
		},
	}
	_, _ = dialer.DialContext(context.Background(), "tcp", "dual.example:443")
	if len(dialed) != 2 || dialed[0] != "1.2.3.4:443" || dialed[1] != "5.6.7.8:443" {
		t.Fatalf("unexpected dial order: %#v", dialed)
	}
}

func TestNewSafeHTTPClient_BlocksRedirectAndUsesSafeDial(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	host, port, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// httptest binds loopback; allowPrivate for this unit transport path only so we can
	// observe redirect handling. Production user config never sets allowPrivate.
	client := NewSafeHTTPClient(2*time.Second, &SafeDialer{
		AllowPrivate: true,
		Lookup: func(ctx context.Context, name string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(host)}, nil
		},
	})
	req, err := http.NewRequest(http.MethodPost, "http://"+net.JoinHostPort(host, port)+"/topic", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect response to be returned without follow, got %d", resp.StatusCode)
	}
}
