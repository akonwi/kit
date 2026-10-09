package promptcommand

import "testing"

func TestParseReadsTheNameAndRawArguments(t *testing.T) {
	cases := []struct {
		text string
		want Invocation
	}{
		{"/claude-fix 123 high", Invocation{Name: "claude-fix", Arguments: "123 high", OK: true}},
		{"/claude-fix", Invocation{Name: "claude-fix", OK: true}},
		{"/claude-fix\n\"auth module\" now", Invocation{Name: "claude-fix", Arguments: "\"auth module\" now", OK: true}},
		{"claude-fix 123", Invocation{}},
		{"/ claude-fix", Invocation{}},
		{"/", Invocation{}},
	}
	for _, test := range cases {
		if got := Parse(test.text); got != test.want {
			t.Errorf("Parse(%q) = %+v, want %+v", test.text, got, test.want)
		}
	}
}
