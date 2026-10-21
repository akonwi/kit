package selfupdate

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// HomebrewFormula is the Homebrew formula Kit is installed from.
const HomebrewFormula = "akonwi/tap/kit"

// maxExecutableBytes bounds the executable extracted from a release archive.
const maxExecutableBytes = 512 << 20

// brewStopDelay is how long an interrupted Homebrew has to roll back an
// upgrade before it is killed.
const brewStopDelay = 2 * time.Minute

// Method is how an installed executable is updated.
type Method string

const (
	// MethodHomebrew upgrades Kit's Homebrew formula.
	MethodHomebrew Method = "homebrew"
	// MethodArchive replaces the executable with one from a release archive.
	MethodArchive Method = "archive"
)

// Installation is where the running executable is installed and how it
// updates.
type Installation struct {
	Method Method
	// Executable is the installed executable with symbolic links resolved.
	Executable string
	// Launcher runs the newest installed executable: the formula's link in
	// Homebrew's opt directory, which moves to each new version even when the
	// formula is unlinked, or Executable.
	Launcher string
	// Brew is Homebrew's executable for MethodHomebrew, or empty when it
	// cannot be found.
	Brew string
}

// Locate describes the installation of executable, such as os.Executable's
// result. An executable inside a Homebrew Cellar is a Homebrew installation;
// any other is updated from release archives.
func Locate(executable string) (Installation, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Installation{}, fmt.Errorf("resolve Kit executable: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Installation{}, fmt.Errorf("resolve Kit executable: %w", err)
	}
	if prefix, _, ok := strings.Cut(filepath.ToSlash(resolved), "/Cellar/kit/"); ok {
		prefix = filepath.FromSlash(prefix)
		// The prefix's own brew manages this Cellar; one on PATH is a
		// fallback for an unusual layout.
		brew := filepath.Join(prefix, "bin", "brew")
		if _, err := os.Stat(brew); err != nil {
			brew, _ = exec.LookPath("brew")
		}
		return Installation{
			Method:     MethodHomebrew,
			Executable: resolved,
			Launcher:   filepath.Join(prefix, "opt", "kit", "bin", "kit"),
			Brew:       brew,
		}, nil
	}
	return Installation{Method: MethodArchive, Executable: resolved, Launcher: resolved}, nil
}

// Describe says how release will be installed, for a confirmation prompt.
func (i Installation) Describe(release Release) string {
	if i.Method == MethodHomebrew {
		return "brew upgrade " + HomebrewFormula
	}
	return "replace " + i.Executable + " with " + ArchiveName(release.Tag, runtime.GOOS, runtime.GOARCH)
}

// ArchiveName is the name of the release archive of the executable for a
// platform, as the release workflow publishes it.
func ArchiveName(tag, goos, goarch string) string {
	return "kit_" + tag + "_" + goos + "_" + goarch + ".tar.gz"
}

// Progress reports bytes received out of total, or a total of zero when the
// size is unknown.
type Progress func(received, total int64)

// Installer installs releases of the terminal executable.
type Installer struct {
	// HTTP downloads archives; nil uses a client with a 10 minute timeout.
	HTTP *http.Client
	// Output receives Homebrew's output as it runs; nil keeps it only for
	// reporting a failure.
	Output io.Writer
	// GOOS and GOARCH select the archive; empty uses the running platform.
	GOOS, GOARCH string
}

// Install installs release over installation and verifies that the newly
// installed executable reports the release's version.
func (in Installer) Install(ctx context.Context, installation Installation, release Release, progress Progress) error {
	if progress == nil {
		progress = func(int64, int64) {}
	}
	switch installation.Method {
	case MethodHomebrew:
		if err := in.brewUpgrade(ctx, installation); err != nil {
			return err
		}
	case MethodArchive:
		if err := in.replace(ctx, installation, release, progress); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown installation method %q", installation.Method)
	}
	installed, err := ExecutableVersion(ctx, installation.Launcher)
	if err != nil {
		return err
	}
	if installed.Compare(release.Version) != 0 {
		if installation.Method == MethodHomebrew {
			return fmt.Errorf("Homebrew installed %s, not %s; its tap may not have the release yet", installed, release.Version)
		}
		return fmt.Errorf("installed executable reports %s, not %s", installed, release.Version)
	}
	return nil
}

func (in Installer) brewUpgrade(ctx context.Context, installation Installation) error {
	if installation.Brew == "" {
		return errors.New("Homebrew's brew command was not found")
	}
	command := exec.CommandContext(ctx, installation.Brew, "upgrade", HomebrewFormula)
	// Killing Homebrew while it relinks can leave no kit on PATH. Its own
	// process group keeps a terminal's Ctrl+C from reaching it directly; a
	// cancellation interrupts it once, which it handles by rolling back, and
	// it is killed only if that takes too long.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = brewStopDelay
	var captured tailBuffer
	var output io.Writer = &captured
	if in.Output != nil {
		output = io.MultiWriter(in.Output, &captured)
	}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if last := captured.lastLine(); last != "" {
			return fmt.Errorf("brew upgrade failed: %s", last)
		}
		return fmt.Errorf("brew upgrade failed: %w", err)
	}
	return nil
}

func (in Installer) replace(ctx context.Context, installation Installation, release Release, progress Progress) error {
	goos, goarch := in.GOOS, in.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	name := ArchiveName(release.Tag, goos, goarch)
	asset, ok := release.Asset(name)
	if !ok {
		return fmt.Errorf("release %s has no %s", release.Tag, name)
	}
	if asset.SHA256 == "" {
		return fmt.Errorf("GitHub reports no SHA-256 digest for %s", name)
	}
	directory := filepath.Dir(installation.Executable)
	removeStaleStaging(directory, time.Now())
	// Staging beside the executable makes the final rename atomic, and fails
	// early when the directory is not writable.
	staged, err := os.CreateTemp(directory, stagingPattern)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", directory, err)
	}
	stagedPath := staged.Name()
	published := false
	defer func() {
		_ = staged.Close()
		if !published {
			_ = os.Remove(stagedPath)
		}
	}()

	archive, err := os.CreateTemp("", "kit-archive-*")
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()
	if err := in.download(ctx, asset, archive, progress); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("read download: %w", err)
	}
	if err := extractExecutable(archive, staged); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	if err := staged.Chmod(0o755); err != nil {
		return fmt.Errorf("make executable: %w", err)
	}
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("sync executable: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close executable: %w", err)
	}
	stagedVersion, err := ExecutableVersion(ctx, stagedPath)
	if err != nil {
		return fmt.Errorf("check downloaded executable: %w", err)
	}
	if stagedVersion.Compare(release.Version) != 0 {
		return fmt.Errorf("downloaded executable reports %s, not %s", stagedVersion, release.Version)
	}
	if err := os.Rename(stagedPath, installation.Executable); err != nil {
		return fmt.Errorf("replace %s: %w", installation.Executable, err)
	}
	published = true
	syncDirectory(directory)
	return nil
}

// stagingPattern names an executable being staged beside the installed one.
const stagingPattern = ".kit-update-*"

// staleStaging is how old a staged executable must be before it is taken to
// be left behind by an update that was killed, not one still running.
const staleStaging = time.Hour

// removeStaleStaging removes staged executables in directory that an update
// left behind when it was killed.
func removeStaleStaging(directory string, now time.Time) {
	matches, _ := filepath.Glob(filepath.Join(directory, stagingPattern))
	for _, match := range matches {
		if info, err := os.Lstat(match); err == nil && info.Mode().IsRegular() && now.Sub(info.ModTime()) > staleStaging {
			_ = os.Remove(match)
		}
	}
}

// syncDirectory makes a rename in directory durable where the platform
// allows it.
func syncDirectory(directory string) {
	if handle, err := os.Open(directory); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}

func (in Installer) download(ctx context.Context, asset Asset, destination io.Writer, progress Progress) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	client := in.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: server responded %s", asset.Name, response.Status)
	}
	total := asset.Size
	if total <= 0 {
		total = max(response.ContentLength, 0)
	}
	hash := sha256.New()
	counter := &countingWriter{total: total, progress: progress}
	limit := int64(maxExecutableBytes)
	if total > 0 {
		limit = total + 1
	}
	written, err := io.Copy(io.MultiWriter(destination, hash, counter), io.LimitReader(response.Body, limit))
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	if total > 0 && written != total {
		return fmt.Errorf("download %s: received %d of %d bytes", asset.Name, written, total)
	}
	if digest := hex.EncodeToString(hash.Sum(nil)); digest != asset.SHA256 {
		return fmt.Errorf("%s does not match its SHA-256 digest on GitHub", asset.Name)
	}
	return nil
}

// extractExecutable copies the archive's top-level `kit` file to destination.
func extractExecutable(archive io.Reader, destination io.Writer) error {
	decompressed, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer decompressed.Close()
	reader := tar.NewReader(decompressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("archive has no kit executable")
		}
		if err != nil {
			return err
		}
		if filepath.Clean(header.Name) != "kit" {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return errors.New("archive's kit is not a regular file")
		}
		if header.Size > maxExecutableBytes {
			return errors.New("archive's kit is too large")
		}
		written, err := io.Copy(destination, io.LimitReader(reader, header.Size))
		if err != nil {
			return err
		}
		if written != header.Size {
			return errors.New("archive's kit is truncated")
		}
		return nil
	}
}

// ExecutableVersion runs `executable version` and returns the release
// version it reports.
func ExecutableVersion(ctx context.Context, executable string) (Version, error) {
	checkContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(checkContext, executable, "version")
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return Version{}, fmt.Errorf("run %s version: %w", executable, err)
	}
	first, _, _ := strings.Cut(string(output), "\n")
	fields := strings.Fields(first)
	if len(fields) < 2 || fields[0] != "kit" {
		return Version{}, fmt.Errorf("%s version printed %q", executable, first)
	}
	parsed, ok := ParseVersion(fields[1])
	if !ok {
		return Version{}, fmt.Errorf("%s is not a release build (version %q)", executable, fields[1])
	}
	return parsed, nil
}

type countingWriter struct {
	received, total int64
	progress        Progress
}

func (w *countingWriter) Write(data []byte) (int, error) {
	w.received += int64(len(data))
	w.progress(w.received, w.total)
	return len(data), nil
}

// tailBuffer keeps the end of a command's output for error reports.
type tailBuffer struct{ data []byte }

const tailBufferBytes = 4 << 10

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.data = append(b.data, data...)
	if len(b.data) > tailBufferBytes {
		b.data = append([]byte(nil), b.data[len(b.data)-tailBufferBytes:]...)
	}
	return len(data), nil
}

func (b *tailBuffer) lastLine() string {
	last := ""
	scanner := bufio.NewScanner(bytes.NewReader(b.data))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			last = line
		}
	}
	return last
}
