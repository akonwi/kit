package toolpresentation

import (
	"reflect"
	"testing"
)

func TestShowOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		call    Call
		result  Result
		running bool
		want    Output
	}{
		{
			name:   "read uses the path it reports",
			call:   Call{Name: "read", Arguments: `{"path":"main.go"}`},
			result: Result{Succeeded: true, Text: "package main", Details: `{"path":"/repo/main.go","lines":1}`},
			want:   Output{Blocks: []Block{{Kind: BlockCode, Path: "/repo/main.go", Text: "package main"}}},
		},
		{
			name:   "a failed read shows its error plainly",
			call:   Call{Name: "read", Arguments: `{"path":"main.go"}`},
			result: Result{Text: "no such file"},
			want:   Output{Blocks: []Block{{Kind: BlockCode, Text: "no such file"}}},
		},
		{
			name:   "write shows the written content",
			call:   Call{Name: "write", Arguments: `{"path":"kit.toml","content":"[server]\nport = 1"}`},
			result: Result{Succeeded: true, Text: "Wrote 2 lines to kit.toml"},
			want:   Output{Blocks: []Block{{Kind: BlockCode, Path: "kit.toml", Text: "[server]\nport = 1"}}},
		},
		{
			name:   "write with cut arguments shows its result",
			call:   Call{Name: "write", ArgumentsTruncated: true},
			result: Result{Succeeded: true, Text: "Wrote 4210 lines to huge.md"},
			want: Output{
				Blocks: []Block{{Kind: BlockCode, Text: "Wrote 4210 lines to huge.md"}},
				Note:   Unavailable,
			},
		},
		{
			name: "edit shows each edit as removed then added text",
			call: Call{Name: "edit", Arguments: `{"path":"a.go","edits":[` +
				`{"oldText":"x := 1","newText":"x := 2"},{"oldText":"","newText":"// new"}]}`},
			result: Result{Succeeded: true, Text: "Edited a.go"},
			want: Output{Blocks: []Block{
				{Kind: BlockRemoved, Path: "a.go", Text: "x := 1", Marker: "-"},
				{Kind: BlockAdded, Path: "a.go", Text: "x := 2", Marker: "+"},
				{Kind: BlockSeparator, Text: "⋯"},
				{Kind: BlockAdded, Path: "a.go", Text: "// new", Marker: "+"},
			}},
		},
		{
			name:   "edit accepts the single form",
			call:   Call{Name: "edit", Arguments: `{"path":"a.go","oldText":"a","newText":"b"}`},
			result: Result{Succeeded: true, Text: "Edited a.go"},
			want: Output{Blocks: []Block{
				{Kind: BlockRemoved, Path: "a.go", Text: "a", Marker: "-"},
				{Kind: BlockAdded, Path: "a.go", Text: "b", Marker: "+"},
			}},
		},
		{
			name:   "a failed edit shows its error",
			call:   Call{Name: "edit", Arguments: `{"path":"a.go","oldText":"a","newText":"b"}`},
			result: Result{Text: "Error:\nno match"},
			want:   Output{Blocks: []Block{{Kind: BlockCode, Text: "Error:\nno match"}}},
		},
		{
			name:   "bash shows its command, then plain output",
			call:   Call{Name: "bash", Arguments: `{"command":"go test ./..."}`},
			result: Result{Succeeded: true, Text: "ok  kit 0.1s"},
			want: Output{Blocks: []Block{
				{Kind: BlockCommand, Text: "go test ./..."},
				{Kind: BlockGap},
				{Kind: BlockCode, Text: "ok  kit 0.1s"},
			}},
		},
		{
			name:   "a file dump reads as the file",
			call:   Call{Name: "bash", Arguments: `{"command":"sed -n '1,4p' ard.toml"}`},
			result: Result{Succeeded: true, Text: "[package]"},
			want: Output{Blocks: []Block{
				{Kind: BlockCommand, Text: "sed -n '1,4p' ard.toml"},
				{Kind: BlockGap},
				{Kind: BlockCode, Path: "ard.toml", Text: "[package]"},
			}},
		},
		{
			name:    "a running dump stays plain",
			call:    Call{Name: "bash", Arguments: `{"command":"cat main.go"}`},
			result:  Result{Text: "package"},
			running: true,
			want: Output{Blocks: []Block{
				{Kind: BlockCommand, Text: "cat main.go"},
				{Kind: BlockGap},
				{Kind: BlockCode, Text: "package"},
			}},
		},
		{
			name: "a unified diff splits into runs",
			call: Call{Name: "bash", Arguments: `{"command":"git diff"}`},
			result: Result{Succeeded: true, Text: "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n" +
				"@@ -1,3 +1,3 @@\n keep\n-old\n+new\n+more\n tail\n"},
			want: Output{Blocks: []Block{
				{Kind: BlockCommand, Text: "git diff"},
				{Kind: BlockGap},
				{Kind: BlockHeader, Text: "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go"},
				{Kind: BlockHunk, Text: "@@ -1,3 +1,3 @@"},
				{Kind: BlockCode, Text: "keep", Marker: " "},
				{Kind: BlockRemoved, Text: "old", Marker: "-"},
				{Kind: BlockAdded, Text: "new\nmore", Marker: "+"},
				{Kind: BlockCode, Text: "tail", Marker: " "},
			}},
		},
		{
			name:   "other tools show their result",
			call:   Call{Name: "grep", Arguments: `{"pattern":"x"}`},
			result: Result{Succeeded: true, Text: "a.go:1: x"},
			want:   Output{Blocks: []Block{{Kind: BlockCode, Text: "a.go:1: x"}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ShowOutput(test.call, test.result, test.running); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("output =\n%#v\nwant\n%#v", got, test.want)
			}
		})
	}
}

func TestDumpedFile(t *testing.T) {
	t.Parallel()
	for command, want := range map[string]string{
		"cat main.go":                   "main.go",
		"cat 'my file.go'":              "my file.go",
		"head -n 20 main.go":            "main.go",
		"head -n20 main.go":             "main.go",
		"tail -50 main.go":              "main.go",
		"sed -n '1,40p' main.go":        "main.go",
		"sed -n '/func/,/^}/p' main.go": "main.go",
		"cat a.go b.go":                 "",
		"cat -n main.go":                "",
		"cat main.go | head":            "",
		"cat main.go > copy.go":         "",
		"cd x && cat main.go":           "",
		"cat main.go; echo done":        "",
		"cat $(ls *.go)":                "",
		"cat \"unterminated":            "",
		"sed 's/a/b/' main.go":          "",
		"sed -n '1,4p' a.go b.go":       "",
		"grep -n x main.go":             "",
		"head -n 5":                     "",
		"cat":                           "",
	} {
		if got := dumpedFile(command); got != want {
			t.Errorf("dumpedFile(%q) = %q, want %q", command, got, want)
		}
	}
}
