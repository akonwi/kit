// Package httpapi binds the typed session protocol catalog to net/http.
package httpapi

import "reflect"

// Operation describes one typed HTTP operation.
type Operation[Params, In, Out any] struct {
	ID      string
	Tag     string
	Method  string
	Path    string
	Success int
	// AdditionalSuccess lists alternate success statuses with the same output.
	AdditionalSuccess []int
	Errors            []ErrorResponse
	// RequestMediaType and ResponseMediaType override application/json for
	// operations that transfer multipart or binary content.
	RequestMediaType   string
	ResponseMediaType  string
	ResponseMediaTypes []string
	// NoProtocol omits the session protocol header for lifecycle endpoints.
	NoProtocol bool
}

// ErrorResponse describes the codes permitted for one non-success status.
// Details maps only codes that require a details object to its concrete type.
type ErrorResponse struct {
	Status  int
	Codes   []ErrorCode
	Details map[ErrorCode]reflect.Type
}

// Descriptor is the reflection-friendly form consumed by contract tooling.
type Descriptor struct {
	ID, Tag, Method, Path string
	Success               int
	AdditionalSuccess     []int
	Params, Input, Output reflect.Type
	Errors                []ErrorResponse
	// Stream is set for server-push operations whose Output is the payload
	// schema of every record.
	Stream                              *StreamDescriptor
	RequestMediaType, ResponseMediaType string
	ResponseMediaTypes                  []string
	NoProtocol                          bool
}

// Describe projects a typed operation into its reflection-friendly form.
func (op Operation[Params, In, Out]) Describe() Descriptor {
	return Descriptor{ID: op.ID, Tag: op.Tag, Method: op.Method, Path: op.Path, Success: op.Success,
		Params: typeOf[Params](), Input: typeOf[In](), Output: typeOf[Out](), AdditionalSuccess: append([]int(nil), op.AdditionalSuccess...), Errors: mergeErrorResponses(op.Errors, CommonErrorResponses),
		RequestMediaType: op.RequestMediaType, ResponseMediaType: op.ResponseMediaType, ResponseMediaTypes: append([]string(nil), op.ResponseMediaTypes...), NoProtocol: op.NoProtocol}
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// NoBody marks an operation with no JSON request body.
type NoBody struct{}
