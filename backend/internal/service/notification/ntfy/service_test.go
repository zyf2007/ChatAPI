package ntfy_test

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/platform/ntfy"
	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	ntfynotify "github.com/zyf2007/ChatAPI/internal/service/notification/ntfy"
)

func publicLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("1.2.3.4")}, nil
}

func testOptions(workers, queue int) ntfynotify.Options {
	return ntfynotify.Options{
		Workers:   workers,
		QueueSize: queue,
		Lookup:    urlsafety.HostLookup(publicLookup),
	}
}

func TestNotifyWaiting_DisabledOrMissingDoesNotSend(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"disabled": {UserID: "disabled", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": false,
			"ntfy_url":         "https://ntfy.sh/topic",
		}},
		"empty": {UserID: "empty", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 4))
	t.Cleanup(func() { _ = svc.Close() })

	svc.NotifyWaiting(context.Background(), "missing", "title", "hello")
	svc.NotifyWaiting(context.Background(), "disabled", "title", "hello")
	svc.NotifyWaiting(context.Background(), "empty", "title", "hello")
	waitFor(t, 500*time.Millisecond, func() bool { return sender.count() == 0 })
	if sender.count() != 0 {
		t.Fatalf("expected no sends, got %d", sender.count())
	}
}

func TestNotifyWaiting_SendsOnceWithTitleAndBody(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 4))
	t.Cleanup(func() { _ = svc.Close() })

	svc.NotifyWaiting(context.Background(), "user_a", "会话标题", "最后用户内容")
	waitFor(t, time.Second, func() bool { return sender.count() == 1 })

	msg := sender.last()
	if msg.URL != "https://ntfy.sh/alice" {
		t.Fatalf("unexpected url: %#v", msg)
	}
	if msg.Title != "ChatAPI · 会话标题" {
		t.Fatalf("unexpected title: %#v", msg)
	}
	if msg.Text != "新请求：\n最后用户内容" {
		t.Fatalf("unexpected body: %#v", msg)
	}
}

func TestNotifyWaiting_DefaultBodyWhenUserTextEmpty(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 2))
	t.Cleanup(func() { _ = svc.Close() })

	svc.NotifyWaiting(context.Background(), "user_a", "", "")
	waitFor(t, time.Second, func() bool { return sender.count() == 1 })
	msg := sender.last()
	if msg.Title != "ChatAPI · ChatAPI" {
		t.Fatalf("unexpected default title: %#v", msg)
	}
	if msg.Text != "收到一条新的模型调用请求，请打开工作台回复。" {
		t.Fatalf("unexpected default body: %#v", msg)
	}
}

func TestDispatcher_SlowSenderDoesNotBlockEnqueue(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	sender := &blockingSender{started: started, release: release}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 2))
	t.Cleanup(func() {
		close(release)
		_ = svc.Close()
	})

	svc.NotifyWaiting(context.Background(), "user_a", "t1", "one")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}

	begin := time.Now()
	// Queue has room for more jobs; enqueue must return immediately while sender is blocked.
	svc.NotifyWaiting(context.Background(), "user_a", "t2", "two")
	svc.NotifyWaiting(context.Background(), "user_a", "t3", "three")
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("enqueue blocked for %s", elapsed)
	}
}

func TestDispatcher_QueueFullDropsWithoutBlocking(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	sender := &blockingSender{started: started, release: release}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 1))
	t.Cleanup(func() {
		close(release)
		_ = svc.Close()
	})

	svc.NotifyWaiting(context.Background(), "user_a", "t1", "one")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}
	// Fill the single queue slot.
	svc.NotifyWaiting(context.Background(), "user_a", "t2", "two")

	begin := time.Now()
	// This must drop immediately (queue full) without blocking Submit-equivalent path.
	svc.NotifyWaiting(context.Background(), "user_a", "t3", "three")
	if elapsed := time.Since(begin); elapsed > 50*time.Millisecond {
		t.Fatalf("full-queue drop blocked for %s", elapsed)
	}
}

func TestDispatcher_CloseIdempotent(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(2, 2))
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	// After close, notify must not panic.
	svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
}

func TestDispatcher_ConcurrentCloseSafe(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(2, 4))

	const n = 16
	errs := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			errs <- svc.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent close: %v", err)
		}
	}
	svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
	if sender.count() != 0 {
		t.Fatalf("expected no send after concurrent close, got %d", sender.count())
	}
}

func TestDispatcher_CloseCancelsStuckSender(t *testing.T) {
	started := make(chan struct{})
	// Never released unless ctx is canceled — models a stuck network Send.
	sender := &blockingSender{started: started, release: make(chan struct{})}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	opts := testOptions(1, 2)
	opts.CloseTimeout = 80 * time.Millisecond
	opts.SendTimeout = 30 * time.Second // must not be the exit path; lifeCtx cancel is.
	svc := ntfynotify.NewWithOptions(store, sender, nil, opts)

	svc.NotifyWaiting(context.Background(), "user_a", "t1", "one")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sender did not start")
	}

	begin := time.Now()
	err := svc.Close()
	elapsed := time.Since(begin)
	if !errors.Is(err, ntfynotify.ErrCloseTimedOut) {
		t.Fatalf("expected close timeout, got %v", err)
	}
	// Drain window + join workers; must not wait for sendTimeout (30s).
	if elapsed > 2*time.Second {
		t.Fatalf("close took too long with stuck sender: %s", elapsed)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("close returned before drain window: %s", elapsed)
	}
}

func TestDispatcher_CloseDrainsQueuedThenCancelsBacklog(t *testing.T) {
	var started atomic.Int32
	var completed atomic.Int32
	// Each send blocks until ctx cancel or ~long timeout; first few may start.
	sender := &ctxAwareSlowSender{started: &started, completed: &completed, hold: 5 * time.Second}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	opts := testOptions(1, 8)
	opts.CloseTimeout = 100 * time.Millisecond
	opts.SendTimeout = 10 * time.Second
	svc := ntfynotify.NewWithOptions(store, sender, nil, opts)

	// Fill queue with backlog while single worker is occupied by the first slow send.
	for i := 0; i < 6; i++ {
		svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
	}
	waitFor(t, time.Second, func() bool { return started.Load() >= 1 })

	err := svc.Close()
	if !errors.Is(err, ntfynotify.ErrCloseTimedOut) {
		t.Fatalf("expected close timeout with backlog, got %v", err)
	}
	// Not all backlog items should complete as successful sends after cancel.
	if completed.Load() >= 6 {
		t.Fatalf("expected cancel to drop backlog, completed=%d", completed.Load())
	}
	// After Close returns, further enqueue is dropped.
	svc.NotifyWaiting(context.Background(), "user_a", "after", "x")
	time.Sleep(30 * time.Millisecond)
	if started.Load() > 6 {
		t.Fatalf("unexpected send after close: started=%d", started.Load())
	}
}

func TestDispatcher_CloseAfterEnqueueDoesNotPanic(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{configs: map[string]common.UserConfig{
		"user_a": {UserID: "user_a", Key: "settings", Value: map[string]any{
			"ntfy_url_enabled": true,
			"ntfy_url":         "https://ntfy.sh/alice",
		}},
	}}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(2, 16))

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.Close()
	}()
	wg.Wait()
	// Second close + enqueue after close must stay safe.
	if err := svc.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
}

func TestDispatcher_NotFoundConfigDoesNotSend(t *testing.T) {
	sender := &recordingSender{}
	store := &configStoreStub{err: common.ErrNotFound}
	svc := ntfynotify.NewWithOptions(store, sender, nil, testOptions(1, 2))
	t.Cleanup(func() { _ = svc.Close() })
	svc.NotifyWaiting(context.Background(), "user_a", "t", "x")
	time.Sleep(50 * time.Millisecond)
	if sender.count() != 0 {
		t.Fatalf("expected no send on not found, got %d", sender.count())
	}
}

type recordingSender struct {
	mu       sync.Mutex
	messages []ntfy.Message
}

func (s *recordingSender) Send(_ context.Context, message ntfy.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return nil
}

func (s *recordingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *recordingSender) last() ntfy.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return ntfy.Message{}
	}
	return s.messages[len(s.messages)-1]
}

type blockingSender struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSender) Send(ctx context.Context, message ntfy.Message) error {
	s.once.Do(func() {
		select {
		case s.started <- struct{}{}:
		default:
		}
	})
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ctxAwareSlowSender increments started on entry and completed only on success.
type ctxAwareSlowSender struct {
	started   *atomic.Int32
	completed *atomic.Int32
	hold      time.Duration
}

func (s *ctxAwareSlowSender) Send(ctx context.Context, message ntfy.Message) error {
	s.started.Add(1)
	timer := time.NewTimer(s.hold)
	defer timer.Stop()
	select {
	case <-timer.C:
		s.completed.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type configStoreStub struct {
	configs map[string]common.UserConfig
	err     error
	mu      sync.Mutex
	sets    int
}

func (s *configStoreStub) GetUserConfig(_ context.Context, userID string, key string) (common.UserConfig, error) {
	if s.err != nil {
		return common.UserConfig{}, s.err
	}
	if s.configs == nil {
		return common.UserConfig{}, common.ErrNotFound
	}
	item, ok := s.configs[userID]
	if !ok {
		return common.UserConfig{}, common.ErrNotFound
	}
	if key != "" && item.Key != "" && item.Key != key {
		return common.UserConfig{}, common.ErrNotFound
	}
	return item, nil
}

func (s *configStoreStub) SetUserConfig(_ context.Context, input common.SetUserConfigInput) (common.UserConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.configs == nil {
		s.configs = map[string]common.UserConfig{}
	}
	item := common.UserConfig{UserID: input.UserID, Key: input.Key, Value: input.Value}
	s.configs[input.UserID] = item
	return item, nil
}

func (s *configStoreStub) DeleteUserConfig(context.Context, string, string) error { return nil }
func (s *configStoreStub) ListUserConfigs(context.Context, string) ([]common.UserConfig, error) {
	return nil, nil
}
func (s *configStoreStub) GetSystemConfig(context.Context, string) (common.SystemConfig, error) {
	return common.SystemConfig{}, common.ErrNotFound
}
func (s *configStoreStub) SetSystemConfig(context.Context, common.SetSystemConfigInput) (common.SystemConfig, error) {
	return common.SystemConfig{}, nil
}
func (s *configStoreStub) DeleteSystemConfig(context.Context, string) error { return nil }
func (s *configStoreStub) ListSystemConfigs(context.Context) ([]common.SystemConfig, error) {
	return nil, nil
}

var _ configrepo.Store = (*configStoreStub)(nil)

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
