package postgresql

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListConversations(ctx context.Context) ([]common.Conversation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(metadata_json->>'response_id', '')
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		s.logger(ctx).Warn("postgresql list requests failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.Conversation, 0)
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list requests row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) ListConversationsForOwnerPage(ctx context.Context, ownerID string, before time.Time, beforeID string, limit int) ([]common.Conversation, error) {
	if limit <= 0 {
		return []common.Conversation{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(metadata_json->>'response_id', '')
		FROM conversations
		WHERE COALESCE(metadata_json->>'owner_id', '') = $1
			AND ($3 = '' OR (updated_at, id) < ($2, $3))
		ORDER BY updated_at DESC, id DESC
		LIMIT $4
	`, strings.TrimSpace(ownerID), before, strings.TrimSpace(beforeID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]common.Conversation, 0, limit)
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (common.Conversation, error) {
	return scanConversation(s.pool.QueryRow(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(metadata_json->>'response_id', '')
		FROM conversations
		WHERE id = $1
	`, strings.TrimSpace(conversationID)))
}

func (s *Store) FindConversationByToolCallID(ctx context.Context, ownerID string, toolCallID string) (common.Conversation, error) {
	return scanConversation(s.pool.QueryRow(ctx, `
		SELECT c.id, c.title, c.created_at, c.updated_at, c.last_message_at, c.message_count, c.last_message_preview, c.last_user_text, c.metadata_json, COALESCE(c.metadata_json->>'response_id', '')
		FROM conversations c
		JOIN messages m ON m.conversation_id = c.id
		WHERE COALESCE(c.metadata_json->>'owner_id', '') = $1
			AND COALESCE(m.metadata_json->>'tool_call_id', '') = $2
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, strings.TrimSpace(ownerID), strings.TrimSpace(toolCallID)))
}

func (s *Store) ListRequests(ctx context.Context) ([]common.Request, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			c.id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.metadata_json->'request_debug'->>'request_id' IS NOT NULL
		ORDER BY c.updated_at DESC, m.created_at DESC, m.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.Request, 0)
	for rows.Next() {
		item, err := scanRequestRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetRequest(ctx context.Context, requestID string) (common.Request, error) {
	item, err := scanRequestRow(s.pool.QueryRow(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.metadata_json->'request_debug'->>'request_id' = $1
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, strings.TrimSpace(requestID)))
	if err != nil {
		s.logger(ctx).Warn("postgresql get request failed", zap.String("request.id", requestID), zap.Error(err))
		return common.Request{}, err
	}
	if item.RequestID == "" {
		item.RequestID = strings.TrimSpace(requestID)
	}
	return item, nil
}

func (s *Store) GetLatestRequestForConversation(ctx context.Context, conversationID string) (common.Request, error) {
	item, err := scanRequestRow(s.pool.QueryRow(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.conversation_id = $1
			AND m.metadata_json->'request_debug'->>'request_id' IS NOT NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, strings.TrimSpace(conversationID)))
	if err != nil {
		return common.Request{}, err
	}
	return item, nil
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]common.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, role, content, created_at, status, response_id, metadata_json
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC, id ASC
	`, strings.TrimSpace(conversationID))
	if err != nil {
		s.logger(ctx).Warn("postgresql list messages failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.Message, 0)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list messages row iteration failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteConversations(ctx context.Context, conversationIDs []string) (common.DeleteConversationsResult, error) {
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	if len(conversationIDs) == 0 {
		return common.DeleteConversationsResult{}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result common.DeleteConversationsResult
	rows, err := tx.Query(ctx, `
		SELECT id, COALESCE(metadata_json->>'owner_id', '')
		FROM conversations
		WHERE id = ANY($1)
	`, conversationIDs)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	for rows.Next() {
		var item common.DeletedConversation
		if err := rows.Scan(&item.ID, &item.OwnerID); err != nil {
			rows.Close()
			return common.DeleteConversationsResult{}, err
		}
		result.DeletedConversationItems = append(result.DeletedConversationItems, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE conversation_id = ANY($1)`, conversationIDs).Scan(&result.DeletedMessages); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM media_asset_refs WHERE conversation_id = ANY($1)) +
			(SELECT COUNT(*) FROM media_asset_event_refs WHERE conversation_id = ANY($1))
	`, conversationIDs).Scan(&result.DeletedAssetRefs); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM conversations WHERE id = ANY($1)`, conversationIDs)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	result.DeletedConversations = int(tag.RowsAffected())
	if err := tx.Commit(ctx); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	return result, nil
}

func (s *Store) ExpirePendingTurns(ctx context.Context, cutoff time.Time) (common.ExpirePendingTurnsResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE last_message_at < $1
			AND COALESCE(metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
	`, cutoff)
	if err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	type candidate struct {
		id       string
		metadata map[string]any
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		var metadataJSON []byte
		if err := rows.Scan(&item.id, &metadataJSON); err != nil {
			rows.Close()
			return common.ExpirePendingTurnsResult{}, err
		}
		item.metadata = ensureMap(parseJSONMap(metadataJSON))
		item.metadata["realtime_status"] = "expired"
		item.metadata["realtime_draft_text"] = ""
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	if len(candidates) == 0 {
		return common.ExpirePendingTurnsResult{}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	var result common.ExpirePendingTurnsResult
	for _, item := range candidates {
		tag, err := tx.Exec(ctx, `
			UPDATE conversations
			SET updated_at = $1, metadata_json = $2::jsonb
			WHERE id = $3
				AND COALESCE(metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
		`, now, mustJSON(item.metadata), item.id)
		if err != nil {
			return common.ExpirePendingTurnsResult{}, err
		}
		result.ExpiredConversations += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	return result, nil
}

func (s *Store) CreatePendingTurn(ctx context.Context, input common.CreatePendingInput) (common.Conversation, common.Message, error) {
	now := time.Now().UTC()
	metadata := map[string]any{}
	if input.ReuseConversation {
		existing, err := s.GetConversation(ctx, input.ConversationID)
		if err != nil {
			return common.Conversation{}, common.Message{}, err
		}
		metadata = ensureMap(existing.Metadata)
	} else {
		metadata = map[string]any{
			"owner_id":       strings.TrimSpace(input.OwnerID),
			"request_format": strings.TrimSpace(input.RequestFormat),
			"model":          strings.TrimSpace(input.Model),
		}
	}
	metadata["realtime_status"] = "waiting"
	metadata["realtime_draft_text"] = ""
	metadata["request_id"] = strings.TrimSpace(input.RequestID)
	metadata["response_id"] = strings.TrimSpace(input.ResponseID)
	if strings.TrimSpace(input.Model) != "" {
		metadata["model"] = strings.TrimSpace(input.Model)
	}
	userMessageMetadata := map[string]any{
		"request_format": strings.TrimSpace(input.RequestFormat),
		"model":          strings.TrimSpace(input.Model),
		"request_debug": map[string]any{
			"request_id":       strings.TrimSpace(input.RequestID),
			"response_id":      strings.TrimSpace(input.ResponseID),
			"model":            strings.TrimSpace(input.Model),
			"request_format":   strings.TrimSpace(input.RequestFormat),
			"request_keys":     keysOf(input.RequestBody),
			"request_method":   strings.TrimSpace(input.RequestMethod),
			"request_path":     strings.TrimSpace(input.RequestPath),
			"request_query":    input.RequestQuery,
			"request_headers":  input.RequestHeaders,
			"system_text":      strings.TrimSpace(input.SystemContent),
			"developer_text":   strings.TrimSpace(input.DeveloperContent),
			"assistant_text":   strings.TrimSpace(input.AssistantContent),
			"input_text":       input.UserContent,
			"request_body":     input.RequestBody,
			"raw_request_body": input.RawRequestBody,
			"request_options":  input.RequestOptions,
			"option_chips":     input.OptionChips,
			"tool_schemas":     input.ToolSchemas,
			"builtin_tools":    input.BuiltinTools,
			"tool_choice":      input.ToolChoice,
			"response_format":  input.ResponseFormat,
		},
	}
	conversation := common.Conversation{
		ID:         strings.TrimSpace(input.ConversationID),
		Metadata:   metadata,
		ResponseID: strings.TrimSpace(input.ResponseID),
	}
	if input.ReuseConversation {
		existing, err := s.GetConversation(ctx, input.ConversationID)
		if err != nil {
			return common.Conversation{}, common.Message{}, err
		}
		conversation = existing
		conversation.Metadata = metadata
		conversation.ResponseID = strings.TrimSpace(input.ResponseID)
		conversation.UpdatedAt = now
		conversation.LastMessageAt = now
		conversation.MessageCount += 1
		conversation.LastUserText = input.UserContent
		conversation.LastMessagePreview = input.UserContent
		if strings.TrimSpace(conversation.Title) == "" {
			conversation.Title = buildConversationTitle(input.UserContent)
		}
	} else {
		conversation.Title = buildConversationTitle(input.UserContent)
		conversation.LastUserText = input.UserContent
		conversation.CreatedAt = now
		conversation.UpdatedAt = now
		conversation.LastMessageAt = now
		conversation.MessageCount = 1
		conversation.LastMessagePreview = input.UserContent
	}
	responseID := strings.TrimSpace(input.ResponseID)
	message := common.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "user",
		Content:    firstNonEmpty(input.UserMessageContent, input.UserContent),
		CreatedAt:  now,
		Status:     "pending",
		ResponseID: &responseID,
		Metadata:   userMessageMetadata,
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql create pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if input.ReuseConversation {
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET title = $1, updated_at = $2, last_message_at = $3, message_count = $4, last_message_preview = $5, last_user_text = $6, metadata_json = $7::jsonb
			WHERE id = $8
		`,
			conversation.Title,
			conversation.UpdatedAt,
			conversation.LastMessageAt,
			conversation.MessageCount,
			conversation.LastMessagePreview,
			conversation.LastUserText,
			mustJSON(metadata),
			conversation.ID,
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversations(
				id, title, created_at, updated_at, last_message_at,
				message_count, last_message_preview, last_user_text, metadata_json
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		`,
			conversation.ID,
			conversation.Title,
			now,
			now,
			now,
			conversation.MessageCount,
			conversation.LastMessagePreview,
			conversation.LastUserText,
			mustJSON(metadata),
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`,
		message.ID,
		conversation.ID,
		message.Role,
		message.Content,
		now,
		message.Status,
		responseID,
		mustJSON(userMessageMetadata),
	); err != nil {
		return common.Conversation{}, common.Message{}, err
	}

	for _, asset := range input.PreparedImages {
		assetID := "asset_" + uuid.NewString()
		refID := "assetref_" + uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_assets(
				id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`,
			assetID,
			strings.TrimSpace(input.OwnerID),
			strings.TrimSpace(asset.FileID),
			strings.TrimSpace(asset.Path),
			strings.TrimSpace(asset.MediaType),
			asset.Bytes,
			strings.TrimSpace(asset.SHA256),
			asset.Width,
			asset.Height,
			strings.TrimSpace(asset.SourceKind),
			strings.TrimSpace(asset.OriginalName),
			strings.TrimSpace(asset.OriginalMediaType),
			now,
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO media_asset_refs(
				id, asset_id, file_id, owner_id, request_id, conversation_id, message_id, input_part_index, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`,
			refID,
			assetID,
			strings.TrimSpace(asset.FileID),
			strings.TrimSpace(input.OwnerID),
			strings.TrimSpace(input.RequestID),
			conversation.ID,
			message.ID,
			asset.InputPartIndex,
			now,
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql create pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn created", zap.String("conversation.id", input.ConversationID), zap.String("request.id", input.RequestID), zap.String("owner.id", input.OwnerID))
	return conversation, message, nil
}

func (s *Store) UpdateDraft(ctx context.Context, input common.UpdateDraftInput) (common.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isDraftWritable(metadata) {
		return common.Conversation{}, common.ErrTurnConflict
	}
	metadata["realtime_draft_text"] = input.DraftText
	metadata["realtime_output_segments"] = input.OutputSegments
	metadata["realtime_status"] = "streaming"
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.pool.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, metadata_json = $2::jsonb
		WHERE id = $3
	`, conversation.UpdatedAt, mustJSON(metadata), conversation.ID); err != nil {
		s.logger(ctx).Warn("postgresql update draft failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, err
	}
	s.logger(ctx).Debug("postgresql draft updated", zap.String("conversation.id", input.ConversationID), zap.Int("draft.length", len([]rune(input.DraftText))))
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input common.CompletePendingInput) (common.Conversation, common.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.Conversation{}, common.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return common.Conversation{}, common.Message{}, common.ErrTurnConflict
	}
	draftText, _ := metadata["realtime_draft_text"].(string)
	finalText := input.OutputText
	// Tool payloads never inherit draft answer text. Only ordinary completions may
	// fall back to the streamed draft when OutputText is empty.
	// tool_result Content must still materialize from ToolOutput when OutputText is
	// empty so message.Content, metadata.output, and workspace typed text stay aligned.
	if input.Mode == "tool_result" {
		finalText = stringValue(finalText, input.ToolOutput)
	} else if finalText == "" && input.Mode != "tool_call" {
		finalText = draftText
	}
	now := time.Now().UTC()
	metadata["realtime_status"] = "closed"
	metadata["realtime_draft_text"] = ""
	metadata["realtime_output_segments"] = nil

	messageMetadata := map[string]any{
		"response_mode": input.Mode,
	}
	// Tool turns store payload in arguments/output. Ordinary answer/thinking segments
	// do not apply; fail-safe omit output_segments so dirty draft state cannot stick.
	if input.Mode != "tool_call" && input.Mode != "tool_result" {
		messageMetadata["output_segments"] = input.OutputSegments
	}
	if input.ToolName != "" {
		messageMetadata["tool_name"] = input.ToolName
	}
	if input.ToolCallID != "" {
		messageMetadata["tool_call_id"] = input.ToolCallID
	}
	if input.Mode == "tool_call" {
		messageMetadata["arguments"] = finalText
	}
	if input.Mode == "tool_result" {
		messageMetadata["output"] = stringValue(input.ToolOutput, finalText)
	}
	if input.ReasoningStreamMode != "" {
		messageMetadata["reasoning_stream_mode"] = input.ReasoningStreamMode
	}
	if len(input.OutputPolicy) > 0 {
		messageMetadata["output_policy"] = input.OutputPolicy
	}
	responseID := strings.TrimSpace(input.ResponseID)
	message := common.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "assistant",
		Content:    finalText,
		CreatedAt:  now,
		Status:     "completed",
		ResponseID: &responseID,
		Metadata:   messageMetadata,
	}

	conversation.Metadata = metadata
	conversation.UpdatedAt = now
	conversation.LastMessageAt = now
	conversation.MessageCount += 1
	conversation.LastMessagePreview = input.OutputPreview
	if conversation.LastMessagePreview == "" {
		conversation.LastMessagePreview = finalText
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql complete pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, message.ID, conversation.ID, message.Role, message.Content, now, message.Status, responseID, mustJSON(messageMetadata)); err != nil {
		return common.Conversation{}, common.Message{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, last_message_at = $2, message_count = $3, last_message_preview = $4, metadata_json = $5::jsonb
		WHERE id = $6
	`, now, now, conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return common.Conversation{}, common.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql complete pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn completed", zap.String("conversation.id", input.ConversationID), zap.String("response.id", input.ResponseID), zap.String("mode", input.Mode))
	return conversation, message, nil
}

func (s *Store) AbortPendingTurn(ctx context.Context, input common.AbortPendingInput) (common.Conversation, common.Message, error) {
	result, err := s.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: input.ConversationID,
		Reason:         input.Reason,
	})
	return result.Conversation, result.Message, err
}

func (s *Store) AbortPendingTurnWithEvent(ctx context.Context, input common.PendingTurnLifecycleMutationInput) (common.PendingTurnMutationResult, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return common.PendingTurnMutationResult{}, common.ErrTurnConflict
	}
	metadata["realtime_status"] = "aborted"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()
	conversation.Metadata = metadata
	conversation.UpdatedAt = now

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.logger(ctx).Warn("postgresql abort pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.PendingTurnMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, metadata_json = $2::jsonb
		WHERE id = $3
	`, now, mustJSON(metadata), conversation.ID); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	event := buildConversationEventFromInput(conversation, common.AppendConversationEventInput{
		ID:             input.EventID,
		ConversationID: conversation.ID,
		OwnerID:        input.Identity.OwnerID,
		Type:           firstString(input.EventType, "request_aborted"),
		Level:          firstString(input.EventLevel, "warn"),
		Title:          firstString(input.EventTitle, "Request Aborted"),
		Detail:         firstString(input.EventDetail, input.Reason),
		RequestID:      input.Identity.RequestID,
		Metadata:       ensureMap(input.EventMetadata),
		CreatedAt:      input.EventCreatedAt,
	}, now)
	if err := insertConversationEventPostgreSQL(ctx, tx, event); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger(ctx).Warn("postgresql abort pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.PendingTurnMutationResult{}, err
	}
	s.logger(ctx).Debug("postgresql pending turn aborted", zap.String("conversation.id", input.ConversationID))
	return common.PendingTurnMutationResult{
		Conversation: conversation,
		Message:      common.Message{},
		Event:        event,
	}, nil
}

func (s *Store) DisconnectPendingTurn(ctx context.Context, input common.DisconnectPendingInput) (common.Conversation, common.Message, error) {
	result, err := s.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: input.ConversationID,
		Reason:         input.Reason,
	})
	return result.Conversation, result.Message, err
}

func (s *Store) DisconnectPendingTurnWithEvent(ctx context.Context, input common.PendingTurnLifecycleMutationInput) (common.PendingTurnMutationResult, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if isPendingRequestDisconnected(metadata) {
		return common.PendingTurnMutationResult{Conversation: conversation}, common.ErrPendingDisconnected
	}
	if !isTurnCompletable(metadata) {
		return common.PendingTurnMutationResult{}, common.ErrTurnConflict
	}
	metadata["realtime_status"] = "disconnected"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()
	conversation.Metadata = metadata
	conversation.UpdatedAt = now

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET updated_at = $1, metadata_json = $2::jsonb
		WHERE id = $3
	`, now, mustJSON(metadata), conversation.ID); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	event := buildConversationEventFromInput(conversation, common.AppendConversationEventInput{
		ID:             input.EventID,
		ConversationID: conversation.ID,
		OwnerID:        input.Identity.OwnerID,
		Type:           firstString(input.EventType, "request_disconnected"),
		Level:          firstString(input.EventLevel, "warn"),
		Title:          firstString(input.EventTitle, "Request Disconnected"),
		Detail:         firstString(input.EventDetail, input.Reason),
		RequestID:      input.Identity.RequestID,
		Metadata:       ensureMap(input.EventMetadata),
		CreatedAt:      input.EventCreatedAt,
	}, now)
	if err := insertConversationEventPostgreSQL(ctx, tx, event); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	return common.PendingTurnMutationResult{
		Conversation: conversation,
		Message:      common.Message{},
		Event:        event,
	}, nil
}

func (s *Store) DisconnectAllPendingTurns(ctx context.Context, reason string) (common.ExpirePendingTurnsResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id
		FROM conversations
		WHERE COALESCE(metadata_json->>'realtime_status', '') IN ('waiting', 'streaming')
	`)
	if err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return common.ExpirePendingTurnsResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}

	result := common.ExpirePendingTurnsResult{}
	for _, id := range ids {
		if _, _, err := s.DisconnectPendingTurn(ctx, common.DisconnectPendingInput{
			ConversationID: id,
			Reason:         reason,
		}); err == nil {
			result.ExpiredConversations++
		}
	}
	return result, nil
}

func scanConversation(row rowScanner) (common.Conversation, error) {
	var item common.Conversation
	var metadataJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.LastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Conversation{}, common.ErrNotFound
		}
		return common.Conversation{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func scanRequestRow(scanner rowScanner) (common.Request, error) {
	var item common.Request
	var messageMetadataJSON []byte
	var conversationMetadataJSON []byte
	if err := scanner.Scan(
		&item.ConversationID,
		&item.CreatedAt,
		&messageMetadataJSON,
		&item.UpdatedAt,
		&conversationMetadataJSON,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Request{}, common.ErrNotFound
		}
		return common.Request{}, err
	}

	messageMetadata := parseJSONMap(messageMetadataJSON)
	requestDebug, _ := messageMetadata["request_debug"].(map[string]any)
	conversationMetadata := parseJSONMap(conversationMetadataJSON)

	item.RequestID = metadataString(requestDebug, "request_id", "")
	item.OwnerID = metadataString(conversationMetadata, "owner_id", "")
	item.ResponseID = metadataString(requestDebug, "response_id", "")
	item.RequestFormat = metadataString(requestDebug, "request_format", "")
	item.Model = metadataString(requestDebug, "model", "")
	item.InputText = metadataString(requestDebug, "input_text", "")
	item.RequestMethod = metadataString(requestDebug, "request_method", "")
	item.RequestPath = metadataString(requestDebug, "request_path", "")
	item.RequestQuery = parseStringSliceMap(requestDebug["request_query"])
	item.RequestHeaders = parseStringSliceMap(requestDebug["request_headers"])
	item.Status = metadataString(conversationMetadata, "realtime_status", "")
	item.Metadata = messageMetadata
	item.RequestBody, _ = requestDebug["request_body"].(map[string]any)
	item.RawRequestBody, _ = requestDebug["raw_request_body"].(map[string]any)
	item.RequestOptions, _ = requestDebug["request_options"].(map[string]any)
	item.ToolSchemas, _ = requestDebug["tool_schemas"].([]any)
	item.BuiltinTools, _ = requestDebug["builtin_tools"].([]any)
	item.ToolChoice = parseRequestToolChoice(requestDebug["tool_choice"])
	item.ResponseFormat = parseRequestResponseFormat(requestDebug["response_format"])
	item.SystemText = metadataString(requestDebug, "system_text", "")
	item.DeveloperText = metadataString(requestDebug, "developer_text", "")
	item.AssistantText = metadataString(requestDebug, "assistant_text", "")
	return item, nil
}

func parseStringSliceMap(value any) map[string][]string {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string][]string, len(record))
	for key, raw := range record {
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, text)
		}
		if len(values) == 0 {
			continue
		}
		result[key] = values
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseRequestToolChoice(value any) common.RequestToolChoice {
	record, _ := value.(map[string]any)
	return common.RequestToolChoice{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
}

func parseRequestResponseFormat(value any) common.RequestResponseFormat {
	record, _ := value.(map[string]any)
	format := common.RequestResponseFormat{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
	format.Schema, _ = record["schema"].(map[string]any)
	return format
}

func scanMessage(row rowScanner) (common.Message, error) {
	var item common.Message
	var status *string
	var responseID *string
	var metadataJSON []byte
	if err := row.Scan(&item.ID, &item.Role, &item.Content, &item.CreatedAt, &status, &responseID, &metadataJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Message{}, common.ErrNotFound
		}
		return common.Message{}, err
	}
	if status != nil {
		item.Status = *status
	}
	item.ResponseID = responseID
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func keysOf(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildConversationTitle(userContent string) string {
	text := strings.TrimSpace(userContent)
	if text == "" {
		return "新会话"
	}
	runes := []rune(text)
	if len(runes) > 24 {
		return string(runes[:24])
	}
	return text
}

func stringValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isDraftWritable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func isTurnCompletable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func isPendingRequestDisconnected(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "disconnected"
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, _ := metadata[key].(string)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
