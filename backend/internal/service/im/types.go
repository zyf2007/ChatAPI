package im

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrProviderNotReady = errors.New("IM provider is not ready")
	ErrReauthRequired   = errors.New("IM provider requires reauthentication")
)

const ProviderClawBot = "clawbot"

type LoginState string

const (
	LoginWaiting       LoginState = "waiting"
	LoginScanned       LoginState = "scanned"
	LoginVerifyNeeded  LoginState = "verify_required"
	LoginVerifyBlocked LoginState = "verify_blocked"
	LoginExpired       LoginState = "expired"
	LoginAlreadyBound  LoginState = "already_bound"
	LoginConnected     LoginState = "connected"
)

type LoginChallenge struct {
	Provider  string
	Opaque    json.RawMessage
	QRCodeURL string
	ExpiresAt time.Time
}

type LoginPollResult struct {
	State     LoginState
	Message   string
	Challenge LoginChallenge
	Account   *Account
}

type Account struct {
	Provider        string
	OwnerID         string
	ExternalBotID   string
	ExternalOwnerID string
	Endpoint        string
	Credentials     json.RawMessage
	State           json.RawMessage
	ConnectedAt     time.Time
}

type InboundMessage struct {
	ID               string
	Sequence         int64
	From             string
	To               string
	ContextToken     string
	ReadinessVersion string
	Text             string
	Direct           bool
	Complete         bool
}

type OutboundMessage struct {
	To           string
	ContextToken string
	Text         string
	ClientID     string
}

type ProviderCallbacks struct {
	HandleInbound func(context.Context, InboundMessage) error
	Checkpoint    func(context.Context, json.RawMessage) error
	ReportError   func(error)
}

type Provider interface {
	ID() string
	StartLogin(context.Context, *Account) (LoginChallenge, error)
	PollLogin(context.Context, LoginChallenge, string) (LoginPollResult, error)
	Run(context.Context, Account, ProviderCallbacks) error
	Send(context.Context, Account, OutboundMessage) error
	Ready(Account) bool
	// ReadinessVersion changes only when the provider obtains a fresh reply
	// context; cursor-only checkpoints must retain the same opaque value.
	ReadinessVersion(Account) string
}
