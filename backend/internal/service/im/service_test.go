package im

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

const testMasterKey = "test-master-key-for-im-account-encryption"

func TestSplitCommandAndPendingSelection(t *testing.T) {
	command, argument := splitCommand("/abort\noperator requested")
	if command != "/abort" || argument != "operator requested" {
		t.Fatalf("command=%q argument=%q", command, argument)
	}
	pending := &fakePending{}
	pending.set(
		&turnsvc.PendingTurn{OwnerID: "owner-1", ConversationID: "aaaa-1111", RequestID: "req-a", Model: "a", CreatedAt: time.Unix(1, 0)},
		&turnsvc.PendingTurn{OwnerID: "owner-1", ConversationID: "bbbb-2222", RequestID: "req-b", Model: "b", CreatedAt: time.Unix(2, 0)},
	)
	service := NewService(newFakeStore(), pending, nil, testMasterKey, zap.NewNop())
	current, err := service.currentPending("owner-1")
	if err != nil || current.ConversationID != "bbbb-2222" {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	selected, err := service.selectPending("owner-1", "aaaa")
	if err != nil || selected.ConversationID != "aaaa-1111" {
		t.Fatalf("selected=%#v err=%v", selected, err)
	}
}

func TestDisconnectWaitsForOldCheckpointBeforeDeletingConfig(t *testing.T) {
	store := newFakeStore()
	store.setConfigStarted = make(chan struct{})
	store.setConfigBlock = make(chan struct{})
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	provider := newFakeProvider()
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":false}`), ConnectedAt: time.Now().UTC(),
	}
	done := make(chan struct{})
	close(done)
	runtime := &accountRuntime{ownerID: "owner-1", generation: 1, cancel: func() {}, done: done}
	service.accounts["owner-1"] = account
	service.runtimes["owner-1"] = runtime
	service.accountGen["owner-1"] = 1
	checkpointDone := make(chan error, 1)
	go func() {
		runtime.barrier.Lock()
		defer runtime.barrier.Unlock()
		checkpointDone <- service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true}`))
	}()
	<-store.setConfigStarted
	disconnectDone := make(chan error, 1)
	go func() { disconnectDone <- service.Disconnect(context.Background(), "owner-1") }()
	waitFor(t, time.Second, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return service.runtimes["owner-1"] == nil
	})
	select {
	case err := <-disconnectDone:
		t.Fatalf("disconnect returned before checkpoint save completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if store.wasDeleteCalled() {
		t.Fatal("disconnect deleted config while old checkpoint could still write")
	}
	close(store.setConfigBlock)
	if err := <-checkpointDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint error = %v", err)
	}
	if err := <-disconnectDone; err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUserConfig(context.Background(), "owner-1", accountConfigKey); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("checkpoint resurrected deleted config: %v", err)
	}
}

func TestCheckpointFailureKeepsPreviousInMemoryState(t *testing.T) {
	store := newFakeStore()
	store.setConfigErr = errors.New("write failed")
	provider := newFakeProvider()
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":false}`), ConnectedAt: time.Now().UTC(),
	}
	runtime := &accountRuntime{ownerID: "owner-1", generation: 1, done: make(chan struct{})}
	service.accounts["owner-1"] = account
	service.runtimes["owner-1"] = runtime
	service.accountGen["owner-1"] = 1
	if err := service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true}`)); err == nil {
		t.Fatal("checkpoint should fail")
	}
	if string(service.accounts["owner-1"].State) != `{"ready":false}` {
		t.Fatalf("state advanced after persistence failure: %s", service.accounts["owner-1"].State)
	}
}

func TestCheckpointRequeuesWaitingOnlyOnFreshContextTransition(t *testing.T) {
	store := newFakeStore()
	provider := newFakeProvider()
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true,"context":"stale","context_generation":1}`), ConnectedAt: time.Now().UTC(),
	}
	runtime := &accountRuntime{ownerID: "owner-1", generation: 1, done: make(chan struct{})}
	service.accounts["owner-1"] = account
	service.runtimes["owner-1"] = runtime
	service.statuses["owner-1"] = &runtimeStatus{workerState: "running", contextInvalid: true, invalidReadinessVersion: "1"}
	service.accountGen["owner-1"] = 1
	service.latestWaiting["owner-1"] = chatevents.WaitingTurn{OwnerID: "owner-1", ConversationID: "conv", RequestID: "req"}
	if service.statusLocked("owner-1").Ready {
		t.Fatal("invalidated context should not report ready")
	}
	if err := service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true,"context":"stale","context_generation":1,"cursor":"next"}`)); err != nil {
		t.Fatal(err)
	}
	if service.statusLocked("owner-1").Ready {
		t.Fatal("cursor-only checkpoint restored an invalid context")
	}
	if _, ok := service.notification["owner-1"]; ok {
		t.Fatal("cursor-only checkpoint requeued waiting notification")
	}
	if err := service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true,"context":"fresh","context_generation":2,"cursor":"next"}`)); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.notification["owner-1"]; !ok {
		t.Fatal("fresh context transition did not requeue waiting notification")
	}
	if !service.statusLocked("owner-1").Ready {
		t.Fatal("fresh context checkpoint did not restore ready status")
	}
	delete(service.notification, "owner-1")
	if err := service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true,"context":"fresh","context_generation":2,"cursor":"later"}`)); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.notification["owner-1"]; ok {
		t.Fatal("ready-to-ready checkpoint requeued waiting notification")
	}
}

func TestQueuedNotificationDoesNotChangeVisibleSelection(t *testing.T) {
	service := NewService(newFakeStore(), &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop())
	service.selected["owner-1"] = "visible-conversation"
	service.HandleChatEvent(context.Background(), chatevents.Event{Type: chatevents.TypeTurnWaiting, WaitingTurn: &chatevents.WaitingTurn{
		OwnerID: "owner-1", ConversationID: "queued-conversation", RequestID: "queued-request",
	}})
	if service.selected["owner-1"] != "visible-conversation" {
		t.Fatalf("queued notification changed selected request: %q", service.selected["owner-1"])
	}
}

func TestFailedNotificationMarksItsContextBeforeFreshCheckpoint(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	pending := &fakePending{}
	pending.set(&turnsvc.PendingTurn{OwnerID: "owner-1", ConversationID: "conv", RequestID: "req", CreatedAt: time.Now()})
	provider := newFakeProvider()
	provider.sendStarted = make(chan struct{})
	provider.sendBlock = make(chan struct{})
	provider.sendErr = ErrProviderNotReady
	service := NewService(store, pending, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true,"context_generation":4}`), ConnectedAt: time.Now().UTC(),
	}
	runtime := &accountRuntime{ownerID: "owner-1", generation: 1, done: make(chan struct{})}
	service.accounts["owner-1"] = account
	service.runtimes["owner-1"] = runtime
	service.statuses["owner-1"] = &runtimeStatus{workerState: "running"}
	service.accountGen["owner-1"] = 1
	notificationDone := make(chan struct{})
	go func() {
		service.sendWaitingNotification(context.Background(), "owner-1", chatevents.WaitingTurn{
			OwnerID: "owner-1", ConversationID: "conv", RequestID: "req", Model: "gpt-test", LastUserText: "question",
		})
		close(notificationDone)
	}()
	<-provider.sendStarted
	checkpointReady := make(chan struct{})
	checkpointDone := make(chan error, 1)
	go func() {
		close(checkpointReady)
		runtime.barrier.Lock()
		defer runtime.barrier.Unlock()
		checkpointDone <- service.checkpoint(context.Background(), "owner-1", 1, runtime, json.RawMessage(`{"ready":true,"context_generation":5}`))
	}()
	<-checkpointReady
	time.Sleep(10 * time.Millisecond)
	close(provider.sendBlock)
	<-notificationDone
	if err := <-checkpointDone; err != nil {
		t.Fatal(err)
	}
	if !service.statusLocked("owner-1").Ready {
		t.Fatalf("fresh checkpoint was invalidated by older send failure: %+v", service.statusLocked("owner-1"))
	}
}

func TestSuccessfulNotificationSelectsItsPendingRequest(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	pending := &fakePending{}
	pending.set(
		&turnsvc.PendingTurn{OwnerID: "owner-1", ConversationID: "old-conversation", RequestID: "old-request", CreatedAt: time.Unix(1, 0)},
		&turnsvc.PendingTurn{OwnerID: "owner-1", ConversationID: "new-conversation", RequestID: "new-request", CreatedAt: time.Unix(2, 0)},
	)
	provider := newFakeProvider()
	service := NewService(store, pending, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true}`), ConnectedAt: time.Now().UTC(),
	}
	runtime := &accountRuntime{ownerID: "owner-1", generation: 1, done: make(chan struct{})}
	service.accounts["owner-1"] = account
	service.runtimes["owner-1"] = runtime
	service.statuses["owner-1"] = &runtimeStatus{workerState: "running"}
	service.accountGen["owner-1"] = 1
	service.selected["owner-1"] = "old-conversation"
	service.sendWaitingNotification(context.Background(), "owner-1", chatevents.WaitingTurn{
		OwnerID: "owner-1", ConversationID: "new-conversation", RequestID: "new-request", Model: "gpt-test", LastUserText: "new question",
	})
	if service.selected["owner-1"] != "new-conversation" {
		t.Fatalf("selected = %q", service.selected["owner-1"])
	}
}

func TestServiceConnectNotifyReplyAndDisconnect(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	pending := &fakePending{}
	controller := &fakeController{pending: pending, commands: make(chan controlsvc.Command, 8)}
	provider := newFakeProvider()
	service := NewService(store, pending, controller, testMasterKey, zap.NewNop(), provider)

	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- service.Run(runCtx) }()
	waitFor(t, time.Second, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return service.running
	})

	login, err := service.BeginLogin(context.Background(), "owner-1", ProviderClawBot)
	if err != nil {
		t.Fatal(err)
	}
	connected, err := service.PollLogin(context.Background(), "owner-1", login.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if connected.State != LoginConnected || connected.Status == nil || !connected.Status.Ready {
		t.Fatalf("connected = %#v", connected)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}

	stored := store.configValue("owner-1", accountConfigKey)
	encoded, _ := json.Marshal(stored)
	if strings.Contains(string(encoded), "plain-token") || strings.Contains(string(encoded), "plain-context") {
		t.Fatalf("plaintext secret persisted: %s", encoded)
	}

	turn := &turnsvc.PendingTurn{
		OwnerID: "owner-1", ConversationID: "conversation-1234", RequestID: "request-1234",
		ResponseID: "response-1234", Model: "gpt-test", CreatedAt: time.Now().UTC(),
	}
	pending.set(turn)
	service.HandleChatEvent(context.Background(), chatevents.Event{Type: chatevents.TypeTurnWaiting, WaitingTurn: &chatevents.WaitingTurn{
		OwnerID: turn.OwnerID, ConversationID: turn.ConversationID, RequestID: turn.RequestID,
		ResponseID: turn.ResponseID, Model: turn.Model, LastUserText: "please answer",
	}})
	notification := provider.nextSent(t)
	if !strings.Contains(notification.Text, "ChatAPI 新请求") || notification.ClientID == "" {
		t.Fatalf("notification = %#v", notification)
	}

	provider.inbound <- InboundMessage{ID: "message-1", From: "wechat-owner", ContextToken: "context-new", Text: "final answer", Direct: true, Complete: true}
	command := controller.nextCommand(t)
	if command.OwnerID != "owner-1" || command.RequestID != turn.RequestID || command.Action.Kind != turnsvc.TurnControlStreamComplete || command.Action.OutputText != "final answer" {
		t.Fatalf("command = %#v", command)
	}
	if got, ok := actor.FromContext(controller.lastContext()); !ok || got.UserID != "owner-1" || got.Source != "im" {
		t.Fatalf("actor = %#v, ok=%v", got, ok)
	}
	ack := provider.nextSent(t)
	if !strings.Contains(ack.Text, "已结束请求") || ack.ContextToken != "context-new" {
		t.Fatalf("ack = %#v", ack)
	}

	if err := service.Disconnect(context.Background(), "owner-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUserConfig(context.Background(), "owner-1", accountConfigKey); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("config survived disconnect: %v", err)
	}
	status, err := service.GetStatus(context.Background(), "owner-1")
	if err != nil || status.Connected {
		t.Fatalf("status = %#v, err=%v", status, err)
	}

	cancelRun()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not stop")
	}
}

func TestServiceRestoresEncryptedAccount(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true,"context":"plain-context"}`), ConnectedAt: time.Now().UTC(),
	}
	if err := saveAccount(context.Background(), store, testMasterKey, account); err != nil {
		t.Fatal(err)
	}
	provider := newFakeProvider()
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("restored provider did not start")
	}
	status, err := service.GetStatus(context.Background(), "owner-1")
	if err != nil || !status.Connected || !status.Ready {
		t.Fatalf("status = %#v, err=%v", status, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRestoreCannotResurrectAfterConcurrentDisconnect(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	account := Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true}`), ConnectedAt: time.Now().UTC(),
	}
	if err := saveAccount(context.Background(), store, testMasterKey, account); err != nil {
		t.Fatal(err)
	}
	store.getConfigStarted = make(chan struct{})
	store.getConfigBlock = make(chan struct{})
	provider := newFakeProvider()
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	<-store.getConfigStarted
	disconnected := make(chan error, 1)
	go func() { disconnected <- service.Disconnect(context.Background(), "owner-1") }()
	close(store.getConfigBlock)
	if err := <-disconnected; err != nil {
		t.Fatal(err)
	}
	status, err := service.GetStatus(context.Background(), "owner-1")
	if err != nil || status.Connected {
		t.Fatalf("status = %#v, err=%v", status, err)
	}
	if _, err := store.GetUserConfig(context.Background(), "owner-1", accountConfigKey); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("config survived restore/disconnect race: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAlreadyBoundUsesExistingOwnerConnection(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	provider := newFakeProvider()
	provider.pollResult = &LoginPollResult{State: LoginAlreadyBound, Message: "already connected"}
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)
	service.accounts["owner-1"] = Account{
		Provider: ProviderClawBot, OwnerID: "owner-1", ExternalBotID: "bot", ExternalOwnerID: "wechat-owner",
		Endpoint: "https://ilinkai.weixin.qq.com", Credentials: json.RawMessage(`{"token":"plain-token"}`),
		State: json.RawMessage(`{"ready":true}`), ConnectedAt: time.Now().UTC(),
	}
	service.statuses["owner-1"] = &runtimeStatus{workerState: "running"}
	login, err := service.BeginLogin(context.Background(), "owner-1", ProviderClawBot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PollLogin(context.Background(), "owner-1", login.SessionID, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != LoginConnected || result.Status == nil || !result.Status.Connected || !provider.startExisting {
		t.Fatalf("result = %#v, startExisting=%v", result, provider.startExisting)
	}
}

func TestServiceRejectsConcurrentOrInvalidatedLoginPoll(t *testing.T) {
	store := newFakeStore()
	store.users["owner-1"] = common.User{ID: "owner-1", IsActive: true}
	provider := newFakeProvider()
	provider.pollBlock = make(chan struct{})
	provider.pollStarted = make(chan struct{})
	service := NewService(store, &fakePending{}, &fakeController{commands: make(chan controlsvc.Command, 8)}, testMasterKey, zap.NewNop(), provider)

	login, err := service.BeginLogin(context.Background(), "owner-1", ProviderClawBot)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.PollLogin(context.Background(), "owner-1", login.SessionID, "")
		firstDone <- err
	}()
	<-provider.pollStarted
	if _, err := service.PollLogin(context.Background(), "owner-1", login.SessionID, ""); !errors.Is(err, ErrLoginBusy) {
		t.Fatalf("second poll error = %v", err)
	}
	if err := service.Disconnect(context.Background(), "owner-1"); err != nil {
		t.Fatal(err)
	}
	close(provider.pollBlock)
	if err := <-firstDone; !errors.Is(err, ErrLoginNotFound) {
		t.Fatalf("invalidated poll error = %v", err)
	}
	if _, err := store.GetUserConfig(context.Background(), "owner-1", accountConfigKey); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("invalidated login persisted an account: %v", err)
	}
}

type fakeStore struct {
	mu               sync.Mutex
	users            map[string]common.User
	configs          map[string]common.UserConfig
	getConfigStarted chan struct{}
	getConfigBlock   chan struct{}
	getConfigOnce    sync.Once
	setConfigErr     error
	setConfigStarted chan struct{}
	setConfigBlock   chan struct{}
	setConfigOnce    sync.Once
	deleteCalled     bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: make(map[string]common.User), configs: make(map[string]common.UserConfig)}
}

func (s *fakeStore) ListUsers(context.Context) ([]common.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]common.User, 0, len(s.users))
	for _, user := range s.users {
		items = append(items, user)
	}
	return items, nil
}

func (s *fakeStore) GetUser(_ context.Context, id string) (common.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return common.User{}, common.ErrNotFound
	}
	return user, nil
}

func (s *fakeStore) GetUserConfig(_ context.Context, userID, key string) (common.UserConfig, error) {
	if s.getConfigStarted != nil {
		s.getConfigOnce.Do(func() { close(s.getConfigStarted) })
	}
	if s.getConfigBlock != nil {
		<-s.getConfigBlock
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.configs[userID+"\x00"+key]
	if !ok {
		return common.UserConfig{}, common.ErrNotFound
	}
	return value, nil
}

func (s *fakeStore) SetUserConfig(_ context.Context, input common.SetUserConfigInput) (common.UserConfig, error) {
	if s.setConfigStarted != nil {
		s.setConfigOnce.Do(func() { close(s.setConfigStarted) })
	}
	if s.setConfigBlock != nil {
		<-s.setConfigBlock
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setConfigErr != nil {
		return common.UserConfig{}, s.setConfigErr
	}
	value := common.UserConfig{UserID: input.UserID, Key: input.Key, Value: input.Value, UpdatedAt: time.Now().UTC()}
	s.configs[input.UserID+"\x00"+input.Key] = value
	return value, nil
}

func (s *fakeStore) DeleteUserConfig(_ context.Context, userID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalled = true
	mapKey := userID + "\x00" + key
	if _, ok := s.configs[mapKey]; !ok {
		return common.ErrNotFound
	}
	delete(s.configs, mapKey)
	return nil
}

func (s *fakeStore) wasDeleteCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteCalled
}

func (s *fakeStore) configValue(userID, key string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configs[userID+"\x00"+key].Value
}

type fakePending struct {
	mu    sync.Mutex
	items []*turnsvc.PendingTurn
}

func (p *fakePending) ListByOwnerID(ownerID string) []*turnsvc.PendingTurn {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*turnsvc.PendingTurn
	for _, item := range p.items {
		if item.OwnerID == ownerID {
			copy := *item
			out = append(out, &copy)
		}
	}
	return out
}

func (p *fakePending) set(items ...*turnsvc.PendingTurn) {
	p.mu.Lock()
	p.items = items
	p.mu.Unlock()
}

func (p *fakePending) remove(requestID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, item := range p.items {
		if item.RequestID == requestID {
			p.items = append(p.items[:i], p.items[i+1:]...)
			return
		}
	}
}

type fakeController struct {
	pending  *fakePending
	commands chan controlsvc.Command
	mu       sync.Mutex
	ctx      context.Context
}

func (c *fakeController) Execute(ctx context.Context, command controlsvc.Command) (controlsvc.Result, error) {
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
	c.commands <- command
	if c.pending != nil {
		c.pending.remove(command.RequestID)
	}
	return controlsvc.Result{}, nil
}

func (c *fakeController) nextCommand(t *testing.T) controlsvc.Command {
	t.Helper()
	select {
	case command := <-c.commands:
		return command
	case <-time.After(time.Second):
		t.Fatal("control command not received")
		return controlsvc.Command{}
	}
}

func (c *fakeController) lastContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

type fakeProvider struct {
	started       chan struct{}
	inbound       chan InboundMessage
	sent          chan OutboundMessage
	pollBlock     chan struct{}
	pollStarted   chan struct{}
	startOnce     sync.Once
	pollOnce      sync.Once
	pollResult    *LoginPollResult
	startExisting bool
	sendBlock     chan struct{}
	sendStarted   chan struct{}
	sendOnce      sync.Once
	sendErr       error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{started: make(chan struct{}), inbound: make(chan InboundMessage, 8), sent: make(chan OutboundMessage, 16)}
}

func (p *fakeProvider) ID() string { return ProviderClawBot }

func (p *fakeProvider) StartLogin(_ context.Context, existing *Account) (LoginChallenge, error) {
	p.startExisting = existing != nil
	return LoginChallenge{Provider: p.ID(), Opaque: json.RawMessage(`{}`), QRCodeURL: "https://weixin.qq.com/x/test", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (p *fakeProvider) PollLogin(context.Context, LoginChallenge, string) (LoginPollResult, error) {
	if p.pollStarted != nil {
		p.pollOnce.Do(func() { close(p.pollStarted) })
	}
	if p.pollBlock != nil {
		<-p.pollBlock
	}
	if p.pollResult != nil {
		return *p.pollResult, nil
	}
	return LoginPollResult{State: LoginConnected, Message: "connected", Account: &Account{
		Provider: p.ID(), ExternalBotID: "bot-1", ExternalOwnerID: "wechat-owner", Endpoint: "https://ilinkai.weixin.qq.com",
		Credentials: json.RawMessage(`{"token":"plain-token"}`), State: json.RawMessage(`{"ready":true,"context":"plain-context"}`), ConnectedAt: time.Now().UTC(),
	}}, nil
}

func (p *fakeProvider) Run(ctx context.Context, _ Account, callbacks ProviderCallbacks) error {
	p.startOnce.Do(func() { close(p.started) })
	for {
		select {
		case <-ctx.Done():
			if callbacks.Checkpoint != nil {
				_ = callbacks.Checkpoint(context.Background(), json.RawMessage(`{"late":"plain-context"}`))
			}
			return nil
		case inbound := <-p.inbound:
			if callbacks.HandleInbound != nil {
				if err := callbacks.HandleInbound(ctx, inbound); err != nil {
					return err
				}
			}
			if callbacks.Checkpoint != nil {
				if err := callbacks.Checkpoint(ctx, json.RawMessage(`{"ready":true,"context":"plain-context"}`)); err != nil {
					return err
				}
			}
		}
	}
}

func (p *fakeProvider) Send(_ context.Context, _ Account, outgoing OutboundMessage) error {
	p.sent <- outgoing
	if p.sendStarted != nil {
		p.sendOnce.Do(func() { close(p.sendStarted) })
	}
	if p.sendBlock != nil {
		<-p.sendBlock
	}
	return p.sendErr
}

func (p *fakeProvider) Ready(account Account) bool {
	return strings.Contains(string(account.State), `"ready":true`)
}

func (p *fakeProvider) ReadinessVersion(account Account) string {
	var state struct {
		ContextGeneration json.Number `json:"context_generation"`
	}
	if json.Unmarshal(account.State, &state) != nil {
		return ""
	}
	return state.ContextGeneration.String()
}

func (p *fakeProvider) nextSent(t *testing.T) OutboundMessage {
	t.Helper()
	select {
	case sent := <-p.sent:
		return sent
	case <-time.After(time.Second):
		t.Fatal("outbound message not sent")
		return OutboundMessage{}
	}
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
