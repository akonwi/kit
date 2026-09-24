//go:build darwin || linux

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/akonwi/kit/internal/childproc"
)

// MaxStderrBytes is the retained diagnostic tail for one plugin process.
const MaxStderrBytes = childproc.MaxStderrBytes

// Process is one supervised plugin process. Plugins use Kit's shared child
// process supervisor; this package owns only manifest-derived launch inputs.
type Process = childproc.Process

// StartProcess launches directly from the installation directory, inherits the
// environment, and gives the child its own process group. Session identity
// never comes from process-global cwd.
func StartProcess(ctx context.Context, installation Installation) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(installation.Root) {
		return nil, errors.New("plugin installation root must be absolute")
	}
	data, err := json.Marshal(installation.Manifest)
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(data, nil)
	if err != nil {
		return nil, err
	}
	executable := manifest.Transport.Command
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		executable = filepath.Join(installation.Root, executable)
	}
	process, err := childproc.Start(ctx, childproc.Spec{
		Path: executable,
		Args: manifest.Transport.Args,
		Dir:  installation.Root,
	})
	if err != nil {
		return nil, fmt.Errorf("launch plugin %q: %w", manifest.ID, err)
	}
	return process, nil
}
