// Package sshtunnel reaches a Kit server on another host through the system
// ssh executable, so the user's SSH configuration, agent, and jump hosts
// apply. It asks the host's Kit for its server's status report, checks
// protocol compatibility, and holds a loopback port forward to the server for
// as long as the tunnel is open, opening it again after a disconnect or a
// server restart.
package sshtunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/serverstatus"
	"github.com/akonwi/kit/internal/version"
)

// ErrNotRunning reports a host with no running Kit server, from a query that
// does not start one.
var ErrNotRunning = errors.New("no Kit server is running")

// ErrClosed reports use of a closed tunnel.
var ErrClosed = errors.New("the SSH tunnel is closed")

const (
	// queryTimeout bounds a non-interactive status query.
	queryTimeout = 30 * time.Second
	// forwardTimeout bounds a non-interactive forward becoming ready, and
	// any forward carrying its first response once ssh holds the port.
	forwardTimeout = 15 * time.Second
	// forwardAttempts bounds retries when another program takes the chosen
	// local port first.
	forwardAttempts = 3
	// stopGrace is how long ssh has to exit after its input closes, and
	// again after SIGTERM, before it is killed.
	stopGrace = 2 * time.Second
	// outputLimit bounds the ssh output kept for reports and error messages.
	outputLimit = 64 << 10
	// readyMarker is what a forward's remote command prints once ssh holds
	// the local port and the session is open.
	readyMarker = "kit-forward-ready"
	// watchSeconds is how often a forward's remote command checks the
	// server process.
	watchSeconds = 2
)

// Options configure a tunnel.
type Options struct {
	// Destination is what ssh connects to: [user@]host, an alias from the
	// user's SSH configuration, or an ssh:// URL.
	Destination string
	// Start starts the host's Kit server when none is running while the
	// tunnel first opens. Reopening never starts it, so a server stopped on
	// its host stays stopped.
	Start bool
	// Interactive lets ssh ask for passwords, passphrases, and host key
	// confirmation on the terminal while the tunnel first opens, and shows
	// ssh's diagnostics on Stderr. Reopening never prompts.
	Interactive bool
	// Stderr receives ssh's diagnostics while an interactive tunnel opens.
	Stderr io.Writer
	// SSH is the ssh executable; empty means "ssh" on PATH.
	SSH string
}

// ValidateDestination checks that destination can be passed to ssh as a
// destination and never as an option: [user@]host, an alias, or an ssh://
// URL, in the characters OpenSSH accepts for them.
func ValidateDestination(destination string) error {
	switch {
	case destination == "":
		return errors.New("an SSH destination is required")
	case strings.HasPrefix(destination, "-"):
		return fmt.Errorf("SSH destination %q cannot start with '-'", destination)
	case len(destination) > 1024:
		return errors.New("SSH destination is too long")
	}
	for _, r := range destination {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-@:/[]%+", r)) {
			return fmt.Errorf("SSH destination %q may contain only letters, digits, and . _ - @ : / [ ] %% +", destination)
		}
	}
	return nil
}

// Query asks the host's Kit for its server's status report, starting the
// server first when options.Start is set. It returns ErrNotRunning when the
// server is not running and options.Start is not set.
func Query(ctx context.Context, options Options) (serverstatus.Report, error) {
	if err := ValidateDestination(options.Destination); err != nil {
		return serverstatus.Report{}, err
	}
	// A failed start still reports a running server, such as an older one
	// left after Kit was upgraded on the host, which this client may still
	// be able to use.
	remote := "kit server status --json"
	if options.Start {
		remote = "kit server start >/dev/null; " + remote
	}
	if !options.Interactive {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, queryTimeout)
		defer cancel()
	}
	command := exec.CommandContext(ctx, executable(options), sshArguments(options.Interactive, true, nil, options.Destination, shell(remote))...)
	terminate(command)
	stdout := &tail{limit: outputLimit}
	diagnostics := &tail{limit: outputLimit}
	command.Stdout = stdout
	command.Stderr = diagnostics
	if options.Interactive && options.Stderr != nil {
		command.Stderr = io.MultiWriter(options.Stderr, diagnostics)
	}
	err := command.Run()
	if ctx.Err() != nil {
		return serverstatus.Report{}, fmt.Errorf("ask %s for its Kit server: %w", options.Destination, ctx.Err())
	}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return serverstatus.Report{}, queryFailure(options, exit.ExitCode(), diagnostics.String())
	case err != nil:
		return serverstatus.Report{}, fmt.Errorf("run ssh: %w", err)
	}
	report, err := serverstatus.Decode([]byte(lastLine(stdout.String())))
	if err != nil {
		return serverstatus.Report{}, fmt.Errorf("the Kit on %s: %w", options.Destination, err)
	}
	return report, nil
}

// queryFailure explains a failed status query from its exit status: 255 is
// ssh's own failure, 127 a missing command, 2 a usage error from a Kit too
// old to report status as JSON, and 1 a server that is not running.
func queryFailure(options Options, code int, diagnostics string) error {
	switch code {
	case 255:
		detail := lastNonEmptyLine(diagnostics)
		if detail == "" {
			detail = "ssh failed"
		}
		return fmt.Errorf("connect to %s over SSH: %s", options.Destination, detail)
	case 127:
		return fmt.Errorf("kit is not on PATH on %s for commands run over SSH; add its directory to PATH in a file your shell reads for non-interactive commands, such as ~/.zshenv or ~/.bashrc", options.Destination)
	case 2:
		return fmt.Errorf("the Kit on %s is too old to attach over SSH; upgrade it", options.Destination)
	case 1:
		if !options.Start {
			return fmt.Errorf("%w on %s", ErrNotRunning, options.Destination)
		}
	}
	detail := allLines(diagnostics)
	if detail == "" {
		detail = "exit status " + strconv.Itoa(code)
	}
	return fmt.Errorf("start the Kit server on %s: %s", options.Destination, detail)
}

// Tunnel holds a port forward to a Kit server on another host.
type Tunnel struct {
	options  Options
	lifetime context.Context
	cancel   context.CancelFunc

	mu      sync.Mutex
	current *forward
}

// forward is one ssh process forwarding a local port to one run of the
// server. Its remote command ends when the server process does or when its
// input closes, so the forward ends when the server stops, the connection
// drops, this process exits, or it is stopped.
type forward struct {
	connection  clienttransport.Connection
	destination string
	spec        string
	options     Options
	command     *exec.Cmd
	input       io.WriteCloser
	exited      chan struct{}
	stderr      *tail
}

func (f *forward) alive() bool {
	select {
	case <-f.exited:
		return false
	default:
		return true
	}
}

// stop closes the forward's input, which ends its remote command, then
// terminates and finally kills ssh if it lingers. A connection shared
// through ControlMaster keeps forwards a client requested, so it also asks
// the master to cancel the forward.
func (f *forward) stop() {
	_ = f.input.Close()
	if f.wait(stopGrace) {
		f.cancelShared()
		return
	}
	_ = f.command.Process.Signal(syscall.SIGTERM)
	if !f.wait(stopGrace) {
		_ = f.command.Process.Kill()
		f.wait(stopGrace)
	}
	f.cancelShared()
}

func (f *forward) wait(limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-f.exited:
		return true
	case <-timer.C:
		return false
	}
}

func (f *forward) cancelShared() {
	ctx, cancel := context.WithTimeout(context.Background(), stopGrace)
	defer cancel()
	command := exec.CommandContext(ctx, executable(f.options), "-o", "BatchMode=yes", "-O", "cancel", "-L", f.spec, "--", f.destination)
	terminate(command)
	_ = command.Run()
}

// Open queries the host and forwards a local port to its server. An
// interactive tunnel may prompt on the terminal while it opens.
func Open(ctx context.Context, options Options) (*Tunnel, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	tunnel := &Tunnel{options: options, lifetime: lifetime, cancel: cancel}
	stop := context.AfterFunc(ctx, cancel)
	current, err := tunnel.open(lifetime, options.Interactive, options.Start)
	if !stop() || err != nil {
		cancel()
		if current != nil {
			current.stop()
		}
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	tunnel.current = current
	return tunnel, nil
}

// Resolve returns the connection through the current forward, opening a new
// one, without prompting or starting the server, when the last one ended.
func (t *Tunnel) Resolve() (clienttransport.Connection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lifetime.Err() != nil {
		return clienttransport.Connection{}, ErrClosed
	}
	if t.current != nil && t.current.alive() {
		return t.current.connection, nil
	}
	if t.current != nil {
		t.current.stop()
		t.current = nil
	}
	current, err := t.open(t.lifetime, false, false)
	if err != nil {
		if t.lifetime.Err() != nil {
			return clienttransport.Connection{}, ErrClosed
		}
		return clienttransport.Connection{}, err
	}
	t.current = current
	return current.connection, nil
}

// Invalidate drops the forward that carried connection when the server
// rejected it, so the next Resolve finds the server's current run.
func (t *Tunnel) Invalidate(connection clienttransport.Connection) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && t.current.connection.InstanceID == connection.InstanceID && t.current.connection.URL == connection.URL {
		t.current.stop()
		t.current = nil
	}
}

// Close ends the forward, interrupting a reopen in progress. It is safe to
// call more than once.
func (t *Tunnel) Close() error {
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		t.current.stop()
		t.current = nil
	}
	return nil
}

func (t *Tunnel) open(ctx context.Context, interactive, start bool) (*forward, error) {
	options := t.options
	options.Interactive, options.Start = interactive, start
	report, err := Query(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := compatible(options.Destination, report); err != nil {
		return nil, err
	}
	host, port, err := report.Address()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for range forwardAttempts {
		current, err := t.forward(ctx, options, report, host, port)
		if err == nil {
			return current, nil
		}
		lastErr = err
		if !errors.Is(err, errPortTaken) {
			break
		}
	}
	return nil, lastErr
}

var errPortTaken = errors.New("the local port was taken")

// compatible explains a protocol mismatch with the upgrade that resolves it.
// A client never replaces a server it reaches over SSH.
func compatible(destination string, report serverstatus.Report) error {
	if clienttransport.CheckCompatibility(report.ProtocolVersion, report.KitVersion) == nil {
		return nil
	}
	action := "upgrade Kit on " + destination
	if report.ProtocolVersion > version.SessionProtocolVersion {
		action = "upgrade this Kit"
	}
	return incompatible(fmt.Sprintf("the Kit server on %s is incompatible: it runs Kit %s (protocol %d) and this is Kit %s (protocol %d); %s",
		destination, report.KitVersion, report.ProtocolVersion, version.Version, version.SessionProtocolVersion, action))
}

// incompatible is a protocol mismatch, matching ErrIncompatibleServer.
type incompatible string

func (e incompatible) Error() string { return string(e) }
func (e incompatible) Unwrap() error { return clienttransport.ErrIncompatibleServer }

// waitScript is a forward's remote command. Instead of holding the forward
// with -N, it reports that the session is open, which happens only after ssh
// holds the local port, then waits until the server process exits or its
// input closes. The watcher loops only while the waiting shell lives, because
// some SSH servers kill that shell outright when the connection drops, and it
// ends that shell only after finding it alive and the server gone.
func waitScript(serverPID int) string {
	return fmt.Sprintf("echo %s; (while kill -0 $$; do kill -0 %d || { kill $$; exit; }; sleep %d; done) >/dev/null 2>&1 & read line; kill $! 2>/dev/null", readyMarker, serverPID, watchSeconds)
}

func (t *Tunnel) forward(ctx context.Context, options Options, report serverstatus.Report, host string, port int) (*forward, error) {
	local, err := freePort()
	if err != nil {
		return nil, err
	}
	spec := fmt.Sprintf("127.0.0.1:%d:%s:%d", local, forwardHost(host), port)
	extra := []string{
		"-L", spec,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}
	command := exec.Command(executable(options), sshArguments(options.Interactive, false, extra, options.Destination, shell(waitScript(report.PID)))...)
	command.WaitDelay = stopGrace
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	diagnostics := &tail{limit: outputLimit}
	command.Stderr = diagnostics
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("run ssh: %w", err)
	}
	current := &forward{
		connection: clienttransport.Connection{
			URL: fmt.Sprintf("http://127.0.0.1:%d", local), Token: report.Token, InstanceID: report.InstanceID,
			PID: report.PID, ProtocolVersion: report.ProtocolVersion, KitVersion: report.KitVersion,
			VerifyPublishedIdentity: true,
		},
		destination: options.Destination, spec: spec, options: options,
		command: command, input: input, exited: make(chan struct{}), stderr: diagnostics,
	}
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(output)
		signalled := false
		for scanner.Scan() {
			if !signalled && strings.TrimSpace(scanner.Text()) == readyMarker {
				close(ready)
				signalled = true
			}
		}
		_ = command.Wait()
		close(current.exited)
	}()
	if err := waitForForward(ctx, current, ready, options.Interactive); err != nil {
		current.stop()
		detail := diagnostics.String()
		if strings.Contains(detail, "Address already in use") || strings.Contains(detail, "cannot listen to port") {
			return nil, errPortTaken
		}
		if line := lastNonEmptyLine(detail); line != "" {
			return nil, fmt.Errorf("forward a port to the Kit server on %s: %s", options.Destination, line)
		}
		return nil, fmt.Errorf("forward a port to the Kit server on %s: %w", options.Destination, err)
	}
	return current, nil
}

// waitForForward waits until ssh reports it holds the local port, without a
// deadline while the user may be answering a prompt, then until the forward
// carries a response from the server. No request reaches the port, and no
// token leaves this process, before ssh holds it.
func waitForForward(ctx context.Context, current *forward, ready <-chan struct{}, interactive bool) error {
	if !interactive {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, forwardTimeout)
		defer cancel()
	}
	select {
	case <-ready:
	case <-current.exited:
		return errors.New("ssh exited")
	case <-ctx.Done():
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, forwardTimeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current.connection.URL+"/v1/health", nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+current.connection.Token)
		if response, err := client.Do(request); err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-current.exited:
			return errors.New("ssh exited")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sshArguments builds an ssh command line. Extra options precede the
// destination, which follows "--" so it is never read as an option. ssh
// prompts on the terminal itself, never through its standard input.
func sshArguments(interactive, noInput bool, extra []string, destination, remote string) []string {
	arguments := []string{"-T", "-o", "ConnectTimeout=10"}
	if noInput {
		arguments = append([]string{"-n"}, arguments...)
	}
	if !interactive {
		arguments = append(arguments, "-o", "BatchMode=yes")
	}
	arguments = append(arguments, extra...)
	return append(arguments, "--", destination, remote)
}

// terminate makes a command's cancellation ask ssh to exit, so it can end
// the processes it started, and bounds waiting for its output afterwards.
func terminate(command *exec.Cmd) {
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = stopGrace
}

// shell runs script with sh on the host, whatever the user's login shell.
func shell(script string) string {
	return "sh -c '" + strings.ReplaceAll(script, "'", `'\''`) + "'"
}

func executable(options Options) string {
	if options.SSH != "" {
		return options.SSH
	}
	return "ssh"
}

// forwardHost writes an IPv6 address in the brackets ssh expects.
func forwardHost(host string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("choose a local port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func lastLine(text string) string {
	text = strings.TrimRight(text, "\r\n")
	if index := strings.LastIndexByte(text, '\n'); index >= 0 {
		text = text[index+1:]
	}
	return strings.TrimSpace(text)
}

// lastNonEmptyLine is the last line of ssh's diagnostics, without control
// characters, for an error message.
func lastNonEmptyLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return printable(lines[len(lines)-1])
}

// allLines joins ssh's diagnostic lines, without control characters, for an
// error message.
func allLines(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = printable(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}

func printable(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text))
}

// tail keeps the last limit bytes written to it.
type tail struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.limit {
		t.data = append([]byte(nil), t.data[len(t.data)-t.limit:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}
