package clawbot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
)

const (
	defaultBaseURL      = "https://ilinkai.weixin.qq.com"
	defaultBotType      = "3"
	maxResponseBytes    = 1 << 20
	maxQRCodeTokenBytes = 4096
	maxQRCodeURLBytes   = 4096
	maxOutboundText     = 8000
	regularTimeout      = 15 * time.Second
	loginPollTimeout    = 38 * time.Second
	ilinkAppID          = "bot"
	ilinkChannelVersion = "2.4.6"
	ilinkClientVersion  = "132102" // 2.4.6 encoded as 0x00MMNNPP.
)

var (
	ErrStaleToken     = errors.New("clawbot token is stale")
	ErrContextExpired = errors.New("clawbot reply context is stale")
)

type APIError struct {
	Operation string
	Status    int
	Ret       int
	ErrCode   int
	Message   string
}

func (e *APIError) Error() string {
	if e == nil {
		return "clawbot API error"
	}
	return fmt.Sprintf("clawbot %s failed (status=%d ret=%d errcode=%d)", e.Operation, e.Status, e.Ret, e.ErrCode)
}

type Client struct {
	httpClient            *http.Client
	loginBaseURL          string
	allowTestHTTP         bool
	allowTestEndpointHost string
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = urlsafety.NewSafeHTTPClient(45*time.Second, nil)
	}
	clone := *client
	// iLink advertises endpoint changes in signed JSON fields. Never follow HTTP
	// redirects with bot credentials; the caller receives the 3xx as an error.
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{httpClient: &clone, loginBaseURL: defaultBaseURL}
}

type loginChallenge struct {
	QRCode string `json:"qrcode"`
	Base   string `json:"base"`
}

type qrCodeResponse struct {
	QRCode    string `json:"qrcode"`
	QRCodeURL string `json:"qrcode_img_content"`
}

type qrStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	BotID        string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	OwnerID      string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

type textItem struct {
	Text string `json:"text,omitempty"`
}

type messageItem struct {
	Type        int       `json:"type,omitempty"`
	IsCompleted bool      `json:"is_completed,omitempty"`
	MessageID   string    `json:"msg_id,omitempty"`
	Text        *textItem `json:"text_item,omitempty"`
}

type message struct {
	Sequence     int64         `json:"seq,omitempty"`
	MessageID    int64         `json:"message_id,omitempty"`
	From         string        `json:"from_user_id"`
	To           string        `json:"to_user_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	Items        []messageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type updatesResponse struct {
	Ret                    int       `json:"ret"`
	ErrCode                int       `json:"errcode"`
	ErrorMessage           string    `json:"errmsg"`
	Messages               []message `json:"msgs"`
	Cursor                 string    `json:"get_updates_buf"`
	LongPollingTimeoutMsec int       `json:"longpolling_timeout_ms"`
}

type sendResponse struct {
	Ret          int    `json:"ret"`
	ErrCode      int    `json:"errcode"`
	ErrorMessage string `json:"errmsg"`
}

func (c *Client) StartLogin(ctx context.Context, localTokens []string) (loginChallenge, string, error) {
	if len(localTokens) > 1 {
		return loginChallenge{}, "", errors.New("clawbot login accepts at most one local token")
	}
	normalizedTokens := make([]string, len(localTokens))
	for index, token := range localTokens {
		normalizedTokens[index] = strings.TrimSpace(token)
		if normalizedTokens[index] == "" || len(normalizedTokens[index]) > 64*1024 {
			return loginChallenge{}, "", errors.New("invalid clawbot local token")
		}
	}
	var response qrCodeResponse
	err := c.doJSON(ctx, http.MethodPost, c.loginBaseURL, "/ilink/bot/get_bot_qrcode", url.Values{"bot_type": {defaultBotType}}, "", map[string]any{
		"local_token_list": normalizedTokens,
	}, regularTimeout, &response)
	if err != nil {
		return loginChallenge{}, "", err
	}
	response.QRCode = strings.TrimSpace(response.QRCode)
	response.QRCodeURL = strings.TrimSpace(response.QRCodeURL)
	if response.QRCode == "" || len(response.QRCode) > maxQRCodeTokenBytes {
		return loginChallenge{}, "", errors.New("clawbot returned an invalid QR token")
	}
	if err := validatePublicQRCodeURL(response.QRCodeURL); err != nil {
		return loginChallenge{}, "", err
	}
	return loginChallenge{QRCode: response.QRCode, Base: c.loginBaseURL}, response.QRCodeURL, nil
}

func (c *Client) PollLogin(ctx context.Context, challenge loginChallenge, verifyCode string) (qrStatusResponse, loginChallenge, error) {
	if len(challenge.QRCode) == 0 || len(challenge.QRCode) > maxQRCodeTokenBytes {
		return qrStatusResponse{}, challenge, errors.New("invalid clawbot login challenge")
	}
	query := url.Values{"qrcode": {challenge.QRCode}}
	verifyCode = strings.TrimSpace(verifyCode)
	if verifyCode != "" {
		if len(verifyCode) > 12 {
			return qrStatusResponse{}, challenge, errors.New("verification code is too long")
		}
		for _, character := range verifyCode {
			if character < '0' || character > '9' {
				return qrStatusResponse{}, challenge, errors.New("verification code must contain only digits")
			}
		}
		query.Set("verify_code", verifyCode)
	}
	var response qrStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, challenge.Base, "/ilink/bot/get_qrcode_status", query, "", nil, loginPollTimeout, &response); err != nil {
		return qrStatusResponse{}, challenge, err
	}
	if response.Status == "scaned_but_redirect" {
		redirectURL, err := endpointFromRedirectHost(response.RedirectHost, c.allowTestHTTP, c.allowTestEndpointHost)
		if err != nil {
			return qrStatusResponse{}, challenge, err
		}
		challenge.Base = redirectURL
	}
	return response, challenge, nil
}

func (c *Client) GetUpdates(ctx context.Context, endpoint, token, cursor string, timeout time.Duration) (updatesResponse, error) {
	var response updatesResponse
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	if timeout > 40*time.Second {
		timeout = 40 * time.Second
	}
	err := c.doJSON(ctx, http.MethodPost, endpoint, "/ilink/bot/getupdates", nil, token, map[string]any{
		"get_updates_buf": cursor,
		"base_info":       requestBaseInfo(),
	}, timeout, &response)
	if err != nil {
		return updatesResponse{}, err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		apiErr := &APIError{Operation: "getupdates", Ret: response.Ret, ErrCode: response.ErrCode, Message: response.ErrorMessage}
		if response.ErrCode == -14 || response.Ret == -14 {
			return updatesResponse{}, fmt.Errorf("%w: %v", ErrStaleToken, apiErr)
		}
		return updatesResponse{}, apiErr
	}
	return response, nil
}

func (c *Client) SendText(ctx context.Context, endpoint, token string, outgoing outboundText) error {
	if !utf8.ValidString(outgoing.Text) || len([]rune(outgoing.Text)) > maxOutboundText {
		return errors.New("clawbot outbound text is invalid or too long")
	}
	if strings.TrimSpace(outgoing.To) == "" || strings.TrimSpace(outgoing.ClientID) == "" {
		return errors.New("clawbot outbound recipient and client id are required")
	}
	request := map[string]any{
		"msg": message{
			To:           strings.TrimSpace(outgoing.To),
			ClientID:     strings.TrimSpace(outgoing.ClientID),
			MessageType:  2,
			MessageState: 2,
			Items:        []messageItem{{Type: 1, IsCompleted: true, MessageID: strings.TrimSpace(outgoing.ClientID), Text: &textItem{Text: outgoing.Text}}},
			ContextToken: strings.TrimSpace(outgoing.ContextToken),
		},
		"base_info": requestBaseInfo(),
	}
	var response sendResponse
	if err := c.doJSON(ctx, http.MethodPost, endpoint, "/ilink/bot/sendmessage", nil, token, request, regularTimeout, &response); err != nil {
		return err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		apiErr := &APIError{Operation: "sendmessage", Ret: response.Ret, ErrCode: response.ErrCode, Message: response.ErrorMessage}
		if response.ErrCode == -14 || response.Ret == -14 {
			return fmt.Errorf("%w: %v", ErrStaleToken, apiErr)
		}
		if response.ErrCode == -2 || response.Ret == -2 {
			return fmt.Errorf("%w: %v", ErrContextExpired, apiErr)
		}
		return apiErr
	}
	return nil
}

func (c *Client) NotifyStart(ctx context.Context, endpoint, token string) error {
	return c.notify(ctx, endpoint, token, "/ilink/bot/msg/notifystart")
}

func (c *Client) NotifyStop(ctx context.Context, endpoint, token string) error {
	return c.notify(ctx, endpoint, token, "/ilink/bot/msg/notifystop")
}

func (c *Client) notify(ctx context.Context, endpoint, token, path string) error {
	var response sendResponse
	if err := c.doJSON(ctx, http.MethodPost, endpoint, path, nil, token, map[string]any{"base_info": requestBaseInfo()}, 5*time.Second, &response); err != nil {
		return err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		apiErr := &APIError{Operation: strings.TrimPrefix(path, "/ilink/bot/"), Ret: response.Ret, ErrCode: response.ErrCode, Message: response.ErrorMessage}
		if response.ErrCode == -14 || response.Ret == -14 {
			return fmt.Errorf("%w: %v", ErrStaleToken, apiErr)
		}
		return apiErr
	}
	return nil
}

type outboundText struct {
	To           string
	ContextToken string
	Text         string
	ClientID     string
}

func requestBaseInfo() baseInfo {
	return baseInfo{ChannelVersion: ilinkChannelVersion, BotAgent: "ChatAPI/1.0.0"}
}

func (c *Client) doJSON(ctx context.Context, method, rawBase, path string, query url.Values, token string, body any, timeout time.Duration, output any) error {
	base, err := parseEndpointURL(rawBase, c.allowTestHTTP, c.allowTestEndpointHost)
	if err != nil {
		return err
	}
	requestURL := *base
	requestURL.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
	requestURL.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode clawbot request: %w", err)
		}
		if len(encoded) > maxResponseBytes {
			return errors.New("clawbot request is too large")
		}
		reader = bytes.NewReader(encoded)
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, requestURL.String(), reader)
	if err != nil {
		return fmt.Errorf("create clawbot request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	req.Header.Set("iLink-App-Id", ilinkAppID)
	req.Header.Set("iLink-App-ClientVersion", ilinkClientVersion)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("clawbot request failed: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read clawbot response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("clawbot response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Operation: path, Status: resp.StatusCode}
	}
	if output == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode clawbot response: %w", err)
	}
	return nil
}

func randomWechatUIN() string {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	value := binary.BigEndian.Uint32(data[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(value), 10)))
}

func parseEndpointURL(raw string, allowTestHTTP bool, testHost string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("invalid clawbot endpoint")
	}
	if err := validateEndpointURL(u, allowTestHTTP, testHost); err != nil {
		return nil, err
	}
	return u, nil
}

func validateEndpointURL(u *url.URL, allowTestHTTP bool, testHost string) error {
	if u == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid clawbot endpoint")
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if allowTestHTTP && u.Scheme == "http" && hostname == strings.ToLower(strings.TrimSpace(testHost)) {
		return nil
	}
	if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("clawbot endpoint must use HTTPS on the default port")
	}
	if hostname != "weixin.qq.com" && !strings.HasSuffix(hostname, ".weixin.qq.com") {
		return errors.New("clawbot endpoint host is not trusted")
	}
	return nil
}

// pi-lens-ignore: go-bare-error
func endpointFromRedirectHost(host string, allowTestHTTP bool, testHost string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/?#@") {
		return "", errors.New("invalid clawbot redirect host")
	}
	scheme := "https"
	probe := &url.URL{Host: host}
	if allowTestHTTP && strings.EqualFold(strings.TrimSuffix(probe.Hostname(), "."), testHost) {
		scheme = "http"
	}
	u := &url.URL{Scheme: scheme, Host: host}
	if err := validateEndpointURL(u, allowTestHTTP, testHost); err != nil {
		return "", fmt.Errorf("validate clawbot redirect host: %w", err)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func validatePublicQRCodeURL(raw string) error {
	if len(raw) == 0 || len(raw) > maxQRCodeURLBytes || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("clawbot returned an invalid QR URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("clawbot returned an invalid QR URL")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	trusted := host == "weixin.qq.com" || strings.HasSuffix(host, ".weixin.qq.com") || host == "wechat.com" || strings.HasSuffix(host, ".wechat.com")
	if !trusted {
		return errors.New("clawbot returned an untrusted QR URL")
	}
	return nil
}
