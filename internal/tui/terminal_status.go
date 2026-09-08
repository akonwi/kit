package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/term"
)

type terminalStatusState uint8

const (
	terminalStatusIdle terminalStatusState = iota
	terminalStatusRunning
	terminalStatusFeedback
)

type terminalProgressState uint8

const (
	terminalProgressRemove terminalProgressState = iota
	terminalProgressIndeterminate
	terminalProgressPaused
)

const (
	terminalStatusRunningMarker  = "●"
	terminalStatusFeedbackMarker = "?"
	terminalStringTerminator     = "\x1b\\"
)

type terminalStatusReporter struct {
	mu sync.Mutex

	write              func(string) bool
	progressSupported  bool
	titleRendered      bool
	progressRendered   bool
	title              string
	progress           terminalProgressState
	progressTarget     terminalProgressState
	progressTargetSet  bool
	progressFailures   int
	progressRetryAfter time.Time
	sessionName        string
	cwd                string
}

func newTerminalStatusReporter() *terminalStatusReporter {
	return &terminalStatusReporter{
		write:             writeTerminalSequence,
		progressSupported: supportsTerminalProgress(os.Getenv),
	}
}

// Update projects one title/progress state. Raw progress failures retry with
// bounded backoff only when another application frame is naturally requested.
func (r *terminalStatusReporter) Update(now time.Time, sessionName, cwd string, state terminalStatusState, setTitle func(string)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessionName = sessionName
	r.cwd = cwd
	title := formatTerminalTitle(sessionName, cwd, state)
	if setTitle != nil && (!r.titleRendered || title != r.title) {
		setTitle(title)
		r.titleRendered = true
		r.title = title
	}
	progress := terminalProgressForStatus(state)
	if !r.progressTargetSet || progress != r.progressTarget {
		r.progressTarget = progress
		r.progressTargetSet = true
		r.progressFailures = 0
		r.progressRetryAfter = time.Time{}
	}
	if !r.progressSupported || r.progressRendered && progress == r.progress ||
		r.progressFailures >= 3 || now.Before(r.progressRetryAfter) {
		return
	}
	if r.write == nil || !r.write(terminalProgressSequence(progress)) {
		r.progressFailures++
		r.progressRetryAfter = now.Add(time.Duration(1<<uint(r.progressFailures-1)) * 250 * time.Millisecond)
		return
	}
	r.progressRendered = true
	r.progress = progress
	r.progressFailures = 0
	r.progressRetryAfter = time.Time{}
}

func (r *terminalStatusReporter) Bell() {
	if r == nil || r.write == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.write("\a")
}

func (r *terminalStatusReporter) Close() {
	if r == nil || r.write == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.titleRendered && !r.progressRendered {
		return
	}
	idleTitle := formatTerminalTitle(r.sessionName, r.cwd, terminalStatusIdle)
	sequence := terminalTitleSequence(idleTitle)
	if r.progressSupported {
		sequence += terminalProgressSequence(terminalProgressRemove)
	}
	r.write(sequence)
	r.titleRendered = true
	r.progressRendered = r.progressSupported
	r.title = idleTitle
	r.progress = terminalProgressRemove
}

func formatTerminalTitle(sessionName, cwd string, state terminalStatusState) string {
	name := sanitizeTerminalTitlePart(sessionName)
	base := sanitizeTerminalTitlePart(filepath.Base(filepath.Clean(cwd)))
	if cwd == "" || base == "." {
		base = ""
	}
	parts := []string{"kit"}
	if name != "" {
		parts = append(parts, name)
	}
	if base != "" {
		parts = append(parts, base)
	}
	title := strings.Join(parts, " - ")
	switch state {
	case terminalStatusRunning:
		return terminalStatusRunningMarker + " " + title
	case terminalStatusFeedback:
		return terminalStatusFeedbackMarker + " " + title
	default:
		return title
	}
}

func sanitizeTerminalTitlePart(value string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) {
			return -1
		}
		return character
	}, value))
}

func resolveTerminalStatus(running, feedback bool) terminalStatusState {
	if feedback {
		return terminalStatusFeedback
	}
	if running {
		return terminalStatusRunning
	}
	return terminalStatusIdle
}

func terminalProgressForStatus(state terminalStatusState) terminalProgressState {
	switch state {
	case terminalStatusRunning:
		return terminalProgressIndeterminate
	case terminalStatusFeedback:
		return terminalProgressPaused
	default:
		return terminalProgressRemove
	}
}

func terminalTitleSequence(title string) string {
	return "\x1b]2;" + title + terminalStringTerminator
}

func terminalProgressSequence(state terminalProgressState) string {
	value := "0"
	switch state {
	case terminalProgressIndeterminate:
		value = "3"
	case terminalProgressPaused:
		value = "4"
	}
	return "\x1b]9;4;" + value + terminalStringTerminator
}

func supportsTerminalProgress(getenv func(string) string) bool {
	if getenv == nil || getenv("TMUX") != "" || getenv("STY") != "" {
		return false
	}
	return strings.EqualFold(getenv("TERM_PROGRAM"), "ghostty") ||
		strings.Contains(strings.ToLower(getenv("TERM")), "ghostty")
}

func writeTerminalSequence(sequence string) bool {
	terminal, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err == nil {
		_, writeErr := io.WriteString(terminal, sequence)
		_ = terminal.Close()
		return writeErr == nil
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}
	_, err = io.WriteString(os.Stderr, sequence)
	return err == nil
}
