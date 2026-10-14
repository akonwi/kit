package savedservers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/akonwi/kit/internal/devices"
)

// Tokens holds the device tokens of saved URL servers, by server name, in a
// file readable only by its owner and separate from provider credentials.
type Tokens struct {
	path string
}

type tokenDocument struct {
	Tokens map[string]string `json:"tokens"`
}

// NewTokens returns a token store for an absolute path.
func NewTokens(path string) (*Tokens, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("server tokens path must be absolute: %q", path)
	}
	return &Tokens{path: filepath.Clean(path)}, nil
}

// Get returns the token saved for a server, or ErrNotFound.
func (t *Tokens) Get(name string) (string, error) {
	tokens, err := t.read()
	if err != nil {
		return "", err
	}
	token, ok := tokens[name]
	if !ok {
		return "", fmt.Errorf("%w: no device token for %q", ErrNotFound, name)
	}
	return token, nil
}

// Set saves a server's token, replacing any earlier one.
func (t *Tokens) Set(name, token string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if !devices.IsToken(token) {
		return errors.New("device token is malformed")
	}
	return t.mutate(func(tokens map[string]string) { tokens[name] = token })
}

// Delete forgets a server's token; forgetting a missing token succeeds.
func (t *Tokens) Delete(name string) error {
	return t.mutate(func(tokens map[string]string) { delete(tokens, name) })
}

func (t *Tokens) mutate(change func(map[string]string)) error {
	unlock, err := lockFile(t.path)
	if err != nil {
		return err
	}
	defer unlock()
	tokens, err := t.read()
	if err != nil {
		return err
	}
	change(tokens)
	data, err := json.MarshalIndent(tokenDocument{Tokens: tokens}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode server tokens: %w", err)
	}
	return replaceFile(t.path, append(data, '\n'))
}

func (t *Tokens) read() (map[string]string, error) {
	file, err := os.Open(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read server tokens %q: %w", t.path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect server tokens %q: %w", t.path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("server tokens %q must be readable only by its owner (mode %v)", t.path, info.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read server tokens %q: %w", t.path, err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("server tokens %q exceeds %d bytes", t.path, maxFileSize)
	}
	var decoded tokenDocument
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode server tokens %q: %w", t.path, err)
	}
	if decoded.Tokens == nil {
		decoded.Tokens = map[string]string{}
	}
	return decoded.Tokens, nil
}
