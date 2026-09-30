// Package terminalcolors reads the controlling terminal's default colors
// before the Cooper client takes the terminal. It stands in for terminal color
// queries in Cooper and is removed once Cooper provides them.
package terminalcolors

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"time"

	kittheme "github.com/akonwi/kit/internal/theme"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Timeout bounds the whole query, matching the vaxis client's theme query.
const Timeout = 200 * time.Millisecond

// Colors holds the terminal's reported colors. A zero alpha marks a color the
// terminal did not report.
type Colors struct {
	Foreground kittheme.Color
	Background kittheme.Color
	// Palette holds ANSI colors 0 through 7.
	Palette []kittheme.Color
}

// request asks for the default foreground (OSC 10), background (OSC 11), and
// ANSI palette entries 0–7 (OSC 4). Primary device attributes (DA1) follow as
// a sentinel: terminals answer in order and virtually all answer DA1, so its
// reply means every supported color reply has arrived.
const request = "\x1b]10;?\x07\x1b]11;?\x07" +
	"\x1b]4;0;?\x07\x1b]4;1;?\x07\x1b]4;2;?\x07\x1b]4;3;?\x07" +
	"\x1b]4;4;?\x07\x1b]4;5;?\x07\x1b]4;6;?\x07\x1b]4;7;?\x07" +
	"\x1b[c"

var (
	colorReply  = regexp.MustCompile(`\x1b\](10|11|4;([0-7]));rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})(?:\x07|\x1b\\)`)
	deviceReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// Query reports the controlling terminal's colors. It returns no colors when
// there is no terminal, the terminal cannot enter raw mode, or it does not
// answer within Timeout.
func Query() Colors {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return Parse(nil)
	}
	defer func() { _ = tty.Close() }()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return Parse(nil)
	}
	defer func() { _ = term.Restore(fd, state) }()
	if _, err := tty.WriteString(request); err != nil {
		return Parse(nil)
	}
	deadline := time.Now().Add(Timeout)
	received := make([]byte, 0, 512)
	buffer := make([]byte, 256)
	for !deviceReply.Match(received) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		ready, err := waitReadable(fd, remaining)
		if err != nil && err != unix.EINTR {
			break
		}
		if !ready {
			continue
		}
		count, err := unix.Read(fd, buffer)
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			break
		}
		if count > 0 {
			received = append(received, buffer[:count]...)
		}
	}
	return Parse(received)
}

// Parse extracts color replies from terminal output.
func Parse(received []byte) Colors {
	colors := Colors{Palette: make([]kittheme.Color, 8)}
	for _, match := range colorReply.FindAllSubmatch(received, -1) {
		color := kittheme.Color{R: component(match[3]), G: component(match[4]), B: component(match[5]), A: 255}
		switch {
		case bytes.Equal(match[1], []byte("10")):
			colors.Foreground = color
		case bytes.Equal(match[1], []byte("11")):
			colors.Background = color
		default:
			index, _ := strconv.Atoi(string(match[2]))
			colors.Palette[index] = color
		}
	}
	return colors
}

// component scales a 1–4 digit hexadecimal color component to eight bits.
func component(digits []byte) uint8 {
	value, _ := strconv.ParseUint(string(digits), 16, 32)
	maximum := uint64(1)<<(4*len(digits)) - 1
	return uint8((value*255 + maximum/2) / maximum)
}
