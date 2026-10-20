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

	"github.com/akonwi/kit/internal/sshtunnel"
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
	// ErrUnsupported reports a server saved by a newer Kit, of a kind this
	// one cannot reach.
	ErrUnsupported = errors.New("server is of a kind this Kit cannot reach; upgrade Kit")
)

// Kind is how a saved server is reached.
type Kind string

const (
	// KindHome is the server of a Kit home on this machine.
	KindHome Kind = "home"
	// KindURL is a server served at an HTTP(S) URL, reached with a device
	// token.
	KindURL Kind = "url"
	// KindSSH is the server of the default Kit home on a host reached with
	// the system ssh executable.
	KindSSH Kind = "ssh"
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
	// Destination is what ssh connects to, for KindSSH: [user@]host, an
	// alias from the SSH configuration, or an ssh:// URL.
	Destination string `json:"destination,omitempty"`
}

// Location is the server's home, URL, or SSH destination, for display.
func (s Server) Location() string {
	switch s.Kind {
	case KindURL:
		return s.URL
	case KindSSH:
		return s.Destination
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
	Servers []json.RawMessage `json:"servers"`
}

// saved is the file's contents: the servers this version reaches, and
// entries of kinds it does not know, which a newer version saved and which
// are kept unchanged. raw keeps each server's entry as read, so fields a
// newer version added survive changes to other entries.
type saved struct {
	servers []Server
	others  []other
	raw     map[string]json.RawMessage
}

type other struct {
	name string
	raw  json.RawMessage
}

func (f saved) has(name string) bool {
	for _, server := range f.servers {
		if server.Name == name {
			return true
		}
	}
	for _, entry := range f.others {
		if entry.name == name {
			return true
		}
	}
	return false
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
		if canonical, err := CanonicalURL(s.URL); err != nil || canonical != s.URL || s.Home != "" || s.Destination != "" {
			return fmt.Errorf("server %q URL must be a canonical HTTP(S) origin: %q", s.Name, s.URL)
		}
	case KindSSH:
		if err := sshtunnel.ValidateDestination(s.Destination); err != nil || s.Home != "" || s.URL != "" {
			return fmt.Errorf("server %q SSH destination is invalid: %q", s.Name, s.Destination)
		}
	default:
		return fmt.Errorf("server %q kind %q is not home, url, or ssh", s.Name, s.Kind)
	}
	if s.Kind == KindHome && s.Destination != "" {
		return fmt.Errorf("server %q home must be a clean absolute path: %q", s.Name, s.Home)
	}
	return nil
}

func knownKind(kind Kind) bool {
	return kind == KindHome || kind == KindURL || kind == KindSSH
}

// List returns the saved servers sorted by name. A missing file lists none.
func (s *Store) List() ([]Server, error) {
	file, err := s.read()
	if err != nil {
		return nil, err
	}
	return file.servers, nil
}

// Find returns the server saved under name.
func (s *Store) Find(name string) (Server, error) {
	file, err := s.read()
	if err != nil {
		return Server{}, err
	}
	for _, server := range file.servers {
		if server.Name == name {
			return server, nil
		}
	}
	for _, entry := range file.others {
		if entry.name == name {
			return Server{}, fmt.Errorf("%q: %w", name, ErrUnsupported)
		}
	}
	return Server{}, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// FindURL returns the server saved with url, an origin from CanonicalURL.
func (s *Store) FindURL(url string) (Server, error) {
	file, err := s.read()
	if err != nil {
		return Server{}, err
	}
	for _, server := range file.servers {
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
	return s.mutate(func(file saved) (saved, error) {
		if file.has(server.Name) {
			return saved{}, fmt.Errorf("%w: %q", ErrExists, server.Name)
		}
		file.servers = append(file.servers, server)
		return file, nil
	})
}

// Remove forgets the server saved under name.
func (s *Store) Remove(name string) error {
	return s.mutate(func(file saved) (saved, error) {
		for index, server := range file.servers {
			if server.Name == name {
				file.servers = slices.Delete(file.servers, index, index+1)
				return file, nil
			}
		}
		for index, entry := range file.others {
			if entry.name == name {
				file.others = slices.Delete(file.others, index, index+1)
				return file, nil
			}
		}
		return saved{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	})
}

func (s *Store) mutate(change func(saved) (saved, error)) error {
	unlock, err := lockFile(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	file, err := s.read()
	if err != nil {
		return err
	}
	file, err = change(file)
	if err != nil {
		return err
	}
	return s.write(file)
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

func (s *Store) read() (saved, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return saved{}, nil
	}
	if err != nil {
		return saved{}, fmt.Errorf("read saved servers %q: %w", s.path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return saved{}, fmt.Errorf("read saved servers %q: %w", s.path, err)
	}
	if len(data) > maxFileSize {
		return saved{}, fmt.Errorf("saved servers %q exceeds %d bytes", s.path, maxFileSize)
	}
	var decoded document
	if err := json.Unmarshal(data, &decoded); err != nil {
		return saved{}, fmt.Errorf("decode saved servers %q: %w", s.path, err)
	}
	result := saved{raw: map[string]json.RawMessage{}}
	for _, raw := range decoded.Servers {
		var server Server
		if err := json.Unmarshal(raw, &server); err != nil {
			return saved{}, fmt.Errorf("decode saved servers %q: %w", s.path, err)
		}
		// Entries saved before servers had kinds are Kit homes.
		if server.Kind == "" && server.Home != "" {
			server.Kind = KindHome
		}
		if result.has(server.Name) {
			return saved{}, fmt.Errorf("saved servers %q: server %q is listed twice", s.path, server.Name)
		}
		if !knownKind(server.Kind) && server.Kind != "" {
			if err := ValidateName(server.Name); err != nil {
				return saved{}, fmt.Errorf("saved servers %q: %w", s.path, err)
			}
			result.others = append(result.others, other{name: server.Name, raw: raw})
			continue
		}
		if err := server.validate(); err != nil {
			return saved{}, fmt.Errorf("saved servers %q: %w", s.path, err)
		}
		result.servers = append(result.servers, server)
		result.raw[server.Name] = raw
	}
	slices.SortFunc(result.servers, func(a, b Server) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (s *Store) write(file saved) error {
	type entry struct {
		name string
		raw  json.RawMessage
	}
	entries := make([]entry, 0, len(file.servers)+len(file.others))
	for _, server := range file.servers {
		raw, kept := file.raw[server.Name]
		if !kept {
			var err error
			if raw, err = json.Marshal(server); err != nil {
				return fmt.Errorf("encode saved servers: %w", err)
			}
		}
		entries = append(entries, entry{name: server.Name, raw: raw})
	}
	for _, kept := range file.others {
		entries = append(entries, entry{name: kept.name, raw: kept.raw})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	servers := make([]json.RawMessage, len(entries))
	for index, kept := range entries {
		servers[index] = kept.raw
	}
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
