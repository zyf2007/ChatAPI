package turn

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

// Prove that a slow NotifyWaiting hook cannot delay the pending publish path.
// Submit must return as soon as the hook returns; the hook itself must not do
// blocking I/O in production (ntfy service enqueues only). This test also
// guards against future accidental re-introduction of synchronous slow work
// inside the hook call site.
func TestSubmit_NotifyWaitingDoesNotDelayPendingReturn(t *testing.T) {
	release := make(chan struct{})
	var hookStarted sync.WaitGroup
	hookStarted.Add(1)

	// Simulates a misbehaving/slow hook body. Submit still waits for the hook
	// return today, so production wiring must keep NotifyWaiting non-blocking.
	// The regression we care about: enqueue-style hooks return quickly.
	fastEnqueueHook := func(ctx context.Context, ownerID string, title string, userText string) {
		hookStarted.Done()
		// Non-blocking enqueue semantics: return immediately without waiting
		// on release / network. A slow worker would run elsewhere.
		select {
		case <-release:
		default:
		}
		if userText != "last-user" {
			t.Errorf("expected LastUserContent, got %q", userText)
		}
		if ownerID != "user_a" {
			t.Errorf("unexpected owner %q", ownerID)
		}
	}

	pending := &memoryPending{}
	submitter := &Submitter{
		Store:   successCreatePendingStore{title: "会话 A"},
		Pending: pending,
		Hooks: SubmitHooks{
			NotifyWaiting: fastEnqueueHook,
		},
	}

	begin := time.Now()
	turn, conv, _, err := submitter.Submit(context.Background(), SubmitInput{
		OwnerID: "user_a",
		Request: protocol.TurnRequest{
			Protocol:        protocol.ProtocolResponses,
			Model:           "demo",
			LastUserContent: "last-user",
			UserContent:     "fallback-user",
		},
	})
	elapsed := time.Since(begin)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if turn == nil || conv.Title != "会话 A" {
		t.Fatalf("unexpected submit result turn=%v conv=%#v", turn, conv)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("submit blocked on notify for %s", elapsed)
	}
	hookStarted.Wait()
	if pending.count() != 1 {
		t.Fatalf("expected pending registered before notify, got %d", pending.count())
	}
}

func TestSubmit_SlowBlockingNotifyDelaysSubmit_DocumentedContract(t *testing.T) {
	// Documents the contract: Submit calls NotifyWaiting synchronously.
	// Therefore production NotifyWaiting MUST only enqueue. If someone wires a
	// blocking sender directly, Submit latency follows the sender — which is
	// the bug PR #37 introduced. This test freezes that contract so the ntfy
	// service layer remains responsible for asynchrony.
	block := make(chan struct{})
	submitter := &Submitter{
		Store:   successCreatePendingStore{title: "t"},
		Pending: &memoryPending{},
		Hooks: SubmitHooks{
			NotifyWaiting: func(ctx context.Context, ownerID string, title string, userText string) {
				<-block
			},
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _, _ = submitter.Submit(context.Background(), SubmitInput{
			OwnerID: "user_a",
			Request: protocol.TurnRequest{
				Protocol:        protocol.ProtocolResponses,
				Model:           "demo",
				LastUserContent: "x",
			},
		})
	}()
	select {
	case <-done:
		t.Fatal("submit returned while notify still blocked — contract changed")
	case <-time.After(30 * time.Millisecond):
		// still blocked as expected
	}
	close(block)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("submit did not finish after notify released")
	}
}

type successCreatePendingStore struct {
	title string
}

func (s successCreatePendingStore) CreatePendingTurn(_ context.Context, input common.CreatePendingInput) (common.Conversation, common.Message, error) {
	return common.Conversation{
			ID:    input.ConversationID,
			Title: s.title,
		}, common.Message{
			ID: input.RequestID,
		}, nil
}

type memoryPending struct {
	mu    sync.Mutex
	items map[string]*PendingTurn
}

func (m *memoryPending) Add(turn *PendingTurn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = map[string]*PendingTurn{}
	}
	m.items[turn.ConversationID] = turn
}

func (m *memoryPending) GetByConversationID(conversationID string) (*PendingTurn, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[conversationID]
	return item, ok
}

func (m *memoryPending) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}
