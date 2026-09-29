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

const maxResponseBytes = 8 << 20

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
		body = bytes.NewReader(encoded)
	}
	response, err := transport.DoSessionRequest(ctx, op.Method, path, body, hasBody)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBytes)
	if response.StatusCode != op.Success {
		encoded, _ := io.ReadAll(limited)
		return zero, DecodeOperationError(op, response.StatusCode, encoded)
	}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&zero); err != nil {
		return zero, fmt.Errorf("decode daemon session response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("decode daemon session response: multiple JSON values")
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
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Tag.Get("path")
		if name == "" {
			continue
		}
		if value.Field(i).Kind() != reflect.String {
			return "", fmt.Errorf("httpapi: path parameter %s must be a string", name)
		}
		token := "{" + name + "}"
		if !strings.Contains(path, token) {
			return "", fmt.Errorf("httpapi: path parameter %s is not present in path", name)
		}
		path = strings.ReplaceAll(path, token, url.PathEscape(value.Field(i).String()))
	}
	if strings.ContainsAny(path, "{}") {
		return "", fmt.Errorf("httpapi: path has unbound parameters")
	}
	return path, nil
}
