package toolpresentation

import "testing"

func TestOpensFileFromReadWriteAndEdit(t *testing.T) {
	cases := []struct {
		name   string
		call   Call
		result Result
		want   FileTarget
	}{
		{"read starts at its offset", Call{Name: "read", Arguments: `{"path":"main.go","offset":40,"limit":10}`}, Result{}, FileTarget{Path: "main.go", Line: 40}},
		{"read without an offset starts at the top", Call{Name: "read", Arguments: `{"path":"main.go"}`}, Result{}, FileTarget{Path: "main.go", Line: 1}},
		{"edit carries no line", Call{Name: "edit", Arguments: `{"path":"main.go","edits":[]}`}, Result{}, FileTarget{Path: "main.go"}},
		{"details keep the resolved path", Call{Name: "write", Arguments: `{"path":"main.go"}`}, Result{Succeeded: true, Details: `{"path":"/repo/main.go"}`}, FileTarget{Path: "/repo/main.go"}},
		{"other tools open nothing", Call{Name: "bash", Arguments: `{"command":"ls"}`}, Result{}, FileTarget{}},
		{"truncated arguments open nothing", Call{Name: "read", Arguments: `{"path":"main.go"}`, ArgumentsTruncated: true}, Result{}, FileTarget{}},
	}
	for _, test := range cases {
		if got := OpensFile(test.call, test.result); got != test.want {
			t.Errorf("%s: got %+v, want %+v", test.name, got, test.want)
		}
	}
}

func TestWorkspacePathStaysInsideTheWorkingDirectory(t *testing.T) {
	cases := map[string]string{
		"main.go":                "main.go",
		"./internal/../main.go":  "main.go",
		"/repo/apps/cli/main.go": "apps/cli/main.go",
		"/elsewhere/main.go":     "",
		"../main.go":             "",
		"/repo":                  "",
	}
	for path, want := range cases {
		if got := WorkspacePath("/repo", path); got != want {
			t.Errorf("WorkspacePath(%q) = %q, want %q", path, got, want)
		}
	}
}
