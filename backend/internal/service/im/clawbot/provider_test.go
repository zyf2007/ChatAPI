package clawbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	imsvc "github.com/zyf2007/ChatAPI/internal/service/im"
)

func TestInboundFromMessageEnforcesOwnerDirectFinishedText(t *testing.T) {
	t.Parallel()
	account := imsvc.Account{Provider: imsvc.ProviderClawBot, ExternalBotID: "bot-1", ExternalOwnerID: "owner-1"}
	valid := message{
		MessageID: 42, From: "owner-1", To: "bot-1", MessageType: 1, MessageState: 2,
		ContextToken: "context-1", Items: []messageItem{{Type: 1, IsCompleted: true, Text: &textItem{Text: "answer"}}},
	}
	inbound, ok := inboundFromMessage(account, valid)
	if !ok || inbound.ID != "42" || inbound.Text != "answer" || inbound.ContextToken != "context-1" {
		t.Fatalf("inbound = %#v, ok=%v", inbound, ok)
	}
	withoutTarget := valid
	withoutTarget.To = ""
	if _, ok := inboundFromMessage(account, withoutTarget); !ok {
		t.Fatal("server response without to_user_id should remain valid")
	}

	invalid := []message{
		func() message { value := valid; value.From = "other"; return value }(),
		func() message { value := valid; value.To = "other-bot"; return value }(),
		func() message { value := valid; value.GroupID = "group-1"; return value }(),
		func() message { value := valid; value.MessageType = 2; return value }(),
		func() message { value := valid; value.MessageState = 1; return value }(),
		func() message { value := valid; value.MessageID = 0; return value }(),
	}
	for index, candidate := range invalid {
		if _, ok := inboundFromMessage(account, candidate); ok {
			t.Errorf("invalid[%d] was accepted: %#v", index, candidate)
		}
	}

	media := valid
	media.Items = append(media.Items, messageItem{Type: 2, IsCompleted: true})
	inbound, ok = inboundFromMessage(account, media)
	if !ok || inbound.Text != "" {
		t.Fatalf("unsupported media should reach coordinator without text: %#v, ok=%v", inbound, ok)
	}
}

func TestProviderCheckpointAdvancesReadinessVersionOnlyForInboundContext(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ilink/bot/msg/notifystart", "/ilink/bot/msg/notifystop":
			fmt.Fprint(w, `{"ret":0,"errcode":0}`)
		case "/ilink/bot/getupdates":
			if polls.Add(1) == 1 {
				fmt.Fprint(w, `{"ret":0,"errcode":0,"get_updates_buf":"cursor-only","msgs":[]}`)
				return
			}
			fmt.Fprint(w, `{"ret":0,"errcode":0,"get_updates_buf":"cursor-with-context","msgs":[`+
				`{"message_id":1,"from_user_id":"owner-1","to_user_id":"bot-1","message_type":1,"message_state":2,"context_token":"fresh","item_list":[{"type":1,"text_item":{"text":"hello"}}]}`+
				`]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := NewProvider(newTestClient(t, server))
	credentialsJSON, _ := json.Marshal(credentials{Token: "token"})
	stateJSON, _ := json.Marshal(providerState{Cursor: "cursor-old", ContextToken: "stale", ContextGeneration: 4})
	account := imsvc.Account{
		Provider: imsvc.ProviderClawBot, ExternalBotID: "bot-1", ExternalOwnerID: "owner-1",
		Endpoint: server.URL, Credentials: credentialsJSON, State: stateJSON,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoints []json.RawMessage
	var inboundVersion string
	err := provider.Run(ctx, account, imsvc.ProviderCallbacks{
		HandleInbound: func(_ context.Context, inbound imsvc.InboundMessage) error {
			inboundVersion = inbound.ReadinessVersion
			return nil
		},
		Checkpoint: func(_ context.Context, state json.RawMessage) error {
			checkpoints = append(checkpoints, append(json.RawMessage(nil), state...))
			if len(checkpoints) == 2 {
				cancel()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 2 {
		t.Fatalf("checkpoint count = %d", len(checkpoints))
	}
	if inboundVersion != "5" {
		t.Fatalf("inbound readiness version = %q", inboundVersion)
	}
	account.State = checkpoints[0]
	if version := provider.ReadinessVersion(account); version != "4" {
		t.Fatalf("cursor checkpoint version = %q, state=%s", version, checkpoints[0])
	}
	account.State = checkpoints[1]
	if version := provider.ReadinessVersion(account); version != "5" {
		t.Fatalf("inbound checkpoint version = %q, state=%s", version, checkpoints[1])
	}
}

func TestProviderDoesNotCheckpointBatchCursorOnMessageFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ilink/bot/msg/notifystart", "/ilink/bot/msg/notifystop":
			fmt.Fprint(w, `{"ret":0,"errcode":0}`)
		case "/ilink/bot/getupdates":
			fmt.Fprint(w, `{"ret":0,"errcode":0,"get_updates_buf":"cursor-new","msgs":[`+
				`{"message_id":1,"from_user_id":"owner-1","to_user_id":"bot-1","message_type":1,"message_state":2,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"first"}}]},`+
				`{"message_id":2,"from_user_id":"owner-1","to_user_id":"bot-1","message_type":1,"message_state":2,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"second"}}]}`+
				`]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server)
	provider := NewProvider(client)
	credentialsJSON, _ := json.Marshal(credentials{Token: "token"})
	stateJSON, _ := json.Marshal(providerState{Cursor: "cursor-old"})
	account := imsvc.Account{
		Provider: imsvc.ProviderClawBot, ExternalBotID: "bot-1", ExternalOwnerID: "owner-1",
		Endpoint: server.URL, Credentials: credentialsJSON, State: stateJSON,
	}
	boom := errors.New("second message failed")
	var handled atomic.Int32
	var checkpoints atomic.Int32
	err := provider.Run(context.Background(), account, imsvc.ProviderCallbacks{
		HandleInbound: func(context.Context, imsvc.InboundMessage) error {
			if handled.Add(1) == 2 {
				return boom
			}
			return nil
		},
		Checkpoint: func(context.Context, json.RawMessage) error {
			checkpoints.Add(1)
			return nil
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if handled.Load() != 2 || checkpoints.Load() != 0 {
		t.Fatalf("handled=%d checkpoints=%d", handled.Load(), checkpoints.Load())
	}
}

func TestProviderReadyAndProcessedWindow(t *testing.T) {
	t.Parallel()
	provider := NewProvider(nil)
	credentials, _ := json.Marshal(struct {
		Token string `json:"token"`
	}{Token: "token"})
	state, _ := json.Marshal(providerState{ContextToken: "context"})
	account := imsvc.Account{Provider: imsvc.ProviderClawBot, Credentials: credentials, State: state}
	if !provider.Ready(account) {
		t.Fatal("account with context token should be ready")
	}
	account.State = json.RawMessage(`{}`)
	if provider.Ready(account) {
		t.Fatal("account without context token should not be ready")
	}

	items := make([]string, 0, 140)
	for index := range 140 {
		items = appendProcessed(items, string(rune(index+1)))
	}
	if len(items) != 128 {
		t.Fatalf("processed window length = %d", len(items))
	}
	if !hasProcessed(items, string(rune(140))) || hasProcessed(items, string(rune(1))) {
		t.Fatalf("processed window contents are incorrect")
	}
}
