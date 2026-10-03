package kit_test

import (
	"testing"

	kit "github.com/akonwi/kit/api"
)

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
