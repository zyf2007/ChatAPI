package im

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

var (
	ErrLoginNotFound = errors.New("IM login session not found")
	ErrLoginBusy     = errors.New("IM login poll already in progress")
	ErrOwnerInactive = errors.New("IM owner is inactive")
)

type PendingLookup interface {
	ListByOwnerID(string) []*turnsvc.PendingTurn
}

type Controller interface {
	Execute(context.Context, controlsvc.Command) (controlsvc.Result, error)
}

type ConnectionStatus struct {
	Provider       string     `json:"provider"`
	Connected      bool       `json:"connected"`
	Ready          bool       `json:"ready"`
	WorkerState    string     `json:"worker_state"`
	ReauthRequired bool       `json:"reauth_required"`
	ExternalBotID  string     `json:"external_bot_id,omitempty"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	LastInboundAt  *time.Time `json:"last_inbound_at,omitempty"`
	LastOutboundAt *time.Time `json:"last_outbound_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastErrorAt    *time.Time `json:"last_error_at,omitempty"`
}

type LoginView struct {
	SessionID string            `json:"session_id"`
	State     LoginState        `json:"state"`
	Message   string            `json:"message"`
	QRCodeURL string            `json:"qr_code_url,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
	Status    *ConnectionStatus `json:"connection,omitempty"`
}

type runtimeStatus struct {
	workerState             string
	reauthRequired          bool
	contextInvalid          bool
	invalidReadinessVersion string
	lastInboundAt           *time.Time
	lastOutboundAt          *time.Time
	lastError               string
	lastErrorAt             *time.Time
}

type accountRuntime struct {
	ownerID    string
	generation uint64
	cancel     context.CancelFunc
	done       chan struct{}
	barrier    sync.Mutex
}

type loginSession struct {
	id         string
	ownerID    string
	generation uint64
	challenge  LoginChallenge
	state      LoginState
	message    string
	polling    bool
}

type notificationJob struct {
	waiting chatevents.WaitingTurn
}

type Service struct {
	Store     AccountStore
	Pending   PendingLookup
	Control   Controller
	MasterKey string
	Logger    *zap.Logger

	providers map[string]Provider

	mu             sync.Mutex
	lifeCtx        context.Context
	running        bool
	accounts       map[string]Account
	runtimes       map[string]*accountRuntime
	statuses       map[string]*runtimeStatus
	accountGen     map[string]uint64
	loginGen       map[string]uint64
	logins         map[string]*loginSession
	loginByOwner   map[string]string
	ownerOps       sync.Map
	selected       map[string]string
	latestWaiting  map[string]chatevents.WaitingTurn
	notification   map[string]notificationJob
	notifyInFlight map[string]bool
	notifyWake     chan struct{}
}

func NewService(store AccountStore, pending PendingLookup, control Controller, masterKey string, logger *zap.Logger, providers ...Provider) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Service{
		Store: store, Pending: pending, Control: control, MasterKey: strings.TrimSpace(masterKey), Logger: logger,
		providers: make(map[string]Provider), accounts: make(map[string]Account), runtimes: make(map[string]*accountRuntime),
		statuses: make(map[string]*runtimeStatus), accountGen: make(map[string]uint64), loginGen: make(map[string]uint64),
		logins: make(map[string]*loginSession), loginByOwner: make(map[string]string), selected: make(map[string]string),
		latestWaiting: make(map[string]chatevents.WaitingTurn), notification: make(map[string]notificationJob),
		notifyInFlight: make(map[string]bool), notifyWake: make(chan struct{}, 1),
	}
	for _, provider := range providers {
		if provider != nil && strings.TrimSpace(provider.ID()) != "" {
			s.providers[provider.ID()] = provider
		}
	}
	return s
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("IM service is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.running = true
	s.lifeCtx = runCtx
	s.mu.Unlock()
	defer cancel()

	if err := s.restoreAccounts(runCtx); err != nil {
		s.Logger.Warn("restore IM accounts failed", zap.Error(err))
	}
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.notificationWorker(runCtx)
		}()
	}
	<-runCtx.Done()

	s.mu.Lock()
	runtimes := make([]*accountRuntime, 0, len(s.runtimes))
	for ownerID, runtime := range s.runtimes {
		s.accountGen[ownerID]++
		delete(s.runtimes, ownerID)
		runtimes = append(runtimes, runtime)
	}
	s.running = false
	s.lifeCtx = nil
	s.mu.Unlock()
	for _, runtime := range runtimes {
		s.stopRuntime(runtime)
	}
	workers.Wait()
	return nil
}

func (s *Service) GetStatus(ctx context.Context, ownerID string) (ConnectionStatus, error) {
	if err := s.requireActiveOwner(ctx, ownerID); err != nil {
		return ConnectionStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(strings.TrimSpace(ownerID)), nil
}

func (s *Service) BeginLogin(ctx context.Context, ownerID, providerID string) (LoginView, error) {
	ownerID = strings.TrimSpace(ownerID)
	if err := s.requireActiveOwner(ctx, ownerID); err != nil {
		return LoginView{}, err
	}
	provider := s.providers[strings.TrimSpace(providerID)]
	if provider == nil {
		return LoginView{}, errors.New("unsupported IM provider")
	}
	op := s.ownerOperation(ownerID)
	op.Lock()
	defer op.Unlock()
	s.mu.Lock()
	if existingID := s.loginByOwner[ownerID]; existingID != "" {
		if existing := s.logins[existingID]; existing != nil && time.Now().Before(existing.challenge.ExpiresAt) && (existing.state == LoginWaiting || existing.state == LoginScanned || existing.state == LoginVerifyNeeded) {
			view := s.loginViewLocked(existing)
			s.mu.Unlock()
			return view, nil
		}
	}
	var existingAccount *Account
	if account, ok := s.accounts[ownerID]; ok && account.Provider == provider.ID() {
		copy := account
		existingAccount = &copy
	}
	s.mu.Unlock()
	challenge, err := provider.StartLogin(ctx, existingAccount)
	if err != nil {
		return LoginView{}, err
	}

	s.mu.Lock()
	s.loginGen[ownerID]++
	generation := s.loginGen[ownerID]
	if oldID := s.loginByOwner[ownerID]; oldID != "" {
		delete(s.logins, oldID)
	}
	session := &loginSession{
		id: uuid.NewString(), ownerID: ownerID, generation: generation, challenge: challenge,
		state: LoginWaiting, message: "等待微信扫码确认",
	}
	s.logins[session.id] = session
	s.loginByOwner[ownerID] = session.id
	view := s.loginViewLocked(session)
	s.mu.Unlock()
	return view, nil
}

func (s *Service) PollLogin(ctx context.Context, ownerID, sessionID, verifyCode string) (LoginView, error) {
	ownerID = strings.TrimSpace(ownerID)
	sessionID = strings.TrimSpace(sessionID)
	if err := s.requireActiveOwner(ctx, ownerID); err != nil {
		return LoginView{}, err
	}
	s.mu.Lock()
	session := s.logins[sessionID]
	if session == nil || session.ownerID != ownerID || time.Now().After(session.challenge.ExpiresAt) {
		s.mu.Unlock()
		return LoginView{}, ErrLoginNotFound
	}
	if session.polling {
		s.mu.Unlock()
		return LoginView{}, ErrLoginBusy
	}
	session.polling = true
	generation := session.generation
	challenge := session.challenge
	provider := s.providers[challenge.Provider]
	s.mu.Unlock()
	if provider == nil {
		s.clearLoginPolling(sessionID, generation)
		return LoginView{}, errors.New("IM provider is unavailable")
	}

	result, err := provider.PollLogin(ctx, challenge, verifyCode)
	if err != nil {
		s.clearLoginPolling(sessionID, generation)
		return LoginView{}, err
	}
	if result.State == LoginAlreadyBound {
		op := s.ownerOperation(ownerID)
		op.Lock()
		s.mu.Lock()
		current := s.logins[sessionID]
		if current == nil || current.ownerID != ownerID || current.generation != generation || s.loginGen[ownerID] != generation {
			s.mu.Unlock()
			op.Unlock()
			return LoginView{}, ErrLoginNotFound
		}
		status := s.statusLocked(ownerID)
		if status.Connected {
			delete(s.logins, sessionID)
			delete(s.loginByOwner, ownerID)
			s.mu.Unlock()
			op.Unlock()
			return LoginView{State: LoginConnected, Message: result.Message, ExpiresAt: result.Challenge.ExpiresAt, Status: &status}, nil
		}
		s.mu.Unlock()
		op.Unlock()
	}
	if result.State != LoginConnected || result.Account == nil {
		s.mu.Lock()
		current := s.logins[sessionID]
		if current == nil || current.ownerID != ownerID || current.generation != generation || s.loginGen[ownerID] != generation {
			s.mu.Unlock()
			return LoginView{}, ErrLoginNotFound
		}
		current.polling = false
		current.challenge = result.Challenge
		current.state = result.State
		current.message = result.Message
		view := s.loginViewLocked(current)
		s.mu.Unlock()
		return view, nil
	}

	op := s.ownerOperation(ownerID)
	op.Lock()
	defer op.Unlock()
	s.mu.Lock()
	current := s.logins[sessionID]
	if current == nil || current.ownerID != ownerID || current.generation != generation || s.loginGen[ownerID] != generation {
		s.mu.Unlock()
		return LoginView{}, ErrLoginNotFound
	}
	delete(s.logins, sessionID)
	delete(s.loginByOwner, ownerID)
	account := *result.Account
	account.OwnerID = ownerID
	s.mu.Unlock()

	if err := s.replaceAccount(ctx, account); err != nil {
		return LoginView{}, err
	}
	status, err := s.GetStatus(ctx, ownerID)
	if err != nil {
		return LoginView{}, err
	}
	return LoginView{State: LoginConnected, Message: result.Message, ExpiresAt: result.Challenge.ExpiresAt, Status: &status}, nil
}

func (s *Service) RevokeOwner(ctx context.Context, ownerID string) error {
	return s.Disconnect(ctx, ownerID)
}

func (s *Service) Disconnect(ctx context.Context, ownerID string) error {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return ErrConnectionNotFound
	}
	op := s.ownerOperation(ownerID)
	op.Lock()
	defer op.Unlock()

	s.mu.Lock()
	s.loginGen[ownerID]++
	if sessionID := s.loginByOwner[ownerID]; sessionID != "" {
		delete(s.logins, sessionID)
		delete(s.loginByOwner, ownerID)
	}
	oldAccount, hadAccount, runtime := s.detachOwnerLocked(ownerID)
	s.mu.Unlock()
	s.stopRuntime(runtime)

	err := s.Store.DeleteUserConfig(ctx, ownerID, accountConfigKey)
	if err == nil || errors.Is(err, common.ErrNotFound) {
		return nil
	}
	if hadAccount {
		s.mu.Lock()
		s.accounts[ownerID] = oldAccount
		s.statuses[ownerID] = &runtimeStatus{workerState: "starting", lastError: "disconnect failed"}
		s.startRuntimeLocked(oldAccount)
		s.mu.Unlock()
	}
	return fmt.Errorf("delete IM connection: %w", err)
}

func (s *Service) HandleChatEvent(_ context.Context, event chatevents.Event) {
	if s == nil || event.Type != chatevents.TypeTurnWaiting || event.WaitingTurn == nil {
		return
	}
	waiting := *event.WaitingTurn
	waiting.OwnerID = strings.TrimSpace(waiting.OwnerID)
	if waiting.OwnerID == "" || strings.TrimSpace(waiting.ConversationID) == "" || strings.TrimSpace(waiting.RequestID) == "" {
		return
	}
	s.mu.Lock()
	s.latestWaiting[waiting.OwnerID] = waiting
	s.notification[waiting.OwnerID] = notificationJob{waiting: waiting}
	s.signalNotificationLocked()
	s.mu.Unlock()
}

func (s *Service) replaceAccount(ctx context.Context, account Account) error {
	ownerID := strings.TrimSpace(account.OwnerID)
	if ownerID == "" {
		return errors.New("IM account owner is required")
	}
	s.mu.Lock()
	oldAccount, hadOld, runtime := s.detachOwnerLocked(ownerID)
	s.mu.Unlock()
	s.stopRuntime(runtime)

	if err := saveAccount(ctx, s.Store, s.MasterKey, account); err != nil {
		if hadOld {
			s.mu.Lock()
			s.accounts[ownerID] = oldAccount
			s.statuses[ownerID] = &runtimeStatus{workerState: "starting"}
			s.startRuntimeLocked(oldAccount)
			s.mu.Unlock()
		}
		return err
	}
	s.mu.Lock()
	s.accounts[ownerID] = account
	s.statuses[ownerID] = &runtimeStatus{workerState: "starting"}
	s.startRuntimeLocked(account)
	s.mu.Unlock()
	return nil
}

func (s *Service) restoreAccounts(ctx context.Context) error {
	users, err := s.Store.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		ownerID := strings.TrimSpace(user.ID)
		if !user.IsActive || ownerID == "" {
			continue
		}
		op := s.ownerOperation(ownerID)
		op.Lock()
		currentUser, userErr := s.Store.GetUser(ctx, ownerID)
		if userErr != nil || !currentUser.IsActive {
			op.Unlock()
			continue
		}
		account, loadErr := loadAccount(ctx, s.Store, s.MasterKey, ownerID)
		if errors.Is(loadErr, common.ErrNotFound) {
			op.Unlock()
			continue
		}
		if loadErr != nil {
			s.Logger.Warn("restore IM account failed", zap.String("owner_id", ownerID), zap.Error(loadErr))
			op.Unlock()
			continue
		}
		if s.providers[account.Provider] == nil {
			s.Logger.Warn("restore IM account skipped: provider unavailable", zap.String("owner_id", ownerID), zap.String("provider", account.Provider))
			op.Unlock()
			continue
		}
		s.mu.Lock()
		if !s.running || s.lifeCtx == nil || s.accounts[ownerID].OwnerID != "" || s.runtimes[ownerID] != nil {
			s.mu.Unlock()
			op.Unlock()
			continue
		}
		s.accounts[ownerID] = account
		s.statuses[ownerID] = &runtimeStatus{workerState: "starting"}
		s.startRuntimeLocked(account)
		s.mu.Unlock()
		op.Unlock()
	}
	return nil
}

func (s *Service) startRuntimeLocked(account Account) {
	if !s.running || s.lifeCtx == nil || s.providers[account.Provider] == nil {
		return
	}
	ownerID := account.OwnerID
	if s.runtimes[ownerID] != nil {
		s.Logger.Warn("refusing to overwrite an active IM runtime", zap.String("owner_id", ownerID), zap.String("provider", account.Provider))
		return
	}
	s.accountGen[ownerID]++
	generation := s.accountGen[ownerID]
	runCtx, cancel := context.WithCancel(s.lifeCtx)
	runtime := &accountRuntime{ownerID: ownerID, generation: generation, cancel: cancel, done: make(chan struct{})}
	s.runtimes[ownerID] = runtime
	provider := s.providers[account.Provider]
	status := s.statuses[ownerID]
	if status == nil {
		status = &runtimeStatus{}
		s.statuses[ownerID] = status
	}
	status.workerState = "running"
	go func() {
		err := provider.Run(runCtx, account, ProviderCallbacks{
			HandleInbound: func(ctx context.Context, inbound InboundMessage) error {
				runtime.barrier.Lock()
				defer runtime.barrier.Unlock()
				if !s.runtimeCurrent(ownerID, generation, runtime) {
					return context.Canceled
				}
				return s.handleInbound(ctx, runtime, inbound)
			},
			Checkpoint: func(ctx context.Context, state json.RawMessage) error {
				runtime.barrier.Lock()
				defer runtime.barrier.Unlock()
				return s.checkpoint(ctx, ownerID, generation, runtime, state)
			},
			ReportError: func(err error) { s.recordRuntimeError(ownerID, generation, runtime, err) },
		})
		s.runtimeExited(ownerID, generation, runtime, err)
		close(runtime.done)
	}()
}

// checkpoint is called while runtime.barrier is held. Disconnect/replace first
// invalidate the generation, then wait on that barrier before deleting or
// replacing the stored account. Therefore an old save may finish before the
// delete, but can never commit after a successful disconnect returns.
func (s *Service) checkpoint(ctx context.Context, ownerID string, generation uint64, runtime *accountRuntime, state []byte) error {
	s.mu.Lock()
	if s.runtimes[ownerID] != runtime || s.accountGen[ownerID] != generation {
		s.mu.Unlock()
		return context.Canceled
	}
	account := s.accounts[ownerID]
	provider := s.providers[account.Provider]
	status := s.statuses[ownerID]
	wasReady := provider != nil && provider.Ready(account) && (status == nil || !status.contextInvalid)
	account.State = append(json.RawMessage(nil), state...)
	s.mu.Unlock()
	if err := saveAccount(ctx, s.Store, s.MasterKey, account); err != nil {
		return err
	}
	s.mu.Lock()
	if s.runtimes[ownerID] != runtime || s.accountGen[ownerID] != generation {
		s.mu.Unlock()
		return context.Canceled
	}
	s.accounts[ownerID] = account
	if status != nil && status.contextInvalid {
		candidateVersion := ""
		if provider != nil {
			candidateVersion = provider.ReadinessVersion(account)
		}
		if candidateVersion != "" && candidateVersion != status.invalidReadinessVersion {
			status.contextInvalid = false
			status.invalidReadinessVersion = ""
		}
	}
	isReady := provider != nil && provider.Ready(account) && (status == nil || !status.contextInvalid)
	if latest, ok := s.latestWaiting[ownerID]; ok && !wasReady && isReady {
		s.notification[ownerID] = notificationJob{waiting: latest}
		s.signalNotificationLocked()
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) runtimeCurrent(ownerID string, generation uint64, runtime *accountRuntime) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimes[ownerID] == runtime && s.accountGen[ownerID] == generation
}

func (s *Service) runtimeExited(ownerID string, generation uint64, runtime *accountRuntime, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimes[ownerID] != runtime || s.accountGen[ownerID] != generation {
		return
	}
	status := s.statuses[ownerID]
	if status == nil {
		status = &runtimeStatus{}
		s.statuses[ownerID] = status
	}
	if err == nil || errors.Is(err, context.Canceled) {
		status.workerState = "stopped"
		return
	}
	status.workerState = "error"
	status.lastError = "微信连接已停止，请重新连接"
	now := time.Now().UTC()
	status.lastErrorAt = &now
	if errors.Is(err, ErrReauthRequired) {
		status.reauthRequired = true
	}
	s.Logger.Warn("IM provider stopped", zap.String("owner_id", ownerID), zap.String("provider", s.accounts[ownerID].Provider), zap.Error(err))
}

func (s *Service) recordRuntimeError(ownerID string, generation uint64, runtime *accountRuntime, err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimes[ownerID] != runtime || s.accountGen[ownerID] != generation {
		return
	}
	status := s.statuses[ownerID]
	if status == nil {
		status = &runtimeStatus{}
		s.statuses[ownerID] = status
	}
	status.lastError = "微信网络请求失败，服务会自动重试"
	now := time.Now().UTC()
	status.lastErrorAt = &now
}

func (s *Service) detachOwnerLocked(ownerID string) (Account, bool, *accountRuntime) {
	s.accountGen[ownerID]++
	account, ok := s.accounts[ownerID]
	runtime := s.runtimes[ownerID]
	delete(s.accounts, ownerID)
	delete(s.runtimes, ownerID)
	delete(s.statuses, ownerID)
	delete(s.selected, ownerID)
	delete(s.notification, ownerID)
	delete(s.latestWaiting, ownerID)
	return account, ok, runtime
}

func (s *Service) stopRuntime(runtime *accountRuntime) {
	if runtime == nil {
		return
	}
	runtime.cancel()
	runtime.barrier.Lock()
	runtime.barrier.Unlock()
	waitRuntime(runtime, s.Logger)
}

func waitRuntime(runtime *accountRuntime, logger *zap.Logger) {
	if runtime == nil {
		return
	}
	select {
	case <-runtime.done:
	case <-time.After(8 * time.Second):
		logger.Warn("IM provider stop timed out", zap.String("owner_id", runtime.ownerID))
	}
}

func (s *Service) statusLocked(ownerID string) ConnectionStatus {
	account, connected := s.accounts[ownerID]
	status := ConnectionStatus{Provider: ProviderClawBot, Connected: connected, WorkerState: "disconnected"}
	if !connected {
		return status
	}
	status.Provider = account.Provider
	status.ExternalBotID = account.ExternalBotID
	connectedAt := account.ConnectedAt.UTC()
	status.ConnectedAt = &connectedAt
	if provider := s.providers[account.Provider]; provider != nil {
		status.Ready = provider.Ready(account)
	}
	if runtimeStatus := s.statuses[ownerID]; runtimeStatus != nil {
		status.Ready = status.Ready && !runtimeStatus.contextInvalid
		status.WorkerState = runtimeStatus.workerState
		status.ReauthRequired = runtimeStatus.reauthRequired
		status.LastInboundAt = runtimeStatus.lastInboundAt
		status.LastOutboundAt = runtimeStatus.lastOutboundAt
		status.LastError = runtimeStatus.lastError
		status.LastErrorAt = runtimeStatus.lastErrorAt
	}
	return status
}

func (s *Service) loginViewLocked(session *loginSession) LoginView {
	view := LoginView{
		SessionID: session.id, State: session.state, Message: session.message,
		QRCodeURL: session.challenge.QRCodeURL, ExpiresAt: session.challenge.ExpiresAt,
	}
	status := s.statusLocked(session.ownerID)
	view.Status = &status
	return view
}

func (s *Service) clearLoginPolling(sessionID string, generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.logins[sessionID]; session != nil && session.generation == generation {
		session.polling = false
	}
}

func (s *Service) ownerOperation(ownerID string) *sync.Mutex {
	value, _ := s.ownerOps.LoadOrStore(ownerID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Service) requireActiveOwner(ctx context.Context, ownerID string) error {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || s.Store == nil {
		return ErrOwnerInactive
	}
	user, err := s.Store.GetUser(ctx, ownerID)
	if err != nil {
		return ErrOwnerInactive
	}
	if !user.IsActive {
		return ErrOwnerInactive
	}
	return nil
}

func (s *Service) signalNotificationLocked() {
	select {
	case s.notifyWake <- struct{}{}:
	default:
	}
}

func sortedPending(items []*turnsvc.PendingTurn) []*turnsvc.PendingTurn {
	filtered := make([]*turnsvc.PendingTurn, 0, len(items))
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.ConversationID) != "" && strings.TrimSpace(item.RequestID) != "" {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt.Before(filtered[j].CreatedAt) })
	return filtered
}
