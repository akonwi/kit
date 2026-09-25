package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// decodeToolJSON preserves numbers and rejects ambiguous duplicate object keys.
func decodeToolJSON(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 64 || nodes > 32768 {
			return nil, fmt.Errorf("JSON nesting or collection limit exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			if number, ok := token.(json.Number); ok {
				text := string(number)
				if len(text) > 1024 {
					return nil, fmt.Errorf("JSON number exceeds precision budget")
				}
				if exponent := strings.IndexAny(text, "eE"); exponent >= 0 {
					value, err := strconv.Atoi(text[exponent+1:])
					if err != nil || value < -1024 || value > 1024 {
						return nil, fmt.Errorf("JSON exponent exceeds precision budget")
					}
				}
			}
			return token, nil
		}
		switch delim {
		case '{':
			object := map[string]any{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate object key")
				}
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return nil, fmt.Errorf("invalid object")
			}
			return object, nil
		case '[':
			array := []any{}
			for decoder.More() {
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return nil, fmt.Errorf("invalid array")
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter")
		}
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return value, nil
}

func compilePluginToolSchema(raw json.RawMessage) (*validator.Schema, error) {
	value, err := decodeToolJSON(raw)
	if err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok || root["type"] != "object" {
		return nil, fmt.Errorf("tool schema root must have type object")
	}
	nodes := 0
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		nodes++
		if nodes > 1024 || depth > 16 {
			return fmt.Errorf("schema complexity limit exceeded")
		}
		schema, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("schema must be an object")
		}
		for key, value := range schema {
			switch key {
			case "$schema":
				if value != "https://json-schema.org/draft/2020-12/schema" {
					return fmt.Errorf("unsupported schema dialect")
				}
			case "type":
				switch value {
				case "object", "array", "string", "number", "integer", "boolean", "null":
				default:
					return fmt.Errorf("unsupported schema type")
				}
			case "description", "required", "additionalProperties", "enum", "const", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "pattern", "minItems", "maxItems":
				if key == "additionalProperties" {
					if _, ok := value.(bool); !ok {
						return fmt.Errorf("additionalProperties must be boolean")
					}
				}
			case "properties":
				properties, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("properties must be an object")
				}
				for _, child := range properties {
					if err := visit(child, depth+1); err != nil {
						return err
					}
				}
			case "items":
				if err := visit(value, depth+1); err != nil {
					return err
				}
			case "anyOf":
				choices, ok := value.([]any)
				if !ok {
					return fmt.Errorf("anyOf must be an array")
				}
				for _, child := range choices {
					if err := visit(child, depth+1); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("unsupported schema keyword %q", key)
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return nil, err
	}
	compiler := validator.NewCompiler()
	const location = "urn:kit:plugin-tool"
	if err := compiler.AddResource(location, root); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}
