package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
)

type recording struct {
	state        RecordingState
	lastActionAt time.Time
}

func (s *Service) StartRecording(_ context.Context, ownerID string, conversationID string) (RecordingState, error) {
	ownerID = strings.TrimSpace(ownerID)
	conversationID = strings.TrimSpace(conversationID)
	turn, ok := s.pending.GetByConversationID(conversationID)
	if !ok || turn == nil || strings.TrimSpace(turn.OwnerID) != ownerID {
		return RecordingState{}, ErrPendingNotFound
	}
	if turn.MutationMu != nil {
		turn.MutationMu.Lock()
		defer turn.MutationMu.Unlock()
	}
	currentTurn, ok := s.pending.GetByConversationID(conversationID)
	if !ok || currentTurn == nil || strings.TrimSpace(currentTurn.OwnerID) != ownerID || strings.TrimSpace(currentTurn.RequestID) != strings.TrimSpace(turn.RequestID) {
		return RecordingState{}, ErrPendingNotFound
	}
	now := time.Now().UTC()
	s.mu.Lock()
	if current := s.recordings[ownerID]; current != nil && current.state.Active {
		s.mu.Unlock()
		return RecordingState{}, ErrRecordingConflict
	}
	state := RecordingState{
		Active: true, OwnerID: ownerID, ConversationID: conversationID,
		RequestID: strings.TrimSpace(turn.RequestID), StartedAt: now, Steps: []Step{},
	}
	state.Revision = s.nextRevisionLocked(ownerID)
	s.recordings[ownerID] = &recording{state: state, lastActionAt: now}
	s.mu.Unlock()
	s.publishRecording(state)
	return state, nil
}

func (s *Service) markRecordingTerminated(ownerID string, conversationID string, requestID string) bool {
	s.mu.Lock()
	current := s.recordings[strings.TrimSpace(ownerID)]
	if current == nil || !current.state.Active || current.state.ConversationID != strings.TrimSpace(conversationID) || current.state.RequestID != strings.TrimSpace(requestID) {
		s.mu.Unlock()
		return false
	}
	current.state.Warning = "关联请求已经结束，请停止录制生成规则草稿或取消录制"
	current.state.Revision = s.nextRevisionLocked(ownerID)
	state := cloneRecordingState(current.state)
	s.mu.Unlock()
	s.publishRecording(state)
	return true
}

func (s *Service) RecordingState(ownerID string) RecordingState {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.recordings[strings.TrimSpace(ownerID)]
	if current == nil {
		return RecordingState{Steps: []Step{}}
	}
	return cloneRecordingState(current.state)
}

func (s *Service) StopRecording(ctx context.Context, ownerID string) (RecordingState, error) {
	conversationID, expected, err := s.recordingBinding(ownerID)
	if err != nil {
		return RecordingState{}, err
	}
	if s.control == nil {
		return s.stopRecording(ctx, ownerID, expected)
	}
	var state RecordingState
	err = s.control.Synchronize(ctx, conversationID, func() error {
		var innerErr error
		state, innerErr = s.stopRecording(ctx, ownerID, expected)
		return innerErr
	})
	return state, err
}

func (s *Service) stopRecording(ctx context.Context, ownerID string, expected *recording) (RecordingState, error) {
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	current := s.recordings[ownerID]
	if current == nil || current != expected || !current.state.Active {
		s.mu.Unlock()
		return RecordingState{}, ErrRecordingNotFound
	}
	current.state.Active = false
	state := cloneRecordingState(current.state)
	s.mu.Unlock()

	rule := Rule{
		SchemaVersion: SchemaVersion,
		ID:            "rule_" + uuid.NewString(), OwnerID: ownerID,
		Name:     fmt.Sprintf("录制规则 %s", time.Now().Format("01-02 15:04")),
		Enabled:  false,
		Match:    MatchSpec{ModelPattern: ".*"},
		Playback: PlaybackSpec{Mode: "recorded", FixedIntervalMS: 200, LoopIntervalMS: 1000},
		Steps:    make([]Step, len(state.Steps)),
	}
	copy(rule.Steps, state.Steps)
	saved, err := s.SaveRule(ctx, ownerID, rule)
	if err != nil {
		s.mu.Lock()
		if s.recordings[ownerID] == current {
			current.state.Active = true
			current.state.Warning = "规则草稿保存失败，可重试停止录制：" + err.Error()
			current.state.Revision = s.nextRevisionLocked(ownerID)
			state = cloneRecordingState(current.state)
		}
		s.mu.Unlock()
		s.publishRecording(state)
		return RecordingState{}, err
	}
	s.mu.Lock()
	if s.recordings[ownerID] == current {
		delete(s.recordings, ownerID)
	}
	state.DraftRule = &saved
	state.Revision = s.nextRevisionLocked(ownerID)
	s.mu.Unlock()
	s.publishRecording(state)
	return state, nil
}

func (s *Service) CancelRecording(ctx context.Context, ownerID string) (RecordingState, error) {
	conversationID, expected, err := s.recordingBinding(ownerID)
	if err != nil {
		return RecordingState{}, err
	}
	if s.control == nil {
		return s.cancelRecording(ownerID, expected)
	}
	var state RecordingState
	err = s.control.Synchronize(ctx, conversationID, func() error {
		var innerErr error
		state, innerErr = s.cancelRecording(ownerID, expected)
		return innerErr
	})
	return state, err
}

func (s *Service) cancelRecording(ownerID string, expected *recording) (RecordingState, error) {
	ownerID = strings.TrimSpace(ownerID)
	s.mu.Lock()
	current := s.recordings[ownerID]
	if current == nil || current != expected || !current.state.Active {
		s.mu.Unlock()
		return RecordingState{}, ErrRecordingNotFound
	}
	delete(s.recordings, ownerID)
	state := cloneRecordingState(current.state)
	state.Active = false
	state.Steps = []Step{}
	state.Revision = s.nextRevisionLocked(ownerID)
	s.mu.Unlock()
	s.publishRecording(state)
	return state, nil
}

func (s *Service) recordingBinding(ownerID string) (string, *recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.recordings[strings.TrimSpace(ownerID)]
	if current == nil || !current.state.Active {
		return "", nil, ErrRecordingNotFound
	}
	return current.state.ConversationID, current, nil
}

func (s *Service) ControlApplied(ctx context.Context, applied controlsvc.AppliedCommand) {
	command := applied.Command
	ownerID := strings.TrimSpace(command.OwnerID)
	if command.Source == controlsvc.SourceAutomation {
		return
	}
	autoCompleted, _ := applied.Result.Body["auto_completed"].(bool)
	if !ActionFromTurn(command.Action).Terminal() && !autoCompleted {
		s.markManualTakeover(command.ConversationID, command.RequestID)
	}
	recordSource := command.Source == "" || command.Source == controlsvc.SourceAPI || command.Source == controlsvc.SourceWorkspace || command.Source == controlsvc.SourceIM
	requestID := strings.TrimSpace(command.RequestID)
	now := time.Now().UTC()
	s.mu.Lock()
	current := s.recordings[ownerID]
	if recordSource && current != nil && current.state.Active &&
		current.state.ConversationID == strings.TrimSpace(command.ConversationID) &&
		current.state.RequestID == requestID {
		action := ActionFromTurn(command.Action)
		if len(current.state.Steps) >= maxRuleSteps {
			current.state.Warning = "录制步骤已达到上限"
			current.state.Revision = s.nextRevisionLocked(ownerID)
			state := cloneRecordingState(current.state)
			s.mu.Unlock()
			s.publishRecording(state)
			return
		}
		if !action.Recordable() {
			current.state.Warning = "生图结果绑定当前请求，不能录入可复用自动化规则"
			current.state.Revision = s.nextRevisionLocked(ownerID)
			state := cloneRecordingState(current.state)
			s.mu.Unlock()
			s.publishRecording(state)
			return
		}
		delay := now.Sub(current.lastActionAt).Milliseconds()
		if delay < 0 {
			delay = 0
		}
		if delay > maxDelayMS {
			current.state.Warning = "操作间隔超过 24 小时，当前步骤未录制"
			current.state.Revision = s.nextRevisionLocked(ownerID)
			state := cloneRecordingState(current.state)
			s.mu.Unlock()
			s.publishRecording(state)
			return
		}
		step := Step{ID: "step_" + uuid.NewString(), DelayBeforeMS: delay, Action: action}
		current.state.Warning = ""
		current.state.Steps = append(current.state.Steps, step)
		current.lastActionAt = now
		current.state.Revision = s.nextRevisionLocked(ownerID)
		state := cloneRecordingState(current.state)
		terminal := step.Action.Terminal()
		s.mu.Unlock()
		s.publishRecording(state)
		if terminal {
			go func() { _, _ = s.StopRecording(context.WithoutCancel(ctx), ownerID) }()
		}
	} else {
		s.mu.Unlock()
	}
}

func cloneRecordingState(state RecordingState) RecordingState {
	steps := make([]Step, len(state.Steps))
	copy(steps, state.Steps)
	state.Steps = steps
	if state.DraftRule != nil {
		rule := cloneRule(*state.DraftRule)
		state.DraftRule = &rule
	}
	return state
}

func (s *Service) publishRecording(state RecordingState) {
	if s.events != nil {
		copy := cloneRecordingState(state)
		s.events.PublishAutomationState(context.Background(), StateEvent{OwnerID: state.OwnerID, Recording: &copy})
	}
}
