package ntfy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/ntfy"
	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"go.uber.org/zap"
)

const (
	userSettingsKey = "settings"
	maxBodyRunes    = 280

	defaultWorkers      = 2
	defaultQueueSize    = 64
	defaultSendTimeout  = 5 * time.Second
	defaultCloseTimeout = 5 * time.Second
)

// ErrCloseTimedOut is returned when Close had to cancel in-flight work after the drain window.
var ErrCloseTimedOut = errors.New("ntfy service close timed out; canceled in-flight work")

// Sender posts one ntfy message. Tests inject stubs; production uses platform/ntfy.Client.
type Sender interface {
	Send(ctx context.Context, message ntfy.Message) error
}

// Options configures the bounded notification dispatcher.
type Options struct {
	Workers     int
	QueueSize   int
	SendTimeout time.Duration
	// CloseTimeout is the bounded drain window after Close stops accepting jobs.
	// When the window elapses, the service context is canceled so in-flight Send
	// and remaining queued jobs exit, then Close waits for all workers.
	CloseTimeout time.Duration
	// Lookup is shared with save-time validation when injected (tests).
	Lookup urlsafety.HostLookup
}

// Service sends user-configured ntfy notifications for pending chat turns.
// NotifyWaiting only enqueues work; workers perform config lookup, safety checks, and HTTP.
//
// Lifecycle (graceful but bounded):
//  1. NotifyWaiting enqueues non-blocking while open; drops when full or closed.
//  2. Close stops accepting, drains the queue for up to CloseTimeout.
//  3. On drain deadline, the service context is canceled; in-flight Send aborts
//     and remaining jobs are dropped as workers observe cancellation.
//  4. Close returns only after every worker has exited (no leftover service goroutines).
type Service struct {
	configs      configrepo.Store
	sender       Sender
	logger       *zap.Logger
	lookup       urlsafety.HostLookup
	sendTimeout  time.Duration
	closeTimeout time.Duration

	// lifeCtx spans the service lifetime; canceled when the drain window ends.
	lifeCtx context.Context
	cancel  context.CancelFunc

	jobs chan notifyJob
	wg   sync.WaitGroup

	closeOnce sync.Once
	// closedDone is closed after the single close sequence finishes (workers gone).
	closedDone chan struct{}
	closeErr   error

	mu     sync.RWMutex
	closed bool
}

type notifyJob struct {
	ownerID string
	title   string
	// userText is the last user content captured at enqueue time (owner-isolated fact).
	userText string
}

func New(configs configrepo.Store, sender Sender, logger *zap.Logger) *Service {
	return NewWithOptions(configs, sender, logger, Options{})
}

func NewWithOptions(configs configrepo.Store, sender Sender, logger *zap.Logger, opts Options) *Service {
	if sender == nil {
		sender = ntfy.NewClient(nil)
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	sendTimeout := opts.SendTimeout
	if sendTimeout <= 0 {
		sendTimeout = defaultSendTimeout
	}
	closeTimeout := opts.CloseTimeout
	if closeTimeout <= 0 {
		closeTimeout = defaultCloseTimeout
	}

	lifeCtx, cancel := context.WithCancel(context.Background())
	s := &Service{
		configs:      configs,
		sender:       sender,
		logger:       logger,
		lookup:       opts.Lookup,
		sendTimeout:  sendTimeout,
		closeTimeout: closeTimeout,
		lifeCtx:      lifeCtx,
		cancel:       cancel,
		jobs:         make(chan notifyJob, queueSize),
		closedDone:   make(chan struct{}),
	}
	s.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go s.worker()
	}
	return s
}

// NotifyWaiting is intended for Submitter.Hooks.NotifyWaiting.
// It never blocks on config lookup, DNS, or HTTP; queue overflow drops with a log.
// Enqueue is non-blocking and protected against close races (no send-on-closed panic).
func (s *Service) NotifyWaiting(ctx context.Context, ownerID string, title string, userText string) {
	if s == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}
	job := notifyJob{
		ownerID:  ownerID,
		title:    strings.TrimSpace(title),
		userText: strings.TrimSpace(userText),
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", ownerID),
		).Debug("ntfy notify skipped: service closed")
		return
	}
	select {
	case s.jobs <- job:
	default:
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", ownerID),
			zap.Int("queue.capacity", cap(s.jobs)),
		).Warn("ntfy notify dropped: queue full")
	}
}

// Close stops accepting jobs, drains the queue within a bounded window, cancels
// in-flight work if the window elapses, and waits for all workers to exit.
// Close is idempotent and safe for concurrent callers; only one close sequence runs.
// When Close returns, no service-owned goroutines remain.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(s.shutdown)
	<-s.closedDone
	return s.closeErr
}

func (s *Service) shutdown() {
	defer close(s.closedDone)

	s.mu.Lock()
	s.closed = true
	close(s.jobs)
	s.mu.Unlock()

	// Single waiter for this service lifetime: either drains cleanly or
	// cancels after the deadline and then joins workers. No leaked goroutine
	// on return — the waiter exits when wg.Wait returns.
	waitDone := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(waitDone)
	}()

	timer := time.NewTimer(s.closeTimeout)
	defer timer.Stop()

	select {
	case <-waitDone:
		// Queue drained and all sends finished within the window.
		s.cancel()
		s.closeErr = nil
		return
	case <-timer.C:
		// Bounded drain expired: cancel service lifecycle so in-flight Send
		// and any remaining process() calls observe lifeCtx.Done() and exit.
		s.cancel()
		<-waitDone
		s.closeErr = ErrCloseTimedOut
		return
	}
}

func (s *Service) worker() {
	defer s.wg.Done()
	for job := range s.jobs {
		// After cancel, drop remaining queued jobs without starting new sends.
		if s.lifeCtx.Err() != nil {
			continue
		}
		s.process(job)
	}
}

func (s *Service) process(job notifyJob) {
	if s.lifeCtx.Err() != nil {
		return
	}
	if s.configs == nil || s.sender == nil {
		return
	}

	url, enabled, err := s.resolveNtfyTarget(s.lifeCtx, job.ownerID)
	if err != nil {
		if s.lifeCtx.Err() != nil {
			return
		}
		logging.BindContext(s.logger, s.lifeCtx,
			zap.String("owner.id", job.ownerID),
			zap.Error(err),
		).Debug("ntfy target resolve skipped")
		return
	}
	if !enabled || url == "" {
		return
	}
	if s.lifeCtx.Err() != nil {
		return
	}

	safety := urlsafety.ValidateNtfyURLContext(s.lifeCtx, url, false, s.lookup)
	if !safety.OK {
		if s.lifeCtx.Err() != nil {
			return
		}
		logging.BindContext(s.logger, s.lifeCtx,
			zap.String("owner.id", job.ownerID),
			zap.String("ntfy.reason", safety.Reason),
		).Warn("ntfy url rejected")
		return
	}

	msgTitle := job.title
	if msgTitle == "" {
		msgTitle = "ChatAPI"
	}
	msgTitle = "ChatAPI · " + truncateRunes(msgTitle, 64)

	body := job.userText
	if body == "" {
		body = "收到一条新的模型调用请求，请打开工作台回复。"
	} else {
		body = "新请求：\n" + truncateRunes(body, maxBodyRunes)
	}

	// Per-message timeout nested under the service lifecycle context so Close
	// can abort a stuck Send while still bounding healthy sends.
	sendCtx, cancel := context.WithTimeout(s.lifeCtx, s.sendTimeout)
	defer cancel()

	if err := s.sender.Send(sendCtx, ntfy.Message{
		URL:   url,
		Title: msgTitle,
		Text:  body,
	}); err != nil {
		if s.lifeCtx.Err() != nil {
			return
		}
		logging.BindContext(s.logger, s.lifeCtx,
			zap.String("owner.id", job.ownerID),
			zap.Error(err),
		).Warn("ntfy send failed")
		return
	}

	logging.BindContext(s.logger, s.lifeCtx,
		zap.String("owner.id", job.ownerID),
	).Info("ntfy notification sent")
}

func (s *Service) resolveNtfyTarget(ctx context.Context, ownerID string) (url string, enabled bool, err error) {
	item, err := s.configs.GetUserConfig(ctx, ownerID, userSettingsKey)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	value := item.Value
	if value == nil {
		return "", false, nil
	}

	enabled = asBool(value["ntfy_url_enabled"])
	url = strings.TrimSpace(fmt.Sprint(value["ntfy_url"]))
	if url == "<nil>" {
		url = ""
	}
	if !enabled || url == "" {
		return "", false, nil
	}
	return url, true, nil
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func truncateRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || text == "" {
		return text
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}
