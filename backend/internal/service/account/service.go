package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/platform/password"
	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

var (
	ErrUserExists    = errors.New("user already exists")
	ErrEmailRequired = errors.New("email is required")
)

type Service struct {
	store       auth.Store
	now         func() time.Time
	revokeOwner func(context.Context, string) error
}

type CreateUserInput struct {
	ID           string
	Username     string
	Email        string
	Password     string
	PasswordHash string
	Role         string
	IsActive     bool
	LocalAdmin   bool
	LastLoginAt  *time.Time
}

type UpdateUserInput struct {
	ID           string
	Username     string
	Email        string
	Password     string
	PasswordHash string
	Role         string
	IsActive     bool
	LocalAdmin   bool
	LastLoginAt  *time.Time
}

type UserPage struct {
	Items    []common.User
	Page     int
	PageSize int
	Total    int
}

func NewService(dataStore auth.Store) *Service {
	return &Service{
		store: dataStore,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetOwnerRevoker(revoke func(context.Context, string) error) {
	if s != nil {
		s.revokeOwner = revoke
	}
}

func (s *Service) GetUser(ctx context.Context, userID string) (common.User, error) {
	return s.store.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) GetUserByEmail(ctx context.Context, email string) (common.User, error) {
	return s.store.GetUserByEmail(ctx, normalizeEmail(email))
}

func (s *Service) GetUserByUsername(ctx context.Context, username string) (common.User, error) {
	return s.store.GetUserByUsername(ctx, strings.TrimSpace(username))
}

func (s *Service) LookupUserByIdentifier(ctx context.Context, identifier string) (common.User, error) {
	identifier = strings.TrimSpace(identifier)
	if strings.Contains(identifier, "@") {
		return s.GetUserByEmail(ctx, identifier)
	}
	return s.GetUserByUsername(ctx, identifier)
}

func (s *Service) ListUsers(ctx context.Context) ([]common.User, error) {
	return s.store.ListUsers(ctx)
}

func (s *Service) ListUsersPage(ctx context.Context, page int, pageSize int, query string) (UserPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := s.store.ListUsersPage(ctx, (page-1)*pageSize, pageSize, strings.TrimSpace(query))
	if err != nil {
		return UserPage{}, err
	}
	return UserPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (common.User, error) {
	email := normalizeEmail(input.Email)
	username := strings.TrimSpace(input.Username)
	if email != "" {
		if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
			return common.User{}, ErrUserExists
		} else if !errors.Is(err, common.ErrNotFound) {
			return common.User{}, err
		}
	}
	if username != "" {
		if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
			return common.User{}, ErrUserExists
		} else if !errors.Is(err, common.ErrNotFound) {
			return common.User{}, err
		}
	}
	passwordHash, err := resolvePasswordHash(strings.TrimSpace(input.Password), strings.TrimSpace(input.PasswordHash))
	if err != nil {
		return common.User{}, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "user_" + uuid.NewString()
	}
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	return s.store.CreateUser(ctx, common.CreateUserInput{
		ID:           id,
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         role,
		IsActive:     input.IsActive,
		LocalAdmin:   input.LocalAdmin,
	})
}

func (s *Service) UpdateUser(ctx context.Context, input UpdateUserInput) (common.User, error) {
	userID := strings.TrimSpace(input.ID)
	passwordHash, err := resolvePasswordHash(strings.TrimSpace(input.Password), strings.TrimSpace(input.PasswordHash))
	if err != nil {
		return common.User{}, err
	}
	if !input.IsActive && s.revokeOwner != nil {
		if err := s.revokeOwner(ctx, userID); err != nil {
			return common.User{}, err
		}
	}
	return s.store.UpdateUser(ctx, common.UpdateUserInput{
		ID:           userID,
		Username:     strings.TrimSpace(input.Username),
		Email:        normalizeEmail(input.Email),
		PasswordHash: passwordHash,
		Role:         strings.TrimSpace(input.Role),
		IsActive:     input.IsActive,
		LocalAdmin:   input.LocalAdmin,
		LastLoginAt:  input.LastLoginAt,
	})
}

func (s *Service) SetUserState(ctx context.Context, userID string, isActive bool) (common.User, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return common.User{}, err
	}
	return s.UpdateUser(ctx, UpdateUserInput{
		ID:           user.ID,
		Username:     user.Username,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		IsActive:     isActive,
		LocalAdmin:   user.LocalAdmin,
		LastLoginAt:  user.LastLoginAt,
	})
}

func (s *Service) SetPassword(ctx context.Context, userID string, newPassword string) (common.User, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return common.User{}, err
	}
	return s.UpdateUser(ctx, UpdateUserInput{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Password:    strings.TrimSpace(newPassword),
		Role:        user.Role,
		IsActive:    user.IsActive,
		LocalAdmin:  user.LocalAdmin,
		LastLoginAt: user.LastLoginAt,
	})
}

func (s *Service) PreviewDeletion(ctx context.Context, userID string) (common.UserDeletionPreview, error) {
	return s.store.PreviewUserDeletion(ctx, strings.TrimSpace(userID))
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if s.revokeOwner != nil {
		if err := s.revokeOwner(ctx, userID); err != nil {
			return err
		}
	}
	return s.store.DeleteUserAccount(ctx, userID)
}

func (s *Service) TransferOwnership(ctx context.Context, sourceUserID string, targetUserID string) (common.UserOwnershipTransferResult, error) {
	return s.store.TransferUserOwnership(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID))
}

func (s *Service) TransferOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (common.UserOwnershipTransferResult, error) {
	return s.store.TransferUserOwnershipSelection(ctx, strings.TrimSpace(sourceUserID), strings.TrimSpace(targetUserID), conversationIDs, filenames)
}

func (s *Service) GetUserIdentity(ctx context.Context, provider string, subject string) (common.UserIdentity, error) {
	return s.store.GetUserIdentity(ctx, strings.TrimSpace(provider), strings.TrimSpace(subject))
}

func (s *Service) ResolveIdentity(ctx context.Context, provider string, subject string) (common.User, common.UserIdentity, error) {
	identity, err := s.GetUserIdentity(ctx, provider, subject)
	if err != nil {
		return common.User{}, common.UserIdentity{}, err
	}
	user, err := s.GetUser(ctx, identity.UserID)
	if err != nil {
		return common.User{}, common.UserIdentity{}, err
	}
	return user, identity, nil
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]common.UserIdentity, error) {
	return s.store.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) UpsertIdentity(ctx context.Context, input common.UpsertUserIdentityInput) (common.UserIdentity, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Subject = strings.TrimSpace(input.Subject)
	input.UserID = strings.TrimSpace(input.UserID)
	input.Email = normalizeEmail(input.Email)
	return s.store.UpsertUserIdentity(ctx, input)
}

func (s *Service) DeleteUserIdentity(ctx context.Context, identityID string, userID string) error {
	return s.store.DeleteUserIdentity(ctx, strings.TrimSpace(identityID), strings.TrimSpace(userID))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func resolvePasswordHash(passwordText string, existingHash string) (string, error) {
	if passwordText == "" {
		return existingHash, nil
	}
	hash, err := password.Hash(passwordText)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}
