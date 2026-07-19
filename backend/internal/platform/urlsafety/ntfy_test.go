package urlsafety

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestValidateNtfyURL(t *testing.T) {
	publicLookup := HostLookup(func(ctx context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "ntfy.sh", "example.com":
			return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
		case "private.example":
			return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
		case "mixed.example":
			return []netip.Addr{
				netip.MustParseAddr("1.2.3.4"),
				netip.MustParseAddr("10.0.0.9"),
			}, nil
		default:
			return nil, errors.New("nxdomain")
		}
	})

	cases := []struct {
		name         string
		url          string
		allowPrivate bool
		wantOK       bool
		wantPrivate  bool
	}{
		{name: "empty", url: "", wantOK: true},
		{name: "https_public", url: "https://ntfy.sh/topic", wantOK: true},
		{name: "private_ipv4_blocked", url: "http://127.0.0.1/topic", wantOK: false, wantPrivate: true},
		{name: "private_ipv4_allowed", url: "http://127.0.0.1/topic", allowPrivate: true, wantOK: true, wantPrivate: true},
		{name: "localhost_blocked", url: "http://localhost/topic", wantOK: false, wantPrivate: true},
		{name: "invalid_scheme", url: "ftp://example.com/topic", wantOK: false},
		{name: "metadata_ipv4", url: "http://169.254.169.254/latest", wantOK: false, wantPrivate: true},
		{name: "rfc1918", url: "http://192.168.1.1/topic", wantOK: false, wantPrivate: true},
		{name: "ipv4_mapped_loopback", url: "http://[::ffff:127.0.0.1]/topic", wantOK: false, wantPrivate: true},
		{name: "link_local_v6", url: "http://[fe80::1]/topic", wantOK: false, wantPrivate: true},
		{name: "unspecified_v4", url: "http://0.0.0.0/topic", wantOK: false, wantPrivate: true},
		{name: "dns_private_host", url: "http://private.example/topic", wantOK: false, wantPrivate: true},
		{name: "dns_mixed_fail_closed", url: "http://mixed.example/topic", wantOK: false, wantPrivate: true},
		{name: "dns_failure", url: "http://missing.example/topic", wantOK: false},
		{name: "test_net_1", url: "http://192.0.2.10/topic", wantOK: false, wantPrivate: true},
		{name: "test_net_2", url: "http://198.51.100.10/topic", wantOK: false, wantPrivate: true},
		{name: "test_net_3", url: "http://203.0.113.10/topic", wantOK: false, wantPrivate: true},
		{name: "benchmarking", url: "http://198.18.0.1/topic", wantOK: false, wantPrivate: true},
		{name: "ietf_protocol_assignments", url: "http://192.0.0.1/topic", wantOK: false, wantPrivate: true},
		{name: "reserved_240", url: "http://240.0.0.1/topic", wantOK: false, wantPrivate: true},
		{name: "doc_v6", url: "http://[2001:db8::1]/topic", wantOK: false, wantPrivate: true},
		{name: "userinfo_rejected", url: "https://user:pass@ntfy.sh/topic", wantOK: false},
		{name: "fragment_rejected", url: "https://ntfy.sh/topic#frag", wantOK: false},
		{name: "opaque_rejected", url: "https:ntfy.sh/topic", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateNtfyURLContext(context.Background(), tc.url, tc.allowPrivate, publicLookup)
			if got.OK != tc.wantOK || got.IsPrivate != tc.wantPrivate {
				t.Fatalf("unexpected validation result: %#v", got)
			}
		})
	}
}

func TestParseNtfyURL_DefaultsPort(t *testing.T) {
	parsed, result := ParseNtfyURL("https://ntfy.sh/my-topic")
	if !result.OK || parsed == nil {
		t.Fatalf("parse failed: %#v", result)
	}
	if parsed.Port != "443" || parsed.Hostname != "ntfy.sh" || parsed.Scheme != "https" {
		t.Fatalf("unexpected parsed url: %#v", parsed)
	}
}

func TestParseNtfyURL_RejectsUserinfoAndFragment(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "user", raw: "https://alice@ntfy.sh/topic"},
		{name: "user_pass", raw: "https://alice:secret@ntfy.sh/topic"},
		{name: "fragment", raw: "https://ntfy.sh/topic#section"},
		{name: "opaque", raw: "https:example.com/topic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, result := ParseNtfyURL(tc.raw)
			if result.OK || parsed != nil {
				t.Fatalf("expected rejection, got parsed=%#v result=%#v", parsed, result)
			}
		})
	}
}

func TestParseNtfyURL_AllowsEmptyPathAndNonDefaultPort(t *testing.T) {
	parsed, result := ParseNtfyURL("http://example.com:2586")
	if !result.OK || parsed == nil {
		t.Fatalf("parse failed: %#v", result)
	}
	if parsed.Port != "2586" || parsed.Hostname != "example.com" {
		t.Fatalf("unexpected parsed: %#v", parsed)
	}
}

func TestIsRestrictedAddr_UnmapsIPv4Mapped(t *testing.T) {
	addr := netip.MustParseAddr("::ffff:10.1.2.3")
	if !IsRestrictedAddr(addr) {
		t.Fatal("expected IPv4-mapped private address to be restricted")
	}
}

func TestIsRestrictedAddr_SpecialPurposeRanges(t *testing.T) {
	// Restricted special-purpose / non-public destinations.
	restricted := []string{
		"192.0.0.1",
		"192.0.2.1",
		"198.18.0.1",
		"198.19.255.255",
		"198.51.100.1",
		"203.0.113.1",
		"240.0.0.1",
		"255.255.255.255",
		"2001:db8::1",
		"100::1",
		"2002::1",
		"::ffff:192.0.2.9",
	}
	for _, raw := range restricted {
		if !IsRestrictedAddr(netip.MustParseAddr(raw)) {
			t.Fatalf("expected restricted: %s", raw)
		}
	}

	// Ordinary global unicast — must remain allowed.
	// 1.1.1.1 / 8.8.8.8 / 2606:4700:: are real public anycast/DNS ranges.
	allowed := []string{
		"1.1.1.1",
		"8.8.8.8",
		"142.250.190.14",
		"2606:4700:4700::1111",
		"2001:4860:4860::8888",
	}
	for _, raw := range allowed {
		if IsRestrictedAddr(netip.MustParseAddr(raw)) {
			t.Fatalf("must not restrict global unicast: %s", raw)
		}
	}
}

func TestAssessNtfyHost_MixedDNSFailClosed(t *testing.T) {
	lookup := HostLookup(func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("8.8.8.8"),
			netip.MustParseAddr("10.1.2.3"),
		}, nil
	})
	got := AssessNtfyHost(context.Background(), "mixed.example", false, lookup)
	if got.OK || !got.IsPrivate {
		t.Fatalf("expected fail-closed mixed DNS, got %#v", got)
	}
	// allowPrivate still marks IsPrivate but accepts for lab/test paths.
	got = AssessNtfyHost(context.Background(), "mixed.example", true, lookup)
	if !got.OK || !got.IsPrivate {
		t.Fatalf("expected allowPrivate mixed DNS ok, got %#v", got)
	}
}
