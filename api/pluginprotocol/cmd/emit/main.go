// Command emit writes the generated plugin protocol version 2 schemas, and the
// generated types of the TypeScript plugin helper. Run it from the repository
// root:
//
//	go run ./api/pluginprotocol/cmd/emit
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/akonwi/kit/api/pluginprotocol"
)

func main() {
	directory := "docs/plugin-protocol/v2"
	if len(os.Args) > 1 {
		directory = os.Args[1]
	}
	if err := emit(directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := emitTypeScript("sdk/plugin/kit-plugin.ts"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// emitTypeScript replaces the generated region of the helper at path.
func emitTypeScript(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	region, err := pluginprotocol.TypeScript()
	if err != nil {
		return err
	}
	updated, err := pluginprotocol.ReplaceTypeScriptRegion(string(source), region)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func emit(directory string) error {
	for name, generate := range map[string]func() ([]byte, error){
		"protocol.schema.json": pluginprotocol.Schema,
		"manifest.schema.json": pluginprotocol.ManifestSchema,
	} {
		document, err := generate()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), document, 0o644); err != nil {
			return err
		}
	}
	return nil
}
