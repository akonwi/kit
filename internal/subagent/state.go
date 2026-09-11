package subagent

// TerminalTask reports whether a task cannot transition to further work.
func TerminalTask(state TaskState) bool {
	switch state {
	case TaskCompleted, TaskFailed, TaskAborted, TaskInterrupted:
		return true
	default:
		return false
	}
}

// ValidTaskTransition reports whether one authoritative task transition is legal.
func ValidTaskTransition(from, to TaskState) bool {
	switch from {
	case TaskQueued:
		return to == TaskRunning || to == TaskAborted
	case TaskRunning:
		return TerminalTask(to)
	default:
		return false
	}
}

// ConversationStateForTask maps the latest task state to its conversation state.
func ConversationStateForTask(state TaskState) ConversationState {
	switch state {
	case TaskQueued, TaskRunning:
		return ConversationRunning
	case TaskFailed:
		return ConversationFailed
	case TaskAborted:
		return ConversationAborted
	case TaskInterrupted:
		return ConversationInterrupted
	default:
		return ConversationIdle
	}
}
