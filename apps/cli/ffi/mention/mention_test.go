package mention

import "testing"

func TestObserve(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		previous     string
		next         string
		pasted       bool
		continueWith *string
		wantOpen     bool
		wantOpened   bool
		wantQuery    string
	}{
		{name: "opens at start", previous: "", next: "@", wantOpen: true, wantOpened: true},
		{name: "opens after space", previous: "see ", next: "see @", wantOpen: true, wantOpened: true},
		{name: "opens after newline", previous: "see\n", next: "see\n@", wantOpen: true, wantOpened: true},
		{name: "does not open inside a word", previous: "mail", next: "mail@"},
		{name: "does not open from a paste", previous: "", next: "@", pasted: true},
		{name: "tracks the query", previous: "", next: "@", wantOpened: true, continueWith: ptr("@src"), wantOpen: true, wantQuery: "src"},
		{name: "closes on whitespace", previous: "", next: "@", wantOpened: true, continueWith: ptr("@src ")},
		{name: "closes when the trigger is removed", previous: "", next: "@", wantOpened: true, continueWith: ptr("")},
		{name: "closes on an edit before it", previous: "", next: "@", wantOpened: true, continueWith: ptr("x@")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observed := Observe(State{}, tc.previous, tc.next, tc.pasted, "@")
			opened := observed.Opened
			state := observed.State
			if tc.continueWith != nil {
				state = Observe(state, tc.next, *tc.continueWith, false, "@").State
			}
			if opened != tc.wantOpened || state.Open != tc.wantOpen || state.Query != tc.wantQuery {
				t.Fatalf("opened = %v, open = %v, query = %q; want %v, %v, %q", opened, state.Open, state.Query, tc.wantOpened, tc.wantOpen, tc.wantQuery)
			}
		})
	}
}

func TestAPasteIntoAnOpenMentionClosesIt(t *testing.T) {
	t.Parallel()
	state := Observe(State{}, "", "@", false, "@").State
	if Observe(state, "@", "@src", true, "@").State.Open {
		t.Fatal("a paste kept the mention open")
	}
}

func TestInsertReplacesTheTriggerAndQuery(t *testing.T) {
	t.Parallel()
	state := Observe(State{}, "say ", "say @", false, "@").State
	state = Observe(state, "say @", "say @tui", false, "@").State
	inserted := Insert(state, "say @tui please", "@internal/tui/shell.go ")
	if !inserted.OK || inserted.Text != "say @internal/tui/shell.go  please" || inserted.Cursor != len("say @internal/tui/shell.go ") {
		t.Fatalf("inserted = %+v", inserted)
	}
	if Insert(State{}, "text", "@x ").OK {
		t.Fatal("a closed mention inserted")
	}
}

func TestChangedRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		previous, next        string
		start, oldEnd, newEnd int
	}{
		{previous: "abc", next: "abXc", start: 2, oldEnd: 2, newEnd: 3},
		{previous: "abc", next: "ac", start: 1, oldEnd: 2, newEnd: 1},
		{previous: "abc", next: "aXc", start: 1, oldEnd: 2, newEnd: 2},
		{previous: "same", next: "same", start: 4, oldEnd: 4, newEnd: 4},
	}
	for _, tc := range cases {
		start, oldEnd, newEnd := ChangedRange(tc.previous, tc.next)
		if start != tc.start || oldEnd != tc.oldEnd || newEnd != tc.newEnd {
			t.Fatalf("ChangedRange(%q, %q) = %d, %d, %d", tc.previous, tc.next, start, oldEnd, newEnd)
		}
	}
}

func ptr(value string) *string { return &value }
