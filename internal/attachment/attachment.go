// Package attachment owns durable attachment bytes and metadata.
package attachment

import (
	"context"
	"errors"
	"io"
	"time"
)

const IDPrefix = "attachment_"

var (
	ErrInvalidInput = errors.New("invalid attachment input")
	ErrTooLarge     = errors.New("attachment exceeds size limit")
	ErrNotFound     = errors.New("attachment not found")
)

type Record struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
}

type PutInput struct {
	SessionID string
	Filename  string
	MediaType string
	Content   io.Reader
	MaxBytes  int64
	Width     int
	Height    int
	Validate  func(io.ReadSeeker) error
}

type Store interface {
	Put(context.Context, PutInput) (Record, error)
	Open(context.Context, string, string) (Record, io.ReadCloser, error)
	Remove(context.Context, string, string) error
	RemoveSession(context.Context, string) error
}
