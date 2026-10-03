package kit_test

import (
	"context"
	"testing"

	kit "github.com/akonwi/kit/api"
)

func compilePublicSessionSurface(ctx context.Context, session *kit.Session) {
	_, _ = session.Snapshot(ctx)
	submission, _ := session.SendMessage(ctx, kit.Message{Text: "hello"})
	if submission.Turn != nil {
		_, _ = submission.Turn.Wait(ctx)
		_ = submission.Turn.Abort(ctx)
	}
	execution, _ := session.RunBash(ctx, kit.BashExecutionInput{ExecutionID: "bash_example", Command: "pwd"})
	if execution != nil {
		_, _ = execution.Wait(ctx)
		_ = execution.Abort(ctx)
	}
	updates, _ := session.Watch(ctx)
	if updates != nil {
		_ = updates.Updates()
		_ = updates.Close()
	}
}

func TestPublicContractAliasesAreUsableWithoutContractImport(t *testing.T) {
	message := kit.Message{Text: "Review this repository"}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}

	status := kit.TurnInfo{SessionID: "session_test", TurnID: "turn_test", Status: kit.TurnStatusRunning}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
}
