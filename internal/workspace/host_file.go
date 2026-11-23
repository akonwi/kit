package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	protocol "github.com/akonwi/kit/api/contract"
)

// expandHome returns the absolute, cleaned path a request names.
func (s *Service) expandHome(requested string) (string, error) {
	if requested != "~" && !strings.HasPrefix(requested, "~/") {
		return filepath.Clean(requested), nil
	}
	home, err := s.home()
	if err != nil || !filepath.IsAbs(home) {
		return "", &Error{Code: Unavailable, Message: "the server's home directory is unknown"}
	}
	name := filepath.Clean(filepath.Join(home, strings.TrimPrefix(requested, "~")))
	if protocol.ValidateHostFilePath(name, false) != nil {
		return "", hostInputError(errors.New("host file path is invalid"), name)
	}
	return name, nil
}

func hostInputError(err error, value string) *Error {
	if len(value) > protocol.MaxWorkspacePathBytes {
		return &Error{Code: LimitExceeded, Message: err.Error(), Details: map[string]string{"limit": "path_bytes"}}
	}
	return &Error{Code: InvalidPath, Message: err.Error()}
}

func classifyHost(err error) *Error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &Error{Code: NotFound, Message: "file does not exist"}
	case errors.Is(err, os.ErrPermission):
		return &Error{Code: PermissionDenied, Message: "permission denied"}
	case errors.Is(err, syscall.ENAMETOOLONG):
		return &Error{Code: LimitExceeded, Message: "path is too long", Details: map[string]string{"limit": "path_bytes"}}
	case errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.EISDIR), errors.Is(err, syscall.ELOOP),
		errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENODEV):
		return &Error{Code: NotFile, Message: "path is not a regular file"}
	default:
		return &Error{Code: Unavailable, Message: "the file is temporarily unavailable"}
	}
}
