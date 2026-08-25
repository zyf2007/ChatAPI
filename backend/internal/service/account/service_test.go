package account

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/auth"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func TestInactiveUpdateAndDeleteRevokeIMOwnerFirst(t *testing.T) {
	t.Parallel()
	t.Run("inactive update", func(t *testing.T) {
		store := &revocationStore{}
		service := NewService(store)
		service.SetOwnerRevoker(func(_ context.Context, ownerID string) error {
			store.events = append(store.events, "revoke:"+ownerID)
			return nil
		})
		_, err := service.UpdateUser(context.Background(), UpdateUserInput{ID: "owner-1", PasswordHash: "hash", IsActive: false})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(store.events, []string{"revoke:owner-1", "update:owner-1"}) {
			t.Fatalf("events = %#v", store.events)
		}
	})
	t.Run("delete", func(t *testing.T) {
		store := &revocationStore{}
		service := NewService(store)
		service.SetOwnerRevoker(func(_ context.Context, ownerID string) error {
			store.events = append(store.events, "revoke:"+ownerID)
			return nil
		})
		if err := service.DeleteUser(context.Background(), "owner-1"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(store.events, []string{"revoke:owner-1", "delete:owner-1"}) {
			t.Fatalf("events = %#v", store.events)
		}
	})
}

func TestRevocationFailureBlocksInactiveUpdate(t *testing.T) {
	t.Parallel()
	store := &revocationStore{}
	service := NewService(store)
	want := errors.New("revoke failed")
	service.SetOwnerRevoker(func(context.Context, string) error { return want })
	_, err := service.UpdateUser(context.Background(), UpdateUserInput{ID: "owner-1", PasswordHash: "hash", IsActive: false})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if len(store.events) != 0 {
		t.Fatalf("store mutated after failed revoke: %#v", store.events)
	}
}

type revocationStore struct {
	auth.Store
	events []string
}

func (s *revocationStore) UpdateUser(_ context.Context, input common.UpdateUserInput) (common.User, error) {
	s.events = append(s.events, "update:"+input.ID)
	return common.User{ID: input.ID, IsActive: input.IsActive}, nil
}

func (s *revocationStore) DeleteUserAccount(_ context.Context, userID string) error {
	s.events = append(s.events, "delete:"+userID)
	return nil
}
