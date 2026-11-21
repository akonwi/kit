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
	// OptionalInstance lets a request omit the instance header, so a client can
	// learn the running server instance from the response.
	OptionalInstance bool
	// Unauthenticated accepts requests without a credential and without an
	// instance header, as redeeming a pairing code does.
	Unauthenticated bool
	// ResponseHeaders are the headers that success and empty responses carry.
	ResponseHeaders []ResponseHeader
	// EmptyResponses are statuses, such as HTTP conditional responses, that
	// carry no body.
	EmptyResponses []EmptyResponse
	// Deprecated marks an operation clients should stop using; servers keep
	// serving it until a later release removes it.
	Deprecated bool
}

// ResponseHeader declares one header of an operation's success and empty
// responses.
type ResponseHeader struct {
	Name        string
	Description string
	// Required headers appear on every success response.
	Required bool
}

// EmptyResponse declares a non-error status that has no body.
type EmptyResponse struct {
	Status      int
	Description string
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
	OptionalInstance                    bool
	Unauthenticated                     bool
	ResponseHeaders                     []ResponseHeader
	EmptyResponses                      []EmptyResponse
	Deprecated                          bool
}

// Describe projects a typed operation into its reflection-friendly form.
func (op Operation[Params, In, Out]) Describe() Descriptor {
	return Descriptor{ID: op.ID, Tag: op.Tag, Method: op.Method, Path: op.Path, Success: op.Success,
		Params: typeOf[Params](), Input: typeOf[In](), Output: typeOf[Out](), AdditionalSuccess: append([]int(nil), op.AdditionalSuccess...), Errors: mergeErrorResponses(op.Errors, CommonErrorResponses),
		RequestMediaType: op.RequestMediaType, ResponseMediaType: op.ResponseMediaType, ResponseMediaTypes: append([]string(nil), op.ResponseMediaTypes...), NoProtocol: op.NoProtocol,
		OptionalInstance: op.OptionalInstance, Unauthenticated: op.Unauthenticated, Deprecated: op.Deprecated,
		ResponseHeaders: append([]ResponseHeader(nil), op.ResponseHeaders...), EmptyResponses: append([]EmptyResponse(nil), op.EmptyResponses...)}
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// NoBody marks an operation with no JSON request body.
type NoBody struct{}
