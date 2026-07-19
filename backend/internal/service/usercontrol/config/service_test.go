package config_test

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	userconfig "github.com/zyf2007/ChatAPI/internal/service/usercontrol/config"
)

func TestConfigServiceGetAndUpdate(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	svc := userconfig.New(userconfig.Deps{Configs: st, Chat: st})
	original := map[string]any{"theme": "dark", "nested": map[string]any{"x": 1}}

	item, err := svc.UpdateUserConfig(ctx, " user_a ", original)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if item.UserID != "user_a" || item.Key != "settings" {
		t.Fatalf("unexpected config item: %#v", item)
	}
	item.Value["theme"] = "light"
	if original["theme"] != "dark" {
		t.Fatalf("input map should not be mutated: %#v", original)
	}

	got, err := svc.GetUserConfig(ctx, "user_a")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.Value["theme"] != "dark" {
		t.Fatalf("unexpected config value: %#v", got.Value)
	}
}

func TestUpdateUserConfig_EnabledInvalidURLRejected(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	lookup := urlsafety.HostLookup(func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
	})
	svc := userconfig.New(userconfig.Deps{Configs: st, Chat: st, Lookup: lookup})

	_, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": true,
		"ntfy_url":         "http://127.0.0.1:9/private-topic",
	})
	if !errors.Is(err, userconfig.ErrInvalidNtfyConfig) {
		t.Fatalf("expected invalid ntfy config, got %v", err)
	}
	if _, err := svc.GetUserConfig(ctx, "user_a"); err == nil {
		t.Fatal("invalid enabled config must not be persisted")
	}

	_, err = svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": true,
		"ntfy_url":         "",
	})
	if !errors.Is(err, userconfig.ErrInvalidNtfyConfig) {
		t.Fatalf("expected empty enabled url rejection, got %v", err)
	}
}

func TestUpdateUserConfig_EnabledValidURLPersisted(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	lookup := urlsafety.HostLookup(func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host != "ntfy.sh" {
			t.Fatalf("unexpected host %s", host)
		}
		return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
	})
	svc := userconfig.New(userconfig.Deps{Configs: st, Chat: st, Lookup: lookup})

	item, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": true,
		"ntfy_url":         "https://ntfy.sh/alice",
		"theme":            "dark",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if item.Value["ntfy_url"] != "https://ntfy.sh/alice" {
		t.Fatalf("unexpected saved value: %#v", item.Value)
	}
}

func TestUpdateUserConfig_DisabledPolicy(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	lookup := urlsafety.HostLookup(func(ctx context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "ntfy.sh":
			return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
		case "draft.invalid":
			return nil, errors.New("nxdomain")
		default:
			return nil, errors.New("nxdomain")
		}
	})
	svc := userconfig.New(userconfig.Deps{Configs: st, Chat: st, Lookup: lookup})

	// disabled + empty URL: allowed (clear / unset).
	if _, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": false,
		"ntfy_url":         "",
		"theme":            "dark",
	}); err != nil {
		t.Fatalf("disabled empty: %v", err)
	}

	// disabled + public draft URL: allowed.
	if _, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": false,
		"ntfy_url":         "https://ntfy.sh/draft",
	}); err != nil {
		t.Fatalf("disabled public draft: %v", err)
	}

	// disabled + unresolved hostname draft: allowed (not active; DNS may recover later).
	if _, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": false,
		"ntfy_url":         "https://draft.invalid/topic",
	}); err != nil {
		t.Fatalf("disabled unresolved draft: %v", err)
	}

	// disabled + private/literal restricted: rejected — never persist known-unsafe endpoints.
	// This is not an allow-private capability; private destinations stay closed in both modes.
	_, err := svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": false,
		"ntfy_url":         "http://127.0.0.1:9/private-topic",
	})
	if !errors.Is(err, userconfig.ErrInvalidNtfyConfig) {
		t.Fatalf("expected private disabled rejection, got %v", err)
	}

	// disabled + invalid scheme: rejected.
	_, err = svc.UpdateUserConfig(ctx, "user_a", map[string]any{
		"ntfy_url_enabled": false,
		"ntfy_url":         "ftp://example.com/x",
	})
	if !errors.Is(err, userconfig.ErrInvalidNtfyConfig) {
		t.Fatalf("expected invalid scheme rejection, got %v", err)
	}
}

func openConfigStore(t *testing.T) repositorycontract.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	return st
}
