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
		{"saved SSH server", attachTarget{Name: "work", SSH: "devbox"}, "session_1", false, "attach work --session session_1"},
		{"another home", attachTarget{Name: "home", Home: "/tmp/kit-home"}, "session_1", false, "attach /tmp/kit-home --session session_1"},
		{"temporary session", attachTarget{}, "session_1", true, "--temp --cwd /src/kit"},
		{"no session", attachTarget{}, "", false, ""},
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
	if _, requested := none.take(); requested {
		t.Fatal("a missing relaunch was requested")
	}
	relaunch := &Relaunch{}
	if _, requested := relaunch.take(); requested {
		t.Fatal("an unrequested relaunch was requested")
	}
	relaunch.Request("session_1", "/opt/homebrew/opt/kit/bin/kit", true)
	want := relaunchRequest{sessionID: "session_1", launcher: "/opt/homebrew/opt/kit/bin/kit", restartServer: true}
	if request, requested := relaunch.take(); !requested || request != want {
		t.Fatalf("take() = %+v, %v; want %+v", request, requested, want)
	}
}
