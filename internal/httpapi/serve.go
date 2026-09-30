package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
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
		name := typ.Field(i).Tag.Get("path")
		if name == "" {
			continue
		}
		if value.Field(i).Kind() != reflect.String || !value.Field(i).CanSet() {
			return fmt.Errorf("httpapi: path parameter %s must be a settable string", name)
		}
		value.Field(i).SetString(r.PathValue(name))
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
