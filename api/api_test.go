package kit_test

import (
	"context"
	"fmt"
	"testing"

	kit "github.com/akonwi/kit/api"
)

func ExampleClient() {
	ctx := context.Background()
	client, err := kit.Connect(ctx, kit.Local())
	if err != nil {
		return
	}
	defer client.Close()

	sessions, err := client.ListSessions(ctx, kit.ListSessionsOptions{})
	if err != nil || len(sessions) == 0 {
		return
	}
	session, err := client.Attach(ctx, sessions[0].ID)
	if err != nil {
		return
	}
	submission, err := session.SendMessage(ctx, kit.Message{Text: "Summarize this repository"})
	if err != nil || submission.Turn == nil {
		return
	}

	updates, err := session.Watch(ctx)
	if err != nil {
		return
	}
	select {
	case update := <-updates.Updates():
		if update.Snapshot != nil {
			fmt.Printf("attached to %s\n", update.Snapshot.Session.ID)
		}
	case <-ctx.Done():
	}
	_ = updates.Close()

	outcome, err := submission.Turn.Wait(ctx)
	if err == nil {
		fmt.Println(outcome.Text)
	}
}

func compilePublicSessionSurface(ctx context.Context, session *kit.Session) {
	_, _ = session.Snapshot(ctx)
	submission, _ := session.SendMessage(ctx, kit.Message{Text: "hello"})
	_, _ = session.SubmitPromptCommand(ctx, "review", "--staged")
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
