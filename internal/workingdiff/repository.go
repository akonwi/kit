package workingdiff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/akonwi/kit/internal/protocol"
)

type repository struct {
	root, gitdir, common          string
	gitdirID, commonID, objectsID string
	linked                        bool
	authorityDigest               [32]byte
	config                        map[string]string
}
type boundedWriter struct {
	b     bytes.Buffer
	n     int
	limit int
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	if w.n > w.limit {
		return 0, errors.New("git output limit")
	}
	return w.b.Write(p)
}

type gitRunner struct{ exe, home string }

func newGitRunner() (gitRunner, error) {
	exe, err := exec.LookPath("git")
	if err != nil {
		return gitRunner{}, err
	}
	home, err := os.MkdirTemp("", "kit-diff-git-")
	if err != nil {
		return gitRunner{}, err
	}
	if err = os.Chmod(home, 0700); err != nil {
		return gitRunner{}, err
	}
	return gitRunner{exe, home}, nil
}
func (r gitRunner) run(ctx context.Context, repo *repository, input []byte, limit int, args ...string) ([]byte, error) {
	base := []string{"--no-pager", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "core.askPass=", "-c", "diff.external=", "-c", "interactive.diffFilter="}
	if repo != nil {
		base = append(base, "--git-dir="+repo.gitdir, "--work-tree="+repo.root)
	}
	base = append(base, args...)
	cmd := exec.CommandContext(ctx, r.exe, base...)
	cmd.Dir = r.home
	path := os.Getenv("PATH")
	tmp := os.Getenv("TMPDIR")
	cmd.Env = []string{"PATH=" + path, "HOME=" + r.home, "XDG_CONFIG_HOME=" + r.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_PAGER=cat", "PAGER=cat", "LC_ALL=C", "TMPDIR=" + tmp}
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedWriter{limit: limit}
	stderr := &boundedWriter{limit: 32 << 10}
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil, err
	}
	return out.b.Bytes(), nil
}
func metadataIdentity(path string) (string, error) {
	info, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("metadata identity unavailable")
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}

func unsafeMeta(path string) (bool, error) {
	info, err := os.Lstat(filepath.Clean(path))
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return true, nil
	}
	return info.Mode().Perm()&0022 != 0, nil
}
func admitControl(h io.Writer, path string, optional, directory bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && optional {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || directory != info.IsDir() {
		return errors.New("unsafe control kind")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe control authority")
	}
	fmt.Fprintf(h, "%s:%d:%d:%d\x00", path, st.Dev, st.Ino, info.Mode())
	return nil
}

func readSmall(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("file too large")
	}
	return b, nil
}
func (s *Service) discoverRepository(ctx context.Context, root string) (*repository, error) {
	root = filepath.Clean(root)
	dot := filepath.Join(root, ".git")
	info, err := os.Lstat(dot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &Error{Code: NotRepository, Message: "workspace is not a repository"}
	}
	if err != nil {
		return nil, repoUnavailable()
	}
	repo := &repository{root: root, config: map[string]string{}}
	if info.IsDir() {
		repo.gitdir = dot
		repo.common = dot
	} else if info.Mode().IsRegular() {
		data, e := readSmall(dot, 4096)
		if e != nil {
			return nil, unsupported("gitfile")
		}
		line := strings.TrimSuffix(string(data), "\n")
		if strings.Contains(line, "\n") || !strings.HasPrefix(line, "gitdir: ") {
			return nil, unsupported("gitfile")
		}
		target := strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		target = filepath.Clean(target)
		repo.gitdir = target
		commondir, e := readSmall(filepath.Join(target, "commondir"), 4096)
		if e != nil {
			return nil, unsupported("gitfile")
		}
		common := strings.TrimSpace(string(commondir))
		if !filepath.IsAbs(common) {
			common = filepath.Join(target, common)
		}
		repo.common = filepath.Clean(common)
		repo.linked = true
		if filepath.Dir(target) != filepath.Join(repo.common, "worktrees") {
			return nil, unsupported("gitfile")
		}
		back, e := readSmall(filepath.Join(target, "gitdir"), 4096)
		if e != nil || !samePath(strings.TrimSpace(string(back)), dot) {
			return nil, unsupported("gitfile")
		}
	} else {
		return nil, unsupported("gitfile")
	}
	for _, p := range []string{repo.gitdir, repo.common} {
		bad, e := unsafeMeta(p)
		if e != nil || bad {
			return nil, unsupported("metadata_authority")
		}
	}
	repo.gitdirID, err = metadataIdentity(repo.gitdir)
	if err != nil {
		return nil, unsupported("metadata_authority")
	}
	repo.commonID, err = metadataIdentity(repo.common)
	if err != nil {
		return nil, unsupported("metadata_authority")
	}
	objects := filepath.Join(repo.common, "objects")
	if bad, e := unsafeMeta(objects); e != nil || bad {
		return nil, unsupported("metadata_authority")
	}
	repo.objectsID, err = metadataIdentity(objects)
	if err != nil {
		return nil, unsupported("metadata_authority")
	}
	if info, e := os.Lstat(objects); e != nil || !info.IsDir() {
		return nil, unsupported("metadata_authority")
	}
	if alt, err := readSmall(filepath.Join(objects, "info", "alternates"), 4096); err == nil && len(bytes.TrimSpace(alt)) > 0 {
		return nil, unsupported("object_alternates")
	}
	authority := sha256.New()
	controls := []struct {
		path                string
		optional, directory bool
	}{
		{dot, false, info.IsDir()}, {repo.gitdir, false, true}, {repo.common, false, true}, {objects, false, true},
		{filepath.Join(repo.common, "refs"), true, true}, {filepath.Join(repo.gitdir, "HEAD"), false, false},
		{filepath.Join(repo.gitdir, "index"), true, false}, {filepath.Join(repo.gitdir, "commondir"), !repo.linked, false},
		{filepath.Join(repo.gitdir, "gitdir"), !repo.linked, false}, {filepath.Join(repo.common, "packed-refs"), true, false},
		{filepath.Join(repo.common, "shallow"), true, false}, {filepath.Join(repo.common, "info", "exclude"), true, false},
		{filepath.Join(repo.common, "info", "attributes"), true, false},
	}
	for _, control := range controls {
		if err := admitControl(authority, control.path, control.optional, control.directory); err != nil {
			return nil, unsupported("metadata_authority")
		}
	}
	headData, err := readSmall(filepath.Join(repo.gitdir, "HEAD"), 4096)
	if err != nil {
		return nil, unsupported("metadata_authority")
	}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(string(headData)), "ref: "); ok {
		if !strings.HasPrefix(ref, "refs/") || protocol.ValidateWorkspacePath(ref, false) != nil {
			return nil, unsupported("repository_format")
		}
		refPath := filepath.Join(repo.common, filepath.FromSlash(ref))
		for parent := filepath.Dir(refPath); strings.HasPrefix(parent, repo.common) && parent != repo.common; parent = filepath.Dir(parent) {
			if err := admitControl(authority, parent, true, true); err != nil {
				return nil, unsupported("metadata_authority")
			}
		}
		if _, statErr := os.Lstat(refPath); statErr == nil {
			if err := admitControl(authority, refPath, false, false); err != nil {
				return nil, unsupported("metadata_authority")
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, unsupported("metadata_authority")
		}
	}
	configs := []string{filepath.Join(repo.common, "config")}
	if repo.linked {
		configs = append(configs, filepath.Join(repo.gitdir, "config.worktree"))
	}
	for _, p := range configs {
		_, e := readSmall(p, 1<<20)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, unsupported("config_authority")
		}
		bad, e := unsafeMeta(p)
		if e != nil || bad {
			return nil, unsupported("metadata_authority")
		}
		parsed, runErr := s.runner.run(ctx, nil, nil, 1<<20, "config", "--file", p, "--no-includes", "-z", "--list")
		if runErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, unsupported("config_authority")
		}
		if err := parseConfigRecords(repo.config, parsed); err != nil {
			return nil, unsupported("config_authority")
		}
	}
	copy(repo.authorityDigest[:], authority.Sum(nil))
	for k, v := range repo.config {
		if k == "core.bare" || k == "core.filemode" || k == "core.sparsecheckout" || k == "core.sparsecheckoutcone" || k == "extensions.worktreeconfig" || strings.HasSuffix(k, ".promisor") {
			canonical, ok := gitBool(v)
			if !ok {
				return nil, unsupported("config_authority")
			}
			repo.config[k] = canonical
		}
	}
	if repo.config["core.bare"] == "true" {
		return nil, unsupported("bare_repository")
	}
	if _, ok := repo.config["core.worktree"]; ok {
		return nil, unsupported("config_authority")
	}
	for k, v := range repo.config {
		if strings.HasPrefix(k, "include.") || strings.HasPrefix(k, "includeif.") || k == "include.path" {
			return nil, unsupported("config_authority")
		}
		if (strings.HasPrefix(k, "remote.") && strings.HasSuffix(k, ".promisor") && v == "true") || strings.Contains(k, "partialclonefilter") {
			return nil, unsupported("partial_clone")
		}
	}
	for k := range repo.config {
		if strings.HasPrefix(k, "extensions.") && k != "extensions.objectformat" && k != "extensions.worktreeconfig" {
			return nil, unsupported("repository_format")
		}
	}
	format := repo.config["extensions.objectformat"]
	if format != "" && format != "sha1" && format != "sha256" {
		return nil, unsupported("repository_format")
	}
	if v := repo.config["core.repositoryformatversion"]; v != "" && v != "0" && v != "1" {
		return nil, unsupported("repository_format")
	}
	if repo.config["core.sparsecheckout"] == "true" || repo.config["core.sparsecheckoutcone"] == "true" {
		return nil, unsupported("sparse_checkout")
	}
	repo.config = semanticConfig(repo.config)
	return repo, nil
}
func gitBool(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "true", "yes", "on", "1":
		return "true", true
	case "false", "no", "off", "0":
		return "false", true
	default:
		return "", false
	}
}

func parseConfigRecords(dst map[string]string, data []byte) error {
	for _, record := range splitNUL(data) {
		key, value, ok := strings.Cut(string(record), "\n")
		if !ok || key == "" {
			return errors.New("malformed git config output")
		}
		dst[strings.ToLower(key)] = strings.ToLower(value)
	}
	return nil
}
func gitMode(s string) (uint32, error) { v, e := strconv.ParseUint(s, 8, 32); return uint32(v), e }
func unsupported(reason string) error {
	return &Error{Code: UnsupportedRepository, Message: "repository is unsupported", Details: map[string]string{"reason": reason}}
}
func commandFailure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return repoUnavailable()
}
func repoUnavailable() error {
	return &Error{Code: RepositoryUnavailable, Message: "repository is unavailable"}
}
func (r repository) String() string { return fmt.Sprintf("repository(%t)", r.linked) }
