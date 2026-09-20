package protocol

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestScratchpadRevisionUsesCanonicalDecimalString(t *testing.T) {
	record := Scratchpad{
		OwnerSessionID: "session_0123456789abcdef0123456789abcdef",
		Content:        "# Notes\n",
		Revision:       ScratchpadRevision(math.MaxInt64),
		UpdatedAt:      "2026-03-23T12:34:56.123456789Z",
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"revision":"9223372036854775807"`) {
		t.Fatalf("encoded record = %s", encoded)
	}
	var decoded Scratchpad
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != record {
		t.Fatalf("decoded record = %+v, %v", decoded, err)
	}
}

func TestScratchpadRevisionRejectsNonCanonicalJSON(t *testing.T) {
	for _, encoded := range []string{
		`0`, `1`, `"0"`, `"+1"`, `"01"`, `"-1"`, `"9223372036854775808"`,
		`"\u0031"`, `"\u002d1"`, `"1\u0030"`,
		`"\\u0031"`, `" 1"`, `"1 "`,
	} {
		var revision ScratchpadRevision
		if err := json.Unmarshal([]byte(encoded), &revision); err == nil {
			t.Fatalf("revision %s was accepted", encoded)
		}
	}
}

func TestScratchpadValidation(t *testing.T) {
	record := Scratchpad{
		OwnerSessionID: "session_0123456789abcdef0123456789abcdef",
		Content:        strings.Repeat("x", 64<<10),
		Revision:       1,
		UpdatedAt:      "2026-03-23T12:34:56Z",
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	input := UpdateScratchpadInput{ExpectedRevision: 1, Content: record.Content}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	record.Revision = 2
	if err := record.ValidateApplied(input); err != nil {
		t.Fatal(err)
	}
	record.Content = "other"
	if err := record.ValidateApplied(input); err == nil {
		t.Fatal("mismatched content was accepted")
	}
}
