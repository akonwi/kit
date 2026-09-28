package main

import (
	"fmt"
	"os"

	kitopenapi "github.com/akonwi/kit/internal/httpapi/openapi"
)

func main() {
	output := "api/kit-session.openapi.json"
	if len(os.Args) > 1 {
		output = os.Args[1]
	}
	document, err := kitopenapi.Emit()
	if err == nil {
		err = os.WriteFile(output, document, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
