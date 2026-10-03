package kit

import (
	"bytes"
	"os"
	"testing"

	"github.com/akonwi/kit/api/internal/aliasgen"
)

func TestContractAliasesAreCurrent(t *testing.T) {
	generated, err := aliasgen.Generate("contract")
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("contract_aliases.generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, generated) {
		t.Fatal("contract aliases are stale; run go generate ./api")
	}
}
