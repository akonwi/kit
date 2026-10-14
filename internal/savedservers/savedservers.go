// Package savedservers owns the named Kit servers a client can attach to. The
// list lives in one JSON file in the client's Kit home and holds no secrets;
// device tokens for URL servers live in a separate private file.
package savedservers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gofrs/flock"
)

// LocalName refers to the default server of the client's own Kit home. It is
// reserved and never saved.
const LocalName = "local"

const (
	maxNameLength = 32
	maxFileSize   = 1 << 20
)

var (
	// ErrExists reports adding a name that is already saved.
	ErrExists = errors.New("server name is already saved")
	// ErrNotFound reports a name that is not saved.
	ErrNotFound = errors.New("server name is not saved")
)

// Kind is how a saved server is reached.
type Kind string

const (
	// KindHome is the server of a Kit home on this machine.
	KindHome Kind = "home"
	// KindURL is a server served at an HTTP(S) URL, reached with a device
	// token.
	KindURL Kind = "url"
)

// Server is one saved server.
type Server struct {
	// Name is how commands and pickers refer to the server.
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Home is the absolute path of the server's Kit home, for KindHome.
	Home string `json:"home,omitempty"`
	// URL is the server's HTTP(S) origin, for KindURL.
	URL string `json:"url,omitempty"`
}

// Location is the server's home or URL, for display.
func (s Server) Location() string {
	if s.Kind == KindURL {
		return s.URL
	}
	return s.Home
}

// CanonicalURL validates an HTTP(S) server URL and returns its origin,
// `scheme://host[:port]`, lowercased and without a path.
func CanonicalURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("server URL %q must be an HTTP(S) origin such as https://kit.example.com", value)
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host), nil
}

type document struct {
	Servers []Server `json:"servers"`
}

// Store reads and changes the saved-server file at one path. Changes from
// independent processes are serialized by a lock beside the file.
type Store struct {
	path string
}

// NewStore returns a store for an absolute path.
func NewStore(path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("saved servers path must be absolute: %q", path)
	}
	return &Store{path: filepath.Clean(path)}, nil
}

// ValidateName reports whether name can be saved: 1 to 32 lowercase letters,
// digits, '.', '-', or '_', starting with a letter or digit, and not
// LocalName.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("server name is required")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("server name %q is longer than %d characters", name, maxNameLength)
	}
	for index, character := range name {
		alphanumeric := (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
		if alphanumeric || (index > 0 && strings.ContainsRune(".-_", character)) {
			continue
		}
		return fmt.Errorf("server name %q must be lowercase letters, digits, '.', '-', or '_', starting with a letter or digit", name)
	}
	if name == LocalName {
		return fmt.Errorf("server name %q is reserved for this home's own server", LocalName)
	}
	return nil
}

func (s Server) validate() error {
	if err := ValidateName(s.Name); err != nil {
		return err
	}
	switch s.Kind {
	case KindHome:
		if s.URL != "" || s.Home == "" || !filepath.IsAbs(s.Home) || filepath.Clean(s.Home) != s.Home {
			return fmt.Errorf("server %q home must be a clean absolute path: %q", s.Name, s.Home)
		}
	case KindURL:
		if canonical, err := CanonicalURL(s.URL); err != nil || canonical != s.URL || s.Home != "" {
			return fmt.Errorf("server %q URL must be a canonical HTTP(S) origin: %q", s.Name, s.URL)
		}
	default:
		return fmt.Errorf("server %q kind %q is not home or url", s.Name, s.Kind)
	}
	return nil
}

// List returns the saved servers sorted by name. A missing file lists none.
func (s *Store) List() ([]Server, error) {
	servers, err := s.read()
	if err != nil {
		return nil, err
	}
	return servers, nil
}

// Find returns the server saved under name.
func (s *Store) Find(name string) (Server, error) {
	servers, err := s.read()
	if err != nil {
		return Server{}, err
	}
	for _, server := range servers {
		if server.Name == name {
			return server, nil
		}
	}
	return Server{}, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// FindURL returns the server saved with url, an origin from CanonicalURL.
func (s *Store) FindURL(url string) (Server, error) {
	servers, err := s.read()
	if err != nil {
		return Server{}, err
	}
	for _, server := range servers {
		if server.Kind == KindURL && server.URL == url {
			return server, nil
		}
	}
	return Server{}, fmt.Errorf("%w: %q", ErrNotFound, url)
}

// Add saves server under a new name.
func (s *Store) Add(server Server) error {
	if err := server.validate(); err != nil {
		return err
	}
	return s.mutate(func(servers []Server) ([]Server, error) {
		for _, saved := range servers {
			if saved.Name == server.Name {
				return nil, fmt.Errorf("%w: %q", ErrExists, server.Name)
			}
		}
		return append(servers, server), nil
	})
}

// Remove forgets the server saved under name.
func (s *Store) Remove(name string) error {
	return s.mutate(func(servers []Server) ([]Server, error) {
		for index, saved := range servers {
			if saved.Name == name {
				return slices.Delete(servers, index, index+1), nil
			}
		}
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	})
}

func (s *Store) mutate(change func([]Server) ([]Server, error)) error {
	unlock, err := lockFile(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	servers, err := s.read()
	if err != nil {
		return err
	}
	servers, err = change(servers)
	if err != nil {
		return err
	}
	return s.write(servers)
}

// lockFile creates path's directory and holds the lock beside path, which
// serializes changes from independent processes.
func lockFile(path string) (func(), error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create directory %q: %w", directory, err)
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("lock %q: %w", path, err)
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s *Store) read() ([]Server, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read saved servers %q: %w", s.path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read saved servers %q: %w", s.path, err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("saved servers %q exceeds %d bytes", s.path, maxFileSize)
	}
	var decoded document
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode saved servers %q: %w", s.path, err)
	}
	seen := make(map[string]bool, len(decoded.Servers))
	for index, server := range decoded.Servers {
		// Entries saved before servers had kinds are Kit homes.
		if server.Kind == "" && server.Home != "" {
			server.Kind = KindHome
			decoded.Servers[index] = server
		}
		if err := server.validate(); err != nil {
			return nil, fmt.Errorf("saved servers %q: %w", s.path, err)
		}
		if seen[server.Name] {
			return nil, fmt.Errorf("saved servers %q: server %q is listed twice", s.path, server.Name)
		}
		seen[server.Name] = true
	}
	slices.SortFunc(decoded.Servers, func(a, b Server) int { return strings.Compare(a.Name, b.Name) })
	return decoded.Servers, nil
}

func (s *Store) write(servers []Server) error {
	if servers == nil {
		servers = []Server{}
	}
	slices.SortFunc(servers, func(a, b Server) int { return strings.Compare(a.Name, b.Name) })
	data, err := json.MarshalIndent(document{Servers: servers}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode saved servers: %w", err)
	}
	data = append(data, '\n')
	return replaceFile(s.path, data)
}

// replaceFile atomically replaces path with data, readable only by its owner.
func replaceFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	name := temporary.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %q: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
