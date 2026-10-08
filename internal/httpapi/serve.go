package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestError reports malformed HTTP input before an operation handler runs.
type RequestError struct{ Err error }

func (e *RequestError) Error() string { return e.Err.Error() }
func (e *RequestError) Unwrap() error { return e.Err }

// ServeOptions configures shared HTTP binding behavior.
type ServeOptions struct {
	MaxRequestBytes int64
	WriteError      func(http.ResponseWriter, error)
	// StreamHeartbeat shortens the stream heartbeat interval for tests. Zero
	// or values above StreamHeartbeatInterval use StreamHeartbeatInterval.
	StreamHeartbeat time.Duration
	// StreamShutdown ends open streams cleanly when it is done, so a graceful
	// server shutdown does not wait for attached clients to disconnect. Nil
	// leaves streams bounded only by their request.
	StreamShutdown context.Context
}

var registered = struct {
	sync.Mutex
	ids map[string]int
}{ids: make(map[string]int)}

func registerOperation(id string) {
	registered.Lock()
	registered.ids[id]++
	registered.Unlock()
}

// HandleRaw registers a catalog operation whose multipart, binary, or other
// specialized wire handling is owned by fn.
func HandleRaw[Params, In, Out any](mux *http.ServeMux, op Operation[Params, In, Out], fn http.HandlerFunc) {
	registerOperation(op.ID)
	mux.HandleFunc(op.Method+" "+op.Path, fn)
}

// Handle registers a typed operation on mux. Semantic validation remains the handler's responsibility.
func Handle[Params, In, Out any](mux *http.ServeMux, options ServeOptions, op Operation[Params, In, Out], fn func(context.Context, Params, In) (Out, error)) {
	registerOperation(op.ID)
	mux.HandleFunc(op.Method+" "+op.Path, func(w http.ResponseWriter, r *http.Request) {
		var params Params
		if err := bindPathParams(r, &params); err != nil {
			options.WriteError(w, NewAPIError(http.StatusBadRequest, ErrorInvalidRequest, "invalid request", nil))
			return
		}
		var input In
		if typeOf[In]() != typeOf[NoBody]() {
			if err := decodeJSON(w, r, options.MaxRequestBytes, &input); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					options.WriteError(w, NewAPIError(http.StatusRequestEntityTooLarge, ErrorLimitExceeded, "request body is too large", nil))
				} else {
					options.WriteError(w, NewAPIError(http.StatusBadRequest, ErrorInvalidRequest, "invalid request", nil))
				}
				return
			}
		}
		output, err := fn(r.Context(), params, input)
		if err != nil {
			options.WriteError(w, err)
			return
		}
		if typeOf[Out]() == typeOf[NoBody]() {
			w.WriteHeader(op.Success)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(op.Success)
		_ = json.NewEncoder(w).Encode(output)
	})
}

func bindPathParams(r *http.Request, target any) error {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("httpapi: path params must be a struct")
	}
	value = value.Elem()
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Tag.Get("path")
		if name != "" {
			if value.Field(i).Kind() != reflect.String || !value.Field(i).CanSet() {
				return fmt.Errorf("httpapi: path parameter %s must be a settable string", name)
			}
			value.Field(i).SetString(r.PathValue(name))
			continue
		}
		if raw := field.Tag.Get("query"); raw != "" {
			name, _, _ := strings.Cut(raw, ",")
			if err := bindParameter(value.Field(i), r.URL.Query()[name]); err != nil {
				return fmt.Errorf("httpapi: query parameter %s: %w", name, err)
			}
			continue
		}
		if raw := field.Tag.Get("header"); raw != "" {
			name, _, _ := strings.Cut(raw, ",")
			if err := bindParameter(value.Field(i), r.Header.Values(name)); err != nil {
				return fmt.Errorf("httpapi: header parameter %s: %w", name, err)
			}
		}
	}
	return nil
}

func bindParameter(field reflect.Value, values []string) error {
	if !field.CanSet() || len(values) == 0 {
		return nil
	}
	if field.Kind() == reflect.Slice {
		if field.Type().Elem().Kind() != reflect.String {
			return errors.New("unsupported slice type")
		}
		field.Set(reflect.ValueOf(append([]string(nil), values...)))
		return nil
	}
	if len(values) != 1 {
		return errors.New("must appear once")
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(values[0])
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(values[0], 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(values[0], 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(parsed)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(values[0])
		if err != nil {
			return err
		}
		field.SetBool(parsed)
	default:
		return errors.New("unsupported parameter type")
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	if limit <= 0 {
		limit = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
