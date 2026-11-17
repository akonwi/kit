package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// OpenMembers reports that a snapshot's top-level members are open: a client
// ignores a member it doesn't know (ADR 0072). The members it knows, and every
// record inside them, are still decoded strictly.
func (SessionSnapshot) OpenMembers() bool { return true }

var sessionSnapshotMembers = sync.OnceValue(func() map[string]bool {
	members := map[string]bool{}
	typ := reflect.TypeFor[SessionSnapshot]()
	for i := 0; i < typ.NumField(); i++ {
		if name, _, ok := jsonMember(typ.Field(i)); ok {
			members[name] = true
		}
	}
	return members
})

// UnmarshalJSON decodes a snapshot, skipping top-level members this protocol
// doesn't declare and strictly decoding the rest.
func (snapshot *SessionSnapshot) UnmarshalJSON(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("decode SessionSnapshot: %w", err)
	}
	if members == nil {
		return fmt.Errorf("decode SessionSnapshot: expected an object")
	}
	known := sessionSnapshotMembers()
	for name := range members {
		if !known[name] {
			delete(members, name)
		}
	}
	filtered, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("decode SessionSnapshot: %w", err)
	}
	type plain SessionSnapshot
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(filtered))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode SessionSnapshot: %w", err)
	}
	*snapshot = SessionSnapshot(decoded)
	return nil
}
