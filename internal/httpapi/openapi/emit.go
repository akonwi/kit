// Package openapi emits the published session OpenAPI contract.
package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/version"
	"github.com/invopop/jsonschema"
)

type enumValues interface{ EnumValues() []string }

// Emit returns the deterministic OpenAPI 3.1 session contract.
func Emit() ([]byte, error) {
	components := map[string]any{}
	descriptors := httpapi.Catalog()
	for _, descriptor := range descriptors {
		for _, typ := range []reflect.Type{descriptor.Input, descriptor.Output} {
			if typ == reflect.TypeOf(httpapi.NoBody{}) {
				continue
			}
			if _, err := addTypeSchema(components, typ); err != nil {
				return nil, err
			}
		}
		for _, response := range descriptor.Errors {
			for _, typ := range response.Bodies {
				if _, err := addTypeSchema(components, typ); err != nil {
					return nil, err
				}
			}
		}
	}
	paths := map[string]any{}
	for _, descriptor := range descriptors {
		operation := map[string]any{
			"operationId": descriptor.ID, "tags": []string{descriptor.Tag},
			"parameters": operationParameters(descriptor), "responses": operationResponses(descriptor),
			"security": []any{map[string]any{"daemonBearer": []any{}}},
		}
		if descriptor.Input != reflect.TypeOf(httpapi.NoBody{}) {
			operation["requestBody"] = map[string]any{"required": true, "content": jsonContent(schemaRef(descriptor.Input))}
		}
		pathItem, _ := paths[descriptor.Path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[descriptor.Path] = pathItem
		}
		pathItem[strings.ToLower(descriptor.Method)] = operation
	}
	document := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Kit Session API", "version": fmt.Sprint(version.SessionProtocolVersion)},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{"daemonBearer": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas":         components,
		},
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func operationParameters(descriptor httpapi.Descriptor) []any {
	parameters := []any{
		map[string]any{"name": httpapi.InstanceHeader, "in": "header", "required": true, "schema": map[string]any{"type": "string"}},
		map[string]any{"name": httpapi.ProtocolHeader, "in": "header", "required": true, "schema": map[string]any{"type": "integer", "enum": []int{version.SessionProtocolVersion}}},
	}
	typ := descriptor.Params
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Tag.Get("path"); name != "" {
			parameters = append(parameters, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
	}
	return parameters
}

func operationResponses(descriptor httpapi.Descriptor) map[string]any {
	responses := map[string]any{fmt.Sprint(descriptor.Success): map[string]any{"description": "Success", "content": jsonContent(schemaRef(descriptor.Output))}}
	for _, response := range descriptor.Errors {
		variants := make([]any, 0, len(response.Bodies)+len(response.ScratchpadCodes))
		for _, typ := range response.Bodies {
			if schemaName(typ) == "ScratchpadTypedErrorEnvelope" && len(response.ScratchpadCodes) > 0 {
				for _, code := range response.ScratchpadCodes {
					variants = append(variants, scratchpadErrorSchema(code))
				}
				continue
			}
			variants = append(variants, schemaRef(typ))
		}
		var schema any = variants[0]
		if len(variants) > 1 {
			schema = map[string]any{"oneOf": variants}
		}
		responses[fmt.Sprint(response.Status)] = map[string]any{"description": http.StatusText(response.Status), "content": jsonContent(schema)}
	}
	return responses
}

func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

func scratchpadErrorSchema(code protocol.ScratchpadErrorCode) map[string]any {
	message := map[protocol.ScratchpadErrorCode]string{
		protocol.ScratchpadInvalidContent: "scratchpad content is invalid", protocol.ScratchpadTooLarge: "scratchpad content is too large",
		protocol.ScratchpadRevisionConflict: "scratchpad revision conflict", protocol.ScratchpadRevisionExhausted: "scratchpad revision is exhausted",
		protocol.ScratchpadMigrationRequired: "scratchpad migration is required", protocol.ScratchpadUnsupported: "scratchpad is unsupported for this session",
		protocol.ScratchpadUnavailable: "scratchpad is unavailable",
	}[code]
	details := map[string]any{"type": "object", "additionalProperties": false}
	if code == protocol.ScratchpadRevisionConflict {
		details["properties"] = map[string]any{"scratchpad": map[string]any{"$ref": "#/components/schemas/Scratchpad"}}
		details["required"] = []string{"scratchpad"}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"error"}, "properties": map[string]any{
		"error": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"code", "message", "details"}, "properties": map[string]any{
			"code": map[string]any{"type": "string", "enum": []string{string(code)}}, "message": map[string]any{"type": "string", "enum": []string{message}}, "details": details,
		}},
	}}
}
func schemaRef(typ reflect.Type) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + schemaName(typ)}
}
func schemaName(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Name()
}

func addTypeSchema(components map[string]any, typ reflect.Type) (string, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	name := schemaName(typ)
	if _, exists := components[name]; exists {
		return name, nil
	}
	reflector := jsonschema.Reflector{}
	reflected := reflector.ReflectFromType(typ)
	encoded, err := json.Marshal(reflected)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(encoded, &root); err != nil {
		return "", err
	}
	definitions, _ := root["$defs"].(map[string]any)
	for definitionName, definition := range definitions {
		components[definitionName] = definition
	}
	var schema map[string]any
	if ref, ok := root["$ref"].(string); ok {
		definitionName := strings.TrimPrefix(ref, "#/$defs/")
		schema, _ = components[definitionName].(map[string]any)
		if definitionName != name {
			components[name] = schema
		}
	} else {
		schema = root
		components[name] = schema
	}
	for _, component := range components {
		rewriteRefs(component)
	}
	patchSchema(components, schema, typ)
	return name, nil
}

func rewriteRefs(value any) {
	switch value := value.(type) {
	case map[string]any:
		if ref, ok := value["$ref"].(string); ok {
			value["$ref"] = strings.Replace(ref, "#/$defs/", "#/components/schemas/", 1)
		}
		delete(value, "$schema")
		delete(value, "$id")
		delete(value, "$defs")
		delete(value, "not")
		delete(value, "allOf")
		for _, child := range value {
			rewriteRefs(child)
		}
	case []any:
		for _, child := range value {
			rewriteRefs(child)
		}
	}
}

func patchSchema(components map[string]any, schema map[string]any, typ reflect.Type) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		for key := range schema {
			delete(schema, key)
		}
		return
	}
	if typ.Kind() == reflect.Int64 {
		schema["format"] = "int64"
	}
	if typ.Kind() == reflect.Uint64 {
		schema["format"], schema["minimum"] = "uint64", 0
	}
	if typ.Kind() == reflect.Uint32 {
		schema["format"], schema["minimum"] = "uint32", 0
	}
	if enum, ok := reflect.New(typ).Elem().Interface().(enumValues); ok {
		values := enum.EnumValues()
		schema["enum"] = values
	}
	if typ.PkgPath() == "github.com/akonwi/kit/internal/protocol" && typ.Name() == "ScratchpadRevision" {
		for key := range schema {
			delete(schema, key)
		}
		schema["type"] = "string"
		schema["pattern"] = `^[1-9][0-9]*$`
		return
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	properties, _ := schema["properties"].(map[string]any)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == "" {
			jsonName = field.Name
		}
		if jsonName == "-" {
			continue
		}
		if field.Type == reflect.TypeOf(json.RawMessage{}) {
			properties[jsonName] = map[string]any{}
			continue
		}
		property, _ := properties[jsonName].(map[string]any)
		if property == nil {
			continue
		}
		target := property
		if ref, ok := property["$ref"].(string); ok {
			target, _ = components[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
		}
		if target != nil {
			patchSchema(components, target, field.Type)
		}
		base := field.Type
		for base.Kind() == reflect.Pointer {
			base = base.Elem()
		}
		if (base.Kind() == reflect.Slice || base.Kind() == reflect.Map) && options != "omitempty" {
			if value, ok := property["type"].(string); ok {
				property["type"] = []any{value, "null"}
			}
		}
	}
}

// SchemaForTesting emits one post-processed component schema for focused tests.
func SchemaForTesting(typ reflect.Type) (map[string]any, error) {
	components := map[string]any{}
	name, err := addTypeSchema(components, typ)
	if err != nil {
		return nil, err
	}
	schema, _ := components[name].(map[string]any)
	return schema, nil
}
