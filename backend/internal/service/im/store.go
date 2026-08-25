package im

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/platform/secretbox"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

const accountConfigKey = "im.account.clawbot"

var ErrConnectionNotFound = errors.New("IM connection not found")

type AccountStore interface {
	ListUsers(context.Context) ([]common.User, error)
	GetUser(context.Context, string) (common.User, error)
	GetUserConfig(context.Context, string, string) (common.UserConfig, error)
	SetUserConfig(context.Context, common.SetUserConfigInput) (common.UserConfig, error)
	DeleteUserConfig(context.Context, string, string) error
}

type storedAccount struct {
	Version         int       `json:"version"`
	Provider        string    `json:"provider"`
	ExternalBotID   string    `json:"external_bot_id"`
	ExternalOwnerID string    `json:"external_owner_id"`
	Endpoint        string    `json:"endpoint"`
	Secret          string    `json:"secret_ciphertext"`
	ConnectedAt     time.Time `json:"connected_at"`
}

type accountSecret struct {
	Credentials json.RawMessage `json:"credentials"`
	State       json.RawMessage `json:"state"`
}

func saveAccount(ctx context.Context, store AccountStore, masterKey string, account Account) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	secretJSON, err := json.Marshal(accountSecret{Credentials: account.Credentials, State: account.State})
	if err != nil {
		return fmt.Errorf("encode IM account secret: %w", err)
	}
	if len(secretJSON) > 1<<20 {
		return errors.New("IM account secret exceeds safety limit")
	}
	sealed, err := secretbox.Seal(string(secretJSON), masterKey)
	if err != nil {
		return fmt.Errorf("seal IM account secret: %w", err)
	}
	record := storedAccount{
		Version: 1, Provider: account.Provider, ExternalBotID: account.ExternalBotID,
		ExternalOwnerID: account.ExternalOwnerID, Endpoint: account.Endpoint,
		Secret: sealed, ConnectedAt: account.ConnectedAt.UTC(),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode IM account: %w", err)
	}
	value := make(map[string]any)
	if err := json.Unmarshal(encoded, &value); err != nil {
		return fmt.Errorf("encode IM account value: %w", err)
	}
	_, err = store.SetUserConfig(ctx, common.SetUserConfigInput{UserID: account.OwnerID, Key: accountConfigKey, Value: value})
	if err != nil {
		return fmt.Errorf("save IM account: %w", err)
	}
	return nil
}

func loadAccount(ctx context.Context, store AccountStore, masterKey, ownerID string) (Account, error) {
	record, err := store.GetUserConfig(ctx, strings.TrimSpace(ownerID), accountConfigKey)
	if err != nil {
		return Account{}, err
	}
	encoded, err := json.Marshal(record.Value)
	if err != nil {
		return Account{}, fmt.Errorf("decode IM account value: %w", err)
	}
	var stored storedAccount
	if err := json.Unmarshal(encoded, &stored); err != nil || stored.Version != 1 || strings.TrimSpace(stored.Secret) == "" || len(stored.Secret) > 2<<20 {
		return Account{}, errors.New("invalid stored IM account")
	}
	plaintext, err := secretbox.Open(stored.Secret, masterKey)
	if err != nil {
		return Account{}, fmt.Errorf("open IM account secret: %w", err)
	}
	var secret accountSecret
	if err := json.Unmarshal([]byte(plaintext), &secret); err != nil {
		return Account{}, errors.New("invalid stored IM account secret")
	}
	account := Account{
		Provider: strings.TrimSpace(stored.Provider), OwnerID: strings.TrimSpace(ownerID),
		ExternalBotID: strings.TrimSpace(stored.ExternalBotID), ExternalOwnerID: strings.TrimSpace(stored.ExternalOwnerID),
		Endpoint: strings.TrimSpace(stored.Endpoint), Credentials: secret.Credentials, State: secret.State,
		ConnectedAt: stored.ConnectedAt.UTC(),
	}
	if err := validateAccount(account); err != nil {
		return Account{}, errors.New("stored IM account is incomplete or exceeds safety limits")
	}
	return account, nil
}

func validateAccount(account Account) error {
	if strings.TrimSpace(account.Provider) == "" || strings.TrimSpace(account.OwnerID) == "" || strings.TrimSpace(account.ExternalBotID) == "" || strings.TrimSpace(account.ExternalOwnerID) == "" || strings.TrimSpace(account.Endpoint) == "" || len(account.Credentials) == 0 {
		return errors.New("IM account is incomplete")
	}
	if len(account.Provider) > 64 || len(account.OwnerID) > 256 || len(account.ExternalBotID) > 512 || len(account.ExternalOwnerID) > 512 || len(account.Endpoint) > 2048 || len(account.Credentials)+len(account.State) > 1<<20 {
		return errors.New("IM account exceeds safety limits")
	}
	return nil
}
