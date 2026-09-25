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
	terminalStatusFeedbackMarker     = "?"
	terminalStringTerminator         = "\x1b\\"
	terminalTitleFrameDuration       = 160 * time.Millisecond
	terminalMultiplexerFrameDuration = time.Second
)

var terminalStatusRunningFrames = [...]string{
	spinnerFrames[0], spinnerFrames[2], spinnerFrames[4], spinnerFrames[6], spinnerFrames[8],
}

type terminalStatusReporter struct {
	mu sync.Mutex

	write                   func(string) bool
	progressSupported       bool
	titleFrameDuration      time.Duration
	titleRendered           bool
	progressRendered        bool
	title                   string
	progress                terminalProgressState
	progressTarget          terminalProgressState
	progressTargetSet       bool
	progressFailures        int
	progressRetryAfter      time.Time
	status                  terminalStatusState
	statusSet               bool
	runID                   string
	runningAnimationStarted time.Time
	sessionName             string
	cwd                     string
}

func newTerminalStatusReporter() *terminalStatusReporter {
	return &terminalStatusReporter{
		write:              writeTerminalSequence,
		progressSupported:  supportsTerminalProgress(os.Getenv),
		titleFrameDuration: terminalTitleAnimationDuration(os.Getenv),
	}
}

// Update projects one title/progress state. Raw progress failures retry with
// bounded backoff only when another application frame is naturally requested.
func (r *terminalStatusReporter) Update(now time.Time, sessionName, cwd, runID string, state terminalStatusState, setTitle func(string)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessionName = sessionName
	r.cwd = cwd
	replacementRun := r.runID != "" && runID != "" && runID != r.runID
	if !r.statusSet || state != r.status || replacementRun {
		r.status = state
		r.statusSet = true
		if state == terminalStatusRunning {
			r.runningAnimationStarted = now
		}
	}
	r.runID = runID
	title := formatTerminalTitleWithMarker(sessionName, cwd, terminalStatusMarker(
		state, now, r.runningAnimationStarted, r.titleFrameDuration,
	))
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
	return formatTerminalTitleWithMarker(sessionName, cwd, terminalStatusMarker(
		state, time.Time{}, time.Time{}, terminalTitleFrameDuration,
	))
}

func formatTerminalTitleWithMarker(sessionName, cwd, marker string) string {
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
	if marker != "" {
		return marker + " " + title
	}
	return title
}

func terminalStatusMarker(state terminalStatusState, now, runningStarted time.Time, frameDuration time.Duration) string {
	switch state {
	case terminalStatusRunning:
		if frameDuration <= 0 {
			frameDuration = terminalTitleFrameDuration
		}
		elapsed := now.Sub(runningStarted)
		if elapsed < 0 {
			elapsed = 0
		}
		return terminalStatusRunningFrames[int(elapsed/frameDuration)%len(terminalStatusRunningFrames)]
	case terminalStatusFeedback:
		return terminalStatusFeedbackMarker
	default:
		return ""
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
	if !running {
		return terminalStatusIdle
	}
	if feedback {
		return terminalStatusFeedback
	}
	return terminalStatusRunning
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

func terminalTitleAnimationDuration(getenv func(string) string) time.Duration {
	if getenv != nil && (getenv("TMUX") != "" || getenv("STY") != "") {
		return terminalMultiplexerFrameDuration
	}
	return terminalTitleFrameDuration
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
