package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) ListConversations(ctx context.Context) ([]common.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.Conversation, 0)
	for rows.Next() {
		var item common.Conversation
		var createdAt, updatedAt, lastMessageAt string
		var metadataJSON string
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&createdAt,
			&updatedAt,
			&lastMessageAt,
			&item.MessageCount,
			&item.LastMessagePreview,
			&item.LastUserText,
			&metadataJSON,
			&item.ResponseID,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.LastMessageAt = parseTime(lastMessageAt)
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListConversationsForOwnerPage(ctx context.Context, ownerID string, before time.Time, beforeID string, limit int) ([]common.Conversation, error) {
	if limit <= 0 {
		return []common.Conversation{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?
			AND (? = '' OR updated_at < ? OR (updated_at = ? AND id < ?))
		ORDER BY updated_at DESC, id DESC
		LIMIT ?
	`, strings.TrimSpace(ownerID), strings.TrimSpace(beforeID), formatTime(before), formatTime(before), strings.TrimSpace(beforeID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]common.Conversation, 0, limit)
	for rows.Next() {
		var item common.Conversation
		var createdAt, updatedAt, lastMessageAt, metadataJSON string
		if err := rows.Scan(&item.ID, &item.Title, &createdAt, &updatedAt, &lastMessageAt, &item.MessageCount, &item.LastMessagePreview, &item.LastUserText, &metadataJSON, &item.ResponseID); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.LastMessageAt = parseTime(lastMessageAt)
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (common.Conversation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		WHERE id = ?
	`, conversationID)

	var item common.Conversation
	var createdAt, updatedAt, lastMessageAt string
	var metadataJSON string
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&createdAt,
		&updatedAt,
		&lastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger(ctx).Warn("sqlite get conversation not found", zap.String("conversation.id", conversationID))
			return common.Conversation{}, errNotFound
		}
		s.logger(ctx).Warn("sqlite get conversation failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return common.Conversation{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.LastMessageAt = parseTime(lastMessageAt)
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func (s *Store) FindConversationByToolCallID(ctx context.Context, ownerID string, toolCallID string) (common.Conversation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.title, c.created_at, c.updated_at, c.last_message_at, c.message_count, c.last_message_preview, c.last_user_text, c.metadata_json, COALESCE(json_extract(c.metadata_json, '$.response_id'), '')
		FROM conversations c
		JOIN messages m ON m.conversation_id = c.id
		WHERE json_extract(c.metadata_json, '$.owner_id') = ?
			AND COALESCE(json_extract(m.metadata_json, '$.tool_call_id'), '') = ?
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, strings.TrimSpace(ownerID), strings.TrimSpace(toolCallID))

	var item common.Conversation
	var createdAt, updatedAt, lastMessageAt string
	var metadataJSON string
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&createdAt,
		&updatedAt,
		&lastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Conversation{}, errNotFound
		}
		return common.Conversation{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.LastMessageAt = parseTime(lastMessageAt)
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func (s *Store) ListRequests(ctx context.Context) ([]common.Request, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') IS NOT NULL
		ORDER BY c.updated_at DESC, m.created_at DESC, m.id DESC
	`)
	if err != nil {
		s.logger(ctx).Warn("sqlite list requests failed", zap.Error(err))
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
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list requests row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) GetRequest(ctx context.Context, requestID string) (common.Request, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') = ?
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, requestID)

	item, err := scanRequestRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger(ctx).Warn("sqlite get request not found", zap.String("request.id", requestID))
			return common.Request{}, errNotFound
		}
		s.logger(ctx).Warn("sqlite get request failed", zap.String("request.id", requestID), zap.Error(err))
		return common.Request{}, err
	}
	if item.RequestID == "" {
		item.RequestID = requestID
	}
	return item, nil
}

func (s *Store) GetLatestRequestForConversation(ctx context.Context, conversationID string) (common.Request, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND m.conversation_id = ?
			AND json_extract(m.metadata_json, '$.request_debug.request_id') IS NOT NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, conversationID)

	item, err := scanRequestRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Request{}, errNotFound
		}
		return common.Request{}, err
	}
	return item, nil
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]common.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, content, created_at, status, response_id, metadata_json
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at ASC, id ASC
	`, conversationID)
	if err != nil {
		s.logger(ctx).Warn("sqlite list messages failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.Message, 0)
	for rows.Next() {
		var item common.Message
		var createdAt string
		var status sql.NullString
		var responseID sql.NullString
		var metadataJSON string
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &createdAt, &status, &responseID, &metadataJSON); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.Status = status.String
		if responseID.Valid {
			item.ResponseID = &responseID.String
		}
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list messages row iteration failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteConversations(ctx context.Context, conversationIDs []string) (common.DeleteConversationsResult, error) {
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	if len(conversationIDs) == 0 {
		return common.DeleteConversationsResult{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(conversationIDs)), ",")
	args := make([]any, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		args = append(args, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result common.DeleteConversationsResult
	listDeletedQuery := fmt.Sprintf(`
		SELECT id, COALESCE(json_extract(metadata_json, '$.owner_id'), '')
		FROM conversations
		WHERE id IN (%s)
	`, placeholders)
	rows, err := tx.QueryContext(ctx, listDeletedQuery, args...)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	for rows.Next() {
		var item common.DeletedConversation
		if err := rows.Scan(&item.ID, &item.OwnerID); err != nil {
			_ = rows.Close()
			return common.DeleteConversationsResult{}, err
		}
		result.DeletedConversationItems = append(result.DeletedConversationItems, item)
	}
	if err := rows.Close(); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	if err := rows.Err(); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	countMessagesQuery := fmt.Sprintf(`SELECT COUNT(*) FROM messages WHERE conversation_id IN (%s)`, placeholders)
	if err := tx.QueryRowContext(ctx, countMessagesQuery, args...).Scan(&result.DeletedMessages); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	countAssetRefsQuery := fmt.Sprintf(`
		SELECT
			(SELECT COUNT(*) FROM media_asset_refs WHERE conversation_id IN (%s)) +
			(SELECT COUNT(*) FROM media_asset_event_refs WHERE conversation_id IN (%s))
	`, placeholders, placeholders)
	assetArgs := append(append([]any(nil), args...), args...)
	if err := tx.QueryRowContext(ctx, countAssetRefsQuery, assetArgs...).Scan(&result.DeletedAssetRefs); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	deleteQuery := fmt.Sprintf(`DELETE FROM conversations WHERE id IN (%s)`, placeholders)
	res, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return common.DeleteConversationsResult{}, err
	}
	result.DeletedConversations = int(rowsAffected)
	if err := tx.Commit(); err != nil {
		return common.DeleteConversationsResult{}, err
	}
	return result, nil
}

func (s *Store) ExpirePendingTurns(ctx context.Context, cutoff time.Time) (common.ExpirePendingTurnsResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE last_message_at < ?
			AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
	`, formatTime(cutoff))
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
		var metadataJSON string
		if err := rows.Scan(&item.id, &metadataJSON); err != nil {
			_ = rows.Close()
			return common.ExpirePendingTurnsResult{}, err
		}
		item.metadata = ensureMap(parseJSONMap(metadataJSON))
		item.metadata["realtime_status"] = "expired"
		item.metadata["realtime_draft_text"] = ""
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	if err := rows.Err(); err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	if len(candidates) == 0 {
		return common.ExpirePendingTurnsResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	var result common.ExpirePendingTurnsResult
	for _, item := range candidates {
		res, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET updated_at = ?, metadata_json = ?
			WHERE id = ?
				AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
		`, formatTime(now), mustJSON(item.metadata), item.id)
		if err != nil {
			return common.ExpirePendingTurnsResult{}, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return common.ExpirePendingTurnsResult{}, err
		}
		result.ExpiredConversations += int(affected)
	}
	if err := tx.Commit(); err != nil {
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
			"owner_id":       input.OwnerID,
			"request_format": input.RequestFormat,
			"model":          input.Model,
		}
	}
	metadata["realtime_status"] = "waiting"
	metadata["realtime_draft_text"] = ""
	metadata["request_id"] = input.RequestID
	metadata["response_id"] = input.ResponseID
	if strings.TrimSpace(input.Model) != "" {
		metadata["model"] = input.Model
	}
	userMessageMetadata := map[string]any{
		"request_format": input.RequestFormat,
		"model":          input.Model,
		"request_debug": map[string]any{
			"request_id":       input.RequestID,
			"response_id":      input.ResponseID,
			"model":            input.Model,
			"request_format":   input.RequestFormat,
			"request_keys":     keysOf(input.RequestBody),
			"request_method":   input.RequestMethod,
			"request_path":     input.RequestPath,
			"request_query":    input.RequestQuery,
			"request_headers":  input.RequestHeaders,
			"system_text":      input.SystemContent,
			"developer_text":   input.DeveloperContent,
			"assistant_text":   input.AssistantContent,
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
		ID:         input.ConversationID,
		Metadata:   metadata,
		ResponseID: input.ResponseID,
	}
	if input.ReuseConversation {
		existing, err := s.GetConversation(ctx, input.ConversationID)
		if err != nil {
			return common.Conversation{}, common.Message{}, err
		}
		conversation = existing
		conversation.Metadata = metadata
		conversation.ResponseID = input.ResponseID
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
	responseID := input.ResponseID
	message := common.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "user",
		Content:    firstNonEmpty(input.UserMessageContent, input.UserContent),
		CreatedAt:  now,
		Status:     "pending",
		ResponseID: &responseID,
		Metadata:   userMessageMetadata,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite create pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if input.ReuseConversation {
		if _, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET title = ?, updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, last_user_text = ?, metadata_json = ?
			WHERE id = ?
		`,
			conversation.Title,
			formatTime(conversation.UpdatedAt),
			formatTime(conversation.LastMessageAt),
			conversation.MessageCount,
			conversation.LastMessagePreview,
			conversation.LastUserText,
			mustJSON(metadata),
			conversation.ID,
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO conversations(
				id, title, created_at, updated_at, last_message_at,
				message_count, last_message_preview, last_user_text, metadata_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			conversation.ID,
			conversation.Title,
			formatTime(now),
			formatTime(now),
			formatTime(now),
			conversation.MessageCount,
			conversation.LastMessagePreview,
			conversation.LastUserText,
			mustJSON(metadata),
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		message.ID,
		conversation.ID,
		message.Role,
		message.Content,
		formatTime(now),
		message.Status,
		responseID,
		mustJSON(userMessageMetadata),
	); err != nil {
		return common.Conversation{}, common.Message{}, err
	}

	for _, asset := range input.PreparedImages {
		assetID := "asset_" + uuid.NewString()
		refID := "assetref_" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO media_assets(
				id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			assetID,
			input.OwnerID,
			asset.FileID,
			asset.Path,
			asset.MediaType,
			asset.Bytes,
			asset.SHA256,
			asset.Width,
			asset.Height,
			asset.SourceKind,
			asset.OriginalName,
			asset.OriginalMediaType,
			formatTime(now),
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO media_asset_refs(
				id, asset_id, file_id, owner_id, request_id, conversation_id, message_id, input_part_index, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			refID,
			assetID,
			asset.FileID,
			input.OwnerID,
			input.RequestID,
			conversation.ID,
			message.ID,
			asset.InputPartIndex,
			formatTime(now),
		); err != nil {
			return common.Conversation{}, common.Message{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite create pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn created", zap.String("conversation.id", input.ConversationID), zap.String("request.id", input.RequestID), zap.String("owner.id", input.OwnerID))
	return conversation, message, nil
}

func (s *Store) ListMediaAssets(ctx context.Context) ([]common.MediaAsset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		FROM media_assets
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateMediaAsset(ctx context.Context, input common.CreateMediaAssetInput) (common.MediaAsset, error) {
	return s.createMediaAsset(ctx, input, "", "")
}

func (s *Store) CreateStagedMediaAsset(ctx context.Context, input common.CreateStagedMediaAssetInput) (common.MediaAsset, error) {
	return s.createMediaAsset(ctx, input.Asset, strings.TrimSpace(input.ConversationID), strings.TrimSpace(input.RequestID))
}

func (s *Store) createMediaAsset(ctx context.Context, input common.CreateMediaAssetInput, conversationID string, requestID string) (common.MediaAsset, error) {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	item := common.MediaAsset{
		ID: input.ID, OwnerID: input.OwnerID, FileID: input.FileID, Path: input.Path,
		MediaType: input.MediaType, Bytes: input.Bytes, SHA256: input.SHA256,
		Width: input.Width, Height: input.Height, SourceKind: input.SourceKind,
		OriginalName: input.OriginalName, OriginalMediaType: input.OriginalMediaType, CreatedAt: createdAt,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.MediaAsset{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO media_assets(
			id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.OwnerID, item.FileID, item.Path, item.MediaType, item.Bytes, item.SHA256, item.Width, item.Height, item.SourceKind, item.OriginalName, item.OriginalMediaType, formatTime(item.CreatedAt))
	if err != nil {
		return common.MediaAsset{}, err
	}
	if conversationID != "" {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO media_asset_staging(asset_id, owner_id, conversation_id, request_id, created_at)
			SELECT ?, ?, ?, ?, ?
			WHERE EXISTS (
				SELECT 1 FROM conversations c
				JOIN messages m ON m.conversation_id = c.id
				WHERE c.id = ?
				  AND COALESCE(json_extract(c.metadata_json, '$.owner_id'), '') = ?
				  AND COALESCE(json_extract(c.metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
				  AND COALESCE(json_extract(c.metadata_json, '$.request_id'), '') = ?
				  AND COALESCE(json_extract(m.metadata_json, '$.request_debug.request_id'), '') = ?
			)
		`, item.ID, item.OwnerID, conversationID, requestID, formatTime(item.CreatedAt), conversationID, item.OwnerID, requestID, requestID)
		if err != nil {
			return common.MediaAsset{}, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return common.MediaAsset{}, err
		}
		if rows != 1 {
			return common.MediaAsset{}, common.ErrNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return common.MediaAsset{}, err
	}
	return item, nil
}

func (s *Store) GetMediaAssetByID(ctx context.Context, assetID string) (common.MediaAsset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		FROM media_assets
		WHERE id = ?
	`, strings.TrimSpace(assetID))
	return scanMediaAsset(row)
}

func (s *Store) GetStagedMediaAsset(ctx context.Context, assetID string, ownerID string, conversationID string, requestID string) (common.MediaAsset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT a.id, a.owner_id, a.file_id, a.path, a.media_type, a.bytes, a.sha256, a.width, a.height, a.source_kind, a.original_name, a.original_media_type, a.created_at
		FROM media_assets a
		JOIN media_asset_staging st ON st.asset_id = a.id
		WHERE a.id = ? AND st.owner_id = ? AND st.conversation_id = ? AND st.request_id = ?
	`, strings.TrimSpace(assetID), strings.TrimSpace(ownerID), strings.TrimSpace(conversationID), strings.TrimSpace(requestID))
	return scanMediaAsset(row)
}

func (s *Store) GetMediaAssetByFileID(ctx context.Context, fileID string) (common.MediaAsset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		FROM media_assets
		WHERE file_id = ?
	`, strings.TrimSpace(fileID))
	item, err := scanMediaAsset(row)
	if err != nil {
		return common.MediaAsset{}, err
	}
	return item, nil
}

func (s *Store) ListOrphanMediaAssets(ctx context.Context) ([]common.MediaAsset, error) {
	cutoff := formatTime(time.Now().UTC().Add(-24 * time.Hour))
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.owner_id, a.file_id, a.path, a.media_type, a.bytes, a.sha256, a.width, a.height, a.source_kind, a.original_name, a.original_media_type, a.created_at
		FROM media_assets a
			LEFT JOIN media_asset_refs r ON r.asset_id = a.id
			LEFT JOIN media_asset_event_refs er ON er.asset_id = a.id
			LEFT JOIN media_asset_staging st ON st.asset_id = a.id
			WHERE r.id IS NULL AND er.id IS NULL
			  AND (st.asset_id IS NULL OR st.created_at < ?)
			ORDER BY a.created_at ASC, a.id ASC
		`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteMediaAssetsByIDs(ctx context.Context, ids []string) (int, error) {
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	query := fmt.Sprintf(`DELETE FROM media_assets WHERE id IN (%s)`, placeholders)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rowsAffected), nil
}

func (s *Store) UpdateDraft(ctx context.Context, input common.UpdateDraftInput) (common.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isDraftWritable(metadata) {
		return common.Conversation{}, errConflict
	}
	metadata["realtime_draft_text"] = input.DraftText
	metadata["realtime_output_segments"] = input.OutputSegments
	metadata["realtime_status"] = "streaming"
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(conversation.UpdatedAt), mustJSON(metadata), conversation.ID); err != nil {
		s.logger(ctx).Warn("sqlite update draft failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, err
	}
	s.logger(ctx).Debug("sqlite draft updated", zap.String("conversation.id", input.ConversationID), zap.Int("draft.length", len([]rune(input.DraftText))))
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input common.CompletePendingInput) (common.Conversation, common.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return common.Conversation{}, common.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return common.Conversation{}, common.Message{}, errConflict
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
	responseID := input.ResponseID
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite complete pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, conversation.ID, message.Role, message.Content, formatTime(now), message.Status, responseID, mustJSON(messageMetadata)); err != nil {
		return common.Conversation{}, common.Message{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return common.Conversation{}, common.Message{}, err
	}

	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite complete pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.Conversation{}, common.Message{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn completed", zap.String("conversation.id", input.ConversationID), zap.String("response.id", input.ResponseID), zap.String("mode", input.Mode))
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
		return common.PendingTurnMutationResult{}, errConflict
	}
	metadata["realtime_status"] = "aborted"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()
	conversation.Metadata = metadata
	conversation.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger(ctx).Warn("sqlite abort pending turn begin tx failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.PendingTurnMutationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), mustJSON(metadata), conversation.ID); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	event := buildConversationEventFromInput(conversation, common.AppendConversationEventInput{
		ID:             input.EventID,
		ConversationID: conversation.ID,
		OwnerID:        input.Identity.OwnerID,
		Type:           stringValue(input.EventType, "request_aborted"),
		Level:          stringValue(input.EventLevel, "warn"),
		Title:          stringValue(input.EventTitle, "Request Aborted"),
		Detail:         stringValue(input.EventDetail, input.Reason),
		RequestID:      input.Identity.RequestID,
		Metadata:       ensureMap(input.EventMetadata),
		CreatedAt:      input.EventCreatedAt,
	}, now)
	if err := insertConversationEventSQLite(ctx, tx, event); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		s.logger(ctx).Warn("sqlite abort pending turn commit failed", zap.String("conversation.id", input.ConversationID), zap.Error(err))
		return common.PendingTurnMutationResult{}, err
	}
	s.logger(ctx).Debug("sqlite pending turn aborted", zap.String("conversation.id", input.ConversationID))
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
		return common.PendingTurnMutationResult{}, errConflict
	}
	metadata["realtime_status"] = "disconnected"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()
	conversation.Metadata = metadata
	conversation.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), mustJSON(metadata), conversation.ID); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	event := buildConversationEventFromInput(conversation, common.AppendConversationEventInput{
		ID:             input.EventID,
		ConversationID: conversation.ID,
		OwnerID:        input.Identity.OwnerID,
		Type:           stringValue(input.EventType, "request_disconnected"),
		Level:          stringValue(input.EventLevel, "warn"),
		Title:          stringValue(input.EventTitle, "Request Disconnected"),
		Detail:         stringValue(input.EventDetail, input.Reason),
		RequestID:      input.Identity.RequestID,
		Metadata:       ensureMap(input.EventMetadata),
		CreatedAt:      input.EventCreatedAt,
	}, now)
	if err := insertConversationEventSQLite(ctx, tx, event); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return common.PendingTurnMutationResult{}, err
	}
	return common.PendingTurnMutationResult{
		Conversation: conversation,
		Message:      common.Message{},
		Event:        event,
	}, nil
}

func (s *Store) DisconnectAllPendingTurns(ctx context.Context, reason string) (common.ExpirePendingTurnsResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
	`)
	if err != nil {
		return common.ExpirePendingTurnsResult{}, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		var metadataJSON string
		if err := rows.Scan(&id, &metadataJSON); err != nil {
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
