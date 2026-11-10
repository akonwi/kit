package acp

import (
	"encoding/json"
	"slices"

	protocol "github.com/akonwi/kit/api/contract"
)

// configOptions describes a session's model and thinking level as ACP select
// options. The model option lists the catalog's available models, grouped
// by provider, and the session's model even when it is unavailable. The
// thinking option is always present, so the list a client holds keeps its
// options when the model changes; a model without thinking levels offers
// only "off".
func configOptions(info protocol.SessionInfo, catalog protocol.ModelCatalog) []configOption {
	var groups []configSelectGroup
	group := func(provider string) *configSelectGroup {
		for index := range groups {
			if groups[index].Group == provider {
				return &groups[index]
			}
		}
		groups = append(groups, configSelectGroup{Group: provider, Name: provider})
		return &groups[len(groups)-1]
	}
	var current *protocol.ModelCapability
	for index, model := range catalog.Models {
		if model.ID == info.Model {
			current = &catalog.Models[index]
		}
		if !model.Available && model.ID != info.Model {
			continue
		}
		name := model.Name
		if name == "" {
			name = model.ID
		}
		target := group(model.Provider)
		target.Options = append(target.Options, configSelectOption{Value: model.ID, Name: name})
	}
	if current == nil && info.Model != "" {
		target := group("current")
		target.Options = append(target.Options, configSelectOption{Value: info.Model, Name: info.Model})
	}
	options := []configOption{{
		ID: configOptionModel, Name: "Model", Category: "model", Type: "select",
		CurrentValue: info.Model, Options: groups,
	}}
	var levels []string
	if current != nil {
		for _, level := range current.ThinkingLevels {
			levels = append(levels, string(level))
		}
	}
	if info.ThinkingLevel != "" && !slices.Contains(levels, info.ThinkingLevel) {
		levels = append(levels, info.ThinkingLevel)
	}
	selected := info.ThinkingLevel
	if len(levels) == 0 {
		levels = []string{string(protocol.ThinkingOff)}
		selected = string(protocol.ThinkingOff)
	}
	values := make([]configSelectOption, 0, len(levels))
	for _, level := range levels {
		values = append(values, configSelectOption{Value: level, Name: thinkingName(level)})
	}
	return append(options, configOption{
		ID: configOptionThinking, Name: "Thinking", Category: "thought_level", Type: "select",
		CurrentValue: selected, Options: values,
	})
}

func thinkingName(level string) string {
	switch protocol.ThinkingLevel(level) {
	case protocol.ThinkingOff:
		return "Off"
	case protocol.ThinkingMinimal:
		return "Minimal"
	case protocol.ThinkingLow:
		return "Low"
	case protocol.ThinkingMedium:
		return "Medium"
	case protocol.ThinkingHigh:
		return "High"
	case protocol.ThinkingXHigh:
		return "Extra high"
	case protocol.ThinkingMax:
		return "Max"
	}
	return level
}

// currentValue returns the option id's current value.
func currentValue(options []configOption, id string) string {
	for _, option := range options {
		if option.ID == id {
			return option.CurrentValue
		}
	}
	return ""
}

// hasOption reports whether options offers value for the option id.
func hasOption(options []configOption, id, value string) bool {
	for _, option := range options {
		if option.ID != id {
			continue
		}
		switch values := option.Options.(type) {
		case []configSelectOption:
			for _, candidate := range values {
				if candidate.Value == value {
					return true
				}
			}
		case []configSelectGroup:
			for _, group := range values {
				for _, candidate := range group.Options {
					if candidate.Value == value {
						return true
					}
				}
			}
		}
	}
	return false
}

// configValue returns the select value a session/set_config_option request
// chooses. Kit offers no boolean options.
func configValue(input setConfigOptionParams) (string, error) {
	if input.ConfigID == "" {
		return "", invalidParams("configId is required")
	}
	if input.ConfigID != configOptionModel && input.ConfigID != configOptionThinking {
		return "", invalidParams("unknown config option %q", input.ConfigID)
	}
	if input.Type != "" && input.Type != "select" {
		return "", invalidParams("config option %q is a select option", input.ConfigID)
	}
	var value string
	if err := json.Unmarshal(input.Value, &value); err != nil || value == "" {
		return "", invalidParams("value must be one of the option's values")
	}
	return value, nil
}

// availableCommands lists the session's prompt commands and compact.
func availableCommands(commands []protocol.PromptCommand) []availableCommand {
	result := make([]availableCommand, 0, len(commands)+1)
	for _, command := range commands {
		description := command.Description
		if description == "" {
			description = "Run /" + command.Name
		}
		entry := availableCommand{Name: command.Name, Description: description}
		if command.ArgumentHint != "" {
			entry.Input = &availableCommandInput{Hint: command.ArgumentHint}
		}
		result = append(result, entry)
	}
	return append(result, availableCommand{Name: compactCommand, Description: "Compact the session's context"})
}
