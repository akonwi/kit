package cli

import (
	"strings"
	"testing"
)

func TestRelaunchArgumentsReopenTheSessionOnTheSameServer(t *testing.T) {
	cases := []struct {
		name      string
		target    attachTarget
		session   string
		temporary bool
		want      string
	}{
		{"this home's server", attachTarget{}, "session_1", false, "--session session_1"},
		{"saved URL server", attachTarget{Name: "devbox", URL: "https://devbox.example"}, "session_1", false, "attach https://devbox.example --session session_1"},
		{"SSH server", attachTarget{Name: "me@devbox", SSH: "me@devbox"}, "session_1", false, "attach me@devbox --session session_1"},
		{"another home", attachTarget{Name: "home", Home: "/tmp/kit-home"}, "session_1", false, "attach /tmp/kit-home --session session_1"},
		{"temporary session", attachTarget{}, "session_1", true, "--temp --cwd /src/kit"},
	}
	for _, c := range cases {
		got := strings.Join(relaunchArguments(c.target, c.session, c.temporary, "/src/kit"), " ")
		if got != c.want {
			t.Errorf("%s: arguments = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestRelaunchRecordsTheRequest(t *testing.T) {
	var none *Relaunch
	if _, _, requested := none.take(); requested {
		t.Fatal("a missing relaunch was requested")
	}
	relaunch := &Relaunch{}
	if _, _, requested := relaunch.take(); requested {
		t.Fatal("an unrequested relaunch was requested")
	}
	relaunch.Request("session_1", true)
	if session, restart, requested := relaunch.take(); !requested || !restart || session != "session_1" {
		t.Fatalf("take() = %q, %v, %v", session, restart, requested)
	}
}
