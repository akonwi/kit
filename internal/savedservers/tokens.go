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
// Each token is bound to the URL it was issued for and is returned only for
// that URL, so it is never sent to another server saved under the name later.
type Tokens struct {
	path string
}

type tokenEntry struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type tokenDocument struct {
	Tokens map[string]tokenEntry `json:"tokens"`
}

// NewTokens returns a token store for an absolute path.
func NewTokens(path string) (*Tokens, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("server tokens path must be absolute: %q", path)
	}
	return &Tokens{path: filepath.Clean(path)}, nil
}

// Get returns the token saved for a server at serverURL, or ErrNotFound when
// none is saved for that name and URL.
func (t *Tokens) Get(name, serverURL string) (string, error) {
	tokens, err := t.read()
	if err != nil {
		return "", err
	}
	entry, ok := tokens[name]
	if canonical, err := CanonicalURL(serverURL); !ok || err != nil || entry.URL != canonical {
		return "", fmt.Errorf("%w: no device token for %q at %s", ErrNotFound, name, serverURL)
	}
	return entry.Token, nil
}

// Set saves the token a server at serverURL issued, replacing any earlier
// one for the name.
func (t *Tokens) Set(name, serverURL, token string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	canonical, err := CanonicalURL(serverURL)
	if err != nil {
		return err
	}
	if !devices.IsToken(token) {
		return errors.New("device token is malformed")
	}
	return t.mutate(func(tokens map[string]tokenEntry) { tokens[name] = tokenEntry{URL: canonical, Token: token} })
}

// Delete forgets a server's token; forgetting a missing token succeeds.
func (t *Tokens) Delete(name string) error {
	return t.mutate(func(tokens map[string]tokenEntry) { delete(tokens, name) })
}

func (t *Tokens) mutate(change func(map[string]tokenEntry)) error {
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

func (t *Tokens) read() (map[string]tokenEntry, error) {
	file, err := os.Open(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]tokenEntry{}, nil
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
		decoded.Tokens = map[string]tokenEntry{}
	}
	return decoded.Tokens, nil
}
