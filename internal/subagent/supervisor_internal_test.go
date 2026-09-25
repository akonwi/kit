package subagent

import "testing"

func TestConversationLocksAreReferenceCountedAndEvicted(t *testing.T) {
	t.Parallel()
	supervisor := &Supervisor{conversationLocks: make(map[ConversationID]*conversationLock)}
	conversationID := ConversationID("subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	supervisor.mu.Lock()
	first := supervisor.retainConversationLockLocked(conversationID)
	second := supervisor.retainConversationLockLocked(conversationID)
	if first != second || first.refs != 2 || len(supervisor.conversationLocks) != 1 {
		t.Fatalf("retained lock = first:%p second:%p refs:%d entries:%d", first, second, first.refs, len(supervisor.conversationLocks))
	}
	supervisor.releaseConversationLockLocked(conversationID, first)
	if first.refs != 1 || len(supervisor.conversationLocks) != 1 {
		t.Fatalf("lock evicted while retained: refs:%d entries:%d", first.refs, len(supervisor.conversationLocks))
	}
	supervisor.mu.Unlock()

	supervisor.releaseConversationLock(conversationID, second)
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if first.refs != 0 || len(supervisor.conversationLocks) != 0 {
		t.Fatalf("released lock retained: refs:%d entries:%d", first.refs, len(supervisor.conversationLocks))
	}
}
