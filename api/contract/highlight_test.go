package contract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validHighlight() Highlight {
	return Highlight{Language: "go", Lines: [][]int{{0, 4, 0, 5, 4, 1}, {}}, Roles: []string{"keyword", "function"}}
}

func TestHighlightValidatesAgainstItsText(t *testing.T) {
	t.Parallel()
	text := "func main() {\n}\n"
	if err := validHighlight().ValidateText(text); err != nil {
		t.Fatalf("ValidateText() = %v", err)
	}
	if err := (Highlight{Language: "go", Lines: [][]int{}, Roles: []string{}}).ValidateText(""); err != nil {
		t.Fatalf("empty text: %v", err)
	}
	// "😀" is two UTF-16 code units, and "\r" stays in its line.
	astral := Highlight{Language: "go", Lines: [][]int{{0, 3, 0}}, Roles: []string{"string"}}
	if err := astral.ValidateText("😀a\r\n"); err != nil {
		t.Fatalf("astral text: %v", err)
	}
	for name, mutate := range map[string]func(*Highlight){
		"missing language":   func(h *Highlight) { h.Language = "" },
		"control language":   func(h *Highlight) { h.Language = "g\x1bo" },
		"null lines":         func(h *Highlight) { h.Lines = nil },
		"null roles":         func(h *Highlight) { h.Roles = nil },
		"null line":          func(h *Highlight) { h.Lines[1] = nil },
		"too few lines":      func(h *Highlight) { h.Lines = h.Lines[:1] },
		"too many lines":     func(h *Highlight) { h.Lines = append(h.Lines, []int{}) },
		"partial triple":     func(h *Highlight) { h.Lines[0] = h.Lines[0][:4] },
		"overlapping ranges": func(h *Highlight) { h.Lines[0][3] = 3 },
		"unordered ranges":   func(h *Highlight) { h.Lines[0] = []int{5, 4, 1, 0, 4, 0} },
		"empty range":        func(h *Highlight) { h.Lines[0][1] = 0 },
		"negative start":     func(h *Highlight) { h.Lines[0][0] = -1 },
		"past line end":      func(h *Highlight) { h.Lines[0][4] = 9 },
		"unknown role index": func(h *Highlight) { h.Lines[0][2] = 2 },
		"negative role":      func(h *Highlight) { h.Lines[0][2] = -1 },
		"duplicate role":     func(h *Highlight) { h.Roles[1] = "keyword" },
		"empty role":         func(h *Highlight) { h.Roles[1] = "" },
		"long role":          func(h *Highlight) { h.Roles[1] = strings.Repeat("r", MaxHighlightRoleBytes+1) },
	} {
		highlight := validHighlight()
		mutate(&highlight)
		if err := highlight.ValidateText(text); err == nil {
			t.Errorf("ValidateText() accepted %s", name)
		}
	}
	if err := astral.ValidateText("😀\n"); err == nil {
		t.Error("ValidateText() accepted a range past an astral character's two code units")
	}
}

func TestHighlightAcceptsRolesItDoesNotKnow(t *testing.T) {
	t.Parallel()
	highlight := Highlight{Language: "go", Lines: [][]int{{0, 1, 0}}, Roles: []string{"someFutureRole"}}
	if err := highlight.ValidateText("a"); err != nil {
		t.Fatalf("ValidateText() = %v", err)
	}
}

func TestHighlightCodeInputBounds(t *testing.T) {
	t.Parallel()
	block := func(language string, size int) HighlightCodeBlock {
		return HighlightCodeBlock{Language: language, Source: strings.Repeat("a", size)}
	}
	for name, input := range map[string]HighlightCodeInput{
		"no blocks":           {},
		"empty language":      {Blocks: []HighlightCodeBlock{block("", 3)}},
		"largest block":       {Blocks: []HighlightCodeBlock{block("go", MaxHighlightCodeBlockBytes)}},
		"largest total":       {Blocks: []HighlightCodeBlock{block("go", MaxHighlightCodeBlockBytes), block("go", MaxHighlightCodeTotalBytes-MaxHighlightCodeBlockBytes)}},
		"most blocks":         {Blocks: make([]HighlightCodeBlock, MaxHighlightCodeBlocks)},
		"longest language":    {Blocks: []HighlightCodeBlock{block(strings.Repeat("l", MaxHighlightLanguageBytes), 1)}},
		"non-ASCII in source": {Blocks: []HighlightCodeBlock{{Language: "go", Source: "\"😀\"\t\x1b\r\n"}}},
	} {
		if err := input.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v", name, err)
		}
	}
	for name, test := range map[string]struct {
		input HighlightCodeInput
		limit HighlightCodeLimit
	}{
		"too many blocks":   {HighlightCodeInput{Blocks: make([]HighlightCodeBlock, MaxHighlightCodeBlocks+1)}, HighlightCodeLimitBlocks},
		"block too large":   {HighlightCodeInput{Blocks: []HighlightCodeBlock{block("go", MaxHighlightCodeBlockBytes+1)}}, HighlightCodeLimitBlockBytes},
		"total too large":   {HighlightCodeInput{Blocks: []HighlightCodeBlock{block("go", MaxHighlightCodeBlockBytes), block("go", MaxHighlightCodeTotalBytes-MaxHighlightCodeBlockBytes+1)}}, HighlightCodeLimitTotalBytes},
		"language too long": {HighlightCodeInput{Blocks: []HighlightCodeBlock{block(strings.Repeat("l", MaxHighlightLanguageBytes+1), 1)}}, HighlightCodeLimitLanguageBytes},
	} {
		var limit *HighlightCodeLimitError
		if err := test.input.Validate(); !errors.As(err, &limit) || limit.Limit != test.limit {
			t.Errorf("%s: Validate() = %v, want limit %q", name, err, test.limit)
		}
	}
	var limit *HighlightCodeLimitError
	if err := (HighlightCodeInput{Blocks: []HighlightCodeBlock{{Language: "g\no", Source: "x"}}}).Validate(); err == nil || errors.As(err, &limit) {
		t.Errorf("control character in language: Validate() = %v, want an invalid request", err)
	}
}

func TestHighlightCodeResultMatchesItsRequest(t *testing.T) {
	t.Parallel()
	input := HighlightCodeInput{Blocks: []HighlightCodeBlock{{Language: "go", Source: "func main() {\n}\n"}, {Language: "text", Source: "plain"}}}
	highlight := validHighlight()
	valid := HighlightCodeResult{Blocks: []HighlightedCode{{Highlight: &highlight}, {}}}
	if err := valid.Validate(input); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	encoded, _ := json.Marshal(valid)
	want := `{"blocks":[{"highlight":{"language":"go","lines":[[0,4,0,5,4,1],[]],"roles":["keyword","function"]}},{}]}`
	if string(encoded) != want {
		t.Fatalf("wire = %s, want %s", encoded, want)
	}
	for name, result := range map[string]HighlightCodeResult{
		"null blocks":     {},
		"too few blocks":  {Blocks: valid.Blocks[:1]},
		"wrong text":      {Blocks: []HighlightedCode{{}, {Highlight: &highlight}}},
		"too many blocks": {Blocks: append(append([]HighlightedCode(nil), valid.Blocks...), HighlightedCode{})},
	} {
		if err := result.Validate(input); err == nil {
			t.Errorf("Validate() accepted %s", name)
		}
	}
}

// Clients that do not ask for highlighting, including every client built
// before ADR 0062, must see exactly the JSON they did before (#238).
func TestUnhighlightedReadsKeepTheirWireShape(t *testing.T) {
	t.Parallel()
	read := WorkspaceFileRead{SessionID: "s", Workspace: WorkspaceRef{SessionID: "s", CWD: "/w", WorkspaceID: "workspace_A", State: WorkspaceReady, Limits: DefaultWorkspaceLimits()}, Path: "a.go", Revision: "file_A", Size: 2, Encoding: "utf-8", Content: "hi", ReturnedBytes: 2, ReturnedLines: 1}
	encoded, _ := json.Marshal(read)
	want := `{"sessionId":"s","workspace":{"sessionId":"s","cwd":"/w","workspaceId":"workspace_A","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":4,"maxPendingRequests":16}},"path":"a.go","revision":"file_A","size":2,"encoding":"utf-8","content":"hi","returnedBytes":2,"returnedLines":1,"truncated":false}`
	if string(encoded) != want {
		t.Fatalf("workspace read = %s\nwant %s", encoded, want)
	}
	text, _ := json.Marshal(HostFileRead{Payload: HostTextFile{SessionID: "s", Path: "/a", Size: 2, Encoding: "utf-8", Content: "hi", ReturnedBytes: 2, ReturnedLines: 1}})
	if string(text) != `{"kind":"text","sessionId":"s","path":"/a","size":2,"encoding":"utf-8","content":"hi","returnedBytes":2,"returnedLines":1}` {
		t.Fatalf("host text read = %s", text)
	}
	old, newLine := 1, 1
	page, _ := json.Marshal(FileDiffPage{Observation: validObservation(), File: DiffFileSummary{Path: "a", Change: "modified", Old: DiffSide{Kind: "regular", Mode: 0100644}, New: DiffSide{Kind: "regular", Mode: 0100644}, ContentState: "text"}, Computation: DiffComputation{State: "complete"}, Hunks: []DiffHunk{{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []DiffLine{{Kind: "context", OldLine: &old, NewLine: &newLine, Content: "x"}}}}})
	if strings.Contains(string(page), "highlight") {
		t.Fatalf("file diff page = %s carries a highlight it was not asked for", page)
	}
	for name, input := range map[string]any{
		"readWorkspaceFile": ReadWorkspaceFileInput{WorkspaceID: "workspace_A", Path: "a.go"},
		"readHostFile":      ReadHostFileInput{Path: "/a"},
		"readFileDiff":      ReadFileDiffInput{TargetID: "t", TargetRevision: "r", Path: "a"},
	} {
		encoded, _ := json.Marshal(input)
		if strings.Contains(string(encoded), "highlight") {
			t.Errorf("%s request = %s sends a highlight flag it was not given", name, encoded)
		}
	}
}

func TestHighlightedHostTextReadRoundTripsAndValidates(t *testing.T) {
	t.Parallel()
	highlight := Highlight{Language: "go", Lines: [][]int{{0, 2, 0}}, Roles: []string{"keyword"}}
	read := HostFileRead{Payload: HostTextFile{SessionID: "s", Path: "/a.go", Size: 2, Encoding: "utf-8", Content: "go", ReturnedBytes: 2, ReturnedLines: 1, Highlight: &highlight}}
	if err := read.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	encoded, _ := json.Marshal(read)
	want := `{"kind":"text","sessionId":"s","path":"/a.go","size":2,"encoding":"utf-8","content":"go","returnedBytes":2,"returnedLines":1,"highlight":{"language":"go","lines":[[0,2,0]],"roles":["keyword"]}}`
	if string(encoded) != want {
		t.Fatalf("wire = %s\nwant %s", encoded, want)
	}
	var decoded HostFileRead
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if again, _ := json.Marshal(decoded); string(again) != want {
		t.Fatalf("round trip = %s", again)
	}
	if err := json.Unmarshal([]byte(`{"kind":"image","sessionId":"s","path":"/a.gif","size":1,"mediaType":"image/gif","width":1,"height":1,"data":"aA==","highlight":{"language":"go","lines":[],"roles":[]}}`), &decoded); err == nil {
		t.Fatal("decoded an image read carrying a highlight")
	}
	highlight.Lines[0][1] = 3
	if err := read.Validate(); err == nil {
		t.Fatal("Validate() accepted a highlight past its text")
	}
}

func TestFileDiffHighlightCoversEachSidesPageLines(t *testing.T) {
	t.Parallel()
	one, two, three := 1, 2, 3
	page := FileDiffPage{Observation: validObservation(), File: DiffFileSummary{Path: "a.go", Change: "modified", Old: DiffSide{Kind: "regular", Mode: 0100644}, New: DiffSide{Kind: "regular", Mode: 0100644}, ContentState: "text"}, Computation: DiffComputation{State: "complete"}, Hunks: []DiffHunk{{OldStart: 1, OldCount: 2, NewStart: 1, NewCount: 2, Lines: []DiffLine{
		{Kind: "context", OldLine: &one, NewLine: &one, Content: "package a"},
		{Kind: "deletion", OldLine: &two, Content: "var x = 1"},
		{Kind: "addition", NewLine: &two, Content: "var xy = 1"},
		{Kind: "addition", NewLine: &three, Content: "go"},
	}}}}
	old, newSide := page.DiffSideLines()
	if strings.Join(old, "|") != "package a|var x = 1" || strings.Join(newSide, "|") != "package a|var xy = 1|go" {
		t.Fatalf("DiffSideLines() = %q, %q", old, newSide)
	}
	page.Highlight = &FileDiffHighlight{
		Old: &Highlight{Language: "go", Lines: [][]int{{0, 7, 0}, {0, 3, 0}}, Roles: []string{"keyword"}},
		New: &Highlight{Language: "go", Lines: [][]int{{0, 7, 0}, {4, 2, 1}, {}}, Roles: []string{"keyword", "variable"}},
	}
	if err := page.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	page.Highlight.New.Lines = page.Highlight.New.Lines[:2]
	if err := page.Validate(); err == nil {
		t.Fatal("Validate() accepted a new side missing a line")
	}
	page.Highlight = &FileDiffHighlight{}
	if err := page.Validate(); err == nil {
		t.Fatal("Validate() accepted a highlight with neither side")
	}
}
