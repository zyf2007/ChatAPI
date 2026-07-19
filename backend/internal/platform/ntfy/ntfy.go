package ntfy

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
)

var ErrRedirectBlocked = errors.New("ntfy redirect blocked")
var ErrUnexpectedStatus = errors.New("ntfy unexpected status")

type Client struct {
	httpClient *http.Client
}

type Message struct {
	URL   string
	Title string
	Text  string
}

// NewClient builds an ntfy client. When httpClient is nil, a safe dialer client
// is used so DNS rebinding cannot bypass the restricted-address policy.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = urlsafety.NewSafeHTTPClient(5*time.Second, nil)
	}
	return &Client{httpClient: httpClient}
}

// NewClientWithDialer builds a client whose transport re-validates addresses at dial time.
func NewClientWithDialer(timeout time.Duration, dialer *urlsafety.SafeDialer) *Client {
	return NewClient(urlsafety.NewSafeHTTPClient(timeout, dialer))
}

func (c *Client) Send(ctx context.Context, message Message) error {
	if c == nil || c.httpClient == nil {
		return nil
	}
	url := strings.TrimSpace(message.URL)
	text := strings.TrimSpace(message.Text)
	if url == "" || text == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(text))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if title := encodeTitle(strings.TrimSpace(message.Title)); title != "" {
		req.Header.Set("Title", title)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ErrRedirectBlocked
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnexpectedStatus
	}
	return nil
}

func encodeTitle(value string) string {
	if value == "" {
		return ""
	}
	if isLatin1(value) {
		return value
	}
	return mime.BEncoding.Encode("utf-8", value)
}

func isLatin1(value string) bool {
	for _, ch := range value {
		if ch > 255 {
			return false
		}
	}
	return true
}
