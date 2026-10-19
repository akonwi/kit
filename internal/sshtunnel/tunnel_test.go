package sshtunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/version"
)

// TestMain lets the test binary stand in for ssh: with KIT_FAKE_SSH set, it
// answers status queries from a report file and serves port forwards, as
// fakeSSH describes.
func TestMain(m *testing.M) {
	if os.Getenv("KIT_FAKE_SSH") != "" {
		os.Exit(fakeSSH(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeSSH records its arguments, then:
//   - for a status query, exits with the code in query-exit and the message
//     in query-stderr, or prints the report in report;
//   - for a forward, fails while forward-failures counts down, then forwards
//     its local port to the target until the file stop exists.
func fakeSSH(arguments []string) int {
	dir := os.Getenv("KIT_FAKE_SSH")
	if len(arguments) > 3 && arguments[2] == "-O" {
		appendFile(filepath.Join(dir, "cancels"), strings.Join(arguments, " ")+"\n")
		return 255
	}
	appendFile(filepath.Join(dir, "calls"), strings.Join(arguments, " ")+"\n")
	remote := arguments[len(arguments)-1]
	if strings.Contains(remote, "kit server status --json") {
		if code, err := os.ReadFile(filepath.Join(dir, "query-exit")); err == nil {
			message, _ := os.ReadFile(filepath.Join(dir, "query-stderr"))
			fmt.Fprint(os.Stderr, string(message))
			var exit int
			fmt.Sscan(string(code), &exit)
			return exit
		}
		report, _ := os.ReadFile(filepath.Join(dir, "report"))
		fmt.Println("motd from a shell startup file")
		fmt.Print(string(report))
		return 0
	}
	if failures, err := os.ReadFile(filepath.Join(dir, "forward-failures")); err == nil && strings.TrimSpace(string(failures)) != "0" {
		var count int
		fmt.Sscan(string(failures), &count)
		os.WriteFile(filepath.Join(dir, "forward-failures"), []byte(fmt.Sprint(count-1)), 0o600)
		fmt.Fprintln(os.Stderr, "bind [127.0.0.1]:1: Address already in use")
		fmt.Fprintln(os.Stderr, "channel_setup_fwd_listener_tcpip: cannot listen to port: 1")
		return 255
	}
	var spec string
	for index, argument := range arguments {
		if argument == "-L" {
			spec = arguments[index+1]
		}
	}
	parts := strings.SplitN(spec, ":", 3)
	listener, err := net.Listen("tcp", parts[0]+":"+parts[1])
	if err != nil {
		return 255
	}
	go func() {
		for {
			accepted, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer accepted.Close()
				target, err := net.Dial("tcp", parts[2])
				if err != nil {
					return
				}
				defer target.Close()
				go io.Copy(target, accepted)
				io.Copy(accepted, target)
			}()
		}
	}()
	fmt.Println(readyMarker)
	// The remote command ends when its input closes.
	closed := make(chan struct{})
	go func() {
		io.Copy(io.Discard, os.Stdin)
		close(closed)
	}()
	for {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return 0
		}
		select {
		case <-closed:
			appendFile(filepath.Join(dir, "input-closed"), "x\n")
			return 0
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func appendFile(path, text string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		file.WriteString(text)
		file.Close()
	}
}

type fixture struct {
	dir     string
	options Options
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KIT_FAKE_SSH", dir)
	return &fixture{dir: dir, options: Options{Destination: "me@devbox", Start: true, SSH: os.Args[0]}}
}

func (f *fixture) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// serve starts a server standing in for Kit and reports it as the host's.
func (f *fixture) serve(t *testing.T, instance, token string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(writer, instance)
	}))
	t.Cleanup(server.Close)
	f.write(t, "report", fmt.Sprintf(`{"home":"/home/me/.kit","pid":%d,"url":%q,"kitVersion":%q,"protocolVersion":%d,"instanceId":%q,"databaseReady":true,"token":%q}`+"\n",
		os.Getpid(), server.URL, version.Version, version.SessionProtocolVersion, instance, token))
	return server.URL
}

func (f *fixture) calls(t *testing.T) []string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(f.dir, "calls"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func get(t *testing.T, url, token string) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, url+"/v1/health", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request through the forward: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}

func TestOpenStartsTheServerAndForwardsALocalPortToIt(t *testing.T) {
	f := newFixture(t)
	f.options.Interactive = true
	serverURL := f.serve(t, "instance_1", "token_1")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	connection, err := tunnel.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(connection.URL, "http://127.0.0.1:") || connection.Token != "token_1" || connection.InstanceID != "instance_1" ||
		connection.PID != os.Getpid() || !connection.VerifyPublishedIdentity {
		t.Fatalf("connection = %#v", connection)
	}
	if got := get(t, connection.URL, connection.Token); got != "instance_1" {
		t.Fatalf("through the forward: %q", got)
	}
	calls := f.calls(t)
	target := strings.TrimPrefix(serverURL, "http://")
	local := strings.TrimPrefix(connection.URL, "http://")
	want := []string{
		"-n -T -o ConnectTimeout=10 -- me@devbox sh -c 'kit server start >/dev/null; kit server status --json'",
		fmt.Sprintf("-T -o ConnectTimeout=10 -L %s:%s -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -- me@devbox sh -c 'echo kit-forward-ready; (while kill -0 $$; do kill -0 %d || { kill $$; exit; }; sleep 2; done) >/dev/null 2>&1 & read line; kill $! 2>/dev/null'", local, target, os.Getpid()),
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ssh calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

// The forward ends when the server stops; the next resolve finds its
// replacement without prompting.
func TestResolveFollowsTheServerAcrossARestart(t *testing.T) {
	f := newFixture(t)
	f.options.Interactive = true
	f.serve(t, "instance_1", "token_1")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	f.serve(t, "instance_2", "token_2")
	f.write(t, "stop", "")
	deadline := time.Now().Add(5 * time.Second)
	for tunnel.current.alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Remove(filepath.Join(f.dir, "stop")); err != nil {
		t.Fatal(err)
	}
	connection, err := tunnel.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if connection.InstanceID != "instance_2" || get(t, connection.URL, connection.Token) != "instance_2" {
		t.Fatalf("after the restart: %#v", connection)
	}
	calls := f.calls(t)
	if len(calls) != 4 || !strings.Contains(calls[2], "-o BatchMode=yes") || !strings.Contains(calls[3], "-o BatchMode=yes") {
		t.Fatalf("reopening may prompt: %q", calls)
	}
	if !strings.HasSuffix(calls[2], "sh -c 'kit server status --json'") {
		t.Fatalf("reopening starts the server: %q", calls[2])
	}
}

func TestOpenRetriesWhenAnotherProgramTakesTheLocalPort(t *testing.T) {
	f := newFixture(t)
	f.serve(t, "instance_1", "token_1")
	f.write(t, "forward-failures", "2")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	if calls := f.calls(t); len(calls) != 4 {
		t.Fatalf("ssh calls = %q", calls)
	}
}

func TestOpenRefusesAnIncompatibleServerBeforeForwarding(t *testing.T) {
	f := newFixture(t)
	f.write(t, "report", fmt.Sprintf(`{"home":"/h","pid":1,"url":"http://127.0.0.1:2","kitVersion":"0.1.0","protocolVersion":%d,"instanceId":"i","databaseReady":true,"token":"t"}`, version.SessionProtocolVersion-1))
	_, err := Open(t.Context(), f.options)
	want := fmt.Sprintf("the Kit server on me@devbox is incompatible: it runs Kit 0.1.0 (protocol %d) and this is Kit %s (protocol %d); upgrade Kit on me@devbox",
		version.SessionProtocolVersion-1, version.Version, version.SessionProtocolVersion)
	if err == nil || err.Error() != want || !errors.Is(err, clienttransport.ErrIncompatibleServer) {
		t.Fatalf("error = %v", err)
	}
	if calls := f.calls(t); len(calls) != 1 {
		t.Fatalf("forwarded to an incompatible server: %q", calls)
	}
}

func TestQueryExplainsFailures(t *testing.T) {
	for _, test := range []struct {
		name, exit, stderr string
		start              bool
		want               string
		is                 error
	}{
		{name: "ssh", exit: "255", stderr: "ssh: Could not resolve hostname devbox: nodename nor servname provided\n", start: true,
			want: "connect to me@devbox over SSH: ssh: Could not resolve hostname devbox: nodename nor servname provided"},
		{name: "no kit", exit: "127", stderr: "sh: kit: command not found\n", start: true,
			want: "kit is not on PATH on me@devbox for commands run over SSH; add its directory to PATH in a file your shell reads for non-interactive commands, such as ~/.zshenv or ~/.bashrc"},
		{name: "old kit", exit: "2", stderr: "Error: unknown flag: --json\n", start: true, want: "the Kit on me@devbox is too old to attach over SSH; upgrade it"},
		{name: "not running", exit: "1", stderr: "server unavailable: no daemon\n", want: "no Kit server is running on me@devbox", is: ErrNotRunning},
		{name: "start failed", exit: "1", stderr: "kit: start server: database is locked\n", start: true, want: "start the Kit server on me@devbox: kit: start server: database is locked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.options.Start = test.start
			f.write(t, "query-exit", test.exit)
			f.write(t, "query-stderr", test.stderr)
			_, err := Query(t.Context(), f.options)
			if err == nil || err.Error() != test.want || (test.is != nil && !errors.Is(err, test.is)) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestQueryWithoutStartingOnlyAsksForStatus(t *testing.T) {
	f := newFixture(t)
	f.options.Start = false
	f.serve(t, "instance_1", "token_1")
	report, err := Query(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if report.InstanceID != "instance_1" {
		t.Fatalf("report = %#v", report)
	}
	if calls := f.calls(t); len(calls) != 1 || calls[0] != "-n -T -o ConnectTimeout=10 -o BatchMode=yes -- me@devbox sh -c 'kit server status --json'" {
		t.Fatalf("ssh calls = %q", calls)
	}
}

func TestDestinationsAreNeverOptions(t *testing.T) {
	for _, destination := range []string{"", "-oProxyCommand=touch /tmp/x", "me@host evil", "host\n", "host;id", "$(id)@host", "hóst"} {
		if err := ValidateDestination(destination); err == nil {
			t.Fatalf("ValidateDestination(%q) accepted it", destination)
		}
	}
	for _, destination := range []string{"devbox", "me@devbox.example.com", "ssh://me@devbox:2222", "me@[::1]", "fe80::1%en0"} {
		if err := ValidateDestination(destination); err != nil {
			t.Fatalf("ValidateDestination(%q) = %v", destination, err)
		}
	}
	if _, err := Query(context.Background(), Options{Destination: "-oProxyCommand=x"}); err == nil {
		t.Fatal("Query ran ssh with an option for a destination")
	}
}

func TestShellQuotesForSh(t *testing.T) {
	if got := shell("echo 'hi'"); got != `sh -c 'echo '\''hi'\'''` {
		t.Fatalf("shell = %s", got)
	}
}

// Closing a tunnel closes the forward's input, which ends its remote command
// as well as ssh.
func TestCloseEndsTheRemoteCommand(t *testing.T) {
	f := newFixture(t)
	f.serve(t, "instance_1", "token_1")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	current := tunnel.current
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
	if current.alive() {
		t.Fatal("ssh is still running")
	}
	if data, _ := os.ReadFile(filepath.Join(f.dir, "input-closed")); string(data) != "x\n" {
		t.Fatalf("the remote command's input stayed open: %q", data)
	}
	if _, err := tunnel.Resolve(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Resolve() after Close = %v", err)
	}
}

func TestInvalidateDropsOnlyTheRejectedForward(t *testing.T) {
	f := newFixture(t)
	f.serve(t, "instance_1", "token_1")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	connection, _ := tunnel.Resolve()
	tunnel.Invalidate(clienttransport.Connection{URL: connection.URL, InstanceID: "instance_0"})
	if tunnel.current == nil {
		t.Fatal("a stale rejection dropped the current forward")
	}
	f.serve(t, "instance_2", "token_2")
	tunnel.Invalidate(connection)
	again, err := tunnel.Resolve()
	if err != nil || again.InstanceID != "instance_2" {
		t.Fatalf("after invalidating: %#v, %v", again, err)
	}
}

// A server stopped on its host stays stopped while a client is attached.
func TestReopeningReportsAStoppedServer(t *testing.T) {
	f := newFixture(t)
	f.serve(t, "instance_1", "token_1")
	tunnel, err := Open(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()
	f.write(t, "query-exit", "1")
	f.write(t, "query-stderr", "server unavailable: no daemon\n")
	connection, _ := tunnel.Resolve()
	tunnel.Invalidate(connection)
	if _, err := tunnel.Resolve(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Resolve() = %v", err)
	}
}

// remoteWait runs a forward's remote command against a stand-in server
// process the way sshd would, with its input held open. The returned pipe
// reaches EOF once every process the command started has exited.
func remoteWait(t *testing.T) (server, shell *exec.Cmd, done *os.File) {
	t.Helper()
	server = exec.Command("sleep", "60")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})
	done, held, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { done.Close() })
	shell = exec.Command("sh", "-c", waitScript(server.Process.Pid))
	shell.ExtraFiles = []*os.File{held}
	input, err := shell.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close() })
	output, err := shell.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	held.Close()
	t.Cleanup(func() {
		_ = shell.Process.Kill()
		_ = shell.Wait()
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != readyMarker {
		t.Fatalf("remote command printed %q, %v", line, err)
	}
	// Let the watcher pass its first check, so what follows is caught by
	// its loop rather than before it starts.
	time.Sleep(250 * time.Millisecond)
	return server, shell, done
}

func waitForEOF(t *testing.T, pipe *os.File, what string) {
	t.Helper()
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, pipe)
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(3 * watchSeconds * time.Second):
		t.Fatalf("%s: the remote command's processes are still running", what)
	}
}

// Some SSH servers kill the remote command outright when the connection
// drops; its watcher must not outlive it.
func TestRemoteWatcherEndsWithItsShell(t *testing.T) {
	_, shell, done := remoteWait(t)
	if err := shell.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = shell.Wait()
	waitForEOF(t, done, "after the shell was killed")
}

func TestRemoteCommandEndsWhenTheServerExits(t *testing.T) {
	server, shell, done := remoteWait(t)
	if err := server.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = server.Wait()
	waitForEOF(t, done, "after the server exited")
	_ = shell.Wait()
}
