package ntfy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientSend_Success(t *testing.T) {
	var gotTitle, gotBody, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(&http.Client{Timeout: time.Second})
	err := client.Send(context.Background(), Message{
		URL:   server.URL + "/topic",
		Title: "ChatAPI · hello",
		Text:  "新请求：\nping",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotTitle != "ChatAPI · hello" {
		t.Fatalf("unexpected title header: %q", gotTitle)
	}
	if !strings.Contains(gotContentType, "text/plain") {
		t.Fatalf("unexpected content type: %q", gotContentType)
	}
	if gotBody != "新请求：\nping" {
		t.Fatalf("unexpected body: %q", gotBody)
	}
}

func TestClientSend_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(&http.Client{Timeout: time.Second})
	err := client.Send(context.Background(), Message{URL: server.URL, Title: "t", Text: "body"})
	if !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("expected unexpected status, got %v", err)
	}
}

func TestClientSend_RedirectBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/other", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)

	client := NewClient(&http.Client{
		Timeout: time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
	err := client.Send(context.Background(), Message{URL: server.URL, Title: "t", Text: "body"})
	if !errors.Is(err, ErrRedirectBlocked) {
		t.Fatalf("expected redirect blocked, got %v", err)
	}
}

func TestClientSend_NetworkError(t *testing.T) {
	client := NewClient(&http.Client{Timeout: 50 * time.Millisecond})
	err := client.Send(context.Background(), Message{
		URL:   "http://127.0.0.1:1/topic",
		Title: "t",
		Text:  "body",
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestClientSend_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(&http.Client{Timeout: 30 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := client.Send(ctx, Message{URL: server.URL, Title: "t", Text: "body"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClientSend_UTF8TitleEncoded(t *testing.T) {
	var gotTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(&http.Client{Timeout: time.Second})
	if err := client.Send(context.Background(), Message{
		URL:   server.URL,
		Title: "ChatAPI · 你好",
		Text:  "body",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(gotTitle, "=?utf-8?b?") && !strings.Contains(gotTitle, "=?UTF-8?B?") {
		t.Fatalf("expected mime-encoded title, got %q", gotTitle)
	}
}
