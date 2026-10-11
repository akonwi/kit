package toolpresentation

import "testing"

func TestOpensAgentNamesTheAddressedSubagent(t *testing.T) {
	cases := []struct {
		name string
		call Call
		want string
	}{
		{name: "started", call: Call{Name: "subagent", Arguments: `{"action":"start","agent":" code-reviewer "}`}, want: "code-reviewer"},
		{name: "listing", call: Call{Name: "subagent", Arguments: `{"action":"list_agents"}`}},
		{name: "other tool", call: Call{Name: "read", Arguments: `{"agent":"code-reviewer"}`}},
		{name: "truncated", call: Call{Name: "subagent", ArgumentsTruncated: true}},
		{name: "malformed", call: Call{Name: "subagent", Arguments: `{`}},
	}
	for _, tc := range cases {
		if got := OpensAgent(tc.call); got != tc.want {
			t.Errorf("%s: OpensAgent = %q, want %q", tc.name, got, tc.want)
		}
	}
}
