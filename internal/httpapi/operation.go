// Package httpapi binds the typed session protocol catalog to net/http.
package httpapi

import (
	"reflect"

	"github.com/akonwi/kit/internal/protocol"
)

// Operation describes one typed HTTP operation.
type Operation[Params, In, Out any] struct {
	ID      string
	Tag     string
	Method  string
	Path    string
	Success int
	Errors  []ErrorResponse
}

// ErrorResponse describes one non-success response and its JSON body type.
type ErrorResponse struct {
	Status          int
	Bodies          []reflect.Type
	ScratchpadCodes []protocol.ScratchpadErrorCode
}

// Descriptor is the reflection-friendly form consumed by contract tooling.
type Descriptor struct {
	ID, Tag, Method, Path string
	Success               int
	Params, Input, Output reflect.Type
	Errors                []ErrorResponse
}

// Describe projects a typed operation into its reflection-friendly form.
func (op Operation[Params, In, Out]) Describe() Descriptor {
	return Descriptor{ID: op.ID, Tag: op.Tag, Method: op.Method, Path: op.Path, Success: op.Success,
		Params: typeOf[Params](), Input: typeOf[In](), Output: typeOf[Out](), Errors: append([]ErrorResponse(nil), op.Errors...)}
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// NoBody marks an operation with no JSON request body.
type NoBody struct{}
