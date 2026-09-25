package protocol

import (
	"strings"
	"testing"
)

func TestPluginCommandValidation(t *testing.T) {
	command := PluginCommand{ID: "plugin-demo.echo", LocalID: "echo", PluginID: "plugin-demo", Instance: "pluginhost_abc:1", Description: "Echo a message", ArgName: "message"}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	input := PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: "literal $(value)\nnext"}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PluginCommand){func(c *PluginCommand) { c.ID = "other.ignore" }, func(c *PluginCommand) { c.Description = "\x1b" }, func(c *PluginCommand) { c.Instance = "" }, func(c *PluginCommand) { c.LocalID = "Bad" }, func(c *PluginCommand) { c.Category = strings.Repeat("x", 129) }} {
		next := command
		mutate(&next)
		if err := next.Validate(); err == nil {
			t.Fatalf("accepted invalid metadata %#v", next)
		}
	}
	for _, mutate := range []func(*PluginCommandInput){func(c *PluginCommandInput) { c.ID = "ignore" }, func(c *PluginCommandInput) { c.Instance = "\x1b" }, func(c *PluginCommandInput) { c.Args = strings.Repeat("x", 65537) }, func(c *PluginCommandInput) { c.Args = "\x00" }} {
		next := input
		mutate(&next)
		if err := next.Validate(); err == nil {
			t.Fatal("accepted invalid selection")
		}
	}
}
