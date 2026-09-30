package protocol

import "encoding/json"

// sessionEventUnion is the staged payload representation of SessionEvent. It
// shares the current event wire record until consumers move to Payload.
type sessionEventUnion struct {
	StreamID  string              `json:"streamId"`
	Sequence  int64               `json:"sequence"`
	SessionID string              `json:"sessionId"`
	TurnID    string              `json:"turnId"`
	RunID     string              `json:"runId"`
	Payload   SessionEventPayload `json:"-"`
}

// sessionEventWire preserves the pre-union flat field order and JSON tags.
type sessionEventWire SessionEvent

var sessionEventCodec = newUnionCodec[sessionEventUnion, sessionEventWire](sessionEventVariants())

func (event sessionEventUnion) marshalJSON() ([]byte, error) {
	return sessionEventCodec.marshal(event, event.Payload)
}
func (event *sessionEventUnion) unmarshalJSON(data []byte) error {
	var decoded sessionEventUnion
	payload, err := sessionEventCodec.unmarshal(data, &decoded)
	if err != nil {
		return err
	}
	decoded.Payload = payload.(SessionEventPayload)
	*event = decoded
	return nil
}

func decodeSessionEventUnion(data []byte) (sessionEventUnion, error) {
	var event sessionEventUnion
	if err := event.unmarshalJSON(data); err != nil {
		return sessionEventUnion{}, err
	}
	return event, nil
}

func encodeSessionEventUnion(event sessionEventUnion) ([]byte, error) { return event.marshalJSON() }

var _ = json.RawMessage{}
