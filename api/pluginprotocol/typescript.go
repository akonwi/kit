package pluginprotocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TypeScriptBegin and TypeScriptEnd mark the region of the TypeScript plugin
// helper, sdk/plugin/kit-plugin.ts, that TypeScript generates.
const (
	TypeScriptBegin = "// BEGIN GENERATED plugin protocol 2 types: go run ./api/pluginprotocol/cmd/emit"
	TypeScriptEnd   = "// END GENERATED plugin protocol 2 types"
)

// TypeScript returns TypeScript declarations of every version 2 record, the
// methods each side sends with their params and results, and the error
// codes, generated from the protocol schema. It is the content between
// TypeScriptBegin and TypeScriptEnd, without the markers.
func TypeScript() (string, error) {
	raw, err := Schema()
	if err != nil {
		return "", err
	}
	var document struct {
		Defs map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", err
	}
	// Message envelopes are not records; method maps below describe them.
	skipped := map[string]bool{"KitMessage": true, "PluginMessage": true, "SuccessResponse": true, "ErrorResponse": true,
		"KitRequest": true, "KitNotification": true, "PluginRequest": true, "PluginNotification": true}
	for _, method := range Methods() {
		envelope := method.Title + "Request"
		if method.Notification {
			envelope = method.Title + "Notification"
		}
		skipped[envelope] = true
		if method.Direction == Both {
			skipped["Kit"+envelope] = true
			if method.Params != nil {
				skipped["Kit"+method.Params.Name()] = true // Kit's open copy has the same type.
			}
		}
	}
	names := make([]string, 0, len(document.Defs))
	for name := range document.Defs {
		if skipped[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	out.WriteString("/** Any JSON value. */\n")
	out.WriteString("export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };\n")
	for _, name := range names {
		schema, _ := document.Defs[name].(map[string]any)
		out.WriteString("\n")
		writeDoc(&out, "", schema)
		if isObject(schema) {
			fmt.Fprintf(&out, "export interface %s %s\n", name, tsObject(schema, ""))
		} else {
			fmt.Fprintf(&out, "export type %s = %s;\n", name, tsType(schema, ""))
		}
	}

	groups := []struct {
		name, doc    string
		direction    Direction
		notification bool
	}{
		{"PluginRequests", "Requests a plugin sends Kit, with their params and results.", PluginToKit, false},
		{"PluginNotifications", "Notifications a plugin sends Kit, with their params.", PluginToKit, true},
		{"KitRequests", "Requests Kit sends a plugin, with their params and results.", KitToPlugin, false},
		{"KitNotifications", "Notifications Kit sends a plugin, with their params.", KitToPlugin, true},
	}
	for _, group := range groups {
		fmt.Fprintf(&out, "\n/** %s */\nexport interface %s {\n", group.doc, group.name)
		for _, method := range Methods() {
			if !method.Sends(group.direction) || method.Notification != group.notification {
				continue
			}
			params := "undefined"
			if method.Params != nil {
				params = method.Params.Name()
			}
			if group.notification {
				fmt.Fprintf(&out, "  /** %s */\n  %q: %s;\n", method.Summary, method.Name, params)
				continue
			}
			fmt.Fprintf(&out, "  /** %s */\n  %q: { params: %s; result: %s };\n", method.Summary, method.Name, params, method.Result.Name())
		}
		out.WriteString("}\n")
	}

	out.WriteString("\n/** Error codes, by name. */\nexport const ErrorCode = {\n")
	for _, code := range ErrorCodes() {
		fmt.Fprintf(&out, "  /** %s */\n  %s: %d,\n", code.Meaning, code.Name, code.Code)
	}
	out.WriteString("} as const;\n")
	return out.String(), nil
}

// ReplaceTypeScriptRegion returns source with the generated region replaced
// by region.
func ReplaceTypeScriptRegion(source, region string) (string, error) {
	begin := strings.Index(source, TypeScriptBegin+"\n")
	end := strings.Index(source, TypeScriptEnd)
	if begin < 0 || end < begin {
		return "", fmt.Errorf("the generated region markers are missing")
	}
	start := begin + len(TypeScriptBegin) + 1
	return source[:start] + region + source[end:], nil
}

func isObject(schema map[string]any) bool {
	_, hasProperties := schema["properties"]
	return schema["type"] == "object" && hasProperties
}

var identifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func writeDoc(out *strings.Builder, indent string, schema map[string]any) {
	if doc, ok := schema["description"].(string); ok && doc != "" {
		fmt.Fprintf(out, "%s/** %s */\n", indent, strings.ReplaceAll(doc, "*/", "* /"))
	}
}

func tsObject(schema map[string]any, indent string) string {
	properties, _ := schema["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := schema["required"].([]any); ok {
		for _, name := range list {
			required[name.(string)] = true
		}
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("{\n")
	inner := indent + "  "
	for _, name := range names {
		property, _ := properties[name].(map[string]any)
		writeDoc(&out, inner, property)
		key := name
		if !identifier.MatchString(name) {
			key = strconv.Quote(name)
		}
		optional := ""
		if !required[name] {
			optional = "?"
		}
		fmt.Fprintf(&out, "%s%s%s: %s;\n", inner, key, optional, tsType(property, inner))
	}
	out.WriteString(indent + "}")
	return out.String()
}

func tsType(schema map[string]any, indent string) string {
	if reference, ok := schema["$ref"].(string); ok {
		return strings.TrimPrefix(reference, "#/$defs/")
	}
	if value, ok := schema["const"]; ok {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	if values, ok := schema["enum"].([]any); ok {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			encoded, _ := json.Marshal(value)
			parts = append(parts, string(encoded))
		}
		return strings.Join(parts, " | ")
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if choices, ok := schema[key].([]any); ok {
			parts := make([]string, 0, len(choices))
			for _, choice := range choices {
				parts = append(parts, tsType(choice.(map[string]any), indent))
			}
			return strings.Join(parts, " | ")
		}
	}
	if types, ok := schema["type"].([]any); ok {
		parts := make([]string, 0, len(types))
		for _, typ := range types {
			copy := map[string]any{}
			for key, value := range schema {
				copy[key] = value
			}
			copy["type"] = typ
			parts = append(parts, tsType(copy, indent))
		}
		return strings.Join(parts, " | ")
	}
	switch schema["type"] {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			element := tsType(items, indent)
			if strings.Contains(element, " ") {
				return "Array<" + element + ">"
			}
			return element + "[]"
		}
		return "JsonValue[]"
	case "object":
		if _, ok := schema["properties"]; ok {
			return tsObject(schema, indent)
		}
		if values, ok := schema["additionalProperties"].(map[string]any); ok {
			return "{ [key: string]: " + tsType(values, indent) + " }"
		}
		return "{ [key: string]: JsonValue }"
	}
	return "JsonValue"
}
