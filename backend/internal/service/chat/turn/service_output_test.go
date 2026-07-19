package turn_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	"github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	"github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputpolicy"
	"github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	"github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
	"github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

func TestUpdateDraftAutomaticallyCompletesOnCrossChunkStop(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "gpt-4o",
		Options:  protocol.TurnOptions{Stop: []string{"END"}},
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_stop", RequestID: "req_stop", ResponseID: "resp_stop",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_stop", ResponseID: "resp_stop", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_stop"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	first, err := service.UpdateDraft(ctx, conversation.ID, "answer E", "answer", "")
	if err != nil {
		t.Fatal(err)
	}
	if first["draft_text"] != "answer " {
		t.Fatalf("stop prefix was not withheld: %#v", first)
	}
	second, err := service.UpdateDraft(ctx, conversation.ID, "ND ignored", "answer", "")
	if err != nil {
		t.Fatal(err)
	}
	if second["auto_completed"] != true || second["output_text"] != "answer " {
		t.Fatalf("unexpected automatic completion: %#v", second)
	}
	if _, ok := registry.GetByConversationID(conversation.ID); ok {
		t.Fatal("automatically completed turn remained pending")
	}
	messages, err := store.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	policy, _ := last.Metadata["output_policy"].(map[string]any)
	if policy["finish_reason"] != "stop_sequence" || policy["stop_sequence"] != "END" {
		t.Fatalf("missing final output policy metadata: %#v", last.Metadata)
	}
}

func TestExecuteTurnControlRejectsReplacedRequestIdentity(t *testing.T) {
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		ConversationID: "conv", RequestID: "req_new", ResponseID: "resp_new", OwnerID: "owner",
		Events: make(chan turn.PendingEvent, 1), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Pending: registry}
	_, err := service.ExecuteTurnControl(context.Background(), turn.TurnControlCommand{
		ConversationID: "conv", RequestID: "req_old",
		Action: turn.OutputAction{Kind: turn.TurnControlStreamDelta, OutputText: "must not apply"},
	})
	if err != turn.ErrPendingConflict {
		t.Fatalf("expected request identity conflict, got %v", err)
	}
}

func TestExecuteTurnControlAbortsWhenMessageEventLimitIsExceeded(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_event_limit", RequestID: "req_event_limit", ResponseID: "resp_event_limit",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{Protocol: protocol.ProtocolResponses, Model: "gpt-4o"}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registered := &turn.PendingTurn{
		RequestID: "req_event_limit", ResponseID: "resp_event_limit", ConversationID: conversation.ID,
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", NormalizedRequest: request, OutputGuard: guard,
		Runtime:         protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_event_limit"}),
		MaxOutputEvents: 1,
		CreatedAt:       time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	}
	registry.Add(registered)
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	command := turn.TurnControlCommand{
		ConversationID: conversation.ID, RequestID: "req_event_limit",
		Action: turn.OutputAction{Kind: turn.TurnControlStreamDelta, OutputText: "first"},
	}
	stale := command
	stale.RequestID = "req_replaced"
	if _, err := service.ExecuteTurnControl(ctx, stale); err != turn.ErrPendingConflict {
		t.Fatalf("expected stale request rejection, got %v", err)
	}
	if _, err := service.ExecuteTurnControl(ctx, command); err != nil {
		t.Fatalf("first event should be accepted: %v", err)
	}
	command.Action.OutputText = "must not be written"
	result, err := service.ExecuteTurnControl(ctx, command)
	if err != nil {
		t.Fatalf("limit should abort through the protocol path: %v", err)
	}
	if result["aborted"] != true || result["reason"] != "message event limit exceeded" {
		t.Fatalf("unexpected limit result: %#v", result)
	}
	stored, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["realtime_status"] != "aborted" {
		t.Fatalf("expected aborted conversation, got %#v", stored)
	}
	draft, _ := stored.Metadata["realtime_draft_text"].(string)
	if strings.Contains(draft, "must not be written") {
		t.Fatalf("over-limit event changed the draft: %#v", stored.Metadata)
	}
	firstEvent := <-registered.Events
	abortEvent := <-registered.Events
	if firstEvent.Action.Kind != turn.TurnControlStreamDelta {
		t.Fatalf("unexpected first event: %#v", firstEvent)
	}
	if abortEvent.Action.Kind != turn.TurnControlAbort || len(abortEvent.StreamEvents) != 1 || abortEvent.StreamEvents[0].Event != "response.failed" {
		t.Fatalf("expected standard responses abort event: %#v", abortEvent)
	}
}

func TestConcurrentDeltasKeepGuardAndPersistedDraftInSync(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{Protocol: protocol.ProtocolResponses, Model: "gpt-4o"}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_concurrent", RequestID: "req_concurrent", ResponseID: "resp_concurrent",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_concurrent", ResponseID: "resp_concurrent", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_concurrent"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	start := make(chan struct{})
	errorsByDelta := make(chan error, 2)
	var wait sync.WaitGroup
	for _, delta := range []string{"A", "B"} {
		wait.Add(1)
		go func(text string) {
			defer wait.Done()
			<-start
			_, updateErr := service.UpdateDraft(ctx, conversation.ID, text, "answer", "")
			errorsByDelta <- updateErr
		}(delta)
	}
	close(start)
	wait.Wait()
	close(errorsByDelta)
	for updateErr := range errorsByDelta {
		if updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	result, err := service.CompleteConversation(ctx, common.CompletePendingInput{
		ConversationID: conversation.ID, ResponseID: "resp_concurrent", Mode: "answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result["output_text"].(string)
	if output != "AB" && output != "BA" {
		t.Fatalf("concurrent delta was lost from persisted output: %q", output)
	}
}

func TestAutomaticThinkingCompletionPersistsAnswerOnlyContent(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "gpt-4o",
		Options:  protocol.TurnOptions{Stop: []string{"END"}},
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_thinking", RequestID: "req_thinking", ResponseID: "resp_thinking",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_thinking", ResponseID: "resp_thinking", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_thinking"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	if _, err := service.UpdateDraft(ctx, conversation.ID, "reason E", "thinking", "reasoning"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDraft(ctx, conversation.ID, "ND", "thinking", "reasoning"); err != nil {
		t.Fatal(err)
	}
	messages, err := store.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	// Thinking-only completion: Content stays empty; durable truth is output_segments.
	if last.Content != "" {
		t.Fatalf("thinking leaked into answer-only Content: %q", last.Content)
	}
	if strings.Contains(last.Content, "<think>") {
		t.Fatalf("legacy think tags must not be written: %q", last.Content)
	}
	segmentsRaw, _ := last.Metadata["output_segments"]
	segmentsJSON, _ := json.Marshal(segmentsRaw)
	if !strings.Contains(string(segmentsJSON), "reason ") {
		t.Fatalf("thinking segment missing after stop-sequence complete: %#v", last.Metadata["output_segments"])
	}
	completed, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.LastMessagePreview != "reason " {
		t.Fatalf("thinking-only preview should fall back to thinking text: %q", completed.LastMessagePreview)
	}
}

func TestAlternatingSegmentsSurviveDraftReloadAndComplete(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{Protocol: protocol.ProtocolResponses, Model: "gpt-4o"}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_segments", RequestID: "req_segments", ResponseID: "resp_segments",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "question",
	})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	registry := pending.NewPendingRegistry()
	registry.Add(&turn.PendingTurn{
		RequestID: "req_segments", ResponseID: "resp_segments", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_segments"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	})
	service := &turn.Service{Store: store, Pending: registry, OwnerIDFromContext: func(context.Context) string { return "user_a" }}
	steps := []struct {
		text string
		mode string
		rsm  string
	}{
		{"alpha", "thinking", "summary"},
		{"show <think>literal</think> ", "answer", ""},
		{"gamma", "thinking", "reasoning"},
		{"delta", "answer", ""},
	}
	for _, step := range steps {
		if _, err := service.UpdateDraft(ctx, conversation.ID, step.text, step.mode, step.rsm); err != nil {
			t.Fatalf("update draft %s/%s: %v", step.mode, step.text, err)
		}
		reloaded, getErr := store.GetConversation(ctx, conversation.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		state := conversationstate.FromConversation(reloaded)
		if len(state.OutputSegments) == 0 {
			t.Fatalf("segments lost after draft reload: %#v", reloaded.Metadata["realtime_output_segments"])
		}
	}
	result, err := service.CompleteConversation(ctx, common.CompletePendingInput{
		ConversationID: conversation.ID, ResponseID: "resp_segments", Mode: "answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["output_text"] != "show <think>literal</think> delta" {
		t.Fatalf("complete output_text not answer-only: %#v", result)
	}
	messages, err := store.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	if last.Content != "show <think>literal</think> delta" {
		t.Fatalf("persisted content changed: %q", last.Content)
	}
	segments := conversationstate.DecodeOutputSegments(last.Metadata["output_segments"])
	if len(segments) != 4 {
		t.Fatalf("expected 4 segments, got %#v", segments)
	}
	want := []string{"thinking:alpha", "answer:show <think>literal</think> ", "thinking:gamma", "answer:delta"}
	for index, segment := range segments {
		got := segment.Mode + ":" + segment.Text
		if got != want[index] {
			t.Fatalf("segment order/content at %d: got=%q want=%q", index, got, want[index])
		}
	}
}

func TestImageGenerationPersistsURLAndOnlyStreamsDerivedBase64(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_image", RequestID: "req_image", ResponseID: "resp_image",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "draw",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses, Model: "gpt-4o",
		BuiltinTools: []protocol.BuiltinTool{{Kind: "image_generation", Type: "image_generation"}},
	}
	guard, err := outputpolicy.NewGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	pendingTurn := &turn.PendingTurn{
		RequestID: "req_image", ResponseID: "resp_image", ConversationID: conversation.ID,
		OwnerID: "user_a", NormalizedRequest: request, OutputGuard: guard,
		Runtime:   protocolruntime.New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-4o", ResponseID: "resp_image"}),
		CreatedAt: time.Now().UTC(), Events: make(chan turn.PendingEvent, 8), Done: make(chan turn.PendingResult, 1),
	}
	registry := pending.NewPendingRegistry()
	registry.Add(pendingTurn)
	assetService := outputasset.New(config.Config{
		UploadMaxBytes: 1 << 20, MediaMaxBytes: 1 << 20, MediaAVIFQuality: 50,
	}, store, localstore.Store{RootDir: t.TempDir()})
	service := &turn.Service{
		Store: store, Pending: registry, OutputAssets: assetService,
		OwnerIDFromContext: func(context.Context) string { return "user_a" },
	}
	uploaded, err := service.UploadOutputImage(ctx, "user_a", conversation.ID, "result.png", "image/png", bytes.NewReader(outputTestPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := service.EmitBuiltinTool(ctx, turn.TurnControlCommand{
		ConversationID: conversation.ID,
		Action: turn.OutputAction{
			Kind: turn.TurnControlBuiltinTool, BuiltinToolKind: "image_generation", BuiltinToolAssetID: uploaded.AssetID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := body["event"].(common.ConversationEvent)
	metadataJSON, _ := json.Marshal(event.Metadata)
	if strings.Contains(string(metadataJSON), "base64") || strings.Contains(string(metadataJSON), uploaded.URL) {
		t.Fatalf("timeline metadata must not duplicate media authority: %s", metadataJSON)
	}
	if len(event.MediaAssets) != 1 || event.MediaAssets[0].URL != uploaded.URL {
		t.Fatalf("event media ref did not retain URL: %#v", event.MediaAssets)
	}
	var refCount, stagingCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset_event_refs WHERE event_id = ?`, event.ID).Scan(&refCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset_staging WHERE asset_id = ?`, uploaded.AssetID).Scan(&stagingCount); err != nil {
		t.Fatal(err)
	}
	if refCount != 1 || stagingCount != 0 {
		t.Fatalf("unexpected asset lifecycle: refs=%d staging=%d", refCount, stagingCount)
	}
	pendingEvent := <-pendingTurn.Events
	var resultBase64 string
	for _, streamEvent := range pendingEvent.StreamEvents {
		if streamEvent.Event != "response.output_item.done" {
			continue
		}
		data, _ := streamEvent.Data.(map[string]any)
		item, _ := data["item"].(map[string]any)
		resultBase64, _ = item["result"].(string)
	}
	decoded, err := base64.StdEncoding.DecodeString(resultBase64)
	if err != nil || len(decoded) == 0 {
		t.Fatalf("missing protocol image base64: %v", err)
	}
	mediaType, _, _, err := media.InspectImageBytes(decoded)
	if err != nil || mediaType != "image/avif" {
		t.Fatalf("protocol result was not AVIF: media=%q err=%v", mediaType, err)
	}
}

func outputTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{B: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
