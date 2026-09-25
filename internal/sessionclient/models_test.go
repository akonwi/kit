package sessionclient

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestResolveAvailableModel(t *testing.T) {
	t.Parallel()
	catalog := protocol.ModelCatalog{Models: []protocol.ModelCapability{
		{ID: "anthropic/current", Provider: "anthropic", Available: false},
		{ID: "opencode-go/current", Provider: "opencode-go", Available: true},
		{ID: "test/first", Provider: "test", Available: true},
	}}
	for _, test := range []struct {
		name      string
		preferred string
		explicit  bool
		want      string
		wantErr   bool
	}{
		{name: "exact", preferred: "test/first", want: "test/first"},
		{name: "same provider fallback", preferred: "opencode-go/deprecated", want: "opencode-go/current"},
		{name: "first available fallback", want: "opencode-go/current"},
		{name: "explicit unavailable", preferred: "opencode-go/deprecated", explicit: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveAvailableModel(catalog, test.preferred, test.explicit)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("ResolveAvailableModel() = %q, %v; want %q, error=%t", got, err, test.want, test.wantErr)
			}
		})
	}
}
