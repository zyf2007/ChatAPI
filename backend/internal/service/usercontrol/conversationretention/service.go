package conversationretention

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	"go.uber.org/zap"
)

const LimitSettingKey = "user_conversation_limit"

type UserLister interface {
	ListUsers(context.Context) ([]common.User, error)
}

type Pruner interface {
	PruneConversations(context.Context, string, int) (common.DeleteConversationsResult, int, error)
}

type Service struct {
	mode   config.Mode
	users  UserLister
	pruner Pruner
	limit  func(context.Context) int
	logger *zap.Logger
}

func New(mode config.Mode, users UserLister, pruner Pruner, limit func(context.Context) int, logger *zap.Logger) *Service {
	return &Service{mode: mode, users: users, pruner: pruner, limit: limit, logger: logger}
}

func (s *Service) Enforce(ctx context.Context, ownerID string) {
	_ = s.enforce(ctx, ownerID)
}

func (s *Service) enforce(ctx context.Context, ownerID string) error {
	if s == nil || s.pruner == nil || s.limit == nil {
		return nil
	}
	limit := s.limit(ctx)
	if limit <= 0 {
		return nil
	}
	if _, _, err := s.pruner.PruneConversations(ctx, ownerID, limit); err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", ownerID), zap.Int("conversation.limit", limit)).Warn("failed to enforce conversation retention", zap.Error(err))
		return err
	}
	return nil
}

func (s *Service) SettingsUpdated(ctx context.Context, changedKeys []string) error {
	if s == nil || !slices.Contains(changedKeys, LimitSettingKey) {
		return nil
	}
	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if s.mode == config.ModeLab {
		return s.enforce(reconcileCtx, lab.DefaultOwnerID)
	}
	if s.users == nil {
		return nil
	}
	users, err := s.users.ListUsers(reconcileCtx)
	if err != nil {
		logging.BindContext(s.logger, reconcileCtx).Warn("failed to list users for conversation retention reconciliation", zap.Error(err))
		return fmt.Errorf("list users for conversation retention: %w", err)
	}
	var reconcileErrors []error
	for _, user := range users {
		if err := s.enforce(reconcileCtx, user.ID); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("user %s: %w", user.ID, err))
		}
	}
	return errors.Join(reconcileErrors...)
}
