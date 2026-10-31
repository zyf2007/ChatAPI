package conversationretention

import (
	"context"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
)

type fakeUsers struct{ items []common.User }

func (f fakeUsers) ListUsers(context.Context) ([]common.User, error) { return f.items, nil }

type recordingPruner struct{ calls []string }

func (p *recordingPruner) PruneConversations(_ context.Context, ownerID string, keep int) (common.DeleteConversationsResult, int, error) {
	p.calls = append(p.calls, ownerID)
	return common.DeleteConversationsResult{}, keep, nil
}

func TestSettingsUpdatedOnlyReconcilesConversationLimit(t *testing.T) {
	pruner := &recordingPruner{}
	service := New(config.ModeServe, fakeUsers{items: []common.User{{ID: "a"}, {ID: "b"}}}, pruner, func(context.Context) int { return 30 }, nil)

	if err := service.SettingsUpdated(context.Background(), []string{"global_rate_limit_requests"}); err != nil {
		t.Fatal(err)
	}
	if len(pruner.calls) != 0 {
		t.Fatalf("unrelated setting triggered reconciliation: %v", pruner.calls)
	}
	if err := service.SettingsUpdated(context.Background(), []string{LimitSettingKey}); err != nil {
		t.Fatal(err)
	}
	if len(pruner.calls) != 2 || pruner.calls[0] != "a" || pruner.calls[1] != "b" {
		t.Fatalf("unexpected reconciliation calls: %v", pruner.calls)
	}
}

func TestEnforceSkipsUnlimitedPolicy(t *testing.T) {
	pruner := &recordingPruner{}
	service := New(config.ModeServe, nil, pruner, func(context.Context) int { return 0 }, nil)
	service.Enforce(context.Background(), "a")
	if len(pruner.calls) != 0 {
		t.Fatalf("unlimited policy triggered prune: %v", pruner.calls)
	}
}

func TestLabSettingsReconcileWorkspaceWithoutEnumeratingServeUsers(t *testing.T) {
	pruner := &recordingPruner{}
	service := New(config.ModeLab, fakeUsers{items: []common.User{{ID: "serve-user"}}}, pruner, func(context.Context) int { return 5 }, nil)
	if err := service.SettingsUpdated(context.Background(), []string{LimitSettingKey}); err != nil {
		t.Fatal(err)
	}
	if len(pruner.calls) != 1 || pruner.calls[0] != lab.DefaultOwnerID {
		t.Fatalf("wrong retention owners: %v", pruner.calls)
	}
}
