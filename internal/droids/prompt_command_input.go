package droids

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPromptCommandNameBytes      = 128
	maxPromptCommandArgumentsBytes = 128 << 10
	maxPromptCommandTextBytes      = 1 << 20
)

var promptCommandSource = regexp.MustCompile(`^[a-z][a-z_]{0,63}$`)

// wirePromptCommand is the durable invocation record of a prompt command block.
type wirePromptCommand struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Source    string `json:"source"`
}

func promptCommandToWire(input PromptCommandInput) *wirePromptCommand {
	return &wirePromptCommand{Name: input.Name, Arguments: input.Arguments, Source: input.Source}
}

func promptCommandFromWire(text string, wire *wirePromptCommand) (PromptCommandInput, error) {
	if wire == nil {
		return PromptCommandInput{}, fmt.Errorf("droids: prompt command input is missing its invocation")
	}
	value := PromptCommandInput{Name: wire.Name, Arguments: wire.Arguments, Source: wire.Source, Text: text}
	return value, validatePromptCommandInput(value)
}

// validatePromptCommandInput checks a prompt command block's invocation and
// expansion. The expansion must be non-blank; the name is one visible token.
func validatePromptCommandInput(input PromptCommandInput) error {
	if input.Name == "" || len(input.Name) > maxPromptCommandNameBytes || !utf8.ValidString(input.Name) ||
		strings.ContainsFunc(input.Name, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '/' || r == '\\'
		}) {
		return fmt.Errorf("droids: prompt command name is invalid")
	}
	if len(input.Arguments) > maxPromptCommandArgumentsBytes || !utf8.ValidString(input.Arguments) || strings.IndexByte(input.Arguments, 0) >= 0 {
		return fmt.Errorf("droids: prompt command arguments are invalid")
	}
	if !promptCommandSource.MatchString(input.Source) {
		return fmt.Errorf("droids: prompt command source is invalid")
	}
	if strings.TrimSpace(input.Text) == "" || len(input.Text) > maxPromptCommandTextBytes || !utf8.ValidString(input.Text) || strings.IndexByte(input.Text, 0) >= 0 {
		return fmt.Errorf("droids: prompt command text is invalid")
	}
	return nil
}
