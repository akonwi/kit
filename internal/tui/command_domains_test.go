package tui

import (
	"slices"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestBuiltinPaletteCommandsHaveReservedProtocolDomains(t *testing.T) {
	domains := protocol.ReservedCommandDomains()
	for _, command := range paletteCommands() {
		if !slices.Contains(domains, command.Name) {
			t.Errorf("builtin %q needs a reserved protocol domain", command.Name)
		}
	}
}
