package access_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
)

func TestAccessRateLimitClientIdentity(t *testing.T) {
	tests := []struct {
		name       string
		trusted    []string
		peer       string
		first      map[string]string
		second     map[string]string
		secondHost string
		wantSecond bool
	}{
		{name: "untrusted forwarded header", peer: "192.0.2.10:1234", first: map[string]string{"X-Forwarded-For": "198.51.100.1"}, second: map[string]string{"X-Forwarded-For": "198.51.100.2"}},
		{name: "untrusted real IP", peer: "192.0.2.10:1234", first: map[string]string{"X-Real-IP": "198.51.100.1"}, second: map[string]string{"X-Real-IP": "198.51.100.2"}},
		{name: "untrusted peer with configured proxies", trusted: []string{"10.0.0.0/8"}, peer: "192.0.2.10:1234", first: map[string]string{"X-Real-IP": "198.51.100.1"}, second: map[string]string{"X-Real-IP": "198.51.100.2"}},
		{name: "host changes do not reset global limit", peer: "192.0.2.10:1234", secondHost: "other.example"},
		{name: "trusted proxy separates clients", trusted: []string{"10.0.0.1"}, peer: "10.0.0.1:1234", first: map[string]string{"X-Forwarded-For": "198.51.100.1"}, second: map[string]string{"X-Forwarded-For": "198.51.100.2"}, wantSecond: true},
		{name: "trusted real IP separates clients", trusted: []string{"10.0.0.0/8"}, peer: "10.0.0.1:1234", first: map[string]string{"X-Real-IP": "198.51.100.1"}, second: map[string]string{"X-Real-IP": "198.51.100.2"}, wantSecond: true},
		{name: "ignore spoofed prefix in appended chain", trusted: []string{"10.0.0.0/8"}, peer: "10.0.0.1:1234", first: map[string]string{"X-Forwarded-For": "203.0.113.1, 198.51.100.1, 10.0.0.2"}, second: map[string]string{"X-Forwarded-For": "203.0.113.2, 198.51.100.1, 10.0.0.2"}},
		{name: "invalid forwarded headers fall back to peer", trusted: []string{"10.0.0.1"}, peer: "10.0.0.1:1234", first: map[string]string{"X-Forwarded-For": "invalid-one"}, second: map[string]string{"X-Forwarded-For": "invalid-two"}},
		{name: "equivalent IPv6 spellings share bucket", trusted: []string{"10.0.0.1"}, peer: "10.0.0.1:1234", first: map[string]string{"X-Real-IP": "2001:db8::1"}, second: map[string]string{"X-Real-IP": "2001:0db8:0:0:0:0:0:1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Default(config.ModeServe, t.TempDir())
			cfg.AccessRateLimitRequests = 1
			cfg.AccessRateLimitWindow = time.Minute
			cfg.TrustedProxies = test.trusted
			service := authaccess.NewService(cfg, nil, nil)
			request := func(headers map[string]string, host string) bool {
				r := httptest.NewRequest("GET", "http://chatapi.test/api/conversations", nil)
				r.RemoteAddr = test.peer
				if host != "" {
					r.Host = host
				}
				for key, value := range headers {
					r.Header.Set(key, value)
				}
				return service.AllowRequest(r)
			}
			if !request(test.first, "") {
				t.Fatal("first request should pass")
			}
			if got := request(test.second, test.secondHost); got != test.wantSecond {
				t.Fatalf("second request allowed = %v, want %v", got, test.wantSecond)
			}
		})
	}
}
