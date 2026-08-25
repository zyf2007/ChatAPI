package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type memoryRules struct {
	mu    sync.Mutex
	items map[string]common.AutomationRule
}

func newMemoryRules() *memoryRules { return &memoryRules{items: map[string]common.AutomationRule{}} }

func (m *memoryRules) ListAutomationRulesByUser(_ context.Context, ownerID string) ([]common.AutomationRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []common.AutomationRule{}
	for _, item := range m.items {
		if item.UserID == ownerID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *memoryRules) UpsertAutomationRule(_ context.Context, input common.UpsertAutomationRuleInput) (common.AutomationRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	item := common.AutomationRule{ID: input.ID, UserID: input.UserID, Enabled: input.Enabled, Payload: input.Payload, CreatedAt: now, UpdatedAt: now}
	if existing, ok := m.items[input.UserID+"/"+input.ID]; ok {
		item.CreatedAt = existing.CreatedAt
	}
	m.items[input.UserID+"/"+input.ID] = item
	return item, nil
}

func (m *memoryRules) DeleteAutomationRule(_ context.Context, ownerID string, ruleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ownerID + "/" + ruleID
	if _, ok := m.items[key]; !ok {
		return common.ErrNotFound
	}
	delete(m.items, key)
	return nil
}

type memoryPending struct {
	items map[string]*turnsvc.PendingTurn
}

type memoryModelKeys struct {
	items []common.ModelAPIKey
}

func (m memoryModelKeys) ListModelAPIKeysByUser(context.Context, string) ([]common.ModelAPIKey, error) {
	return append([]common.ModelAPIKey(nil), m.items...), nil
}

func (m memoryPending) GetByConversationID(id string) (*turnsvc.PendingTurn, bool) {
	item, ok := m.items[id]
	return item, ok
}

type capturedControl struct{ calls chan controlsvc.Command }

func (c capturedControl) Execute(_ context.Context, command controlsvc.Command) (controlsvc.Result, error) {
	c.calls <- command
	return controlsvc.Result{Body: map[string]any{"ok": true}}, nil
}

func (c capturedControl) Synchronize(_ context.Context, _ string, fn func() error) error { return fn() }

type controlledTurn struct {
	automationCalls chan turnsvc.TurnControlCommand
	manualStarted   chan struct{}
	releaseManual   chan struct{}
	failText        string
}

func (t *controlledTurn) ActiveRequestID(string) (string, bool) { return "req", true }

func (t *controlledTurn) ExecuteTurnControl(_ context.Context, command turnsvc.TurnControlCommand) (map[string]any, error) {
	if command.Action.OutputText == t.failText {
		return nil, context.Canceled
	}
	if command.Action.OutputText == "manual-block" {
		close(t.manualStarted)
		<-t.releaseManual
		return map[string]any{"ok": true}, nil
	}
	t.automationCalls <- command
	return map[string]any{"ok": true}, nil
}

type capturedRealtime struct {
	mu       sync.Mutex
	payloads []any
}

func (r *capturedRealtime) PublishAutomationState(_ context.Context, payload StateEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads = append(r.payloads, payload)
}

func validRule() Rule {
	return Rule{
		SchemaVersion: SchemaVersion, ID: "rule_a", Name: "recorded rule", Enabled: true,
		Match:    MatchSpec{Pattern: `^hello`, ModelPattern: `^demo-.*`, ModelKeyID: "modelkey_a"},
		Playback: PlaybackSpec{Mode: "fixed", FixedIntervalMS: 0},
		Steps:    []Step{{ID: "step_a", Action: Action{Kind: "stream_delta", Mode: "answer", Text: "world"}}},
	}
}

func TestRuleValidationRejectsUnsafeShapes(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Rule)
	}{
		{"empty enabled pattern", func(rule *Rule) { rule.Match.Pattern = "" }},
		{"invalid regex", func(rule *Rule) { rule.Match.Pattern = "(" }},
		{"empty model pattern", func(rule *Rule) { rule.Match.ModelPattern = "" }},
		{"invalid model regex", func(rule *Rule) { rule.Match.ModelPattern = "(" }},
		{"empty model key", func(rule *Rule) { rule.Match.ModelKeyID = "" }},
		{"terminal before end", func(rule *Rule) {
			rule.Steps = []Step{
				{ID: "one", Action: Action{Kind: "stream_complete", Mode: "assistant_message"}},
				{ID: "two", Action: Action{Kind: "stream_delta", Mode: "answer", Text: "late"}},
			}
		}},
		{"unsupported version", func(rule *Rule) { rule.SchemaVersion = 1 }},
		{"request bound image", func(rule *Rule) {
			rule.Steps[0].Action = Action{Kind: "builtin_tool", BuiltinToolKind: "image_generation", BuiltinToolAssetID: "asset"}
		}},
		{"loop without interval", func(rule *Rule) { rule.Playback.Loop = true }},
		{"loop with terminal action", func(rule *Rule) {
			rule.Playback.Loop = true
			rule.Playback.LoopIntervalMS = 10
			rule.Steps[0].Action = Action{Kind: "stream_complete", Mode: "assistant_message"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := validRule()
			tc.edit(&rule)
			if err := ValidateRule(rule); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestMatchConditionsMustAllMatch(t *testing.T) {
	waiting := chatevents.WaitingTurn{
		LastUserText: "please search", Model: "demo.model+fast", ModelKeyID: "modelkey_123",
	}
	cases := []struct {
		name string
		spec MatchSpec
		want bool
	}{
		{name: "all conditions", spec: MatchSpec{Pattern: `^please\s+search$`, ModelPattern: `^demo\..*`, ModelKeyID: "modelkey_123"}, want: true},
		{name: "text mismatch", spec: MatchSpec{Pattern: `^other`, ModelPattern: `^demo\..*`, ModelKeyID: "modelkey_123"}, want: false},
		{name: "model mismatch", spec: MatchSpec{Pattern: `^please`, ModelPattern: `^other`, ModelKeyID: "modelkey_123"}, want: false},
		{name: "key mismatch", spec: MatchSpec{Pattern: `^please`, ModelPattern: `^demo\..*`, ModelKeyID: "modelkey_456"}, want: false},
		{name: "invalid text regex", spec: MatchSpec{Pattern: `(`, ModelPattern: `.*`, ModelKeyID: "modelkey_123"}, want: false},
		{name: "invalid model regex", spec: MatchSpec{Pattern: `.*`, ModelPattern: `(`, ModelKeyID: "modelkey_123"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesWaitingTurn(tc.spec, waiting); got != tc.want {
				t.Fatalf("matchesWaitingTurn() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModelMatchRequiresValidRegex(t *testing.T) {
	rule := validRule()
	rule.Match.ModelPattern = "("
	if err := ValidateRule(rule); err == nil {
		t.Fatal("invalid model regex should be rejected")
	}
}

func TestListRulesMigratesV2MatchConditions(t *testing.T) {
	store := newMemoryRules()
	legacy := validRule()
	legacy.SchemaVersion = 2
	legacy.Match = MatchSpec{Target: "last_user_text", Pattern: `^legacy`}
	payload, err := rulePayload(legacy)
	if err != nil {
		t.Fatal(err)
	}
	store.items["owner/rule_a"] = common.AutomationRule{
		ID: "rule_a", UserID: "owner", Enabled: true, Payload: payload,
	}
	revokedAt := time.Now().UTC()
	service := New(Deps{
		Rules: store,
		ModelKeys: memoryModelKeys{items: []common.ModelAPIKey{
			{ID: "revoked", RevokedAt: &revokedAt},
			{ID: "modelkey_first"},
			{ID: "modelkey_second"},
		}},
	})
	items, err := service.ListRules(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("migrated rules = %#v", items)
	}
	match := items[0].Match
	if items[0].SchemaVersion != SchemaVersion || match.Pattern != `^legacy` || match.ModelPattern != ".*" || match.ModelKeyID != "modelkey_first" {
		t.Fatalf("unexpected migrated rule: %#v", items[0])
	}
	persisted, err := ruleFromStored(store.items["owner/rule_a"])
	if err != nil || persisted.SchemaVersion != SchemaVersion || persisted.Match != match {
		t.Fatalf("migration was not persisted: rule=%#v err=%v", persisted, err)
	}
}

func TestListRulesDisablesV2RuleWithoutActiveModelKey(t *testing.T) {
	store := newMemoryRules()
	legacy := validRule()
	legacy.SchemaVersion = 2
	legacy.Match = MatchSpec{Target: "last_user_text", Pattern: `^legacy`}
	payload, err := rulePayload(legacy)
	if err != nil {
		t.Fatal(err)
	}
	store.items["owner/rule_a"] = common.AutomationRule{ID: "rule_a", UserID: "owner", Enabled: true, Payload: payload}
	service := New(Deps{Rules: store, ModelKeys: memoryModelKeys{}})
	items, err := service.ListRules(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Enabled || items[0].Match.ModelPattern != ".*" || items[0].Match.ModelKeyID != "" {
		t.Fatalf("rule without a model key should be retained but disabled: %#v", items)
	}
}

func TestToolCallArgumentsRoundTripThroughAutomationAction(t *testing.T) {
	input := turnsvc.OutputAction{
		Kind: turnsvc.TurnControlStreamComplete, Mode: "tool_call",
		OutputText: `{"query":"latest"}`, ToolName: "search", ToolCallID: "call_1",
	}
	output := ActionFromTurn(input).TurnAction()
	if output.OutputText != input.OutputText || output.ToolName != input.ToolName || output.ToolCallID != input.ToolCallID {
		t.Fatalf("tool call semantics changed during round trip: %#v", output)
	}
}

func TestListRulesIgnoresLegacyPayload(t *testing.T) {
	store := newMemoryRules()
	store.items["owner/legacy"] = common.AutomationRule{ID: "legacy", UserID: "owner", Enabled: true, Payload: map[string]any{"name": "old"}}
	service := New(Deps{Rules: store})
	items, err := service.ListRules(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("legacy rule must be ignored: %#v", items)
	}
}

func TestRecordingCapturesManualActionsAndPersistsDraft(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	realtime := &capturedRealtime{}
	service := New(Deps{Rules: store, Pending: pending, Events: realtime})
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "hello", Mode: "answer"},
	}})
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceIM,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "wechat", Mode: "answer"},
	}})
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceAutomation,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "ignored", Mode: "answer"},
	}})
	state, err := service.StopRecording(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Steps) != 2 || state.Steps[0].Action.Text != "hello" || state.Steps[1].Action.Text != "wechat" {
		t.Fatalf("unexpected recorded steps: %#v", state.Steps)
	}
	if state.DraftRule == nil || state.DraftRule.Enabled || len(state.DraftRule.Steps) != 2 {
		t.Fatalf("unexpected draft rule: %#v", state.DraftRule)
	}
	snapshot := service.StateSnapshot("owner")
	if snapshot.Revision < state.Revision || snapshot.Recording.Revision != snapshot.Revision {
		t.Fatalf("snapshot did not expose an authoritative empty recording revision: %#v", snapshot)
	}
	if snapshot.Recording.Steps == nil {
		t.Fatalf("snapshot must encode empty recording steps as an array: %#v", snapshot.Recording)
	}
}

func TestEmptyRecordingStateAndDraftEncodeStepsAsArrays(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	service := New(Deps{Rules: store, Pending: pending})
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	active := service.StateSnapshot("owner").Recording
	assertStepsEncodeAsArray(t, active)

	stopped, err := service.StopRecording(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.DraftRule == nil {
		t.Fatal("empty recording did not produce a draft rule")
	}
	assertStepsEncodeAsArray(t, stopped)
}

func assertStepsEncodeAsArray(t *testing.T, state RecordingState) {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"steps":[]`)) {
		t.Fatalf("recording arrays must not encode as null: %s", encoded)
	}
	if state.DraftRule != nil && state.DraftRule.Steps == nil {
		t.Fatalf("draft rule steps must be a non-nil array: %#v", state.DraftRule)
	}
}

func TestMatchingRuleReplaysThroughControl(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 2)
	service := New(Deps{Rules: store, Pending: pending, Control: capturedControl{calls: calls}})
	rule := validRule()
	if _, err := service.SaveRule(context.Background(), "owner", rule); err != nil {
		t.Fatal(err)
	}
	service.HandleChatEvent(context.Background(), chatevents.Event{
		Type:        chatevents.TypeTurnWaiting,
		WaitingTurn: &chatevents.WaitingTurn{OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp", Model: "demo-chat", ModelKeyID: "modelkey_a", LastUserText: "hello there"},
	})
	select {
	case command := <-calls:
		if command.Source != controlsvc.SourceAutomation || command.Action.OutputText != "world" {
			t.Fatalf("unexpected replay command: %#v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("matching rule did not execute")
	}

	service.HandleChatEvent(context.Background(), chatevents.Event{
		Type:        chatevents.TypeTurnWaiting,
		WaitingTurn: &chatevents.WaitingTurn{OwnerID: "owner", ConversationID: "conv", RequestID: "req", Model: "demo-chat", ModelKeyID: "modelkey_a", LastUserText: "no match"},
	})
	select {
	case command := <-calls:
		t.Fatalf("non-matching rule executed: %#v", command)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestManualControlCancelsScheduledReplay(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 1)
	service := New(Deps{Rules: store, Pending: pending, Control: capturedControl{calls: calls}})
	rule := validRule()
	rule.Playback.InitialDelayMS = 100
	service.startExecution(context.Background(), chatevents.WaitingTurn{OwnerID: "owner", ConversationID: "conv", RequestID: "req"}, rule)
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceAPI,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "manual"},
	}})
	select {
	case command := <-calls:
		t.Fatalf("cancelled replay executed: %#v", command)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestLoopingRuleRepeatsUntilManualTakeover(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 8)
	service := New(Deps{Rules: store, Pending: pending, Control: capturedControl{calls: calls}})
	rule := validRule()
	rule.Playback.Loop = true
	rule.Playback.LoopIntervalMS = 5
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, rule)
	for index := 0; index < 3; index++ {
		select {
		case command := <-calls:
			if command.Action.OutputText != "world" {
				t.Fatalf("unexpected loop command: %#v", command)
			}
		case <-time.After(time.Second):
			t.Fatalf("loop stopped before call %d", index+1)
		}
	}
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "manual"},
	}})
	time.Sleep(20 * time.Millisecond)
	state := service.ExecutionStates("owner")[0]
	if state.Status != "cancelled" || state.Cycle < 3 {
		t.Fatalf("unexpected loop terminal state: %#v", state)
	}
}

func TestLoopingRuleCancelsImmediatelyWhenPendingEnds(t *testing.T) {
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 1)
	service := New(Deps{Rules: newMemoryRules(), Pending: pending, Control: capturedControl{calls: calls}})
	rule := validRule()
	rule.Playback.Loop = true
	rule.Playback.LoopIntervalMS = int64(time.Hour / time.Millisecond)
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, rule)
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("first loop action did not execute")
	}
	service.HandleChatEvent(context.Background(), chatevents.Event{
		Type: chatevents.TypeConversationUpserted, OwnerID: "owner", ConversationID: "conv", RequestID: "req",
		Conversation: common.Conversation{ID: "conv", Metadata: map[string]any{"realtime_status": "closed"}},
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		states := service.ExecutionStates("owner")
		if len(states) == 1 && states[0].Status == "cancelled" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("loop execution remained running until its one-hour interval elapsed")
}

func TestFailedManualControlDoesNotCancelScheduledReplay(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	turn := &controlledTurn{automationCalls: make(chan turnsvc.TurnControlCommand, 1), failText: "manual-fail"}
	control := controlsvc.New(nil, turn, nil)
	service := New(Deps{Rules: store, Pending: pending})
	service.SetControl(control)
	control.Subscribe(service)
	rule := validRule()
	rule.Playback.InitialDelayMS = 20
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, rule)
	if _, err := control.Execute(context.Background(), controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "manual-fail"},
	}); err == nil {
		t.Fatal("expected manual control failure")
	}
	select {
	case command := <-turn.automationCalls:
		if command.Action.OutputText != "world" {
			t.Fatalf("unexpected automation command: %#v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("failed manual control cancelled scheduled automation")
	}
}

func TestSuccessfulManualControlCancelsQueuedAutomation(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	turn := &controlledTurn{
		automationCalls: make(chan turnsvc.TurnControlCommand, 1),
		manualStarted:   make(chan struct{}), releaseManual: make(chan struct{}),
	}
	control := controlsvc.New(nil, turn, nil)
	service := New(Deps{Rules: store, Pending: pending})
	service.SetControl(control)
	control.Subscribe(service)
	manualDone := make(chan error, 1)
	go func() {
		_, err := control.Execute(context.Background(), controlsvc.Command{
			OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
			Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "manual-block"},
		})
		manualDone <- err
	}()
	<-turn.manualStarted
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, validRule())
	time.Sleep(20 * time.Millisecond)
	close(turn.releaseManual)
	if err := <-manualDone; err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-turn.automationCalls:
		t.Fatalf("cancelled queued automation reached turn service: %#v", command)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestManualTakeoverBlocksExecutionThatHasNotRegisteredYet(t *testing.T) {
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 1)
	service := New(Deps{Rules: newMemoryRules(), Pending: pending, Control: capturedControl{calls: calls}})
	generation := service.ruleGeneration("owner")
	service.markManualTakeover("conv", "req")
	if service.startExecutionAtGeneration(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, validRule(), generation) {
		t.Fatal("execution registered after manual takeover")
	}
	select {
	case command := <-calls:
		t.Fatalf("blocked execution reached control: %#v", command)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRuleGenerationChangeRejectsStaleMatch(t *testing.T) {
	service := New(Deps{Rules: newMemoryRules(), Pending: memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req"},
	}}})
	generation := service.ruleGeneration("owner")
	service.advanceRuleGeneration("owner")
	if service.startExecutionAtGeneration(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req",
	}, validRule(), generation) {
		t.Fatal("execution registered from stale rule snapshot")
	}
}

func TestStaleRequestAdmissionCannotReplaceCurrentExecution(t *testing.T) {
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req_new", ResponseID: "resp_new"},
	}}
	service := New(Deps{Rules: newMemoryRules(), Pending: pending, Control: capturedControl{calls: make(chan controlsvc.Command, 1)}})
	generation := service.ruleGeneration("owner")
	rule := validRule()
	rule.Playback.InitialDelayMS = 500
	if !service.startExecutionAtGeneration(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req_new", ResponseID: "resp_new",
	}, rule, generation) {
		t.Fatal("current request execution was not admitted")
	}
	service.mu.Lock()
	current := service.executions["conv"]
	service.mu.Unlock()
	if service.startExecutionAtGeneration(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req_old", ResponseID: "resp_old",
	}, rule, generation) {
		t.Fatal("stale request execution was admitted")
	}
	service.mu.Lock()
	stillCurrent := service.executions["conv"]
	service.mu.Unlock()
	if stillCurrent != current {
		t.Fatal("stale request displaced the current execution")
	}
	current.cancel(errors.New("test_complete"))
}

func TestUnrecordableManualActionStillTakesOverExecution(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 1)
	service := New(Deps{Rules: store, Pending: pending, Control: capturedControl{calls: calls}})
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	rule := validRule()
	rule.Playback.InitialDelayMS = 50
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, rule)
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlBuiltinTool, BuiltinToolKind: "image_generation", BuiltinToolAssetID: "asset"},
	}})
	select {
	case command := <-calls:
		t.Fatalf("unrecordable manual action did not cancel automation: %#v", command)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStopRecordingWaitsForSuccessfulControlObserver(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	turn := &controlledTurn{
		automationCalls: make(chan turnsvc.TurnControlCommand, 1),
		manualStarted:   make(chan struct{}), releaseManual: make(chan struct{}),
	}
	control := controlsvc.New(nil, turn, nil)
	service := New(Deps{Rules: store, Pending: pending, Control: control})
	control.Subscribe(service)
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	controlDone := make(chan error, 1)
	go func() {
		_, err := control.Execute(context.Background(), controlsvc.Command{
			OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
			Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "manual-block"},
		})
		controlDone <- err
	}()
	<-turn.manualStarted
	stopDone := make(chan struct {
		state RecordingState
		err   error
	}, 1)
	go func() {
		state, err := service.StopRecording(context.Background(), "owner")
		stopDone <- struct {
			state RecordingState
			err   error
		}{state: state, err: err}
	}()
	select {
	case <-stopDone:
		t.Fatal("stop crossed an in-flight control command")
	case <-time.After(20 * time.Millisecond):
	}
	close(turn.releaseManual)
	if err := <-controlDone; err != nil {
		t.Fatal(err)
	}
	result := <-stopDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.state.DraftRule == nil || len(result.state.DraftRule.Steps) != 1 || result.state.DraftRule.Steps[0].Action.Text != "manual-block" {
		t.Fatalf("stop omitted the successful in-flight action: %#v", result.state.DraftRule)
	}
}

func TestUnmanagedTerminalEventSavesRecordingDraft(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	service := New(Deps{Rules: store, Pending: pending})
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "partial"},
	}})
	service.HandleChatEvent(context.Background(), chatevents.Event{
		Type: chatevents.TypeConversationUpserted, OwnerID: "owner", ConversationID: "conv", RequestID: "req",
		Conversation: common.Conversation{ID: "conv", Metadata: map[string]any{"realtime_status": "disconnected"}},
	})
	waitForRuleCount(t, store, 1)
	if service.RecordingState("owner").Active {
		t.Fatal("recording remained active after unmanaged terminal event")
	}
	for _, item := range store.items {
		rule, err := ruleFromStored(item)
		if err != nil || rule.Enabled || len(rule.Steps) != 1 || rule.Steps[0].Action.Text != "partial" {
			t.Fatalf("unexpected persisted draft: %#v err=%v", rule, err)
		}
	}
}

func TestControlManagedTerminalEventWaitsForRecordedTerminalAction(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	service := New(Deps{Rules: store, Pending: pending})
	if _, err := service.StartRecording(context.Background(), "owner", "conv"); err != nil {
		t.Fatal(err)
	}
	service.HandleChatEvent(context.Background(), chatevents.Event{
		Type: chatevents.TypeConversationUpserted, OwnerID: "owner", ConversationID: "conv", RequestID: "req", ControlManaged: true,
		Conversation: common.Conversation{ID: "conv", Metadata: map[string]any{"realtime_status": "closed"}},
	})
	if !service.RecordingState("owner").Active {
		t.Fatal("control-managed terminal event stopped recording before observer captured the action")
	}
	service.ControlApplied(context.Background(), controlsvc.AppliedCommand{Command: controlsvc.Command{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", Source: controlsvc.SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamComplete, Mode: "assistant_message"},
	}})
	waitForRuleCount(t, store, 1)
	for _, item := range store.items {
		rule, err := ruleFromStored(item)
		if err != nil || len(rule.Steps) != 1 || !rule.Steps[0].Action.Terminal() {
			t.Fatalf("terminal action was not preserved in draft: %#v err=%v", rule, err)
		}
	}
}

func waitForRuleCount(t *testing.T, store *memoryRules, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		actual := len(store.items)
		store.mu.Unlock()
		if actual == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d stored rules", count)
}

func TestRuleCancellationIsOwnerScoped(t *testing.T) {
	service := New(Deps{})
	ctxA, cancelA := context.WithCancelCause(context.Background())
	ctxB, cancelB := context.WithCancelCause(context.Background())
	service.executions["conv_a"] = &execution{state: ExecutionState{OwnerID: "owner_a", RuleID: "same", Status: "running"}, cancel: cancelA}
	service.executions["conv_b"] = &execution{state: ExecutionState{OwnerID: "owner_b", RuleID: "same", Status: "running"}, cancel: cancelB}
	service.cancelRuleExecutions("owner_a", "same", "disabled")
	if context.Cause(ctxA) == nil {
		t.Fatal("owner_a execution was not cancelled")
	}
	if context.Cause(ctxB) != nil {
		t.Fatal("owner_b execution was cancelled by another owner")
	}
	cancelB(nil)
}

func TestReplacedExecutionCannotPublishStaleTerminalState(t *testing.T) {
	store := newMemoryRules()
	pending := memoryPending{items: map[string]*turnsvc.PendingTurn{
		"conv": {OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp"},
	}}
	calls := make(chan controlsvc.Command, 1)
	realtime := &capturedRealtime{}
	service := New(Deps{Rules: store, Pending: pending, Control: capturedControl{calls: calls}, Events: realtime})
	oldRule := validRule()
	oldRule.ID = "rule_old"
	oldRule.Playback.InitialDelayMS = 100
	newRule := validRule()
	newRule.ID = "rule_new"
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, oldRule)
	service.startExecution(context.Background(), chatevents.WaitingTurn{
		OwnerID: "owner", ConversationID: "conv", RequestID: "req", ResponseID: "resp",
	}, newRule)
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("replacement execution did not run")
	}
	time.Sleep(20 * time.Millisecond)
	realtime.mu.Lock()
	defer realtime.mu.Unlock()
	for _, payload := range realtime.payloads {
		event, ok := payload.(StateEvent)
		if ok && event.Execution != nil && event.Execution.RuleID == "rule_old" && event.Execution.Status != "running" {
			t.Fatalf("replaced execution published stale terminal state: %#v", *event.Execution)
		}
	}
}

func TestExpiredExecutionPublishesRemovalTombstone(t *testing.T) {
	realtime := &capturedRealtime{}
	service := New(Deps{Events: realtime})
	current := &execution{
		state:      ExecutionState{Revision: 1, OwnerID: "owner", ConversationID: "conv", RequestID: "req", Status: "completed"},
		finishedAt: time.Now().UTC().Add(-executionStateRetention),
	}
	service.executions["conv"] = current
	service.ownerRevisions["owner"] = 1
	if !service.expireExecution("owner", "conv", current, time.Now().UTC()) {
		t.Fatal("terminal execution was not expired")
	}
	if _, ok := service.executions["conv"]; ok {
		t.Fatal("expired execution remained in registry")
	}
	realtime.mu.Lock()
	defer realtime.mu.Unlock()
	last := realtime.payloads[len(realtime.payloads)-1].(StateEvent)
	if last.Execution == nil || last.Execution.Status != "removed" || last.Execution.Revision <= 1 {
		t.Fatalf("unexpected removal tombstone: %#v", last.Execution)
	}
}
