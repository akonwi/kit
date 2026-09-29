package httpapi

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// maxPayloadDepth bounds nesting in stream payloads before decoding.
const maxPayloadDepth = 32

// decodeStrictPayload decodes one JSON object into target, rejecting unknown
// fields, duplicate keys at any depth, trailing data, and missing or null
// required fields. Required fields are those the contract marks required:
// struct fields without omitempty; slices and maps may be null.
func decodeStrictPayload(data []byte, target any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	if err := decodeStrictJSONObject(data, target); err != nil {
		return err
	}
	return checkRequiredFields(data, reflect.TypeOf(target).Elem())
}

// rejectDuplicateJSONKeys rejects ambiguous keys in every nested object.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var readValue func(int) error
	readValue = func(depth int) error {
		if depth > maxPayloadDepth {
			return errors.New("payload nesting exceeds limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("invalid object key")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := readValue(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := readValue(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

var (
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// checkRequiredFields walks already strictly decoded JSON against typ.
func checkRequiredFields(data []byte, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if isNull(data) || customDecoding(typ) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if err := json.Unmarshal(data, &members); err != nil {
			return err
		}
		return checkRequiredMembers(members, typ)
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		for _, item := range items {
			if err := checkRequiredFields(item, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil {
			return err
		}
		for _, entry := range entries {
			if err := checkRequiredFields(entry, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkRequiredMembers applies encoding/json field promotion: untagged
// embedded structs contribute their fields to the same object. A value
// embedding's fields keep their requiredness; a pointer embedding may be nil,
// so its fields are only checked for nested requirements when present.
func checkRequiredMembers(members map[string]json.RawMessage, typ reflect.Type) error {
	return checkPromotedMembers(members, typ, true)
}

func checkPromotedMembers(members map[string]json.RawMessage, typ reflect.Type, enforce bool) error {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" && options == "" {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := field.Type
			pointer := embedded.Kind() == reflect.Pointer
			if pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct && !customDecoding(embedded) {
				if err := checkPromotedMembers(members, embedded, enforce && !pointer); err != nil {
					return err
				}
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		value, present := members[name]
		nullable := field.Type.Kind() == reflect.Pointer || field.Type.Kind() == reflect.Slice ||
			field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface
		if enforce && !hasOption(options, "omitempty") && (!present || (!nullable && isNull(value))) {
			return fmt.Errorf("missing required field %q", name)
		}
		if present {
			if err := checkRequiredFields(value, field.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}

func customDecoding(typ reflect.Type) bool {
	pointer := reflect.PointerTo(typ)
	return typ == reflect.TypeOf(json.RawMessage{}) || pointer.Implements(jsonUnmarshalerType) || pointer.Implements(textUnmarshalerType)
}

func isNull(data []byte) bool { return bytes.Equal(bytes.TrimSpace(data), []byte("null")) }

func hasOption(options, option string) bool {
	for _, candidate := range strings.Split(options, ",") {
		if candidate == option {
			return true
		}
	}
	return false
}
