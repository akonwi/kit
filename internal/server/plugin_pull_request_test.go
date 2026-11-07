package server

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/pullrequest"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// TestPluginPullRequestChecksConformToThePublicSchema validates the plugin
// Git context projection, at its bounds, against the published protocol schema.
func TestPluginPullRequestChecksConformToThePublicSchema(t *testing.T) {
	const location = "https://kit.invalid/protocol.schema.json"
	raw, err := os.ReadFile("../../apps/web/docs/plugin-protocol/protocol.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := validator.NewCompiler()
	if err := compiler.AddResource(location, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location + "#/$defs/GitChangedParams")
	if err != nil {
		t.Fatal(err)
	}
	bounded := &pullrequest.Status{Number: 42, URL: "https://github.com/a/b/pull/42", RequiredChecks: pullrequest.RequirementPending}
	for range 80 {
		bounded.Checks = append(bounded.Checks, pullrequest.Check{Name: strings.Repeat("é", 200), Workflow: strings.Repeat("w", 300), State: pullrequest.CheckPending, Required: true})
	}
	branch := "main"
	for name, status := range map[string]*pullrequest.Status{
		"no checks": {Number: 7, URL: "https://github.com/a/b/pull/7"},
		"failing":   failingPullRequestStatus(),
		"bounded":   bounded,
	} {
		context := plugin.GitContext{Root: "/repo", Branch: &branch, PullRequest: &plugin.PullRequestContext{Number: status.Number, URL: status.URL, Checks: pluginPullRequestChecks(status)}}
		encoded, err := json.Marshal(struct {
			Git *plugin.GitContext `json:"git"`
		}{&context})
		if err != nil {
			t.Fatal(err)
		}
		value, err := validator.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%s: %s does not match GitChangedParams: %v", name, encoded, err)
		}
	}
	checks := pluginPullRequestChecks(bounded)
	if len(checks.Items) != pullrequest.MaxSummaryChecks || checks.Omitted != 30 || checks.State != "pending" || checks.Required != "pending" {
		t.Fatalf("bounded checks listed %d omitted %d state %s required %s", len(checks.Items), checks.Omitted, checks.State, checks.Required)
	}
}
