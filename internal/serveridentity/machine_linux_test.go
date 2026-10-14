//go:build linux

package serveridentity

import "testing"

func TestParseMachineIDFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		body string
		want string
		ok   bool
	}{
		{body: "0123456789ABCDEF0123456789abcdef\n", want: "0123456789abcdef0123456789abcdef", ok: true},
		{body: "uninitialized\n"},
		{body: "00000000000000000000000000000000\n"},
		{body: "0123456789abcdef\n"},
		{body: ""},
	}
	for _, test := range cases {
		got, ok := parseMachineIDFile([]byte(test.body))
		if got != test.want || ok != test.ok {
			t.Errorf("parseMachineIDFile(%q) = %q, %v; want %q, %v", test.body, got, ok, test.want, test.ok)
		}
	}
}
