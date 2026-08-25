package clawbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	imsvc "github.com/zyf2007/ChatAPI/internal/service/im"
)

func TestClientLoginUpdatesAndSend(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := make(map[string]int)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		if got := r.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
			t.Errorf("AuthorizationType = %q", got)
		}
		if r.Header.Get("X-WECHAT-UIN") == "" {
			t.Error("missing X-WECHAT-UIN")
		}
		if r.Header.Get("iLink-App-Id") != ilinkAppID || r.Header.Get("iLink-App-ClientVersion") != ilinkClientVersion {
			t.Errorf("unexpected iLink client headers: %q %q", r.Header.Get("iLink-App-Id"), r.Header.Get("iLink-App-ClientVersion"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			if r.URL.Query().Get("bot_type") != defaultBotType {
				t.Errorf("bot_type = %q", r.URL.Query().Get("bot_type"))
			}
			var body struct {
				LocalTokens []string `json:"local_token_list"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.LocalTokens) != 1 || body.LocalTokens[0] != "local-token" {
				t.Errorf("local tokens = %#v, err=%v", body.LocalTokens, err)
			}
			fmt.Fprint(w, `{"qrcode":"qr-secret","qrcode_img_content":"https://weixin.qq.com/x/test"}`)
		case "/ilink/bot/get_qrcode_status":
			if r.URL.Query().Get("qrcode") != "qr-secret" || r.URL.Query().Get("verify_code") != "123456" {
				t.Errorf("unexpected login query: %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"status":"confirmed","bot_token":"token-secret","ilink_bot_id":"bot-1","ilink_user_id":"owner-1","baseurl":%q}`, server.URL)
		case "/ilink/bot/getupdates":
			if r.Header.Get("Authorization") != "Bearer token-secret" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			fmt.Fprint(w, `{"ret":0,"errcode":0,"get_updates_buf":"cursor-2","longpolling_timeout_ms":12000,"msgs":[]}`)
		case "/ilink/bot/sendmessage":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			msg, _ := body["msg"].(map[string]any)
			info, _ := body["base_info"].(map[string]any)
			if info["channel_version"] != ilinkChannelVersion || info["bot_agent"] != "ChatAPI/1.0.0" {
				t.Errorf("unexpected base_info: %#v", info)
			}
			_, hasFrom := msg["from_user_id"]
			if !hasFrom || msg["from_user_id"] != "" || msg["to_user_id"] != "owner-1" || msg["context_token"] != "context-1" {
				t.Errorf("unexpected message: %#v", msg)
			}
			fmt.Fprint(w, `{"ret":0,"errcode":0}`)
		case "/ilink/bot/msg/notifystart", "/ilink/bot/msg/notifystop":
			fmt.Fprint(w, `{"ret":0,"errcode":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	challenge, qrURL, err := client.StartLogin(context.Background(), []string{"local-token"})
	if err != nil {
		t.Fatal(err)
	}
	if challenge.QRCode != "qr-secret" || qrURL != "https://weixin.qq.com/x/test" {
		t.Fatalf("unexpected challenge: %#v %q", challenge, qrURL)
	}
	status, _, err := client.PollLogin(context.Background(), challenge, "123456")
	if err != nil {
		t.Fatal(err)
	}
	provider := NewProvider(client)
	account, err := provider.accountFromLogin(status)
	if err != nil {
		t.Fatal(err)
	}
	if account.Endpoint != server.URL {
		t.Fatalf("endpoint = %q", account.Endpoint)
	}
	updates, err := client.GetUpdates(context.Background(), account.Endpoint, "token-secret", "cursor-1", 12*time.Second)
	if err != nil || updates.Cursor != "cursor-2" {
		t.Fatalf("updates = %#v, err=%v", updates, err)
	}
	if err := client.SendText(context.Background(), account.Endpoint, "token-secret", outboundText{
		To: "owner-1", ContextToken: "context-1", Text: "hello", ClientID: "client-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.NotifyStart(context.Background(), account.Endpoint, "token-secret"); err != nil {
		t.Fatal(err)
	}
	if err := client.NotifyStop(context.Background(), account.Endpoint, "token-secret"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/ilink/bot/get_bot_qrcode", "/ilink/bot/get_qrcode_status", "/ilink/bot/getupdates", "/ilink/bot/sendmessage", "/ilink/bot/msg/notifystart", "/ilink/bot/msg/notifystop"} {
		if calls[path] != 1 {
			t.Errorf("calls[%s] = %d", path, calls[path])
		}
	}
}

func TestProviderMapsQRCodeStates(t *testing.T) {
	t.Parallel()
	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	provider := NewProvider(client)
	internal, _ := json.Marshal(loginChallenge{QRCode: "qr", Base: server.URL})
	challenge := imsvc.LoginChallenge{
		Provider: imsvc.ProviderClawBot, Opaque: internal, QRCodeURL: "https://weixin.qq.com/x/test", ExpiresAt: time.Now().Add(time.Minute),
	}
	for _, item := range []struct {
		response string
		want     imsvc.LoginState
	}{
		{`{"status":"wait"}`, imsvc.LoginWaiting},
		{`{"status":"scaned"}`, imsvc.LoginScanned},
		{`{"status":"need_verifycode"}`, imsvc.LoginVerifyNeeded},
		{`{"status":"verify_code_blocked"}`, imsvc.LoginVerifyBlocked},
		{`{"status":"expired"}`, imsvc.LoginExpired},
		{`{"status":"binded_redirect"}`, imsvc.LoginAlreadyBound},
	} {
		response = item.response
		result, err := provider.PollLogin(context.Background(), challenge, "")
		if err != nil {
			t.Fatalf("response %s: %v", item.response, err)
		}
		if result.State != item.want {
			t.Errorf("response %s: state=%s want=%s", item.response, result.State, item.want)
		}
	}
	parsed, _ := url.Parse(server.URL)
	response = fmt.Sprintf(`{"status":"scaned_but_redirect","redirect_host":%q}`, parsed.Host)
	redirected, err := provider.PollLogin(context.Background(), challenge, "")
	if err != nil || redirected.State != imsvc.LoginWaiting {
		t.Fatalf("redirect state = %#v, err=%v", redirected, err)
	}
	var redirectedChallenge loginChallenge
	if json.Unmarshal(redirected.Challenge.Opaque, &redirectedChallenge) != nil || redirectedChallenge.Base != server.URL {
		t.Fatalf("redirected challenge = %#v", redirectedChallenge)
	}
}

func TestClientRejectsUntrustedEndpointsAndQRCode(t *testing.T) {
	t.Parallel()
	badEndpoints := []string{
		"http://ilinkai.weixin.qq.com",
		"https://evilweixin.qq.com",
		"https://user@ilinkai.weixin.qq.com",
		"https://ilinkai.weixin.qq.com:8443",
		"https://ilinkai.weixin.qq.com/path",
		"https://ilinkai.weixin.qq.com?token=x",
	}
	for _, raw := range badEndpoints {
		if _, err := parseEndpointURL(raw, false, ""); err == nil {
			t.Errorf("expected endpoint rejection: %s", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "https://example.com/qr", "https://weixin.qq.com/#fragment", strings.Repeat("x", maxQRCodeURLBytes+1)} {
		if err := validatePublicQRCodeURL(raw); err == nil {
			t.Errorf("expected QR URL rejection: %q", raw)
		}
	}
	if _, err := parseEndpointURL("https://ilinkai.weixin.qq.com", false, ""); err != nil {
		t.Fatalf("trusted endpoint rejected: %v", err)
	}
}

func TestClientBoundsResponsesAndClassifiesStaleToken(t *testing.T) {
	t.Parallel()
	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		}))
		defer server.Close()
		client := newTestClient(t, server)
		_, err := client.GetUpdates(context.Background(), server.URL, "token", "", 5*time.Second)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("redirect is not followed", func(t *testing.T) {
		followed := make(chan struct{}, 1)
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed <- struct{}{} }))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusFound)
		}))
		defer redirect.Close()
		client := newTestClient(t, redirect)
		_, err := client.GetUpdates(context.Background(), redirect.URL, "token", "", 5*time.Second)
		if err == nil {
			t.Fatal("redirect response should fail")
		}
		select {
		case <-followed:
			t.Fatal("credentialed redirect was followed")
		default:
		}
	})
	t.Run("stale context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"ret":-2,"errcode":0}`)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		err := client.SendText(context.Background(), server.URL, "token", outboundText{To: "owner", ContextToken: "stale", Text: "hello", ClientID: "client"})
		if !errors.Is(err, ErrContextExpired) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stale ret", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"ret":-14,"errcode":0}`)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		_, err := client.GetUpdates(context.Background(), server.URL, "token", "", 5*time.Second)
		if !errors.Is(err, ErrStaleToken) {
			t.Fatalf("err = %v", err)
		}
	})
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(server.Client())
	client.loginBaseURL = server.URL
	client.allowTestHTTP = true
	client.allowTestEndpointHost = parsed.Hostname()
	return client
}
