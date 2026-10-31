package clientip_test

import (
	"net/http/httptest"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/clientip"
)

func TestFromRequest(t *testing.T) {
	tests := []struct {
		name      string
		peer      string
		trusted   []string
		forwarded []string
		real      string
		want      string
	}{
		{name: "direct IPv4", peer: "192.0.2.1:1234", want: "192.0.2.1"},
		{name: "direct IPv6", peer: "[2001:0db8::1]:1234", want: "2001:db8::1"},
		{name: "mapped IPv4", peer: "[::ffff:192.0.2.1]:1234", want: "192.0.2.1"},
		{name: "untrusted forwarding", peer: "192.0.2.1:1234", forwarded: []string{"198.51.100.1"}, real: "203.0.113.1", want: "192.0.2.1"},
		{name: "trusted exact IP", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, forwarded: []string{"198.51.100.1"}, want: "198.51.100.1"},
		{name: "IPv6 proxy CIDR", peer: "[2001:db8::1]:1234", trusted: []string{"2001:db8::/64"}, forwarded: []string{"198.51.100.1"}, want: "198.51.100.1"},
		{name: "multiple proxy hops and header lines", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.0/8"}, forwarded: []string{"203.0.113.1, 198.51.100.1", "10.0.0.2"}, want: "198.51.100.1"},
		{name: "forwarded takes precedence over real IP", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, forwarded: []string{"198.51.100.1"}, real: "203.0.113.1", want: "198.51.100.1"},
		{name: "real IP fallback", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, real: "2001:0db8:0:0:0:0:0:1", want: "2001:db8::1"},
		{name: "invalid chain falls back to peer", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, forwarded: []string{"198.51.100.1, invalid"}, real: "203.0.113.1", want: "10.0.0.1"},
		{name: "empty chain falls back to peer", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, forwarded: []string{""}, real: "203.0.113.1", want: "10.0.0.1"},
		{name: "invalid real IP falls back to peer", peer: "10.0.0.1:1234", trusted: []string{"10.0.0.1"}, real: "invalid", want: "10.0.0.1"},
		{name: "invalid trust rule", peer: "10.0.0.1:1234", trusted: []string{"invalid"}, real: "198.51.100.1", want: "10.0.0.1"},
		{name: "invalid peer cannot trust headers", peer: "invalid", trusted: []string{"0.0.0.0/0"}, real: "198.51.100.1", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://chatapi.test/", nil)
			r.RemoteAddr = test.peer
			for _, value := range test.forwarded {
				r.Header.Add("X-Forwarded-For", value)
			}
			r.Header.Set("X-Real-IP", test.real)
			if got := clientip.FromRequest(r, test.trusted); got != test.want {
				t.Fatalf("client IP = %q, want %q", got, test.want)
			}
		})
	}
	if got := clientip.FromRequest(nil, nil); got != "" {
		t.Fatalf("nil request IP = %q", got)
	}
}
