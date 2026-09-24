package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// MaxFrameBytes bounds bytes before the newline delimiter (including CR
	// when the peer uses CRLF), matching the v1 receive limit.
	MaxFrameBytes = 16 * 1024 * 1024
	// MaxQueuedBytes bounds one endpoint's queued and currently writing output.
	MaxQueuedBytes = 32 * 1024 * 1024
	// MaxIncomingRequests bounds concurrent plugin-to-host requests.
	MaxIncomingRequests = 128
	// MaxBatchMessages bounds decoded batch metadata independently of frame bytes.
	MaxBatchMessages = 1024
)

// RPCError is a JSON-RPC application or transport error. Data is optional JSON.
// Errors received from the peer retain their code, message, and data.
type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("plugin RPC %d: %s", e.Code, e.Message) }

func rpcError(code int64, message string) *RPCError { return &RPCError{Code: code, Message: message} }

type rpcMessage struct {
	invalid bool            // Local invalid-batch reply, never an incoming response.
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  *string         `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func requestMessage(id json.RawMessage, method string, params json.RawMessage) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", ID: id, Method: &method, Params: params}
}

func responseMessage(id json.RawMessage, result json.RawMessage, err error) rpcMessage {
	message := rpcMessage{JSONRPC: "2.0", ID: id}
	if err != nil {
		var application *RPCError
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			message.Error = rpcError(-32001, "Request cancelled")
		} else if errors.As(err, &application) {
			message.Error = application
		} else {
			message.Error = rpcError(-32603, err.Error())
		}
	} else {
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		message.Result = result
	}
	return message
}

// canonicalInteger preserves arbitrary JSON integer identity without float
// rounding or expanding exponent notation into an attacker-sized allocation.
func canonicalInteger(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(raw), "e")
	whole, fractional, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fractional, "0")
	if digits == "" {
		return "0", nil
	}
	trimmed := strings.TrimRight(digits, "0")
	power, err := normalizeExponent(exponent, hasExponent, len(digits)-len(trimmed)-len(fractional))
	if err != nil {
		return "", err
	}
	sign := ""
	if negative {
		sign = "-"
	}
	return sign + trimmed + "e" + power, nil
}

// normalizeExponent performs linear lexical arithmetic, including huge exponent
// representations, rather than parsing untrusted arbitrary-precision decimals.
func normalizeExponent(raw string, present bool, offset int) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	magnitude := strings.TrimLeft(strings.TrimLeft(raw, "+-"), "0")
	if !present || magnitude == "" {
		magnitude = "0"
	}
	if len(magnitude) <= 18 {
		power, _ := strconv.ParseInt(magnitude, 10, 64)
		if negative {
			power = -power
		}
		power += int64(offset)
		if power < 0 {
			return "", errors.New("fractional request id")
		}
		return strconv.FormatInt(power, 10), nil
	}
	if negative {
		return "", errors.New("fractional request id")
	}
	digits := []byte(magnitude)
	carry := offset
	for i := len(digits) - 1; i >= 0 && carry != 0; i-- {
		value := int(digits[i]-'0') + carry
		digit := value % 10
		if digit < 0 {
			digit += 10
		}
		carry = (value - digit) / 10
		digits[i] = byte(digit) + '0'
	}
	prefix := ""
	if carry > 0 {
		prefix = strconv.Itoa(carry)
	}
	return strings.TrimLeft(prefix+string(digits), "0"), nil
}

func errorCode(raw json.RawMessage) (int64, error) {
	key, err := requestKey(raw)
	if err != nil || !strings.HasPrefix(key, "n:") {
		return 0, errors.New("invalid error code")
	}
	value := strings.TrimPrefix(key, "n:")
	if value == "0" {
		return 0, nil
	}
	digits, exponent, _ := strings.Cut(value, "e")
	power, err := strconv.Atoi(exponent)
	if err != nil || power > 20 || len(digits)+power > 20 {
		return 0, errors.New("error code exceeds signed 64-bit range")
	}
	return strconv.ParseInt(digits+strings.Repeat("0", power), 10, 64)
}

func requestKey(id json.RawMessage) (string, error) {
	id = bytes.TrimSpace(id)
	if len(id) == 0 || !json.Valid(id) {
		return "", errors.New("missing or invalid request id")
	}
	if id[0] == '"' {
		var value string
		if err := json.Unmarshal(id, &value); err != nil {
			return "", err
		}
		return "s:" + value, nil
	}
	if id[0] != '-' && (id[0] < '0' || id[0] > '9') {
		return "", errors.New("request id must be a string or integer")
	}
	integer, err := canonicalInteger(string(id))
	return "n:" + integer, err
}

func invalidRPCMessage(data json.RawMessage) rpcMessage {
	id := json.RawMessage("null")
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) == nil {
		if _, err := requestKey(fields["id"]); err == nil {
			id = fields["id"]
		}
	}
	message := responseMessage(id, nil, rpcError(-32600, "Invalid request"))
	message.invalid = true
	return message
}

func decodeRPCFrame(frame []byte) ([]rpcMessage, bool, error) {
	if !utf8.Valid(frame) || !json.Valid(frame) {
		return nil, false, rpcError(-32700, "Plugin wrote malformed JSON or invalid UTF-8")
	}
	frame = bytes.TrimSpace(frame)
	if frame[0] != '[' {
		message, err := decodeRPCMessage(frame)
		if err != nil {
			return nil, false, rpcError(-32600, err.Error())
		}
		return []rpcMessage{message}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	_, _ = decoder.Token() // The complete frame has already passed json.Valid.
	var messages []rpcMessage
	for decoder.More() {
		if len(messages) == MaxBatchMessages {
			return nil, true, rpcError(-32005, "Batch exceeds element limit")
		}
		var data json.RawMessage
		if err := decoder.Decode(&data); err != nil {
			return nil, true, err
		}
		message, err := decodeRPCMessage(data)
		if err != nil {
			message = invalidRPCMessage(data)
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return []rpcMessage{invalidRPCMessage(nil)}, false, nil
	}
	return messages, true, nil
}

func decodeRPCMessage(data json.RawMessage) (rpcMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return rpcMessage{}, errors.New("JSON-RPC message must be an object")
	}
	var message rpcMessage
	var version string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return rpcMessage{}, errors.New("JSON-RPC version must be 2.0")
	}
	message.JSONRPC = "2.0"
	message.ID = fields["id"]
	if _, ok := fields["method"]; ok {
		var method string
		if bytes.Equal(fields["method"], []byte("null")) || json.Unmarshal(fields["method"], &method) != nil || strings.HasPrefix(method, "rpc.") {
			return rpcMessage{}, errors.New("invalid JSON-RPC method")
		}
		message.Method = &method
		message.Params = fields["params"]
		if len(message.ID) != 0 {
			if _, err := requestKey(message.ID); err != nil {
				return rpcMessage{}, err
			}
		}
		if len(message.Params) != 0 {
			params := bytes.TrimSpace(message.Params)
			if params[0] != '{' && params[0] != '[' {
				return rpcMessage{}, errors.New("JSON-RPC params must be an object or array")
			}
		}
		for key := range fields {
			switch key {
			case "jsonrpc", "id", "method", "params":
			default:
				return rpcMessage{}, fmt.Errorf("unexpected request field %q", key)
			}
		}
	} else {
		if _, err := requestKey(message.ID); err != nil && !(string(message.ID) == "null" && fields["error"] != nil) {
			return rpcMessage{}, err
		}
		_, hasResult := fields["result"]
		_, hasError := fields["error"]
		if hasResult == hasError {
			return rpcMessage{}, errors.New("response requires exactly one of result and error")
		}
		if hasResult {
			message.Result = fields["result"]
		} else {
			var object map[string]json.RawMessage
			if json.Unmarshal(fields["error"], &object) != nil || object == nil {
				return rpcMessage{}, errors.New("invalid RPC error object")
			}
			for key := range object {
				switch key {
				case "code", "message", "data":
				default:
					return rpcMessage{}, errors.New("unknown RPC error field")
				}
			}
			var text string
			if raw := object["message"]; len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
				return rpcMessage{}, errors.New("RPC error requires a message")
			}
			// Error codes use signed 64-bit integers; request IDs never use float64.
			code, err := errorCode(object["code"])
			if err != nil {
				return rpcMessage{}, errors.New("RPC error requires an integer code")
			}
			message.Error = &RPCError{Code: code, Message: text, Data: object["data"]}
		}
		for key := range fields {
			switch key {
			case "jsonrpc", "id", "result", "error":
			default:
				return rpcMessage{}, fmt.Errorf("unexpected response field %q", key)
			}
		}
	}
	return message, nil
}
