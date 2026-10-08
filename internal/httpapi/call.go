package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

// ProtocolError reports a response that violates an operation's declared wire contract.
type ProtocolError struct{ Err error }

func (e *ProtocolError) Error() string { return "session protocol: " + e.Err.Error() }
func (e *ProtocolError) Unwrap() error { return e.Err }

func responseProtocolError(format string, args ...any) error {
	return &ProtocolError{Err: fmt.Errorf(format, args...)}
}

// MaxRequestBytes bounds session request bodies on both sides of the boundary.
const MaxRequestBytes = 1 << 20

// Transport performs an authenticated, compatibility-checked session request.
type Transport interface {
	DoSessionRequest(context.Context, string, string, io.Reader, bool) (*http.Response, error)
}

// Call invokes one typed operation. Semantic validation remains the caller's responsibility.
func Call[Params, In, Out any](ctx context.Context, transport Transport, op Operation[Params, In, Out], params Params, input In) (Out, error) {
	var zero Out
	path, err := operationPath(op.Path, params)
	if err != nil {
		return zero, err
	}
	var body io.Reader
	hasBody := typeOf[In]() != typeOf[NoBody]()
	if hasBody {
		encoded, err := json.Marshal(input)
		if err != nil {
			return zero, fmt.Errorf("encode daemon request: %w", err)
		}
		if len(encoded) > MaxRequestBytes {
			return zero, fmt.Errorf("daemon request body exceeds %d bytes", MaxRequestBytes)
		}
		body = bytes.NewReader(encoded)
	}
	response, err := transport.DoSessionRequest(ctx, op.Method, path, body, hasBody)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		return zero, fmt.Errorf("read daemon session response: %w", err)
	}
	success := response.StatusCode == op.Success
	for _, status := range op.AdditionalSuccess {
		success = success || response.StatusCode == status
	}
	if !success {
		return zero, DecodeOperationError(op, response.StatusCode, encoded)
	}
	if typeOf[Out]() == typeOf[NoBody]() {
		if len(encoded) != 0 {
			return zero, responseProtocolError("daemon returned unexpected response body")
		}
		return zero, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&zero); err != nil {
		return zero, responseProtocolError("decode daemon session response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, responseProtocolError("decode daemon session response: multiple JSON values")
	}
	return zero, nil
}

func operationPath(path string, params any) (string, error) {
	value := reflect.ValueOf(params)
	typ := value.Type()
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", fmt.Errorf("httpapi: nil path params")
		}
		value, typ = value.Elem(), typ.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", fmt.Errorf("httpapi: path params must be a struct")
	}
	query := url.Values{}
	for i := 0; i < typ.NumField(); i++ {
		field, meta := value.Field(i), typ.Field(i)
		if name := meta.Tag.Get("path"); name != "" {
			if field.Kind() != reflect.String {
				return "", fmt.Errorf("httpapi: path parameter %s must be a string", name)
			}
			token := "{" + name + "}"
			if !strings.Contains(path, token) {
				return "", fmt.Errorf("httpapi: path parameter %s is not present in path", name)
			}
			path = strings.ReplaceAll(path, token, url.PathEscape(field.String()))
			continue
		}
		raw := meta.Tag.Get("query")
		if raw == "" {
			continue
		}
		name, options, _ := strings.Cut(raw, ",")
		if err := appendQuery(query, name, field, strings.Contains(options, "omitempty")); err != nil {
			return "", err
		}
	}
	if strings.ContainsAny(path, "{}") {
		return "", fmt.Errorf("httpapi: path has unbound parameters")
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path, nil
}

func appendQuery(query url.Values, name string, value reflect.Value, omitEmpty bool) error {
	if value.Kind() == reflect.Slice {
		for index := 0; index < value.Len(); index++ {
			query.Add(name, fmt.Sprint(value.Index(index).Interface()))
		}
		return nil
	}
	if omitEmpty && value.IsZero() {
		return nil
	}
	switch value.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Bool:
		query.Set(name, fmt.Sprint(value.Interface()))
		return nil
	default:
		return fmt.Errorf("httpapi: query parameter %s has unsupported type", name)
	}
}
