package pluginprotocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// schemaDefiner is a type that describes its own JSON Schema.
type schemaDefiner interface{ JSONSchema() map[string]any }

// enumerated is a string type with a closed set of values.
type enumerated interface{ EnumValues() []string }

var rawMessageType = reflect.TypeFor[json.RawMessage]()

// namedDefinitions are the definitions a field may reference by name with
// the def= schema tag option, or a definition may reference by $ref.
var namedDefinitions = map[string]reflect.Type{
	"RequestID":      reflect.TypeFor[RequestID](),
	"PluginID":       reflect.TypeFor[PluginID](),
	"LocalID":        reflect.TypeFor[LocalID](),
	"ToolID":         reflect.TypeFor[ToolID](),
	"CanonicalID":    reflect.TypeFor[CanonicalID](),
	"ModelToolName":  reflect.TypeFor[ModelToolName](),
	"FooterTarget":   reflect.TypeFor[FooterTarget](),
	"IdempotencyKey": reflect.TypeFor[IdempotencyKey](),
	"ToolSchemaNode": reflect.TypeFor[ToolSchemaNode](),
	"ErrorData":      reflect.TypeFor[ErrorData](),
}

type generator struct {
	defs   map[string]any
	types  map[string]reflect.Type
	closed map[reflect.Type]bool
}

func newGenerator() *generator {
	return &generator{defs: map[string]any{}, types: map[string]reflect.Type{}, closed: map[reflect.Type]bool{}}
}

func ref(name string) map[string]any { return map[string]any{"$ref": "#/$defs/" + name} }

// markClosed records every struct reachable from t as a closed object.
func (g *generator) markClosed(t reflect.Type) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || g.closed[t] {
		return
	}
	g.closed[t] = true
	for i := 0; i < t.NumField(); i++ {
		if field := t.Field(i); field.IsExported() {
			g.markClosed(field.Type)
		}
	}
}

// openCopy defines Kit<Name>, an open copy of the flat object definition of t,
// for the messages Kit sends of a method both sides send.
func (g *generator) openCopy(t reflect.Type) (map[string]any, error) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i).Type
		for field.Kind() == reflect.Pointer || field.Kind() == reflect.Slice {
			field = field.Elem()
		}
		if field.Kind() == reflect.Struct {
			return nil, fmt.Errorf("params %s of a bidirectional method must not nest objects", t)
		}
	}
	name := "Kit" + t.Name()
	if _, ok := g.defs[name]; !ok {
		schema := cloneMap(g.defs[t.Name()].(map[string]any))
		delete(schema, "additionalProperties")
		g.defs[name] = schema
	}
	return ref(name), nil
}

// define ensures a definition for the named type t and returns a reference.
func (g *generator) define(t reflect.Type) (map[string]any, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" || t == rawMessageType {
		return nil, fmt.Errorf("type %s cannot be defined by name", t)
	}
	if existing, ok := g.types[name]; ok {
		if existing != t {
			return nil, fmt.Errorf("definition %s is claimed by %s and %s", name, existing, t)
		}
		return ref(name), nil
	}
	g.types[name] = t
	g.defs[name] = map[string]any{} // Reserve the name for recursive types.
	schema, err := g.body(t)
	if err != nil {
		return nil, err
	}
	g.defs[name] = schema
	return ref(name), nil
}

// body returns the schema of t itself, never a reference to t.
func (g *generator) body(t reflect.Type) (map[string]any, error) {
	if definer, ok := reflect.Zero(t).Interface().(schemaDefiner); ok {
		return cloneMap(definer.JSONSchema()), nil
	}
	if enum, ok := reflect.Zero(t).Interface().(enumerated); ok {
		return map[string]any{"type": "string", "enum": enum.EnumValues()}, nil
	}
	if t.Kind() == reflect.Struct {
		return g.object(t)
	}
	return g.inline(t)
}

// schemaOf returns a reference for named types and an inline schema otherwise.
func (g *generator) schemaOf(t reflect.Type) (map[string]any, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawMessageType {
		return map[string]any{}, nil
	}
	if t.Name() != "" && t.PkgPath() != "" {
		return g.define(t)
	}
	return g.inline(t)
}

func (g *generator) inline(t reflect.Type) (map[string]any, error) {
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Slice:
		items, err := g.schemaOf(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	}
	return nil, fmt.Errorf("unsupported schema type %s", t)
}

type fieldOptions struct {
	nullable, required, object bool
	def                        string
	constraints                map[string]string
}

func parseSchemaTag(tag string) fieldOptions {
	options := fieldOptions{constraints: map[string]string{}}
	for _, part := range strings.Split(tag, ",") {
		key, value, hasValue := strings.Cut(part, "=")
		switch {
		case key == "":
		case key == "nullable" && !hasValue:
			options.nullable = true
		case key == "required" && !hasValue:
			options.required = true
		case key == "object" && !hasValue:
			options.object = true
		case key == "def":
			options.def = value
		default:
			options.constraints[key] = value
		}
	}
	return options
}

func (g *generator) object(t reflect.Type) (map[string]any, error) {
	properties := map[string]any{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, flags, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("%s.%s needs a json name", t.Name(), field.Name)
		}
		optional := strings.Contains(","+flags+",", ",omitempty,")
		options := parseSchemaTag(field.Tag.Get("schema"))
		if field.Type.Kind() == reflect.Pointer && !optional && !options.nullable {
			return nil, fmt.Errorf("%s.%s: a required pointer must be nullable", t.Name(), field.Name)
		}
		var schema map[string]any
		var err error
		switch {
		case options.def != "":
			named, ok := namedDefinitions[options.def]
			if !ok {
				return nil, fmt.Errorf("%s.%s: unknown definition %s", t.Name(), field.Name, options.def)
			}
			schema, err = g.define(named)
		case options.object:
			schema = map[string]any{"type": "object"}
		default:
			schema, err = g.schemaOf(field.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		schema = cloneMap(schema)
		for key, value := range options.constraints {
			switch key {
			case "const":
				if base := baseKind(field.Type); base == reflect.String {
					schema = map[string]any{"const": value}
				} else {
					number, err := strconv.Atoi(value)
					if err != nil {
						return nil, fmt.Errorf("%s.%s: const %q is not an integer", t.Name(), field.Name, value)
					}
					schema = map[string]any{"const": number}
				}
			case "format":
				schema["format"] = value
			case "text", "itemText":
				pattern, err := textPattern(value)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
				}
				target(schema, key)["pattern"] = pattern
			case "url":
				schema["pattern"] = urlPattern()
			case "minLength", "maxLength", "minItems", "maxItems", "minimum", "maximum", "itemMaxLength":
				number, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %s %q is not an integer", t.Name(), field.Name, key, value)
				}
				name := key
				if key == "itemMaxLength" {
					name = "maxLength"
				}
				target(schema, key)[name] = number
			default:
				return nil, fmt.Errorf("%s.%s: unknown schema option %s", t.Name(), field.Name, key)
			}
		}
		if options.nullable {
			if typ, ok := schema["type"].(string); ok {
				schema["type"] = []string{typ, "null"}
			} else {
				schema = map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
			}
		}
		if doc := field.Tag.Get("doc"); doc != "" {
			schema["description"] = doc
		}
		properties[name] = schema
		if !optional || options.required {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	if g.closed[t] {
		schema["additionalProperties"] = false
	}
	return schema, nil
}

// target returns the schema a constraint applies to: an array's items for
// options beginning with item, and the field's own schema otherwise.
func target(schema map[string]any, option string) map[string]any {
	if strings.HasPrefix(option, "item") {
		if items, ok := schema["items"].(map[string]any); ok {
			return items
		}
	}
	return schema
}

func baseKind(t reflect.Type) reflect.Kind {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind()
}

// resolveReferences defines every named definition a schema references.
func (g *generator) resolveReferences() error {
	for {
		missing := map[string]bool{}
		for _, schema := range g.defs {
			collectReferences(schema, missing)
		}
		added := false
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, ok := g.defs[name]; ok {
				continue
			}
			named, ok := namedDefinitions[name]
			if !ok {
				return fmt.Errorf("reference to unknown definition %s", name)
			}
			if _, err := g.define(named); err != nil {
				return err
			}
			added = true
		}
		if !added {
			return nil
		}
	}
}

func collectReferences(value any, into map[string]bool) {
	switch value := value.(type) {
	case map[string]any:
		if target, ok := value["$ref"].(string); ok {
			into[strings.TrimPrefix(target, "#/$defs/")] = true
		}
		for _, child := range value {
			collectReferences(child, into)
		}
	case []any:
		for _, child := range value {
			collectReferences(child, into)
		}
	}
}

func cloneMap(value map[string]any) map[string]any {
	cloned, _ := cloneValue(value).(map[string]any)
	return cloned
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			result[key] = cloneValue(child)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, child := range value {
			result[index] = cloneValue(child)
		}
		return result
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func encodeSchema(document map[string]any) ([]byte, error) {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(escapeInvisible(encoded), '\n'), nil
}

// Schema returns the version 2 protocol schema: every message either side
// may send, each method's params and result, and the error vocabulary.
func Schema() ([]byte, error) {
	g := newGenerator()
	methods := Methods()
	for _, method := range methods {
		if method.Params != nil && method.Sends(PluginToKit) {
			g.markClosed(method.Params)
		}
		if method.Result != nil && method.Direction == KitToPlugin {
			g.markClosed(method.Result)
		}
	}
	// envelope describes method's message. Kit's copy of a bidirectional
	// method's message uses an open params definition, so Kit-sent objects stay
	// open while plugin-sent ones are closed.
	envelope := func(method Method, kitCopy bool) (map[string]any, error) {
		properties := map[string]any{
			"jsonrpc": map[string]any{"const": JSONRPCVersion},
			"method":  map[string]any{"const": method.Name},
		}
		required := []string{"jsonrpc"}
		if !method.Notification {
			properties["id"] = ref("RequestID")
			required = append(required, "id")
		}
		required = append(required, "method")
		if method.Params != nil {
			params, err := g.define(method.Params)
			if err != nil {
				return nil, err
			}
			if kitCopy {
				if params, err = g.openCopy(method.Params); err != nil {
					return nil, err
				}
			}
			properties["params"] = params
			required = append(required, "params")
		}
		return map[string]any{"type": "object", "required": required, "properties": properties, "additionalProperties": false,
			"description": method.Summary}, nil
	}
	groups := map[string][]any{}
	catalog := map[string]any{}
	for _, method := range methods {
		suffix := "Request"
		if method.Notification {
			suffix = "Notification"
		}
		name := method.Title + suffix
		schema, err := envelope(method, false)
		if err != nil {
			return nil, fmt.Errorf("method %s: %w", method.Name, err)
		}
		g.defs[name] = schema
		entry := map[string]any{"title": method.Title, "direction": string(method.Direction), "notification": method.Notification,
			"message": "#/$defs/" + name, "summary": method.Summary}
		if method.Params != nil {
			entry["params"] = "#/$defs/" + method.Params.Name()
		}
		kitName := name
		if method.Direction == Both {
			kitName = "Kit" + name
			schema, err := envelope(method, true)
			if err != nil {
				return nil, fmt.Errorf("method %s: %w", method.Name, err)
			}
			g.defs[kitName] = schema
			entry["kitMessage"] = "#/$defs/" + kitName
			if method.Params != nil {
				entry["kitParams"] = "#/$defs/Kit" + method.Params.Name()
			}
		}
		if method.Result != nil {
			if _, err := g.define(method.Result); err != nil {
				return nil, fmt.Errorf("method %s result: %w", method.Name, err)
			}
			entry["result"] = "#/$defs/" + method.Result.Name()
			codes := make([]int, 0, len(method.Errors))
			for _, code := range method.Errors {
				codes = append(codes, int(code))
			}
			entry["errors"] = codes
		}
		catalog[method.Name] = entry
		for _, direction := range []Direction{PluginToKit, KitToPlugin} {
			if !method.Sends(direction) {
				continue
			}
			sender, message := "Plugin", name
			if direction == KitToPlugin {
				sender, message = "Kit", kitName
			}
			groups[sender+suffix] = append(groups[sender+suffix], ref(message))
		}
	}
	for name, members := range groups {
		g.defs[name] = map[string]any{"anyOf": members}
	}
	g.defs["ErrorObject"] = map[string]any{"type": "object", "required": []string{"code", "message"}, "additionalProperties": false,
		"properties": map[string]any{
			"code":    map[string]any{"type": "integer"},
			"message": map[string]any{"type": "string", "description": "Concise and safe to display."},
			"data":    map[string]any{"description": "Structured details. Errors from Kit use ErrorData."},
		}}
	g.defs["SuccessResponse"] = map[string]any{"type": "object", "required": []string{"jsonrpc", "id", "result"}, "additionalProperties": false,
		"properties": map[string]any{"jsonrpc": map[string]any{"const": JSONRPCVersion}, "id": ref("RequestID"),
			"result": map[string]any{"description": "The result definition of the request's method."}}}
	g.defs["ErrorResponse"] = map[string]any{"type": "object", "required": []string{"jsonrpc", "id", "error"}, "additionalProperties": false,
		"properties": map[string]any{"jsonrpc": map[string]any{"const": JSONRPCVersion},
			"id":    map[string]any{"anyOf": []any{ref("RequestID"), map[string]any{"type": "null"}}},
			"error": ref("ErrorObject")}}
	g.defs["PluginMessage"] = map[string]any{"description": "A line a plugin writes to stdout.",
		"anyOf": []any{ref("PluginRequest"), ref("PluginNotification"), ref("SuccessResponse"), ref("ErrorResponse")}}
	g.defs["KitMessage"] = map[string]any{"description": "A line Kit writes to a plugin's stdin.",
		"anyOf": []any{ref("KitRequest"), ref("KitNotification"), ref("SuccessResponse"), ref("ErrorResponse")}}
	if _, err := g.define(reflect.TypeFor[ErrorData]()); err != nil {
		return nil, err
	}
	if err := g.resolveReferences(); err != nil {
		return nil, err
	}
	errors := []any{}
	for _, code := range ErrorCodes() {
		errors = append(errors, map[string]any{"code": int(code.Code), "meaning": code.Meaning})
	}
	return encodeSchema(map[string]any{
		"$schema":       "https://json-schema.org/draft/2020-12/schema",
		"$id":           SchemaURL,
		"title":         "Kit plugin protocol 2",
		"description":   "JSON-RPC 2.0 messages of Kit plugin protocol version 2, one per line on the plugin's stdin and stdout. Generated from github.com/akonwi/kit/api/pluginprotocol; see docs/plugin-protocol/v2.md.",
		"anyOf":         []any{ref("KitMessage"), ref("PluginMessage")},
		"$defs":         g.defs,
		"x-kit-methods": catalog,
		"x-kit-errors":  errors,
	})
}

// ManifestSchema returns the schema of a manifest that declares version 2.
func ManifestSchema() ([]byte, error) {
	g := newGenerator()
	manifest := reflect.TypeFor[Manifest]()
	g.markClosed(manifest)
	root, err := g.object(manifest)
	if err != nil {
		return nil, err
	}
	if err := g.resolveReferences(); err != nil {
		return nil, err
	}
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = ManifestSchemaURL
	root["title"] = "Kit plugin manifest for protocol 2"
	root["description"] = "plugin.json: how Kit launches a plugin that speaks plugin protocol version 2. Generated from github.com/akonwi/kit/api/pluginprotocol."
	root["$defs"] = g.defs
	return encodeSchema(root)
}
