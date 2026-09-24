//go:build darwin || linux

package mcpruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akonwi/kit/internal/childproc"
	"github.com/akonwi/kit/internal/mcpconfig"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// stdioStopGrace bounds process-group teardown when a session closes.
	stdioStopGrace = 5 * time.Second

	// attributeGrace bounds waiting for a failing server to exit so its stderr
	// can explain an otherwise bare I/O error.
	attributeGrace = 250 * time.Millisecond
)

// stdioFactory returns a factory that mints one fresh transport per call. MCP
// transports are single-use, and the agent core retries a failed connection.
//
// The factory also reclaims a process whose connection was abandoned. go-sdk
// v1.7.0 returns from Client.Connect without closing the connection on some
// post-handshake failures, such as an unsupported negotiated protocol version
// (client.go:389) or a failed subscriptions/listen (client.go:352). Ownership
// has already passed to the SDK at that point, so without this the server would
// survive until runtime shutdown and accumulate one process per failed call.
func (l *Launcher) stdioFactory(server mcpconfig.Server, cwd string) func(context.Context) (sdkmcp.Transport, error) {
	dir := server.Cwd
	if dir == "" {
		dir = cwd
	}
	spec := childproc.Spec{
		Path: server.Command,
		Args: slices.Clone(server.Args),
		Dir:  dir,
		Env:  environ(server.Env),
	}
	owner := &processOwner{name: server.Name}
	return func(context.Context) (sdkmcp.Transport, error) {
		// The agent core calls a factory only when it holds no live session for
		// this namespace, so any process retained here is from a failed attempt.
		owner.reclaim()
		return &stdioTransport{name: server.Name, lifetime: l.lifetime, spec: spec, owner: owner}, nil
	}
}

// processOwner retains the most recent connection for one namespace so an
// abandoned server process can be reclaimed on the next connection attempt.
type processOwner struct {
	name string
	mu   sync.Mutex
	last *processConnection
}

func (o *processOwner) adopt(connection *processConnection) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last = connection
}

func (o *processOwner) reclaim() {
	o.mu.Lock()
	abandoned := o.last
	o.last = nil
	o.mu.Unlock()
	if abandoned != nil {
		_ = abandoned.Close()
	}
}

// environ overlays configured values onto the inherited environment. A nil
// result inherits unchanged; see ADR 0028 for the inheritance decision.
func environ(configured map[string]string) []string {
	if len(configured) == 0 {
		return nil
	}
	inherited := osEnviron()
	overlay := make([]string, 0, len(inherited)+len(configured))
	for _, entry := range inherited {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, overridden := configured[key]; overridden {
				continue
			}
		}
		overlay = append(overlay, entry)
	}
	for key, value := range configured {
		overlay = append(overlay, key+"="+value)
	}
	return overlay
}

// stdioTransport launches a supervised server process on Connect. Starting the
// process here rather than in the factory means a transport that is never
// connected leaves no orphaned child.
type stdioTransport struct {
	name     string
	lifetime context.Context
	spec     childproc.Spec
	owner    *processOwner
}

// Connect starts the server process and speaks newline-delimited JSON over its
// pipes. The process is bound to the runtime lifetime rather than to ctx, which
// the agent core cancels when the originating tool call settles.
func (t *stdioTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	process, err := childproc.Start(t.lifetime, t.spec)
	if err != nil {
		return nil, fmt.Errorf("launch MCP server %q: %w", t.name, err)
	}
	rawConnection, err := (&sdkmcp.IOTransport{Reader: process.Output(), Writer: process.Input()}).Connect(ctx)
	if err != nil {
		stop(process)
		return nil, fmt.Errorf("connect MCP server %q: %w", t.name, errors.Join(err, diagnostics(t.name, process, t.environment())))
	}
	connection := &processConnection{Connection: rawConnection, name: t.name, process: process, environment: t.environment()}
	if t.owner != nil {
		t.owner.adopt(connection)
	}
	return connection, nil
}

// processConnection ties the MCP connection to its supervised process group, so
// closing a namespace terminates the server rather than leaving it resident.
//
// It also attributes I/O failures to the server process. A misconfigured stdio
// server usually fails by exiting during the protocol handshake, which the
// transport observes only as EOF; the retained stderr tail is the sole
// explanation a user can act on.
type processConnection struct {
	sdkmcp.Connection
	name        string
	process     *childproc.Process
	environment []string
	closing     atomic.Bool
	closeOnce   sync.Once
	closeErr    error
}

// environment is the child's effective environment, used to redact inherited
// credentials that a server may echo to stderr.
func (t *stdioTransport) environment() []string {
	if t.spec.Env != nil {
		return t.spec.Env
	}
	return osEnviron()
}

func (c *processConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	message, err := c.Connection.Read(ctx)
	return message, c.attribute(err)
}

func (c *processConnection) Write(ctx context.Context, message jsonrpc.Message) error {
	return c.attribute(c.Connection.Write(ctx, message))
}

// attribute joins the server's stderr tail onto an I/O failure once the process
// has exited. A server that fails during the handshake races the supervisor
// that reaps it, so this waits briefly for the exit rather than reporting a
// bare EOF. Deliberate teardown skips attribution entirely.
func (c *processConnection) attribute(err error) error {
	if err == nil || c.closing.Load() {
		return err
	}
	timer := time.NewTimer(attributeGrace)
	defer timer.Stop()
	select {
	case <-c.process.Done():
	case <-timer.C:
		return err
	}
	return errors.Join(err, diagnostics(c.name, c.process, c.environment))
}

// Close is idempotent and may be called concurrently with Read, as the
// Connection contract requires.
func (c *processConnection) Close() error {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		connErr := c.Connection.Close()
		stopErr := stop(c.process)
		if stopErr != nil {
			stopErr = fmt.Errorf("terminate MCP server %q: %w", c.name, stopErr)
		}
		c.closeErr = errors.Join(connErr, stopErr)
	})
	return c.closeErr
}

func stop(process *childproc.Process) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), stdioStopGrace)
	defer cancel()
	return process.Stop(ctx)
}

// diagnostics surfaces the server's stderr tail, which is the only explanation
// a misconfigured stdio server usually produces. The tail is untrusted output
// from a process that inherited Kit's environment, so it is redacted and
// sanitized before it can reach a log or UI surface.
func diagnostics(name string, process *childproc.Process, environment []string) error {
	tail := sanitizeDiagnostic(process.Stderr(), environment)
	if tail == "" {
		return nil
	}
	return fmt.Errorf("MCP server %q stderr: %s", name, tail)
}
