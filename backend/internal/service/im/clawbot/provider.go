package clawbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	imsvc "github.com/zyf2007/ChatAPI/internal/service/im"
)

var ErrNotReady = fmt.Errorf("%w: send /bind from WeChat first", imsvc.ErrProviderNotReady)

type Provider struct {
	Client *Client
	Now    func() time.Time
}

type credentials struct {
	Token string `json:"token"`
}

type providerState struct {
	Cursor             string   `json:"cursor,omitempty"`
	ContextToken       string   `json:"context_token,omitempty"`
	ContextGeneration  uint64   `json:"context_generation,omitempty"`
	ProcessedMessageID []string `json:"processed_message_ids,omitempty"`
}

func NewProvider(client *Client) *Provider {
	if client == nil {
		client = NewClient(nil)
	}
	return &Provider{Client: client, Now: time.Now}
}

func (p *Provider) ID() string { return imsvc.ProviderClawBot }

func (p *Provider) Ready(account imsvc.Account) bool {
	_, state, err := decodeAccount(account)
	return err == nil && strings.TrimSpace(state.ContextToken) != ""
}

func (p *Provider) ReadinessVersion(account imsvc.Account) string {
	_, state, err := decodeAccount(account)
	if err != nil || state.ContextGeneration == 0 {
		return ""
	}
	return strconv.FormatUint(state.ContextGeneration, 10)
}

func (p *Provider) StartLogin(ctx context.Context, existing *imsvc.Account) (imsvc.LoginChallenge, error) {
	var localTokens []string
	if existing != nil && existing.Provider == p.ID() {
		if creds, _, err := decodeAccount(*existing); err == nil {
			localTokens = []string{creds.Token}
		}
	}
	challenge, qrCodeURL, err := p.Client.StartLogin(ctx, localTokens)
	if err != nil {
		return imsvc.LoginChallenge{}, err
	}
	opaque, err := json.Marshal(challenge)
	if err != nil {
		return imsvc.LoginChallenge{}, fmt.Errorf("encode clawbot login challenge: %w", err)
	}
	return imsvc.LoginChallenge{
		Provider:  p.ID(),
		Opaque:    opaque,
		QRCodeURL: qrCodeURL,
		ExpiresAt: p.now().Add(5 * time.Minute),
	}, nil
}

func (p *Provider) PollLogin(ctx context.Context, challenge imsvc.LoginChallenge, verifyCode string) (imsvc.LoginPollResult, error) {
	var internal loginChallenge
	if challenge.Provider != p.ID() || json.Unmarshal(challenge.Opaque, &internal) != nil {
		return imsvc.LoginPollResult{}, errors.New("invalid clawbot login challenge")
	}
	response, updated, err := p.Client.PollLogin(ctx, internal, verifyCode)
	if err != nil {
		return imsvc.LoginPollResult{}, err
	}
	updatedOpaque, err := json.Marshal(updated)
	if err != nil {
		return imsvc.LoginPollResult{}, fmt.Errorf("encode clawbot login challenge: %w", err)
	}
	challenge.Opaque = updatedOpaque
	result := imsvc.LoginPollResult{Challenge: challenge}
	switch response.Status {
	case "wait", "scaned_but_redirect":
		result.State = imsvc.LoginWaiting
		result.Message = "等待微信扫码确认"
	case "scaned":
		result.State = imsvc.LoginScanned
		result.Message = "已扫码，正在确认"
	case "need_verifycode":
		result.State = imsvc.LoginVerifyNeeded
		result.Message = "请输入手机微信显示的数字"
	case "verify_code_blocked":
		result.State = imsvc.LoginVerifyBlocked
		result.Message = "验证码尝试过多，请重新生成二维码"
	case "expired":
		result.State = imsvc.LoginExpired
		result.Message = "二维码已过期，请重新生成"
	case "binded_redirect":
		result.State = imsvc.LoginAlreadyBound
		result.Message = "该微信 ClawBot 已绑定其他客户端"
	case "confirmed":
		account, err := p.accountFromLogin(response)
		if err != nil {
			return imsvc.LoginPollResult{}, err
		}
		result.State = imsvc.LoginConnected
		result.Message = "微信 ClawBot 已连接"
		result.Account = &account
	default:
		return imsvc.LoginPollResult{}, fmt.Errorf("unknown clawbot QR status %q", response.Status)
	}
	return result, nil
}

func (p *Provider) Run(ctx context.Context, account imsvc.Account, callbacks imsvc.ProviderCallbacks) error {
	creds, state, err := decodeAccount(account)
	if err != nil {
		return err
	}
	if err := p.Client.NotifyStart(ctx, account.Endpoint, creds.Token); err != nil {
		if errors.Is(err, ErrStaleToken) {
			return fmt.Errorf("%w: %v", imsvc.ErrReauthRequired, err)
		}
		report(callbacks, err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Client.NotifyStop(stopCtx, account.Endpoint, creds.Token); err != nil && !errors.Is(err, context.Canceled) {
			report(callbacks, err)
		}
	}()

	longPollTimeout := 35 * time.Second
	backoff := time.Second
	for ctx.Err() == nil {
		response, err := p.Client.GetUpdates(ctx, account.Endpoint, creds.Token, state.Cursor, longPollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrStaleToken) {
				return fmt.Errorf("%w: %v", imsvc.ErrReauthRequired, err)
			}
			report(callbacks, err)
			if !waitContext(ctx, backoff) {
				return nil
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			continue
		}
		backoff = time.Second
		if response.LongPollingTimeoutMsec > 0 {
			longPollTimeout = time.Duration(response.LongPollingTimeoutMsec) * time.Millisecond
		}

		stateChanged := response.Cursor != "" && response.Cursor != state.Cursor
		for _, raw := range response.Messages {
			inbound, ok := inboundFromMessage(account, raw)
			if !ok || hasProcessed(state.ProcessedMessageID, inbound.ID) {
				continue
			}
			if token := strings.TrimSpace(inbound.ContextToken); token != "" {
				state.ContextToken = token
				state.ContextGeneration++
				inbound.ReadinessVersion = strconv.FormatUint(state.ContextGeneration, 10)
			}
			if callbacks.HandleInbound != nil {
				if err := callbacks.HandleInbound(ctx, inbound); err != nil {
					report(callbacks, err)
					return err
				}
			}
			state.ProcessedMessageID = appendProcessed(state.ProcessedMessageID, inbound.ID)
			stateChanged = true
		}
		if response.Cursor != "" {
			state.Cursor = response.Cursor
		}
		if stateChanged && callbacks.Checkpoint != nil {
			encoded, err := json.Marshal(state)
			if err != nil {
				return fmt.Errorf("encode clawbot checkpoint: %w", err)
			}
			if err := callbacks.Checkpoint(ctx, encoded); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, account imsvc.Account, outgoing imsvc.OutboundMessage) error {
	creds, state, err := decodeAccount(account)
	if err != nil {
		return err
	}
	to := strings.TrimSpace(outgoing.To)
	if to == "" {
		to = strings.TrimSpace(account.ExternalOwnerID)
	}
	contextToken := strings.TrimSpace(outgoing.ContextToken)
	if contextToken == "" {
		contextToken = strings.TrimSpace(state.ContextToken)
	}
	if contextToken == "" {
		return ErrNotReady
	}
	clientID := strings.TrimSpace(outgoing.ClientID)
	if clientID == "" {
		clientID = uuid.NewString()
	}
	err = p.Client.SendText(ctx, account.Endpoint, creds.Token, outboundText{
		To: to, ContextToken: contextToken, Text: outgoing.Text, ClientID: clientID,
	})
	if errors.Is(err, ErrStaleToken) {
		return fmt.Errorf("%w: %v", imsvc.ErrReauthRequired, err)
	}
	if errors.Is(err, ErrContextExpired) {
		return fmt.Errorf("%w: %v", imsvc.ErrProviderNotReady, err)
	}
	return err
}

func (p *Provider) accountFromLogin(response qrStatusResponse) (imsvc.Account, error) {
	response.BotToken = strings.TrimSpace(response.BotToken)
	response.BotID = strings.TrimSpace(response.BotID)
	response.OwnerID = strings.TrimSpace(response.OwnerID)
	if response.BotToken == "" || response.BotID == "" || response.OwnerID == "" {
		return imsvc.Account{}, errors.New("clawbot login confirmation omitted required credentials")
	}
	endpoint := strings.TrimSpace(response.BaseURL)
	if endpoint == "" {
		endpoint = defaultBaseURL
	}
	validated, err := parseEndpointURL(endpoint, p.Client.allowTestHTTP, p.Client.allowTestEndpointHost)
	if err != nil {
		return imsvc.Account{}, err
	}
	credentialJSON, err := json.Marshal(credentials{Token: response.BotToken})
	if err != nil {
		return imsvc.Account{}, err
	}
	stateJSON, err := json.Marshal(providerState{})
	if err != nil {
		return imsvc.Account{}, err
	}
	return imsvc.Account{
		Provider: p.ID(), ExternalBotID: response.BotID, ExternalOwnerID: response.OwnerID,
		Endpoint: strings.TrimRight(validated.String(), "/"), Credentials: credentialJSON,
		State: stateJSON, ConnectedAt: p.now(),
	}, nil
}

func decodeAccount(account imsvc.Account) (credentials, providerState, error) {
	var creds credentials
	var state providerState
	if account.Provider != imsvc.ProviderClawBot || json.Unmarshal(account.Credentials, &creds) != nil || strings.TrimSpace(creds.Token) == "" || len(creds.Token) > 64*1024 {
		return credentials{}, providerState{}, errors.New("invalid clawbot credentials")
	}
	if len(account.State) > 0 {
		if err := json.Unmarshal(account.State, &state); err != nil {
			return credentials{}, providerState{}, errors.New("invalid clawbot state")
		}
	}
	if len(state.Cursor) > 512*1024 || len(state.ContextToken) > 64*1024 || len(state.ProcessedMessageID) > 256 {
		return credentials{}, providerState{}, errors.New("clawbot state exceeds safety limits")
	}
	return creds, state, nil
}

func inboundFromMessage(account imsvc.Account, raw message) (imsvc.InboundMessage, bool) {
	if raw.MessageType != 1 || raw.MessageState != 2 || strings.TrimSpace(raw.GroupID) != "" {
		return imsvc.InboundMessage{}, false
	}
	if strings.TrimSpace(raw.From) != strings.TrimSpace(account.ExternalOwnerID) {
		return imsvc.InboundMessage{}, false
	}
	if target := strings.TrimSpace(raw.To); target != "" && target != strings.TrimSpace(account.ExternalBotID) {
		return imsvc.InboundMessage{}, false
	}
	id := ""
	if raw.MessageID != 0 {
		id = strconv.FormatInt(raw.MessageID, 10)
	} else if strings.TrimSpace(raw.ClientID) != "" {
		id = strings.TrimSpace(raw.ClientID)
	}
	if id == "" {
		return imsvc.InboundMessage{}, false
	}
	text := ""
	textItems := 0
	unsupported := false
	for _, item := range raw.Items {
		if item.Type == 1 && item.Text != nil {
			textItems++
			text = item.Text.Text
		} else if item.Type != 0 {
			unsupported = true
		}
	}
	if textItems != 1 || unsupported {
		text = ""
	}
	return imsvc.InboundMessage{
		ID: id, Sequence: raw.Sequence, From: strings.TrimSpace(raw.From), To: strings.TrimSpace(raw.To),
		ContextToken: strings.TrimSpace(raw.ContextToken), Text: text, Direct: true, Complete: true,
	}, true
}

func hasProcessed(items []string, id string) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}

func appendProcessed(items []string, id string) []string {
	items = append(items, id)
	if len(items) > 128 {
		items = append([]string(nil), items[len(items)-128:]...)
	}
	return items
}

func report(callbacks imsvc.ProviderCallbacks, err error) {
	if err != nil && callbacks.ReportError != nil {
		callbacks.ReportError(err)
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}
