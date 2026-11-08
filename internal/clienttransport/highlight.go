package clienttransport

import (
	"context"
	"fmt"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
)

// HighlightCode returns the server's syntax highlighting of a batch of code
// blocks, one result per block in order.
func (c *Client) HighlightCode(ctx context.Context, input protocol.HighlightCodeInput) (protocol.HighlightCodeResult, error) {
	if err := input.Validate(); err != nil {
		return protocol.HighlightCodeResult{}, fmt.Errorf("validate highlight request: %w", err)
	}
	output, err := httpapi.Call(ctx, c, httpapi.HighlightCode, httpapi.ServerPath{}, input)
	if err != nil {
		return protocol.HighlightCodeResult{}, err
	}
	if err := output.Validate(input); err != nil {
		return protocol.HighlightCodeResult{}, protocolErrorf("validate daemon highlight result: %w", err)
	}
	return output, nil
}
