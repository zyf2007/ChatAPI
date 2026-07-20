package storetest

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/repositorycontract"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
)

type NewStoreFunc func(t *testing.T) repositorycontract.Store

const (
	httpStatusOK        = 200
	httpStatusForbidden = 403
)

func RunUserRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("users", func(t *testing.T) {
		testUserRepositoryCreatesUpdatesAndListsUsers(t, newStore)
	})
	t.Run("user_delete_preview_and_purge", func(t *testing.T) {
		testUserRepositoryPreviewsAndDeletesUserAccount(t, newStore)
	})
	t.Run("user_ownership_transfer", func(t *testing.T) {
		testUserRepositoryTransfersOwnership(t, newStore)
	})
	t.Run("user_ownership_transfer_selection", func(t *testing.T) {
		testUserRepositoryTransfersOwnershipSelection(t, newStore)
	})
	t.Run("user_identities", func(t *testing.T) {
		testUserIdentityRepositoryUpsertsByProviderSubject(t, newStore)
	})
}

func RunConfigRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("system_config", func(t *testing.T) {
		testConfigRepositoryUpsertsListsAndDeletesSystemConfig(t, newStore)
	})
	t.Run("user_config", func(t *testing.T) {
		testConfigRepositoryUpsertsListsAndDeletesUserConfig(t, newStore)
	})
}

func RunAuthRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("auth_verification_codes", func(t *testing.T) {
		testAuthVerificationCodeRepositoryDeletesExpiredCodes(t, newStore)
	})
}

func RunAPIKeyRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("app_api_keys", func(t *testing.T) {
		testAppAPIKeyRepositoryCreatesListsUsesAndRevokes(t, newStore)
	})
	t.Run("app_api_key_audit_logs", func(t *testing.T) {
		testAppAPIKeyRepositoryAuditsRequests(t, newStore)
	})
	t.Run("model_api_keys", func(t *testing.T) {
		testModelAPIKeyRepositoryCreatesListsUsesAndRevokes(t, newStore)
	})
}

func RunAuditRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("audit_logs", func(t *testing.T) {
		testAuditRepositoryCreatesFiltersAndLimitsLogs(t, newStore)
	})
}

func RunAutomationRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("automation_rules", func(t *testing.T) {
		testAutomationRuleRepositoryCRUD(t, newStore)
	})
}

func RunStorageRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("uploaded_images", func(t *testing.T) {
		testStorageRepositoryCreatesListsAndDeletesUploadedImages(t, newStore)
	})
	t.Run("storage_file_deletion_failures", func(t *testing.T) {
		testStorageRepositoryUpsertsListsAndDeletesDeletionFailures(t, newStore)
	})
	t.Run("storage_user_quotas", func(t *testing.T) {
		testStorageRepositorySetsListsAndDeletesUserQuotas(t, newStore)
	})
}

func RunConversationRepositoryTests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	t.Run("pending_turn_lifecycle", func(t *testing.T) {
		testConversationRepositoryPendingTurnLifecycle(t, newStore)
	})
	t.Run("conversation_events", func(t *testing.T) {
		testConversationRepositoryConversationEvents(t, newStore)
	})
	t.Run("owner_conversation_pages", func(t *testing.T) {
		testConversationRepositoryOwnerPages(t, newStore)
	})
}

func testConversationRepositoryOwnerPages(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	for index, ownerID := range []string{"owner-a", "owner-b", "owner-a", "owner-a"} {
		_, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
			ConversationID: "page-conversation-" + strconv.Itoa(index),
			RequestID:      "page-request-" + strconv.Itoa(index),
			ResponseID:     "page-response-" + strconv.Itoa(index),
			OwnerID:        ownerID,
			RequestFormat:  "responses",
			UserContent:    "page test",
		})
		if err != nil {
			t.Fatalf("create paged conversation %d: %v", index, err)
		}
		time.Sleep(time.Millisecond)
	}
	first, err := st.ListConversationsForOwnerPage(ctx, "owner-a", time.Time{}, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || !first[0].UpdatedAt.After(first[1].UpdatedAt) {
		t.Fatalf("unexpected first owner page: %#v", first)
	}
	second, err := st.ListConversationsForOwnerPage(ctx, "owner-a", first[1].UpdatedAt, first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID == first[0].ID || second[0].ID == first[1].ID {
		t.Fatalf("unexpected second owner page: %#v", second)
	}
}

func testUserRepositoryCreatesUpdatesAndListsUsers(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	alice, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_alice",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash-1",
		Role:         "admin",
		IsActive:     true,
		LocalAdmin:   true,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if alice.Role != "admin" || !alice.IsActive || !alice.LocalAdmin || alice.CreatedAt.IsZero() {
		t.Fatalf("unexpected alice: %#v", alice)
	}

	bob, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_bob",
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: "hash-2",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if bob.Role != "user" {
		t.Fatalf("empty role should default to user: %#v", bob)
	}

	byEmail, err := st.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if byEmail.ID != alice.ID || byEmail.PasswordHash != "hash-1" {
		t.Fatalf("unexpected user by email: %#v", byEmail)
	}
	byUsername, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if byUsername.ID != alice.ID || byUsername.Email != "alice@example.com" {
		t.Fatalf("unexpected user by username: %#v", byUsername)
	}

	lastLogin := time.Date(2026, 7, 6, 1, 2, 3, 0, time.UTC)
	updated, err := st.UpdateUser(ctx, common.UpdateUserInput{
		ID:           alice.ID,
		Username:     "alice2",
		Email:        "alice2@example.com",
		PasswordHash: "hash-3",
		Role:         "user",
		IsActive:     false,
		LocalAdmin:   false,
		LastLoginAt:  &lastLogin,
	})
	if err != nil {
		t.Fatalf("update alice: %v", err)
	}
	if updated.Username != "alice2" || updated.Email != "alice2@example.com" || updated.IsActive || updated.LocalAdmin {
		t.Fatalf("unexpected updated alice: %#v", updated)
	}
	if updated.LastLoginAt == nil || !updated.LastLoginAt.Equal(lastLogin) {
		t.Fatalf("unexpected last login: %#v", updated.LastLoginAt)
	}

	items, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two users, got %#v", items)
	}
	firstPage, total, err := st.ListUsersPage(ctx, 0, 1)
	if err != nil {
		t.Fatalf("list first user page: %v", err)
	}
	if len(firstPage) != 1 || total != 2 {
		t.Fatalf("unexpected first user page: items=%#v total=%d", firstPage, total)
	}
	secondPage, total, err := st.ListUsersPage(ctx, 1, 1)
	if err != nil {
		t.Fatalf("list second user page: %v", err)
	}
	if len(secondPage) != 1 || total != 2 || secondPage[0].ID == firstPage[0].ID {
		t.Fatalf("unexpected second user page: items=%#v total=%d", secondPage, total)
	}

	if _, err := st.GetUser(ctx, "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing user, got %v", err)
	}
	if _, err := st.GetUserByUsername(ctx, "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing username, got %v", err)
	}
}

func testUserRepositoryPreviewsAndDeletesUserAccount(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	if _, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_delete_blocked",
		Username:     "blocked",
		Email:        "blocked@example.com",
		PasswordHash: "hash-blocked",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create blocked user: %v", err)
	}
	if _, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_delete_blocked",
		RequestID:      "req_delete_blocked",
		ResponseID:     "resp_delete_blocked",
		OwnerID:        "user_delete_blocked",
		RequestFormat:  "responses",
		Model:          "delete-test",
		UserContent:    "blocked",
		RequestBody:    map[string]any{"model": "delete-test"},
	}); err != nil {
		t.Fatalf("create blocked conversation: %v", err)
	}

	blockedPreview, err := st.PreviewUserDeletion(ctx, "user_delete_blocked")
	if err != nil {
		t.Fatalf("preview blocked user deletion: %v", err)
	}
	if blockedPreview.CanDelete || blockedPreview.Counts.OwnedConversations != 1 {
		t.Fatalf("expected blocked preview with owned conversation: %#v", blockedPreview)
	}
	if err := st.DeleteUserAccount(ctx, "user_delete_blocked"); !errors.Is(err, common.ErrTurnConflict) {
		t.Fatalf("expected blocked delete conflict, got %v", err)
	}

	if _, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_delete_ready",
		Username:     "ready",
		Email:        "ready@example.com",
		PasswordHash: "hash-ready",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create ready user: %v", err)
	}
	if _, err := st.UpsertUserIdentity(ctx, common.UpsertUserIdentityInput{
		ID:       "identity_delete_ready",
		UserID:   "user_delete_ready",
		Provider: "oidc",
		Subject:  "delete-ready",
		Email:    "ready@example.com",
	}); err != nil {
		t.Fatalf("create ready identity: %v", err)
	}
	if _, err := st.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: "user_delete_ready",
		Key:    "workspace",
		Value:  map[string]any{"theme": "dark"},
	}); err != nil {
		t.Fatalf("set ready config: %v", err)
	}
	if _, err := st.UpsertAutomationRule(ctx, common.UpsertAutomationRuleInput{
		ID:      "rule_delete_ready",
		UserID:  "user_delete_ready",
		Enabled: true,
		Payload: map[string]any{"match": "x"},
	}); err != nil {
		t.Fatalf("set ready automation rule: %v", err)
	}
	if _, err := st.CreateAppAPIKey(ctx, common.CreateAppAPIKeyInput{
		ID:        "app_key_delete_ready",
		UserID:    "user_delete_ready",
		Name:      "ready-app",
		KeyHash:   "hash",
		KeyPrefix: "capi_ready",
		Scopes:    []string{"requests:read"},
	}); err != nil {
		t.Fatalf("create ready app api key: %v", err)
	}
	if err := st.CreateAppAPIKeyAuditLog(ctx, common.AppAPIKeyAuditLog{
		ID:          "app_audit_delete_ready",
		AppAPIKeyID: "app_key_delete_ready",
		UserID:      "user_delete_ready",
		Route:       "/api/app/requests",
		StatusCode:  httpStatusOK,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create ready app api key audit log: %v", err)
	}
	if _, err := st.CreateModelAPIKey(ctx, common.CreateModelAPIKeyInput{
		ID:            "model_key_delete_ready",
		UserID:        "user_delete_ready",
		Name:          "ready-model",
		KeyCiphertext: "ciphertext",
		KeyPrefix:     "vk_ready",
		Model:         "gpt-test",
	}); err != nil {
		t.Fatalf("create ready model api key: %v", err)
	}
	if _, err := st.SetStorageUserQuota(ctx, "user_delete_ready", 1024); err != nil {
		t.Fatalf("set ready quota: %v", err)
	}
	if _, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/ready.png",
		Filename:  "ready.png",
		OwnerID:   "user_delete_ready",
		Bytes:     55,
		LastError: "busy",
	}); err != nil {
		t.Fatalf("set ready deletion failure: %v", err)
	}
	if _, err := st.CreateAuditLog(ctx, common.CreateAuditLogInput{
		ID:           "audit_delete_ready_actor",
		ActorUserID:  "user_delete_ready",
		ActorRole:    "user",
		ActorSource:  "session",
		EventType:    "user.action",
		ResourceType: "user",
		ResourceID:   "user_delete_ready",
		Action:       "demo",
		Outcome:      "success",
		Metadata:     map[string]any{"user_id": "user_delete_ready"},
	}); err != nil {
		t.Fatalf("create ready actor audit log: %v", err)
	}
	if _, err := st.CreateAuditLog(ctx, common.CreateAuditLogInput{
		ID:           "audit_delete_ready_ref",
		ActorUserID:  "admin",
		ActorRole:    "admin",
		ActorSource:  "session",
		EventType:    "admin.user",
		ResourceType: "user",
		ResourceID:   "user_delete_ready",
		Action:       "review",
		Outcome:      "success",
		Metadata:     map[string]any{"user_id": "user_delete_ready"},
	}); err != nil {
		t.Fatalf("create ready metadata audit log: %v", err)
	}

	preview, err := st.PreviewUserDeletion(ctx, "user_delete_ready")
	if err != nil {
		t.Fatalf("preview ready user deletion: %v", err)
	}
	if !preview.CanDelete {
		t.Fatalf("expected ready preview to allow deletion: %#v", preview)
	}
	if preview.Counts.Identities != 1 || preview.Counts.UserConfigs != 1 || preview.Counts.AutomationRules != 1 ||
		preview.Counts.AppAPIKeys != 1 || preview.Counts.AppAPIKeyAuditLogs != 1 || preview.Counts.ModelAPIKeys != 1 ||
		preview.Counts.StorageUserQuotas != 1 || preview.Counts.StorageDeletionFailures != 1 ||
		preview.Counts.AuditActorLogs != 1 || preview.Counts.AuditMetadataUserReferences != 2 {
		t.Fatalf("unexpected ready deletion preview counts: %#v", preview)
	}

	if err := st.DeleteUserAccount(ctx, "user_delete_ready"); err != nil {
		t.Fatalf("delete ready user account: %v", err)
	}
	if _, err := st.GetUser(ctx, "user_delete_ready"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected deleted user to be missing, got %v", err)
	}
	if identities, err := st.ListUserIdentities(ctx, "user_delete_ready"); err != nil || len(identities) != 0 {
		t.Fatalf("expected deleted identities to be gone, identities=%#v err=%v", identities, err)
	}
	if configs, err := st.ListUserConfigs(ctx, "user_delete_ready"); err != nil || len(configs) != 0 {
		t.Fatalf("expected deleted user configs to be gone, configs=%#v err=%v", configs, err)
	}
	if rules, err := st.ListAutomationRulesByUser(ctx, "user_delete_ready"); err != nil || len(rules) != 0 {
		t.Fatalf("expected deleted automation rules to be gone, rules=%#v err=%v", rules, err)
	}
	if keys, err := st.ListAppAPIKeysByUser(ctx, "user_delete_ready"); err != nil || len(keys) != 0 {
		t.Fatalf("expected deleted app api keys to be gone, keys=%#v err=%v", keys, err)
	}
	if keys, err := st.ListModelAPIKeysByUser(ctx, "user_delete_ready"); err != nil || len(keys) != 0 {
		t.Fatalf("expected deleted model api keys to be gone, keys=%#v err=%v", keys, err)
	}
	if _, err := st.GetStorageUserQuota(ctx, "user_delete_ready"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected deleted user quota to be missing, got %v", err)
	}
	failures, err := st.ListStorageFileDeletionFailures(ctx, 10)
	if err != nil {
		t.Fatalf("list storage deletion failures after delete: %v", err)
	}
	for _, item := range failures {
		if item.OwnerID == "user_delete_ready" {
			t.Fatalf("expected deletion failures for deleted user to be removed: %#v", failures)
		}
	}
	auditCount, err := st.CountAuditLogs(ctx, common.CountAuditLogsInput{ActorUserID: "user_delete_ready"})
	if err != nil {
		t.Fatalf("count actor audit logs after delete: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected actor audit logs to be preserved, got %d", auditCount)
	}
}

func testUserRepositoryTransfersOwnership(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	if _, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_transfer_source",
		Username:     "source",
		Email:        "source@example.com",
		PasswordHash: "hash-source",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create source user: %v", err)
	}
	if _, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:           "user_transfer_target",
		Username:     "target",
		Email:        "target@example.com",
		PasswordHash: "hash-target",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	if _, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_transfer_source",
		RequestID:      "req_transfer_source",
		ResponseID:     "resp_transfer_source",
		OwnerID:        "user_transfer_source",
		RequestFormat:  "responses",
		Model:          "transfer-test",
		UserContent:    "hello transfer",
		RequestBody:    map[string]any{"model": "transfer-test"},
	}); err != nil {
		t.Fatalf("create source conversation: %v", err)
	}
	if _, err := st.CreateUploadedImage(ctx, common.CreateUploadedImageInput{
		ID:               "img_transfer_source",
		OwnerID:          "user_transfer_source",
		Filename:         "transfer-source.png",
		OriginalFilename: "transfer-source.png",
		ContentType:      "image/png",
		Bytes:            128,
		URL:              "/api/uploads/imgs/transfer-source.png",
	}); err != nil {
		t.Fatalf("create source image: %v", err)
	}
	if _, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/transfer-source.png",
		Filename:  "transfer-source.png",
		OwnerID:   "user_transfer_source",
		Bytes:     128,
		LastError: "busy",
	}); err != nil {
		t.Fatalf("create source deletion failure: %v", err)
	}
	if _, err := st.SetStorageUserQuota(ctx, "user_transfer_source", 2048); err != nil {
		t.Fatalf("create source quota: %v", err)
	}

	beforePreview, err := st.PreviewUserDeletion(ctx, "user_transfer_source")
	if err != nil {
		t.Fatalf("preview source before transfer: %v", err)
	}
	if beforePreview.CanDelete || beforePreview.Counts.OwnedConversations != 1 || beforePreview.Counts.OwnedUploadedImages != 1 {
		t.Fatalf("expected ownership blockers before transfer: %#v", beforePreview)
	}

	result, err := st.TransferUserOwnership(ctx, "user_transfer_source", "user_transfer_target")
	if err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}
	if result.TransferredConversations != 1 || result.TransferredUploadedImages != 1 || result.TransferredDeletionFailures != 1 {
		t.Fatalf("unexpected ownership transfer counts: %#v", result)
	}
	if !result.SourceQuotaDeleted || !result.TargetQuotaCreatedFromSource || result.TargetQuotaPreserved {
		t.Fatalf("unexpected quota move result: %#v", result)
	}

	afterPreview, err := st.PreviewUserDeletion(ctx, "user_transfer_source")
	if err != nil {
		t.Fatalf("preview source after transfer: %v", err)
	}
	if !afterPreview.CanDelete || afterPreview.Counts.OwnedConversations != 0 || afterPreview.Counts.OwnedUploadedImages != 0 {
		t.Fatalf("expected source purge blockers to be cleared: %#v", afterPreview)
	}

	conversation, err := st.GetConversation(ctx, "conv_transfer_source")
	if err != nil {
		t.Fatalf("get transferred conversation: %v", err)
	}
	if conversation.Metadata["owner_id"] != "user_transfer_target" {
		t.Fatalf("expected conversation owner to move: %#v", conversation)
	}
	targetImages, err := st.ListUploadedImagesByOwner(ctx, "user_transfer_target")
	if err != nil {
		t.Fatalf("list target uploaded images: %v", err)
	}
	if len(targetImages) != 1 || targetImages[0].ID != "img_transfer_source" {
		t.Fatalf("expected image to move to target owner: %#v", targetImages)
	}
	failures, err := st.ListStorageFileDeletionFailures(ctx, 10)
	if err != nil {
		t.Fatalf("list deletion failures: %v", err)
	}
	if len(failures) != 1 || failures[0].OwnerID != "user_transfer_target" {
		t.Fatalf("expected deletion failure owner to move: %#v", failures)
	}
	if _, err := st.GetStorageUserQuota(ctx, "user_transfer_source"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected source quota to be removed, got %v", err)
	}
	targetQuota, err := st.GetStorageUserQuota(ctx, "user_transfer_target")
	if err != nil || targetQuota.QuotaBytes != 2048 {
		t.Fatalf("expected target quota to be created from source, quota=%#v err=%v", targetQuota, err)
	}

	if _, err := st.SetStorageUserQuota(ctx, "user_transfer_source", 1024); err != nil {
		t.Fatalf("reset source quota: %v", err)
	}
	if _, err := st.SetStorageUserQuota(ctx, "user_transfer_target", 4096); err != nil {
		t.Fatalf("override target quota: %v", err)
	}
	result, err = st.TransferUserOwnership(ctx, "user_transfer_source", "user_transfer_target")
	if err != nil {
		t.Fatalf("transfer ownership with existing target quota: %v", err)
	}
	if !result.SourceQuotaDeleted || result.TargetQuotaCreatedFromSource || !result.TargetQuotaPreserved {
		t.Fatalf("expected target quota preservation semantics: %#v", result)
	}
	targetQuota, err = st.GetStorageUserQuota(ctx, "user_transfer_target")
	if err != nil || targetQuota.QuotaBytes != 4096 {
		t.Fatalf("expected target quota preserved, quota=%#v err=%v", targetQuota, err)
	}
}

func testUserRepositoryTransfersOwnershipSelection(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	for _, item := range []common.CreateUserInput{
		{ID: "user_transfer_select_source", Username: "select-source", Email: "select-source@example.com", PasswordHash: "hash", IsActive: true},
		{ID: "user_transfer_select_target", Username: "select-target", Email: "select-target@example.com", PasswordHash: "hash", IsActive: true},
	} {
		if _, err := st.CreateUser(ctx, item); err != nil {
			t.Fatalf("create user %s: %v", item.ID, err)
		}
	}
	for _, conversationID := range []string{"conv_select_one", "conv_select_two"} {
		if _, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
			ConversationID: conversationID,
			RequestID:      "req_" + conversationID,
			ResponseID:     "resp_" + conversationID,
			OwnerID:        "user_transfer_select_source",
			RequestFormat:  "responses",
			Model:          "select-transfer-test",
			UserContent:    conversationID,
			RequestBody:    map[string]any{"model": "select-transfer-test"},
		}); err != nil {
			t.Fatalf("create conversation %s: %v", conversationID, err)
		}
	}
	for _, filename := range []string{"select-one.png", "select-two.png"} {
		if _, err := st.CreateUploadedImage(ctx, common.CreateUploadedImageInput{
			ID:               "img_" + filename,
			OwnerID:          "user_transfer_select_source",
			Filename:         filename,
			OriginalFilename: filename,
			ContentType:      "image/png",
			Bytes:            64,
			URL:              "/api/uploads/imgs/" + filename,
		}); err != nil {
			t.Fatalf("create upload %s: %v", filename, err)
		}
		if _, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
			Path:      "/tmp/" + filename,
			Filename:  filename,
			OwnerID:   "user_transfer_select_source",
			Bytes:     64,
			LastError: "busy",
		}); err != nil {
			t.Fatalf("create deletion failure %s: %v", filename, err)
		}
	}

	result, err := st.TransferUserOwnershipSelection(ctx, "user_transfer_select_source", "user_transfer_select_target", []string{"conv_select_one"}, []string{"select-one.png"})
	if err != nil {
		t.Fatalf("transfer ownership selection: %v", err)
	}
	if result.TransferredConversations != 1 || result.TransferredUploadedImages != 1 || result.TransferredDeletionFailures != 1 {
		t.Fatalf("unexpected selective transfer result: %#v", result)
	}
	preview, err := st.PreviewUserDeletion(ctx, "user_transfer_select_source")
	if err != nil {
		t.Fatalf("preview after partial transfer: %v", err)
	}
	if preview.CanDelete || preview.Counts.OwnedConversations != 1 || preview.Counts.OwnedUploadedImages != 1 {
		t.Fatalf("expected remaining blockers after partial transfer: %#v", preview)
	}
	conversation, err := st.GetConversation(ctx, "conv_select_one")
	if err != nil {
		t.Fatalf("get moved conversation: %v", err)
	}
	if conversation.Metadata["owner_id"] != "user_transfer_select_target" {
		t.Fatalf("expected selected conversation owner move: %#v", conversation)
	}
	stillOwned, err := st.GetConversation(ctx, "conv_select_two")
	if err != nil {
		t.Fatalf("get remaining conversation: %v", err)
	}
	if stillOwned.Metadata["owner_id"] != "user_transfer_select_source" {
		t.Fatalf("expected unselected conversation to remain: %#v", stillOwned)
	}
	targetImages, err := st.ListUploadedImagesByOwner(ctx, "user_transfer_select_target")
	if err != nil || len(targetImages) != 1 || targetImages[0].Filename != "select-one.png" {
		t.Fatalf("unexpected target selected uploads: %#v err=%v", targetImages, err)
	}

	if _, err := st.TransferUserOwnershipSelection(ctx, "user_transfer_select_source", "user_transfer_select_target", []string{"conv_select_two"}, []string{"select-two.png"}); err != nil {
		t.Fatalf("transfer remaining ownership selection: %v", err)
	}
	preview, err = st.PreviewUserDeletion(ctx, "user_transfer_select_source")
	if err != nil {
		t.Fatalf("preview after full selective transfer: %v", err)
	}
	if !preview.CanDelete || preview.Counts.OwnedConversations != 0 || preview.Counts.OwnedUploadedImages != 0 {
		t.Fatalf("expected blockers cleared after full selective transfer: %#v", preview)
	}
}

func testAuditRepositoryCreatesFiltersAndLimitsLogs(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	first, err := st.CreateAuditLog(ctx, common.CreateAuditLogInput{
		ID:           "audit_1",
		ActorUserID:  "user_audit",
		ActorRole:    "user",
		ActorSource:  "session",
		EventType:    "user.identity",
		ResourceType: "user_identity",
		ResourceID:   "identity_1",
		Action:       "unlink",
		Outcome:      "success",
		IPAddress:    "127.0.0.1",
		UserAgent:    "contract-test",
		Metadata: map[string]any{
			"safe":        "value",
			"status_code": float64(200),
		},
	})
	if err != nil {
		t.Fatalf("create first audit log: %v", err)
	}
	if first.ID != "audit_1" || first.CreatedAt.IsZero() || first.Metadata["safe"] != "value" {
		t.Fatalf("unexpected created audit log: %#v", first)
	}
	if _, err := st.CreateAuditLog(ctx, common.CreateAuditLogInput{
		ID:           "audit_2",
		ActorUserID:  "other_user",
		ActorRole:    "admin",
		ActorSource:  "session",
		EventType:    "admin.runtime",
		ResourceType: "runtime",
		ResourceID:   "runtime",
		Action:       "gc",
		Outcome:      "success",
		Metadata: map[string]any{
			"freed": float64(123),
		},
	}); err != nil {
		t.Fatalf("create second audit log: %v", err)
	}
	if _, err := st.CreateAuditLog(ctx, common.CreateAuditLogInput{
		ID:           "audit_3",
		ActorUserID:  "user_audit",
		ActorRole:    "user",
		ActorSource:  "session",
		EventType:    "user.identity",
		ResourceType: "user_identity",
		ResourceID:   "identity_2",
		Action:       "unlink",
		Outcome:      "failure",
	}); err != nil {
		t.Fatalf("create third audit log: %v", err)
	}

	filtered, err := st.ListAuditLogs(ctx, common.ListAuditLogsInput{
		Limit:       10,
		EventType:   "user.identity",
		ActorUserID: "user_audit",
	})
	if err != nil {
		t.Fatalf("list filtered audit logs: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != "audit_3" || filtered[1].ID != "audit_1" {
		t.Fatalf("unexpected filtered audit logs: %#v", filtered)
	}
	if filtered[1].Metadata["safe"] != "value" {
		t.Fatalf("unexpected filtered metadata: %#v", filtered[1])
	}

	limited, err := st.ListAuditLogs(ctx, common.ListAuditLogsInput{Limit: 1})
	if err != nil {
		t.Fatalf("list limited audit logs: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != "audit_3" {
		t.Fatalf("unexpected limited audit logs: %#v", limited)
	}
}

func testAutomationRuleRepositoryCRUD(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	for _, input := range []common.UpsertAutomationRuleInput{
		{
			ID:      "rule_a",
			UserID:  "user_rules",
			Enabled: true,
			Payload: map[string]any{
				"id":      "rule_a",
				"enabled": true,
				"name":    "Rule A",
			},
		},
		{
			ID:      "rule_b",
			UserID:  "user_rules",
			Enabled: false,
			Payload: map[string]any{
				"id":      "rule_b",
				"enabled": false,
				"name":    "Rule B",
			},
		},
	} {
		if _, err := st.UpsertAutomationRule(ctx, input); err != nil {
			t.Fatalf("upsert initial automation rule: %v", err)
		}
	}
	initial, err := st.ListAutomationRulesByUser(ctx, "user_rules")
	if err != nil {
		t.Fatalf("list initial automation rules: %v", err)
	}
	if len(initial) != 2 {
		t.Fatalf("expected two initial rules, got %#v", initial)
	}

	if _, err := st.UpsertAutomationRule(ctx, common.UpsertAutomationRuleInput{
		ID:      "rule_a",
		UserID:  "user_rules",
		Enabled: false,
		Payload: map[string]any{
			"id":      "rule_a",
			"enabled": false,
			"name":    "Rule A2",
		},
	}); err != nil {
		t.Fatalf("update automation rule: %v", err)
	}
	scoped, err := st.ListAutomationRulesByUser(ctx, "user_rules")
	if err != nil {
		t.Fatalf("list updated automation rules: %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("expected scoped replace to keep other rules, got %#v", scoped)
	}
	byID := map[string]common.AutomationRule{}
	for _, item := range scoped {
		byID[item.ID] = item
	}
	if byID["rule_a"].Payload["name"] != "Rule A2" || byID["rule_a"].Enabled {
		t.Fatalf("expected rule_a to be replaced, got %#v", byID["rule_a"])
	}
	if byID["rule_b"].Payload["name"] != "Rule B" || byID["rule_b"].Enabled {
		t.Fatalf("expected rule_b to remain disabled and unchanged, got %#v", byID["rule_b"])
	}

	otherUser, err := st.ListAutomationRulesByUser(ctx, "other_user")
	if err != nil {
		t.Fatalf("list other user automation rules: %v", err)
	}
	if len(otherUser) != 0 {
		t.Fatalf("expected other user rules to remain isolated: %#v", otherUser)
	}
	if err := st.DeleteAutomationRule(ctx, "user_rules", "rule_a"); err != nil {
		t.Fatalf("delete automation rule: %v", err)
	}
	afterDelete, err := st.ListAutomationRulesByUser(ctx, "user_rules")
	if err != nil || len(afterDelete) != 1 || afterDelete[0].ID != "rule_b" {
		t.Fatalf("unexpected rules after delete: %#v err=%v", afterDelete, err)
	}
}

func testAuthVerificationCodeRepositoryDeletesExpiredCodes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	now := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)

	if _, err := st.UpsertAuthVerificationCode(ctx, common.UpsertAuthVerificationCodeInput{
		Email:          "expired-a@example.com",
		Purpose:        "register",
		CodeHash:       "hash-expired-a",
		FailedAttempts: 0,
		ExpiresAt:      now.Add(-2 * time.Hour),
		LastSentAt:     now.Add(-3 * time.Hour),
	}); err != nil {
		t.Fatalf("create expired code A: %v", err)
	}
	if _, err := st.UpsertAuthVerificationCode(ctx, common.UpsertAuthVerificationCodeInput{
		Email:          "expired-b@example.com",
		Purpose:        "password_reset",
		CodeHash:       "hash-expired-b",
		FailedAttempts: 1,
		ExpiresAt:      now,
		LastSentAt:     now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("create expired code B: %v", err)
	}
	if _, err := st.UpsertAuthVerificationCode(ctx, common.UpsertAuthVerificationCodeInput{
		Email:          "active@example.com",
		Purpose:        "register",
		CodeHash:       "hash-active",
		FailedAttempts: 0,
		ExpiresAt:      now.Add(30 * time.Minute),
		LastSentAt:     now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("create active code: %v", err)
	}

	deleted, err := st.DeleteExpiredAuthVerificationCodes(ctx, now)
	if err != nil {
		t.Fatalf("delete expired verification codes: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected to delete 2 expired codes, got %d", deleted)
	}

	if _, err := st.GetAuthVerificationCode(ctx, "expired-a@example.com", "register"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected expired code A to be deleted, got %v", err)
	}
	if _, err := st.GetAuthVerificationCode(ctx, "expired-b@example.com", "password_reset"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected expired code B to be deleted, got %v", err)
	}
	active, err := st.GetAuthVerificationCode(ctx, "active@example.com", "register")
	if err != nil {
		t.Fatalf("load active code: %v", err)
	}
	if active.CodeHash != "hash-active" {
		t.Fatalf("unexpected active code after cleanup: %#v", active)
	}

	deleted, err = st.DeleteExpiredAuthVerificationCodes(ctx, now)
	if err != nil {
		t.Fatalf("repeat delete expired verification codes: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected second cleanup to delete nothing, got %d", deleted)
	}
}

func testAppAPIKeyRepositoryCreatesListsUsesAndRevokes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	expiresAt := time.Date(2026, 7, 7, 1, 2, 3, 0, time.UTC)
	created, err := st.CreateAppAPIKey(ctx, common.CreateAppAPIKeyInput{
		ID:        "appkey_1",
		UserID:    "user_keys",
		Name:      "automation",
		KeyHash:   "hash",
		KeyPrefix: "ak-demo",
		Scopes:    []string{"requests:read", "requests:respond"},
		ResourceLimits: map[string]any{
			"max_requests_per_minute": float64(10),
			"allowed_source_ips":      []any{"127.0.0.1"},
		},
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("create app api key: %v", err)
	}
	if created.ID != "appkey_1" || created.UserID != "user_keys" || len(created.Scopes) != 2 || created.ExpiresAt == nil {
		t.Fatalf("unexpected created app key: %#v", created)
	}

	byPrefix, err := st.GetAppAPIKeyByPrefix(ctx, "ak-demo")
	if err != nil {
		t.Fatalf("get app key by prefix: %v", err)
	}
	if byPrefix.ID != created.ID || byPrefix.ResourceLimits["max_requests_per_minute"].(float64) != 10 {
		t.Fatalf("unexpected app key by prefix: %#v", byPrefix)
	}

	usedAt := time.Date(2026, 7, 8, 4, 5, 6, 0, time.UTC)
	if err := st.UpdateAppAPIKeyLastUsedAt(ctx, created.ID, usedAt); err != nil {
		t.Fatalf("update app key last used: %v", err)
	}
	items, err := st.ListAppAPIKeysByUser(ctx, "user_keys")
	if err != nil {
		t.Fatalf("list app keys: %v", err)
	}
	if len(items) != 1 || items[0].LastUsedAt == nil || !items[0].LastUsedAt.Equal(usedAt) {
		t.Fatalf("unexpected app key list after last used: %#v", items)
	}

	if err := st.RevokeAppAPIKey(ctx, created.ID, "other_user"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking other user key, got %v", err)
	}
	if err := st.RevokeAppAPIKey(ctx, created.ID, "user_keys"); err != nil {
		t.Fatalf("revoke app key: %v", err)
	}
	revoked, err := st.GetAppAPIKeyByPrefix(ctx, "ak-demo")
	if err != nil {
		t.Fatalf("get revoked app key: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("expected revoked_at after revoke: %#v", revoked)
	}
	if err := st.RevokeAppAPIKey(ctx, created.ID, "user_keys"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking already revoked key, got %v", err)
	}
}

func testAppAPIKeyRepositoryAuditsRequests(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	if err := st.CreateAppAPIKeyAuditLog(ctx, common.AppAPIKeyAuditLog{
		ID:          "applog_1",
		AppAPIKeyID: "appkey_1",
		UserID:      "user_audit",
		Route:       "/api/app/me",
		StatusCode:  httpStatusOK,
		ErrorCode:   "",
		CreatedAt:   time.Date(2026, 7, 9, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create app audit log: %v", err)
	}
	if err := st.CreateAppAPIKeyAuditLog(ctx, common.AppAPIKeyAuditLog{
		ID:          "applog_2",
		AppAPIKeyID: "appkey_2",
		UserID:      "other_user",
		Route:       "/api/app/requests",
		StatusCode:  httpStatusForbidden,
		ErrorCode:   "forbidden",
		CreatedAt:   time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create second app audit log: %v", err)
	}
	items, err := st.ListAppAPIKeyAuditLogs(ctx, common.ListAppAPIKeyAuditLogsInput{UserID: "user_audit", Limit: 10})
	if err != nil {
		t.Fatalf("list filtered app audit logs: %v", err)
	}
	if len(items) != 1 || items[0].ID != "applog_1" || items[0].StatusCode != httpStatusOK {
		t.Fatalf("unexpected filtered app audit logs: %#v", items)
	}
	all, err := st.ListAppAPIKeyAuditLogs(ctx, common.ListAppAPIKeyAuditLogsInput{Limit: 10})
	if err != nil {
		t.Fatalf("list app audit logs: %v", err)
	}
	if len(all) != 2 || all[0].ID != "applog_2" || all[1].ID != "applog_1" {
		t.Fatalf("unexpected app audit log ordering: %#v", all)
	}
}

func testModelAPIKeyRepositoryCreatesListsUsesAndRevokes(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	created, err := st.CreateModelAPIKey(ctx, common.CreateModelAPIKeyInput{
		ID:            "modelkey_1",
		UserID:        "user_model_keys",
		Name:          "default virtual model",
		KeyCiphertext: "ciphertext",
		KeyPrefix:     "sk-demo",
		Model:         "chatapi-demo",
	})
	if err != nil {
		t.Fatalf("create model api key: %v", err)
	}
	if created.ID != "modelkey_1" || created.Model != "chatapi-demo" {
		t.Fatalf("unexpected created model key: %#v", created)
	}
	byPrefix, err := st.GetModelAPIKeyByPrefix(ctx, "sk-demo")
	if err != nil {
		t.Fatalf("get model key by prefix: %v", err)
	}
	if byPrefix.ID != created.ID || byPrefix.KeyCiphertext != "ciphertext" {
		t.Fatalf("unexpected model key by prefix: %#v", byPrefix)
	}
	byID, err := st.GetModelAPIKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get model key by id: %v", err)
	}
	if byID.KeyPrefix != "sk-demo" {
		t.Fatalf("unexpected model key by id: %#v", byID)
	}

	usedAt := time.Date(2026, 7, 10, 4, 5, 6, 0, time.UTC)
	if err := st.UpdateModelAPIKeyLastUsedAt(ctx, created.ID, usedAt); err != nil {
		t.Fatalf("update model key last used: %v", err)
	}
	items, err := st.ListModelAPIKeysByUser(ctx, "user_model_keys")
	if err != nil {
		t.Fatalf("list model keys: %v", err)
	}
	if len(items) != 1 || items[0].LastUsedAt == nil || !items[0].LastUsedAt.Equal(usedAt) {
		t.Fatalf("unexpected model key list after last used: %#v", items)
	}

	if err := st.RevokeModelAPIKey(ctx, created.ID, "other_user"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound revoking other user model key, got %v", err)
	}
	if err := st.RevokeModelAPIKey(ctx, created.ID, "user_model_keys"); err != nil {
		t.Fatalf("revoke model key: %v", err)
	}
	revoked, err := st.GetModelAPIKeyByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get revoked model key: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("expected revoked_at after model revoke: %#v", revoked)
	}
	if _, err := st.GetModelAPIKeyByPrefix(ctx, "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing model prefix, got %v", err)
	}
}

func testUserIdentityRepositoryUpsertsByProviderSubject(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	if _, err := st.CreateUser(ctx, common.CreateUserInput{
		ID:       "user_oidc",
		Email:    "first@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	lastLogin := time.Date(2026, 7, 6, 4, 5, 6, 0, time.UTC)
	identity, err := st.UpsertUserIdentity(ctx, common.UpsertUserIdentityInput{
		ID:            "identity_1",
		UserID:        "user_oidc",
		Provider:      "kirari",
		Subject:       "sub-123",
		Email:         "first@example.com",
		EmailVerified: false,
		Profile: map[string]any{
			"name": "First User",
		},
	})
	if err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	if identity.ID != "identity_1" || identity.EmailVerified || identity.Profile["name"] != "First User" {
		t.Fatalf("unexpected inserted identity: %#v", identity)
	}

	updated, err := st.UpsertUserIdentity(ctx, common.UpsertUserIdentityInput{
		ID:            "identity_ignored",
		UserID:        "user_oidc",
		Provider:      "kirari",
		Subject:       "sub-123",
		Email:         "verified@example.com",
		EmailVerified: true,
		Profile: map[string]any{
			"name": "Verified User",
		},
		LastLoginAt: &lastLogin,
	})
	if err != nil {
		t.Fatalf("update identity: %v", err)
	}
	if updated.ID != "identity_1" {
		t.Fatalf("upsert should keep original id on provider/subject conflict: %#v", updated)
	}
	if updated.Email != "verified@example.com" || !updated.EmailVerified || updated.Profile["name"] != "Verified User" {
		t.Fatalf("unexpected updated identity: %#v", updated)
	}
	if updated.LastLoginAt == nil || !updated.LastLoginAt.Equal(lastLogin) {
		t.Fatalf("unexpected identity last login: %#v", updated.LastLoginAt)
	}

	got, err := st.GetUserIdentity(ctx, "kirari", "sub-123")
	if err != nil {
		t.Fatalf("get identity: %v", err)
	}
	if got.Email != updated.Email {
		t.Fatalf("unexpected fetched identity: %#v", got)
	}

	items, err := st.ListUserIdentities(ctx, "user_oidc")
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(items) != 1 || items[0].Provider != "kirari" {
		t.Fatalf("unexpected identities: %#v", items)
	}

	if _, err := st.GetUserIdentity(ctx, "kirari", "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing identity, got %v", err)
	}
	if err := st.DeleteUserIdentity(ctx, updated.ID, "other_user"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting identity for wrong user, got %v", err)
	}
	if err := st.DeleteUserIdentity(ctx, updated.ID, "user_oidc"); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	if _, err := st.GetUserIdentity(ctx, "kirari", "sub-123"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected deleted identity to be missing, got %v", err)
	}
}

func testConfigRepositoryUpsertsListsAndDeletesSystemConfig(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	item, err := st.SetSystemConfig(ctx, common.SetSystemConfigInput{
		Key: "runtime.gc",
		Value: map[string]any{
			"gogc": 125,
		},
	})
	if err != nil {
		t.Fatalf("set system config: %v", err)
	}
	if item.Key != "runtime.gc" || item.Value["gogc"].(float64) != 125 {
		t.Fatalf("unexpected system config: %#v", item)
	}
	updated, err := st.SetSystemConfig(ctx, common.SetSystemConfigInput{
		Key: "runtime.gc",
		Value: map[string]any{
			"gogc":         80,
			"memory_limit": "512MiB",
		},
	})
	if err != nil {
		t.Fatalf("update system config: %v", err)
	}
	if updated.Value["gogc"].(float64) != 80 || updated.Value["memory_limit"] != "512MiB" {
		t.Fatalf("unexpected updated system config: %#v", updated)
	}
	if !updated.CreatedAt.Equal(item.CreatedAt) {
		t.Fatalf("upsert should keep created_at, before=%s after=%s", item.CreatedAt, updated.CreatedAt)
	}
	lastWrite, err := st.SetSystemConfig(ctx, common.SetSystemConfigInput{Key: "runtime.gc", Value: map[string]any{"gogc": 70}})
	if err != nil {
		t.Fatalf("last-write-wins update: %v", err)
	}
	if lastWrite.Value["gogc"].(float64) != 70 {
		t.Fatalf("last submitted value was not persisted: %#v", lastWrite)
	}

	if _, err := st.SetSystemConfig(ctx, common.SetSystemConfigInput{
		Key: "storage.cleanup",
		Value: map[string]any{
			"enabled": true,
		},
	}); err != nil {
		t.Fatalf("set second system config: %v", err)
	}

	items, err := st.ListSystemConfigs(ctx)
	if err != nil {
		t.Fatalf("list system configs: %v", err)
	}
	if len(items) != 2 || items[0].Key != "runtime.gc" || items[1].Key != "storage.cleanup" {
		t.Fatalf("unexpected system config list: %#v", items)
	}

	if err := st.DeleteSystemConfig(ctx, "runtime.gc"); err != nil {
		t.Fatalf("delete system config: %v", err)
	}
	if _, err := st.GetSystemConfig(ctx, "runtime.gc"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func testConfigRepositoryUpsertsListsAndDeletesUserConfig(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	if _, err := st.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: "user_1",
		Key:    "workspace",
		Value: map[string]any{
			"compact": true,
		},
	}); err != nil {
		t.Fatalf("set user config: %v", err)
	}
	updated, err := st.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: "user_1",
		Key:    "workspace",
		Value: map[string]any{
			"compact": false,
			"theme":   "dark",
		},
	})
	if err != nil {
		t.Fatalf("update user config: %v", err)
	}
	if updated.UserID != "user_1" || updated.Key != "workspace" {
		t.Fatalf("unexpected user config identity: %#v", updated)
	}
	if updated.Value["compact"] != false || updated.Value["theme"] != "dark" {
		t.Fatalf("unexpected user config value: %#v", updated)
	}

	if _, err := st.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: "user_1",
		Key:    "notifications",
		Value: map[string]any{
			"email": false,
		},
	}); err != nil {
		t.Fatalf("set second user config: %v", err)
	}
	if _, err := st.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: "user_2",
		Key:    "workspace",
		Value: map[string]any{
			"compact": true,
		},
	}); err != nil {
		t.Fatalf("set other user config: %v", err)
	}

	items, err := st.ListUserConfigs(ctx, "user_1")
	if err != nil {
		t.Fatalf("list user configs: %v", err)
	}
	if len(items) != 2 || items[0].Key != "notifications" || items[1].Key != "workspace" {
		t.Fatalf("unexpected user config list: %#v", items)
	}

	if err := st.DeleteUserConfig(ctx, "user_1", "workspace"); err != nil {
		t.Fatalf("delete user config: %v", err)
	}
	if _, err := st.GetUserConfig(ctx, "user_1", "workspace"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	otherUserItem, err := st.GetUserConfig(ctx, "user_2", "workspace")
	if err != nil {
		t.Fatalf("other user config should remain: %v", err)
	}
	if otherUserItem.UserID != "user_2" {
		t.Fatalf("unexpected other user item: %#v", otherUserItem)
	}
}

func testStorageRepositoryCreatesListsAndDeletesUploadedImages(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	first, err := st.CreateUploadedImage(ctx, common.CreateUploadedImageInput{
		ID:               "img_1",
		OwnerID:          "user_a",
		Filename:         "img-a.png",
		OriginalFilename: "a.png",
		ContentType:      "image/png",
		Bytes:            123,
		URL:              "/api/uploads/imgs/img-a.png",
	})
	if err != nil {
		t.Fatalf("create first uploaded image: %v", err)
	}
	if first.ID != "img_1" || first.OwnerID != "user_a" || first.CreatedAt.IsZero() {
		t.Fatalf("unexpected first uploaded image: %#v", first)
	}

	second, err := st.CreateUploadedImage(ctx, common.CreateUploadedImageInput{
		ID:               "img_2",
		OwnerID:          "user_a",
		Filename:         "img-b.png",
		OriginalFilename: "b.png",
		ContentType:      "image/png",
		Bytes:            456,
		URL:              "/api/uploads/imgs/img-b.png",
	})
	if err != nil {
		t.Fatalf("create second uploaded image: %v", err)
	}

	if _, err := st.CreateUploadedImage(ctx, common.CreateUploadedImageInput{
		ID:               "img_3",
		OwnerID:          "user_b",
		Filename:         "img-c.png",
		OriginalFilename: "c.png",
		ContentType:      "image/png",
		Bytes:            789,
		URL:              "/api/uploads/imgs/img-c.png",
	}); err != nil {
		t.Fatalf("create third uploaded image: %v", err)
	}

	ownerImages, err := st.ListUploadedImagesByOwner(ctx, "user_a")
	if err != nil {
		t.Fatalf("list owner uploaded images: %v", err)
	}
	if len(ownerImages) != 2 || ownerImages[0].ID != second.ID || ownerImages[1].ID != first.ID {
		t.Fatalf("unexpected owner uploaded images: %#v", ownerImages)
	}

	allImages, err := st.ListUploadedImages(ctx)
	if err != nil {
		t.Fatalf("list all uploaded images: %v", err)
	}
	if len(allImages) != 3 || allImages[0].ID != "img_3" || allImages[2].ID != "img_1" {
		t.Fatalf("unexpected all uploaded images: %#v", allImages)
	}

	deleted, err := st.DeleteUploadedImagesByFilenames(ctx, []string{"img-a.png", "img-a.png", "  ", "missing.png"})
	if err != nil {
		t.Fatalf("delete uploaded images by filename: %v", err)
	}
	if deleted.DeletedImages != 1 {
		t.Fatalf("unexpected deleted uploaded image count: %#v", deleted)
	}

	afterDelete, err := st.ListUploadedImagesByOwner(ctx, "user_a")
	if err != nil {
		t.Fatalf("list uploaded images after delete: %v", err)
	}
	if len(afterDelete) != 1 || afterDelete[0].Filename != "img-b.png" {
		t.Fatalf("unexpected uploaded images after delete: %#v", afterDelete)
	}

	emptyDelete, err := st.DeleteUploadedImagesByFilenames(ctx, []string{"", "   "})
	if err != nil {
		t.Fatalf("delete empty uploaded image list: %v", err)
	}
	if emptyDelete.DeletedImages != 0 {
		t.Fatalf("unexpected deleted count for empty delete: %#v", emptyDelete)
	}
}

func testStorageRepositoryUpsertsListsAndDeletesDeletionFailures(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	first, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/img-a.png",
		Filename:  "img-a.png",
		OwnerID:   "user_a",
		Bytes:     10,
		LastError: "permission denied",
	})
	if err != nil {
		t.Fatalf("create deletion failure: %v", err)
	}
	if first.Attempts != 1 || first.Path != "/tmp/img-a.png" || first.CreatedAt.IsZero() {
		t.Fatalf("unexpected first deletion failure: %#v", first)
	}

	second, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/img-b.png",
		Filename:  "img-b.png",
		OwnerID:   "user_b",
		Bytes:     20,
		LastError: "busy",
	})
	if err != nil {
		t.Fatalf("create second deletion failure: %v", err)
	}

	updated, err := st.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/img-a.png",
		Filename:  "img-a.png",
		OwnerID:   "user_a",
		Bytes:     30,
		LastError: "still busy",
	})
	if err != nil {
		t.Fatalf("update deletion failure: %v", err)
	}
	if updated.Attempts != 2 || updated.Bytes != 30 || updated.LastError != "still busy" {
		t.Fatalf("unexpected updated deletion failure: %#v", updated)
	}

	items, err := st.ListStorageFileDeletionFailures(ctx, 1)
	if err != nil {
		t.Fatalf("list deletion failures with limit: %v", err)
	}
	if len(items) != 1 || items[0].Path != second.Path {
		t.Fatalf("unexpected limited deletion failures: %#v", items)
	}

	allItems, err := st.ListStorageFileDeletionFailures(ctx, 10)
	if err != nil {
		t.Fatalf("list all deletion failures: %v", err)
	}
	if len(allItems) != 2 || allItems[0].Path != second.Path || allItems[1].Path != updated.Path {
		t.Fatalf("unexpected deletion failure ordering: %#v", allItems)
	}

	if err := st.DeleteStorageFileDeletionFailures(ctx, []string{"/tmp/img-b.png", "/tmp/img-b.png", "", "missing"}); err != nil {
		t.Fatalf("delete deletion failures: %v", err)
	}
	remaining, err := st.ListStorageFileDeletionFailures(ctx, 10)
	if err != nil {
		t.Fatalf("list deletion failures after delete: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Path != updated.Path {
		t.Fatalf("unexpected deletion failures after delete: %#v", remaining)
	}

	if err := st.DeleteStorageFileDeletionFailures(ctx, []string{"", "   "}); err != nil {
		t.Fatalf("delete empty deletion failure list: %v", err)
	}
}

func testStorageRepositorySetsListsAndDeletesUserQuotas(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	if _, err := st.GetStorageUserQuota(ctx, "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing storage quota, got %v", err)
	}

	first, err := st.SetStorageUserQuota(ctx, "user_b", 2048)
	if err != nil {
		t.Fatalf("set first storage quota: %v", err)
	}
	if first.OwnerID != "user_b" || first.QuotaBytes != 2048 || first.CreatedAt.IsZero() {
		t.Fatalf("unexpected first storage quota: %#v", first)
	}

	second, err := st.SetStorageUserQuota(ctx, "user_a", 1024)
	if err != nil {
		t.Fatalf("set second storage quota: %v", err)
	}

	updated, err := st.SetStorageUserQuota(ctx, "user_b", 4096)
	if err != nil {
		t.Fatalf("update storage quota: %v", err)
	}
	if updated.QuotaBytes != 4096 {
		t.Fatalf("unexpected updated storage quota: %#v", updated)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("storage quota upsert should keep created_at, before=%s after=%s", first.CreatedAt, updated.CreatedAt)
	}

	items, err := st.ListStorageUserQuotas(ctx)
	if err != nil {
		t.Fatalf("list storage quotas: %v", err)
	}
	if len(items) != 2 || items[0].OwnerID != second.OwnerID || items[1].OwnerID != updated.OwnerID {
		t.Fatalf("unexpected storage quota list: %#v", items)
	}

	got, err := st.GetStorageUserQuota(ctx, "user_b")
	if err != nil {
		t.Fatalf("get storage quota: %v", err)
	}
	if got.QuotaBytes != 4096 {
		t.Fatalf("unexpected fetched storage quota: %#v", got)
	}

	if err := st.DeleteStorageUserQuota(ctx, "user_b"); err != nil {
		t.Fatalf("delete storage quota: %v", err)
	}
	if _, err := st.GetStorageUserQuota(ctx, "user_b"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after deleting storage quota, got %v", err)
	}
}

func testConversationRepositoryPendingTurnLifecycle(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	firstConversation, firstUserMessage, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID:   "conv_waiting",
		RequestID:        "req_waiting",
		ResponseID:       "resp_waiting",
		OwnerID:          "user_a",
		RequestFormat:    "responses",
		Model:            "gpt-test",
		SystemContent:    "system rule",
		DeveloperContent: "developer hint",
		AssistantContent: "previous assistant answer",
		UserContent:      "First question for waiting turn",
		RequestBody: map[string]any{
			"model": "gpt-test",
			"input": []any{
				map[string]any{"type": "input_text", "text": "First question for waiting turn"},
				map[string]any{"type": "input_image", "image_url": "https://example.com/tool.png", "media_type": "image/png"},
			},
		},
		ToolSchemas: []any{
			map[string]any{"name": "tool_a"},
		},
		BuiltinTools: []any{
			map[string]any{"kind": "web_search", "type": "web_search"},
		},
		ToolChoice: common.RequestToolChoice{
			Type: "function",
			Name: "tool_a",
		},
		ResponseFormat: common.RequestResponseFormat{
			Type: "json_schema",
			Name: "answer",
			Schema: map[string]any{
				"type": "object",
			},
		},
	})
	if err != nil {
		t.Fatalf("create first pending turn: %v", err)
	}
	if firstConversation.MessageCount != 1 || firstConversation.ResponseID != "resp_waiting" {
		t.Fatalf("unexpected first conversation: %#v", firstConversation)
	}
	if firstUserMessage.Status != "pending" || firstUserMessage.ResponseID == nil || *firstUserMessage.ResponseID != "resp_waiting" {
		t.Fatalf("unexpected first user message: %#v", firstUserMessage)
	}

	secondConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_abort",
		RequestID:      "req_abort",
		ResponseID:     "resp_abort",
		OwnerID:        "user_b",
		RequestFormat:  "chat.completions",
		Model:          "claude-test",
		UserContent:    "Second question for abort turn",
		RequestBody: map[string]any{
			"model":    "claude-test",
			"messages": []any{map[string]any{"role": "user", "content": "Second question for abort turn"}},
		},
	})
	if err != nil {
		t.Fatalf("create second pending turn: %v", err)
	}

	waitingConversation, err := st.GetConversation(ctx, firstConversation.ID)
	if err != nil {
		t.Fatalf("get waiting conversation: %v", err)
	}
	if waitingConversation.Metadata["owner_id"] != "user_a" || waitingConversation.Metadata["realtime_status"] != "waiting" {
		t.Fatalf("unexpected waiting conversation metadata: %#v", waitingConversation)
	}

	allConversations, err := st.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(allConversations) != 2 {
		t.Fatalf("expected two conversations, got %#v", allConversations)
	}

	firstRequest, err := st.GetRequest(ctx, "req_waiting")
	if err != nil {
		t.Fatalf("get first request: %v", err)
	}
	if firstRequest.ConversationID != firstConversation.ID || firstRequest.OwnerID != "user_a" || firstRequest.Status != "waiting" {
		t.Fatalf("unexpected first request: %#v", firstRequest)
	}
	if firstRequest.RequestBody["model"] != "gpt-test" {
		t.Fatalf("unexpected request body: %#v", firstRequest.RequestBody)
	}
	if len(firstRequest.ToolSchemas) != 1 {
		t.Fatalf("unexpected tool schemas: %#v", firstRequest.ToolSchemas)
	}
	if len(firstRequest.BuiltinTools) != 1 {
		t.Fatalf("unexpected builtin tools: %#v", firstRequest.BuiltinTools)
	}
	if firstRequest.SystemText != "system rule" || firstRequest.DeveloperText != "developer hint" || firstRequest.AssistantText != "previous assistant answer" {
		t.Fatalf("unexpected request context fields: %#v", firstRequest)
	}
	if firstRequest.ToolChoice.Type != "function" || firstRequest.ToolChoice.Name != "tool_a" {
		t.Fatalf("unexpected tool choice: %#v", firstRequest.ToolChoice)
	}
	if firstRequest.ResponseFormat.Type != "json_schema" || firstRequest.ResponseFormat.Name != "answer" {
		t.Fatalf("unexpected response format: %#v", firstRequest.ResponseFormat)
	}

	requests, err := st.ListRequests(ctx)
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected two requests, got %#v", requests)
	}

	draftSegments := []common.OutputSegment{
		{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
		{Mode: "answer", Text: "beta"},
		{Mode: "thinking", Text: "gamma", ReasoningStreamMode: "reasoning"},
		{Mode: "answer", Text: "delta"},
	}
	draftConversation, err := st.UpdateDraft(ctx, common.UpdateDraftInput{
		ConversationID: firstConversation.ID,
		DraftText:      conversationstate.ContentFromSegments(draftSegments),
		OutputSegments: draftSegments,
	})
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if draftConversation.Metadata["realtime_status"] != "streaming" || draftConversation.Metadata["realtime_draft_text"] != "betadelta" {
		t.Fatalf("unexpected draft conversation: %#v", draftConversation)
	}
	// Real repository re-read after UpdateDraft — not the in-memory return value alone.
	reloadedDraft, err := st.GetConversation(ctx, firstConversation.ID)
	if err != nil {
		t.Fatalf("reload draft conversation: %v", err)
	}
	assertOutputSegmentsEqual(t, conversationstate.DecodeOutputSegments(reloadedDraft.Metadata["realtime_output_segments"]), draftSegments)

	// Completing as tool_call must not persist draft answer/thinking segments. Tool
	// payload lives in Content/arguments; output_segments must be empty/omitted.
	toolArgs := `{"city":"Hangzhou"}`
	completedConversation, completedMessage, err := st.CompletePendingTurn(ctx, common.CompletePendingInput{
		ConversationID:      firstConversation.ID,
		ResponseID:          "resp_waiting",
		OutputText:          toolArgs,
		Mode:                "tool_call",
		ToolName:            "tool_a",
		ToolCallID:          "call_1",
		ReasoningStreamMode: "summary",
		// Even if a caller still forwards leftover draft segments, storage must drop them.
		OutputSegments: draftSegments,
		OutputPreview:  toolArgs,
	})
	if err != nil {
		t.Fatalf("complete pending turn: %v", err)
	}
	if completedConversation.MessageCount != 2 || completedConversation.Metadata["realtime_status"] != "closed" {
		t.Fatalf("unexpected completed conversation: %#v", completedConversation)
	}
	if completedMessage.Content != toolArgs || completedMessage.Metadata["tool_name"] != "tool_a" || completedMessage.Metadata["arguments"] != toolArgs {
		t.Fatalf("unexpected completed message: %#v", completedMessage)
	}
	if completedMessage.Metadata["response_mode"] != "tool_call" {
		t.Fatalf("tool completion lost response_mode: %#v", completedMessage.Metadata)
	}
	if segs := conversationstate.DecodeOutputSegments(completedMessage.Metadata["output_segments"]); len(segs) != 0 {
		t.Fatalf("tool_call must not persist output_segments: %#v", segs)
	}
	// Real List/Get after Complete — exercises JSON marshal/unmarshal key stability.
	messages, err := st.ListMessages(ctx, firstConversation.ID)
	if err != nil {
		t.Fatalf("list messages after complete: %v", err)
	}
	var reloadedMessage common.Message
	for _, item := range messages {
		if item.ID == completedMessage.ID {
			reloadedMessage = item
			break
		}
	}
	if reloadedMessage.ID == "" {
		t.Fatalf("completed message missing from ListMessages: %#v", messages)
	}
	if segs := conversationstate.DecodeOutputSegments(reloadedMessage.Metadata["output_segments"]); len(segs) != 0 {
		t.Fatalf("reloaded tool_call must not keep output_segments: %#v", segs)
	}
	if reloadedMessage.Content != toolArgs || reloadedMessage.Metadata["arguments"] != toolArgs {
		t.Fatalf("reloaded tool payload changed: %#v", reloadedMessage)
	}

	if _, err := st.UpdateDraft(ctx, common.UpdateDraftInput{
		ConversationID: firstConversation.ID,
		DraftText:      "should fail",
	}); !errors.Is(err, common.ErrTurnConflict) {
		t.Fatalf("expected ErrTurnConflict updating closed draft, got %v", err)
	}
	if _, _, err := st.CompletePendingTurn(ctx, common.CompletePendingInput{
		ConversationID: firstConversation.ID,
		ResponseID:     "resp_waiting",
		OutputText:     "again",
		Mode:           "assistant_message",
	}); !errors.Is(err, common.ErrTurnConflict) {
		t.Fatalf("expected ErrTurnConflict completing closed turn, got %v", err)
	}

	// Ordinary assistant completion still persists structured segments (symmetric SQLite/PG).
	ordinaryConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_segments",
		RequestID:      "req_segments",
		ResponseID:     "resp_segments",
		OwnerID:        "user_a",
		RequestFormat:  "responses",
		Model:          "gpt-test",
		UserContent:    "segmented answer",
	})
	if err != nil {
		t.Fatalf("create ordinary pending turn: %v", err)
	}
	if _, err := st.UpdateDraft(ctx, common.UpdateDraftInput{
		ConversationID: ordinaryConversation.ID,
		DraftText:      conversationstate.ContentFromSegments(draftSegments),
		OutputSegments: draftSegments,
	}); err != nil {
		t.Fatalf("update ordinary draft: %v", err)
	}
	ordinaryCompleted, ordinaryMessage, err := st.CompletePendingTurn(ctx, common.CompletePendingInput{
		ConversationID: ordinaryConversation.ID,
		ResponseID:     "resp_segments",
		OutputText:     conversationstate.ContentFromSegments(draftSegments),
		Mode:           "assistant_message",
		OutputSegments: draftSegments,
		OutputPreview:  conversationstate.PreviewFromSegments(draftSegments),
	})
	if err != nil {
		t.Fatalf("complete ordinary pending turn: %v", err)
	}
	if ordinaryCompleted.MessageCount != 2 || ordinaryMessage.Content != "betadelta" {
		t.Fatalf("unexpected ordinary completion: conv=%#v msg=%#v", ordinaryCompleted, ordinaryMessage)
	}
	assertOutputSegmentsEqual(t, conversationstate.DecodeOutputSegments(ordinaryMessage.Metadata["output_segments"]), draftSegments)

	abortedResult, err := st.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: secondConversation.ID,
		Reason:         "manual abort",
		Identity:       common.TurnIdentity{OwnerID: "user_b", RequestID: "req_abort"},
		EventType:      "request_aborted",
		EventLevel:     "warn",
		EventTitle:     "Request Aborted",
		EventDetail:    "manual abort",
	})
	if err != nil {
		t.Fatalf("abort pending turn: %v", err)
	}
	abortedConversation := abortedResult.Conversation
	abortedMessage := abortedResult.Message
	if abortedConversation.Metadata["realtime_status"] != "aborted" || abortedConversation.MessageCount != 1 {
		t.Fatalf("unexpected aborted conversation: %#v", abortedConversation)
	}
	if abortedMessage.ID != "" || abortedMessage.Content != "" {
		t.Fatalf("unexpected aborted message: %#v", abortedMessage)
	}
	abortedEvents, err := st.ListConversationEvents(ctx, secondConversation.ID)
	if err != nil {
		t.Fatalf("list aborted events: %v", err)
	}
	if len(abortedEvents) != 1 || abortedEvents[0].RequestID != "req_abort" || abortedEvents[0].Type != "request_aborted" {
		t.Fatalf("unexpected aborted events: %#v", abortedEvents)
	}

	if _, err := st.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: secondConversation.ID,
		Reason:         "again",
	}); !errors.Is(err, common.ErrTurnConflict) {
		t.Fatalf("expected ErrTurnConflict aborting closed turn, got %v", err)
	}

	failingAbortConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_abort_fail",
		RequestID:      "req_abort_fail",
		ResponseID:     "resp_abort_fail",
		OwnerID:        "user_abort_fail",
		RequestFormat:  "responses",
		Model:          "abort-fail-test",
		UserContent:    "abort fail me",
		RequestBody:    map[string]any{"model": "abort-fail-test"},
	})
	if err != nil {
		t.Fatalf("create failing abort turn: %v", err)
	}
	if _, err := st.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: failingAbortConversation.ID,
		Reason:         "should rollback",
		Identity:       common.TurnIdentity{OwnerID: "user_abort_fail", RequestID: "req_abort_fail"},
		EventID:        "evt_duplicate",
		EventType:      "request_aborted",
		EventLevel:     "warn",
		EventTitle:     "Request Aborted",
	}); err != nil {
		t.Fatalf("first abort with fixed event id should succeed: %v", err)
	}

	failingAbortConversation2, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_abort_fail_2",
		RequestID:      "req_abort_fail_2",
		ResponseID:     "resp_abort_fail_2",
		OwnerID:        "user_abort_fail_2",
		RequestFormat:  "responses",
		Model:          "abort-fail-test",
		UserContent:    "abort fail me too",
		RequestBody:    map[string]any{"model": "abort-fail-test"},
	})
	if err != nil {
		t.Fatalf("create second failing abort turn: %v", err)
	}
	if _, err := st.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: failingAbortConversation2.ID,
		Reason:         "should rollback",
		Identity:       common.TurnIdentity{OwnerID: "user_abort_fail_2", RequestID: "req_abort_fail_2"},
		EventID:        "evt_duplicate",
		EventType:      "request_aborted",
		EventLevel:     "warn",
		EventTitle:     "Request Aborted",
	}); err == nil {
		t.Fatal("expected duplicate event insert to fail abort transaction")
	}
	afterFailedAbort, err := st.GetConversation(ctx, failingAbortConversation2.ID)
	if err != nil {
		t.Fatalf("get failed abort conversation: %v", err)
	}
	if afterFailedAbort.Metadata["realtime_status"] != "waiting" {
		t.Fatalf("expected failed abort transaction to preserve waiting state: %#v", afterFailedAbort)
	}

	disconnectedConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_disconnect",
		RequestID:      "req_disconnect",
		ResponseID:     "resp_disconnect",
		OwnerID:        "user_d",
		RequestFormat:  "responses",
		Model:          "disconnect-test",
		UserContent:    "disconnect me",
		RequestBody:    map[string]any{"model": "disconnect-test"},
	})
	if err != nil {
		t.Fatalf("create disconnect pending turn: %v", err)
	}
	disconnectedResult, err := st.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: disconnectedConversation.ID,
		Reason:         "request disconnected",
		Identity:       common.TurnIdentity{OwnerID: "user_d", RequestID: "req_disconnect"},
		EventType:      "request_disconnected",
		EventLevel:     "warn",
		EventTitle:     "Request Disconnected",
		EventDetail:    "request disconnected",
	})
	if err != nil {
		t.Fatalf("disconnect pending turn: %v", err)
	}
	disconnectedConversation = disconnectedResult.Conversation
	disconnectedMessage := disconnectedResult.Message
	if disconnectedConversation.Metadata["realtime_status"] != "disconnected" {
		t.Fatalf("unexpected disconnected conversation: %#v", disconnectedConversation)
	}
	if disconnectedConversation.MessageCount != 1 {
		t.Fatalf("unexpected disconnected conversation message count: %#v", disconnectedConversation)
	}
	if disconnectedMessage.ID != "" || disconnectedMessage.Content != "" {
		t.Fatalf("unexpected disconnected message: %#v", disconnectedMessage)
	}
	disconnectedEvents, err := st.ListConversationEvents(ctx, disconnectedConversation.ID)
	if err != nil {
		t.Fatalf("list disconnected events: %v", err)
	}
	if len(disconnectedEvents) != 1 || disconnectedEvents[0].RequestID != "req_disconnect" || disconnectedEvents[0].Type != "request_disconnected" {
		t.Fatalf("unexpected disconnected events: %#v", disconnectedEvents)
	}
	if _, err := st.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: disconnectedConversation.ID,
		Reason:         "again",
	}); !errors.Is(err, common.ErrPendingDisconnected) {
		t.Fatalf("expected ErrPendingDisconnected, got %v", err)
	}

	failingDisconnectConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_disconnect_fail",
		RequestID:      "req_disconnect_fail",
		ResponseID:     "resp_disconnect_fail",
		OwnerID:        "user_disconnect_fail",
		RequestFormat:  "responses",
		Model:          "disconnect-fail-test",
		UserContent:    "disconnect fail me",
		RequestBody:    map[string]any{"model": "disconnect-fail-test"},
	})
	if err != nil {
		t.Fatalf("create failing disconnect turn: %v", err)
	}
	if _, err := st.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: failingDisconnectConversation.ID,
		Reason:         "should rollback",
		Identity:       common.TurnIdentity{OwnerID: "user_disconnect_fail", RequestID: "req_disconnect_fail"},
		EventID:        "evt_duplicate_disconnect",
		EventType:      "request_disconnected",
		EventLevel:     "warn",
		EventTitle:     "Request Disconnected",
	}); err != nil {
		t.Fatalf("first disconnect with fixed event id should succeed: %v", err)
	}

	failingDisconnectConversation2, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_disconnect_fail_2",
		RequestID:      "req_disconnect_fail_2",
		ResponseID:     "resp_disconnect_fail_2",
		OwnerID:        "user_disconnect_fail_2",
		RequestFormat:  "responses",
		Model:          "disconnect-fail-test",
		UserContent:    "disconnect fail me too",
		RequestBody:    map[string]any{"model": "disconnect-fail-test"},
	})
	if err != nil {
		t.Fatalf("create second failing disconnect turn: %v", err)
	}
	if _, err := st.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: failingDisconnectConversation2.ID,
		Reason:         "should rollback",
		Identity:       common.TurnIdentity{OwnerID: "user_disconnect_fail_2", RequestID: "req_disconnect_fail_2"},
		EventID:        "evt_duplicate_disconnect",
		EventType:      "request_disconnected",
		EventLevel:     "warn",
		EventTitle:     "Request Disconnected",
	}); err == nil {
		t.Fatal("expected duplicate event insert to fail disconnect transaction")
	}
	afterFailedDisconnect, err := st.GetConversation(ctx, failingDisconnectConversation2.ID)
	if err != nil {
		t.Fatalf("get failed disconnect conversation: %v", err)
	}
	if afterFailedDisconnect.Metadata["realtime_status"] != "waiting" {
		t.Fatalf("expected failed disconnect transaction to preserve waiting state: %#v", afterFailedDisconnect)
	}

	expiringConversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_expire",
		RequestID:      "req_expire",
		ResponseID:     "resp_expire",
		OwnerID:        "user_c",
		RequestFormat:  "messages",
		Model:          "model-expire",
		UserContent:    "Third question for expire",
		RequestBody: map[string]any{
			"model":    "model-expire",
			"messages": []any{},
		},
	})
	if err != nil {
		t.Fatalf("create expiring pending turn: %v", err)
	}
	if _, err := st.UpdateDraft(ctx, common.UpdateDraftInput{
		ConversationID: expiringConversation.ID,
		DraftText:      "stale draft",
	}); err != nil {
		t.Fatalf("update expiring draft: %v", err)
	}

	expired, err := st.ExpirePendingTurns(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("expire pending turns: %v", err)
	}
	if expired.ExpiredConversations < 1 {
		t.Fatalf("unexpected expired conversation count: %#v", expired)
	}
	expiredConversation, err := st.GetConversation(ctx, expiringConversation.ID)
	if err != nil {
		t.Fatalf("get expired conversation: %v", err)
	}
	if expiredConversation.Metadata["realtime_status"] != "expired" {
		t.Fatalf("unexpected expired conversation metadata: %#v", expiredConversation)
	}

	messages, err = st.ListMessages(ctx, firstConversation.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected first conversation messages: %#v", messages)
	}

	deleted, err := st.DeleteConversations(ctx, []string{firstConversation.ID, firstConversation.ID, "", "missing"})
	if err != nil {
		t.Fatalf("delete conversations: %v", err)
	}
	if deleted.DeletedConversations != 1 || deleted.DeletedMessages != 2 {
		t.Fatalf("unexpected delete conversations result: %#v", deleted)
	}
	if _, err := st.GetConversation(ctx, firstConversation.ID); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected deleted conversation missing, got %v", err)
	}
}

func testConversationRepositoryConversationEvents(t *testing.T, newStore NewStoreFunc) {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)

	conversation, _, err := st.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_events",
		RequestID:      "req_events",
		ResponseID:     "resp_events",
		OwnerID:        "user_events",
		RequestFormat:  "responses",
		Model:          "events-model",
		UserContent:    "hello events",
		RequestBody:    map[string]any{"model": "events-model"},
	})
	if err != nil {
		t.Fatalf("create pending turn: %v", err)
	}

	first, err := st.AppendConversationEvent(ctx, common.AppendConversationEventInput{
		ConversationID: conversation.ID,
		OwnerID:        "user_events",
		Type:           "request_disconnected",
		Level:          "warn",
		Title:          "Request Disconnected",
		Detail:         "client closed request",
		RequestID:      "req_events",
	})
	if err != nil {
		t.Fatalf("append first conversation event: %v", err)
	}
	second, err := st.AppendConversationEvent(ctx, common.AppendConversationEventInput{
		ConversationID: conversation.ID,
		OwnerID:        "user_events",
		Type:           "request_aborted",
		Level:          "warn",
		Title:          "Request Aborted",
		Detail:         "manual abort",
		RequestID:      "req_events",
		CreatedAt:      first.CreatedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("append second conversation event: %v", err)
	}

	items, err := st.ListConversationEvents(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("list conversation events: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two conversation events, got %#v", items)
	}
	if items[0].ID != first.ID || items[1].ID != second.ID {
		t.Fatalf("unexpected event order: %#v", items)
	}
}

func assertOutputSegmentsEqual(t *testing.T, got, want []common.OutputSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("segment count mismatch got=%#v want=%#v", got, want)
	}
	for index := range want {
		if got[index].Mode != want[index].Mode ||
			got[index].Text != want[index].Text ||
			got[index].ReasoningStreamMode != want[index].ReasoningStreamMode {
			t.Fatalf("segment[%d] mismatch got=%#v want=%#v", index, got[index], want[index])
		}
	}
}
