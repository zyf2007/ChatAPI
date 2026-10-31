package app

import (
	"net/http/httptest"
	"testing"
)

func TestSourceIPRestrictionsUseTrustedClientAddress(t *testing.T) {
	service := &Service{}
	principal := Principal{AllowedSourceIPs: []string{"198.51.100.1"}}
	tests := []struct {
		name      string
		peer      string
		forwarded string
		want      bool
	}{
		{name: "untrusted peer cannot claim allowlisted address", peer: "192.0.2.10:1234", forwarded: "198.51.100.1"},
		{name: "spoofed prefix cannot claim allowlisted address", peer: "10.0.0.1:1234", forwarded: "198.51.100.1, 192.0.2.10"},
		{name: "trusted proxy forwards allowed client", peer: "10.0.0.1:1234", forwarded: "198.51.100.1", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://chatapi.test/", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("X-Forwarded-For", test.forwarded)
			source := RequestSourceIP(r, []string{"10.0.0.1"})
			if got := service.AllowSourceIP(principal, source); got != test.want {
				t.Fatalf("source %q allowed = %v, want %v", source, got, test.want)
			}
		})
	}
}
