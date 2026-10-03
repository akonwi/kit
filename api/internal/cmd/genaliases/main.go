package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/akonwi/kit/api/internal/aliasgen"
)

func main() {
	contractDir := flag.String("contract", "./contract", "contract package directory")
	outputPath := flag.String("output", "./contract_aliases.generated.go", "generated output path")
	flag.Parse()

	output, err := aliasgen.Generate(*contractDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outputPath, output, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write aliases: %v\n", err)
		os.Exit(1)
	}
}
