// Command modelcatalog refreshes Droids' embedded OpenAI and Anthropic
// models.dev snapshot. Run from the module root with go generate ./...
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const catalogURL = "https://models.dev/api.json"

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(catalogURL)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("models.dev returned HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
	if err != nil {
		fatal(err)
	}
	if len(data) > 20<<20 {
		fatal(fmt.Errorf("models.dev catalog exceeds 20 MiB"))
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(data, &catalog); err != nil {
		fatal(err)
	}
	selected := map[string]json.RawMessage{}
	for _, provider := range []string{"anthropic", "openai"} {
		entry, ok := catalog[provider]
		if !ok {
			fatal(fmt.Errorf("models.dev catalog has no %q provider", provider))
		}
		selected[provider] = entry
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile("model_catalog.json", append(encoded, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "modelcatalog:", err)
	os.Exit(1)
}
