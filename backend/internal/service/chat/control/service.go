package control

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	"go.uber.org/zap"
)

type Query interface {
	ListMessagesForOwner(context.Context, string, string) ([]common.Message, error)
}

type Turn interface {
	ExecuteTurnControl(context.Context, turnsvc.TurnControlCommand) (map[string]any, error)
	ActiveRequestID(string) (string, bool)
}

type Service struct {
	Query     Query
	Turn      Turn
	Logger    *zap.Logger
	Observers []Observer
	locksMu   sync.Mutex
	locks     map[string]*conversationLock
}

type CommandSource string

const (
	SourceAPI        CommandSource = "api"
	SourceWorkspace  CommandSource = "workspace"
	SourceAutomation CommandSource = "automation"
	SourceAdmin      CommandSource = "admin"
	SourceLab        CommandSource = "lab"
	SourceIM         CommandSource = "im"
)

type AppliedCommand struct {
	Command Command
	Result  Result
}

type Observer interface {
	ControlApplied(context.Context, AppliedCommand)
}

type conversationLock struct {
	mu   sync.Mutex
	refs int
}

type Command struct {
	OwnerID        string
	ConversationID string
	ResponseID     string
	RequestID      string
	Source         CommandSource
	Action         turnsvc.OutputAction
}

type Result struct {
	Body map[string]any
}

type ErrorKind string

const (
	ErrorKindInvalid     ErrorKind = "invalid"
	ErrorKindForbidden   ErrorKind = "forbidden"
	ErrorKindNotFound    ErrorKind = "not_found"
	ErrorKindConflict    ErrorKind = "conflict"
	ErrorKindUnavailable ErrorKind = "unavailable"
	ErrorKindInternal    ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(query Query, turn Turn, logger *zap.Logger) *Service {
	return &Service{Query: query, Turn: turn, Logger: logger}
}

func (s *Service) Subscribe(observer Observer) {
	if s == nil || observer == nil {
		return
	}
	s.Observers = append(s.Observers, observer)
}

func (s *Service) Execute(ctx context.Context, command Command) (Result, error) {
	if strings.TrimSpace(command.RequestID) == "" && s.Turn != nil {
		if requestID, ok := s.Turn.ActiveRequestID(strings.TrimSpace(command.ConversationID)); ok {
			command.RequestID = requestID
		}
	}
	turnCommand := command.TurnCommand()
	if err := turnCommand.Validate(); err != nil {
		return Result{}, s.controlError(ctx, turnCommand, err, ErrorKindInvalid, "invalid_turn_control_command")
	}
	ownerID := strings.TrimSpace(command.OwnerID)
	if ownerID != "" && s.Query != nil {
		if _, err := s.Query.ListMessagesForOwner(ctx, strings.TrimSpace(turnCommand.ConversationID), ownerID); err != nil {
			kind := ErrorKindNotFound
			code := "turn_control_conversation_not_found"
			if errors.Is(err, turnquerysvc.ErrForbidden) {
				kind = ErrorKindForbidden
				code = "turn_control_forbidden"
			}
			return Result{}, s.controlError(ctx, turnCommand, err, kind, code)
		}
	}
	if s.Turn == nil {
		return Result{}, s.controlError(ctx, turnCommand, errors.New("turn control executor unavailable"), ErrorKindUnavailable, "turn_control_unavailable")
	}
	unlock := s.lockConversation(turnCommand.ConversationID)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	body, err := s.Turn.ExecuteTurnControl(ctx, turnCommand)
	if err != nil {
		return Result{}, s.mapExecutionError(ctx, turnCommand, err)
	}
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", ownerID),
		zap.String("conversation.id", turnCommand.ConversationID),
		zap.String("turn.control.kind", string(turnCommand.Action.Kind)),
	).Info("turn control executed")
	result := Result{Body: body}
	for _, observer := range s.Observers {
		if observer != nil {
			observer.ControlApplied(ctx, AppliedCommand{Command: command, Result: result})
		}
	}
	return result, nil
}

// Synchronize runs fn after all earlier commands for the conversation and before later commands.
func (s *Service) Synchronize(ctx context.Context, conversationID string, fn func() error) error {
	if fn == nil {
		return nil
	}
	unlock := s.lockConversation(conversationID)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func (s *Service) lockConversation(conversationID string) func() {
	conversationID = strings.TrimSpace(conversationID)
	s.locksMu.Lock()
	if s.locks == nil {
		s.locks = map[string]*conversationLock{}
	}
	lock := s.locks[conversationID]
	if lock == nil {
		lock = &conversationLock{}
		s.locks[conversationID] = lock
	}
	lock.refs++
	s.locksMu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.locksMu.Lock()
		lock.refs--
		if lock.refs == 0 && s.locks[conversationID] == lock {
			delete(s.locks, conversationID)
		}
		s.locksMu.Unlock()
	}
}

func (s *Service) mapExecutionError(ctx context.Context, command turnsvc.TurnControlCommand, err error) error {
	switch {
	case errors.Is(err, pendingsvc.ErrPendingConflict), errors.Is(err, common.ErrTurnConflict):
		return s.controlError(ctx, command, err, ErrorKindConflict, "turn_control_conflict")
	case errors.Is(err, pendingsvc.ErrPendingNotFound), errors.Is(err, common.ErrNotFound):
		return s.controlError(ctx, command, err, ErrorKindNotFound, "turn_control_not_found")
	default:
		return s.controlError(ctx, command, err, ErrorKindInternal, "turn_control_failed")
	}
}

func (s *Service) controlError(ctx context.Context, command turnsvc.TurnControlCommand, err error, kind ErrorKind, code string) error {
	level := logging.BindContext(s.Logger, ctx,
		zap.String("conversation.id", command.ConversationID),
		zap.String("turn.control.kind", string(command.Action.Kind)),
		zap.String("error.kind", string(kind)),
		zap.String("error.code", code),
	)
	if kind == ErrorKindInternal || kind == ErrorKindUnavailable {
		level.Error("turn control failed", zap.Error(err))
	} else {
		level.Warn("turn control rejected", zap.Error(err))
	}
	message := code
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return &Error{Kind: kind, Code: code, Message: message, Err: err}
}

func (c Command) TurnCommand() turnsvc.TurnControlCommand {
	return turnsvc.TurnControlCommand{
		ConversationID: strings.TrimSpace(c.ConversationID),
		ResponseID:     strings.TrimSpace(c.ResponseID),
		RequestID:      strings.TrimSpace(c.RequestID),
		Action:         c.Action.Normalized(),
	}
}
