package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidViewIDAcceptsOnlyLowercaseGeneratedIdentifiers(t *testing.T) {
	valid := "view_" + strings.Repeat("0a", 16)
	for id, want := range map[string]bool{
		valid:                                      true,
		strings.ToUpper(valid[:5]) + valid[5:]:     false,
		"view_" + strings.Repeat("0A", 16):         false,
		"view_" + strings.Repeat("0a", 15):         false,
		"view_" + strings.Repeat("zz", 16):         false,
		"clientplugin_" + strings.Repeat("0a", 16): false,
		"": false,
	} {
		if got := ValidViewID(id); got != want {
			t.Errorf("ValidViewID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestViewRevealInputNamesExactlyOneTarget(t *testing.T) {
	for _, test := range []struct {
		input ViewRevealInput
		valid bool
	}{
		{ViewRevealInput{Line: 40}, true},
		{ViewRevealInput{Annotation: 7}, true},
		{ViewRevealInput{Line: MaxViewRevealLine}, true},
		{ViewRevealInput{}, false},
		{ViewRevealInput{Line: 40, Annotation: 7}, false},
		{ViewRevealInput{Line: -1}, false},
		{ViewRevealInput{Line: MaxViewRevealLine + 1}, false},
	} {
		if err := test.input.Validate(); (err == nil) != test.valid {
			t.Errorf("Validate(%+v) = %v, want valid %v", test.input, err, test.valid)
		}
	}
}

func TestViewRevealWireShape(t *testing.T) {
	id := "view_" + strings.Repeat("ab", 16)
	reveal := ViewReveal{SessionID: "session_1", ViewID: id, Line: 40}
	if err := reveal.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(reveal)
	if want := `{"sessionId":"session_1","viewId":"` + id + `","line":40}`; string(encoded) != want {
		t.Fatalf("reveal = %s, want %s", encoded, want)
	}
	for _, invalid := range []ViewReveal{
		{ViewID: id, Line: 1},
		{SessionID: "session_1", ViewID: "view_1", Line: 1},
		{SessionID: "session_1", ViewID: id},
	} {
		if invalid.Validate() == nil {
			t.Errorf("Validate(%+v) accepted an invalid reveal", invalid)
		}
	}
}
