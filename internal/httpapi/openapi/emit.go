// Package openapi emits the published session OpenAPI contract.
package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/httpapi"
	"github.com/akonwi/kit/internal/version"
	"github.com/invopop/jsonschema"
)

type enumValues interface{ EnumValues() []string }
type unionVariants interface {
	UnionVariants() []protocol.UnionVariant
}

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
			for _, typ := range response.Details {
				if _, err := addTypeSchema(components, typ); err != nil {
					return nil, err
				}
			}
		}
	}
	// jsonschema discovers transcript content through parent structs and leaves
	// an empty interface placeholder. Emit its declared discriminated union.
	if _, err := addTypeSchema(components, reflect.TypeFor[protocol.TranscriptContent]()); err != nil {
		return nil, err
	}
	for name, schema := range components {
		components[name] = normalizeSchema(schema)
	}
	paths := map[string]any{}
	for _, descriptor := range descriptors {
		responses, err := operationResponses(descriptor, components)
		if err != nil {
			return nil, err
		}
		operation := map[string]any{
			"operationId": descriptor.ID, "tags": []string{descriptor.Tag},
			"parameters": operationParameters(descriptor), "responses": responses,
			"security": []any{map[string]any{"daemonBearer": []any{}}},
		}
		if stream := descriptor.Stream; stream != nil {
			operation["x-kit-stream"] = map[string]any{
				"records": stream.Records, "resumable": stream.Resumable, "maxRecordBytes": stream.MaxRecordBytes,
			}
		}
		if descriptor.Input != reflect.TypeOf(httpapi.NoBody{}) {
			mediaType := descriptor.RequestMediaType
			if mediaType == "" {
				mediaType = "application/json"
			}
			operation["requestBody"] = map[string]any{"required": true, "content": mediaContent(mediaType, schemaRef(descriptor.Input))}
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
	}
	if !descriptor.NoProtocol {
		parameters = append(parameters, map[string]any{"name": httpapi.ProtocolHeader, "in": "header", "required": true, "schema": map[string]any{"type": "integer", "enum": []int{version.SessionProtocolVersion}}})
	}
	typ := descriptor.Params
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if name := field.Tag.Get("path"); name != "" {
			parameters = append(parameters, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			continue
		}
		if raw := field.Tag.Get("query"); raw != "" {
			name, options, _ := strings.Cut(raw, ",")
			if name == "" {
				continue
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": !strings.Contains(options, "omitempty"), "schema": parameterSchema(field.Type)})
			continue
		}
		if raw := field.Tag.Get("header"); raw != "" {
			name, options, _ := strings.Cut(raw, ",")
			if name == "" {
				continue
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "header", "required": !strings.Contains(options, "omitempty"), "schema": parameterSchema(field.Type)})
		}
	}
	return parameters
}

func parameterSchema(typ reflect.Type) map[string]any {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice {
		return map[string]any{"type": "array", "items": parameterSchema(typ.Elem())}
	}
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{"type": "string"}
	}
}

func operationResponses(descriptor httpapi.Descriptor, components map[string]any) (map[string]any, error) {
	mediaTypes := append([]string(nil), descriptor.ResponseMediaTypes...)
	if len(mediaTypes) == 0 {
		mediaType := descriptor.ResponseMediaType
		if mediaType == "" {
			mediaType = "application/json"
		}
		mediaTypes = []string{mediaType}
	}
	content := map[string]any{}
	for _, mediaType := range mediaTypes {
		content[mediaType] = map[string]any{"schema": schemaRef(descriptor.Output)}
	}
	success := map[string]any{"description": "Success", "content": content}
	if descriptor.Output == reflect.TypeOf(httpapi.NoBody{}) {
		success = map[string]any{"description": "Success"}
	}
	if descriptor.Stream != nil {
		// Every record's data line decodes as the payload schema (ADR 0035).
		success = map[string]any{"description": "Server-sent event stream; each record's data is one payload object",
			"content": map[string]any{"text/event-stream": map[string]any{"schema": schemaRef(descriptor.Output)}}}
	}
	responses := map[string]any{fmt.Sprint(descriptor.Success): success}
	for _, status := range descriptor.AdditionalSuccess {
		responses[fmt.Sprint(status)] = success
	}
	for _, response := range descriptor.Errors {
		schema, err := errorEnvelopeSchema(descriptor.Tag, response, components)
		if err != nil {
			return nil, fmt.Errorf("operation %s status %d: %w", descriptor.ID, response.Status, err)
		}
		responses[fmt.Sprint(response.Status)] = map[string]any{"description": http.StatusText(response.Status), "content": jsonContent(schema)}
	}
	return responses, nil
}

func jsonContent(schema any) map[string]any { return mediaContent("application/json", schema) }

func mediaContent(mediaType string, schema any) map[string]any {
	return map[string]any{mediaType: map[string]any{"schema": schema}}
}

// errorEnvelopeSchema publishes every error status as a union of shared,
// per-code variant schemas discriminated by code (ADR 0034).
func errorEnvelopeSchema(tag string, response httpapi.ErrorResponse, components map[string]any) (map[string]any, error) {
	variants := make([]any, 0, len(response.Codes))
	mapping := make(map[string]any, len(response.Codes))
	for _, code := range response.Codes {
		details, hasDetails := response.Details[code]
		if hasDetails {
			if _, err := addTypeSchema(components, details); err != nil {
				return nil, err
			}
		}
		name := errorVariantName(tag, code, hasDetails)
		variant := errorVariantSchema(code, details, hasDetails)
		if existing, ok := components[name]; ok {
			if !reflect.DeepEqual(existing, variant) {
				return nil, fmt.Errorf("error variant %s is declared with conflicting schemas", name)
			}
		} else {
			components[name] = variant
		}
		ref := "#/components/schemas/" + name
		variants = append(variants, map[string]any{"$ref": ref})
		mapping[string(code)] = ref
	}
	errorSchema := map[string]any{"oneOf": variants, "discriminator": map[string]any{"propertyName": "code", "mapping": mapping}}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"error"},
		"properties": map[string]any{"error": errorSchema}}, nil
}

func errorVariantSchema(code httpapi.ErrorCode, details reflect.Type, hasDetails bool) map[string]any {
	required := []string{"code", "message"}
	properties := map[string]any{
		"code":    map[string]any{"type": "string", "enum": []string{string(code)}},
		"message": map[string]any{"type": "string", "minLength": 1, "maxLength": 1024},
	}
	if hasDetails {
		required = append(required, "details")
		properties["details"] = schemaRef(details)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

// errorVariantName names generic codes without a prefix and always prefixes
// domain codes with their operation tag unless the code already begins with it.
// A generic code that carries details is a domain use of that code.
func errorVariantName(tag string, code httpapi.ErrorCode, hasDetails bool) string {
	name := pascal(string(code))
	if hasDetails || !httpapi.IsGenericErrorCode(code) {
		if !strings.HasPrefix(string(code), tag+"_") {
			name = pascal(tag) + name
		}
	}
	return name + "Error"
}

func pascal(value string) string {
	var name strings.Builder
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		name.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return name.String()
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

// normalizeSchema replaces boolean JSON Schema nodes with object equivalents.
// Swift OpenAPI Generator accepts only mapping schema nodes, while an empty
// object remains the OpenAPI 3.1 equivalent of the permissive true schema.
func normalizeSchema(value any) any {
	if allowed, ok := value.(bool); ok {
		if allowed {
			return map[string]any{}
		}
		return map[string]any{"not": map[string]any{}}
	}
	schema, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if encoding, ok := schema["contentEncoding"].(string); ok && encoding == "base64" {
		delete(schema, "contentEncoding")
		schema["format"] = "byte"
	}
	if types, ok := schema["type"].([]any); ok {
		var nonNull []any
		nullable := false
		for _, typ := range types {
			if typ == "null" {
				nullable = true
			} else {
				nonNull = append(nonNull, typ)
			}
		}
		if nullable && len(nonNull) == 1 {
			schema["type"], schema["nullable"] = nonNull[0], true
		}
	}
	for _, key := range []string{"properties", "patternProperties", "$defs"} {
		if children, ok := schema[key].(map[string]any); ok {
			for name, child := range children {
				children[name] = normalizeSchema(child)
			}
		}
	}
	for _, key := range []string{"items", "not", "if", "then", "else"} {
		if child, exists := schema[key]; exists {
			schema[key] = normalizeSchema(child)
		}
	}
	if child, ok := schema["additionalProperties"].(map[string]any); ok {
		schema["additionalProperties"] = normalizeSchema(child)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, ok := schema[key].([]any); ok {
			for index, child := range children {
				children[index] = normalizeSchema(child)
			}
		}
	}
	return schema
}

func addTypeSchema(components map[string]any, typ reflect.Type) (string, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if err := rejectNullPointers(typ, map[reflect.Type]bool{}); err != nil {
		return "", err
	}
	name := schemaName(typ)
	// Reflection can add a placeholder definition for a union before it is
	// encountered directly. Replace that placeholder with its declared union
	// schema rather than returning the incomplete reflected object.
	if union, ok := reflect.New(typ).Elem().Interface().(unionVariants); ok {
		return addUnionSchema(components, name, union.UnionVariants())
	}
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
	root = normalizeSchema(root).(map[string]any)
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

func addUnionSchema(components map[string]any, name string, variants []protocol.UnionVariant) (string, error) {
	refs := make([]any, 0, len(variants))
	mapping := make(map[string]any, len(variants))
	for _, variant := range variants {
		typ := reflect.TypeOf(variant.Payload)
		variantName, err := addTypeSchema(components, typ)
		if err != nil {
			return "", err
		}
		schema, _ := components[variantName].(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		if properties == nil {
			properties = map[string]any{}
			schema["properties"] = properties
		}
		properties["kind"] = map[string]any{"type": "string", "enum": []string{variant.Kind}}
		required := requiredValues(schema)
		if !contains(required, "kind") {
			schema["required"] = append(required, "kind")
		}
		ref := "#/components/schemas/" + variantName
		refs = append(refs, map[string]any{"$ref": ref})
		mapping[variant.Kind] = ref
	}
	components[name] = map[string]any{"oneOf": refs, "discriminator": map[string]any{"propertyName": "kind", "mapping": mapping}}
	return name, nil
}

func requiredValues(schema map[string]any) []string {
	if values, ok := schema["required"].([]string); ok {
		return append([]string(nil), values...)
	}
	if values, ok := schema["required"].([]any); ok {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// rejectNullPointers fails emission for a pointer field that would encode a
// nil value as null. Optional contract fields are omitted when absent, never
// null (ADR 0031), so every pointer field must be omitempty or omitzero.
func rejectNullPointers(typ reflect.Type, seen map[reflect.Type]bool) error {
	for {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			typ = typ.Elem()
			continue
		}
		break
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return nil
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == "-" && options == "" {
			continue
		}
		if field.Anonymous && jsonName == "" {
			// Promoted fields; a nil embedded pointer contributes nothing.
			if err := rejectNullPointers(field.Type, seen); err != nil {
				return err
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		if field.Type.Kind() == reflect.Pointer && !hasTagOption(options, "omitempty") && !hasTagOption(options, "omitzero") {
			return fmt.Errorf("%s.%s: pointer field must be omitempty or omitzero; optional contract fields are omitted, never null", typ.Name(), field.Name)
		}
		if err := rejectNullPointers(field.Type, seen); err != nil {
			return err
		}
	}
	return nil
}

func hasTagOption(options, option string) bool {
	for options != "" {
		var current string
		current, options, _ = strings.Cut(options, ",")
		if current == option {
			return true
		}
	}
	return false
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
	if _, ok := schema["required"].([]any); ok {
		schema["required"] = requiredValues(schema)
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		elem := typ.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if union, ok := reflect.New(elem).Elem().Interface().(unionVariants); ok && elem.Name() == "SubagentLiveEvent" {
			name, err := addUnionSchema(components, schemaName(elem), union.UnionVariants())
			if err == nil {
				schema["items"] = map[string]any{"$ref": "#/components/schemas/" + name}
			}
			return
		}
		return
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
	if typ.PkgPath() == "github.com/akonwi/kit/api/contract" && typ.Name() == "ScratchpadRevision" {
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
	if typ.PkgPath() == "github.com/akonwi/kit/api/contract" && typ.Name() == "ScratchpadErrorDetails" {
		schema["required"] = []string{"scratchpad"}
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
			// Collection fields without omitempty have explicit zero values on the
			// wire. Publish them as required, non-null collections rather than
			// allowing an absent or null collection.
			delete(property, "nullable")
			if types, ok := property["type"].([]any); ok {
				nonNull := types[:0]
				for _, value := range types {
					if value != "null" {
						nonNull = append(nonNull, value)
					}
				}
				if len(nonNull) == 1 {
					property["type"] = nonNull[0]
				} else {
					property["type"] = nonNull
				}
			}
			required := requiredValues(schema)
			if !contains(required, jsonName) {
				schema["required"] = append(required, jsonName)
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
