package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// UnionVariant describes one kind of a discriminated union (ADR 0032) for
// contract generation: its discriminator value and a zero payload value.
type UnionVariant struct {
	Kind    string
	Payload any
}

// unionMember maps one JSON member between a union's typed fields and its
// flat wire record.
type unionMember struct {
	name     string
	field    int
	wire     int
	required bool
}

// unionVariant is one kind's payload type and member mapping.
type unionVariant struct {
	kind    string
	payload reflect.Type
	members []unionMember
}

// unionCodec encodes a discriminated union through a flat wire struct so that
// the bytes match the pre-union record exactly, and decodes strictly: the kind
// must be known, every member must belong to the common fields or that kind's
// payload, and required members must be present.
type unionCodec struct {
	name     string
	wire     reflect.Type
	kindWire int
	common   []unionMember
	byKind   map[string]*unionVariant
	byType   map[reflect.Type]*unionVariant
	variants []UnionVariant
}

// newUnionCodec builds a codec for union type U whose common fields are every
// JSON-tagged field except the untagged Payload, flat wire struct W, and the
// given variants. It panics on a declaration mismatch, which tests exercise.
func newUnionCodec[U, W any](variants []UnionVariant) *unionCodec {
	union := reflect.TypeFor[U]()
	wire := reflect.TypeFor[W]()
	codec := &unionCodec{name: union.Name(), wire: wire, byKind: map[string]*unionVariant{}, byType: map[reflect.Type]*unionVariant{}, variants: append([]UnionVariant(nil), variants...)}
	wireFields := jsonFieldIndex(wire)
	kindIndex, ok := wireFields["kind"]
	if !ok {
		panic(union.Name() + " wire record has no kind member")
	}
	codec.kindWire = kindIndex
	codec.common = mapUnionMembers(union, wire, wireFields)
	for _, variant := range variants {
		payload := reflect.TypeOf(variant.Payload)
		if _, duplicate := codec.byKind[variant.Kind]; duplicate {
			panic(union.Name() + " declares kind " + variant.Kind + " twice")
		}
		if _, duplicate := codec.byType[payload]; duplicate {
			panic(union.Name() + " declares payload " + payload.Name() + " twice")
		}
		entry := &unionVariant{kind: variant.Kind, payload: payload, members: mapUnionMembers(payload, wire, wireFields)}
		codec.byKind[variant.Kind] = entry
		codec.byType[payload] = entry
	}
	return codec
}

func jsonFieldIndex(typ reflect.Type) map[string]int {
	fields := map[string]int{}
	for i := 0; i < typ.NumField(); i++ {
		if name, _, ok := jsonMember(typ.Field(i)); ok {
			fields[name] = i
		}
	}
	return fields
}

func jsonMember(field reflect.StructField) (string, bool, bool) {
	name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
	if !field.IsExported() || name == "-" || name == "" {
		return "", false, false
	}
	return name, strings.Contains(","+options+",", ",omitempty,"), true
}

func mapUnionMembers(typ, wire reflect.Type, wireFields map[string]int) []unionMember {
	var members []unionMember
	for i := 0; i < typ.NumField(); i++ {
		name, omitempty, ok := jsonMember(typ.Field(i))
		if !ok {
			continue
		}
		index, found := wireFields[name]
		if !found || !typ.Field(i).Type.ConvertibleTo(wire.Field(index).Type) || !wire.Field(index).Type.ConvertibleTo(typ.Field(i).Type) {
			panic(fmt.Sprintf("%s.%s has no matching %s wire member %q", typ.Name(), typ.Field(i).Name, wire.Name(), name))
		}
		members = append(members, unionMember{name: name, field: i, wire: index, required: !omitempty})
	}
	return members
}

// variant returns the declaration for a payload value.
func (c *unionCodec) variant(payload any) (*unionVariant, error) {
	if payload == nil || (reflect.ValueOf(payload).Kind() == reflect.Ptr && reflect.ValueOf(payload).IsNil()) {
		return nil, fmt.Errorf("%s has no payload", c.name)
	}
	variant, ok := c.byType[reflect.TypeOf(payload)]
	if !ok {
		return nil, fmt.Errorf("%s payload %T is not a declared variant", c.name, payload)
	}
	return variant, nil
}

// flatten copies the union's common fields and payload into a new wire value.
func (c *unionCodec) flatten(union reflect.Value, payload any) (reflect.Value, *unionVariant, error) {
	variant, err := c.variant(payload)
	if err != nil {
		return reflect.Value{}, nil, err
	}
	wire := reflect.New(c.wire).Elem()
	for _, member := range c.common {
		wire.Field(member.wire).Set(union.Field(member.field).Convert(c.wire.Field(member.wire).Type))
	}
	value := reflect.ValueOf(payload)
	for _, member := range variant.members {
		wire.Field(member.wire).Set(value.Field(member.field).Convert(c.wire.Field(member.wire).Type))
	}
	wire.Field(c.kindWire).SetString(variant.kind)
	return wire, variant, nil
}

// marshal encodes the union through its flat wire record.
func (c *unionCodec) marshal(union any, payload any) ([]byte, error) {
	wire, _, err := c.flatten(reflect.ValueOf(union), payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire.Interface())
}

// unmarshal strictly decodes data into union (a pointer) and returns the payload.
func (c *unionCodec) unmarshal(data []byte, union any) (any, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("decode %s: %w", c.name, err)
	}
	if members == nil {
		return nil, fmt.Errorf("decode %s: expected an object", c.name)
	}
	var kind string
	if raw, ok := members["kind"]; !ok || json.Unmarshal(raw, &kind) != nil {
		return nil, fmt.Errorf("decode %s: kind is required", c.name)
	}
	variant, ok := c.byKind[kind]
	if !ok {
		return nil, fmt.Errorf("decode %s: unknown kind %q", c.name, kind)
	}
	allowed := map[string]bool{"kind": true}
	for _, member := range append(append([]unionMember(nil), c.common...), variant.members...) {
		allowed[member.name] = true
		if _, present := members[member.name]; member.required && !present {
			return nil, fmt.Errorf("decode %s: kind %q requires %s", c.name, kind, member.name)
		}
	}
	var unexpected []string
	for name := range members {
		if !allowed[name] {
			unexpected = append(unexpected, name)
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return nil, fmt.Errorf("decode %s: kind %q does not carry %s", c.name, kind, strings.Join(unexpected, ", "))
	}
	wire := reflect.New(c.wire)
	if err := json.Unmarshal(data, wire.Interface()); err != nil {
		return nil, fmt.Errorf("decode %s: %w", c.name, err)
	}
	target := reflect.ValueOf(union).Elem()
	for _, member := range c.common {
		target.Field(member.field).Set(wire.Elem().Field(member.wire).Convert(target.Field(member.field).Type()))
	}
	payload := reflect.New(variant.payload).Elem()
	for _, member := range variant.members {
		payload.Field(member.field).Set(wire.Elem().Field(member.wire).Convert(payload.Field(member.field).Type()))
	}
	return payload.Interface(), nil
}
