package kit

import (
	"context"
	"errors"

	protocol "github.com/akonwi/kit/api/contract"
)

type highlightTransport interface {
	HighlightCode(context.Context, protocol.HighlightCodeInput) (protocol.HighlightCodeResult, error)
}

var errNoHighlightTransport = errors.New("highlight transport is unavailable")

// HighlightCode returns the server's syntax highlighting of a batch of code
// blocks, one result per block in order (ADR 0062). A block the server cannot
// color, because its language is unsupported, it is too large, or the
// highlighter is busy, has no Highlight. A request over the bounds of
// HighlightCodeInput is rejected before it is sent.
func (c *Client) HighlightCode(ctx context.Context, input HighlightCodeInput) (HighlightCodeResult, error) {
	if err := input.Validate(); err != nil {
		return HighlightCodeResult{}, err
	}
	operation, cancel, err := c.operationContext(ctx)
	if err != nil {
		return HighlightCodeResult{}, err
	}
	defer cancel()
	if c.highlights == nil {
		return HighlightCodeResult{}, errNoHighlightTransport
	}
	return projectResult(c.highlights.HighlightCode(operation, input))
}
