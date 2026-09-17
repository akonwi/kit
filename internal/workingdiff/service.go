package workingdiff

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

type Error = protocol.DiffError

const (
	InvalidPath           = protocol.DiffErrorInvalidPath
	NotRepository         = protocol.DiffErrorNotRepository
	UnsupportedRepository = protocol.DiffErrorUnsupportedRepository
	StaleWorkspace        = protocol.DiffErrorStaleWorkspace
	StaleTarget           = protocol.DiffErrorStaleTarget
	StaleFile             = protocol.DiffErrorStaleFile
	StaleCursor           = protocol.DiffErrorStaleCursor
	NotFound              = protocol.DiffErrorNotFound
	PermissionDenied      = protocol.DiffErrorPermissionDenied
	LimitExceeded         = protocol.DiffErrorLimit
	CapacityExceeded      = protocol.DiffErrorCapacity
	RepositoryUnavailable = protocol.DiffErrorRepositoryUnavailable
	Unavailable           = protocol.DiffErrorUnavailable
)
const observationTTL = 120 * time.Second

type treeEntry struct {
	mode      uint32
	oid, kind string
}
type indexInfo struct {
	conflict, ita bool
	mode          uint32
	oid           string
}
type control struct {
	head                string
	unborn              bool
	tree                map[string]treeEntry
	index               map[string]indexInfo
	untracked           map[string]bool
	attrs               map[string]map[string]string
	config              map[string]string
	candidates          []string
	digest              [32]byte
	indexSummary        string
	omitted, unexamined int
}
type retainedFile struct {
	summary  protocol.DiffFileSummary
	old, new []byte
	identity string
}
type observation struct {
	protocol.DiffObservation
	repo              *repository
	cwd, rootIdentity string
	control           control
	files             []retainedFile
	allLive           map[string]workspace.DiffEntry
	touched, expires  time.Time
	size              int64
}
type Service struct {
	mu           sync.Mutex
	runner       gitRunner
	workspaces   *workspace.Service
	observations map[string]*observation
	key          [32]byte
	active       chan struct{}
	gates        map[string]chan struct{}
	pending      map[string]int
	now          func() time.Time
}

func NewService(ws *workspace.Service) (*Service, error) {
	r, e := newGitRunner()
	if e != nil {
		return nil, e
	}
	s := &Service{runner: r, workspaces: ws, observations: map[string]*observation{}, active: make(chan struct{}, 8), gates: map[string]chan struct{}{}, pending: map[string]int{}, now: time.Now}
	_, e = rand.Read(s.key[:])
	return s, e
}
func (s *Service) RemoveSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, o := range s.observations {
		if o.SessionID == id {
			delete(s.observations, k)
		}
	}
	delete(s.pending, id)
	delete(s.gates, id)
}
func (s *Service) acquire(ctx context.Context, session string) (func(), error) {
	s.mu.Lock()
	if s.pending[session] >= 16 {
		s.mu.Unlock()
		return nil, &Error{Code: CapacityExceeded, Message: "diff request queue is full", Details: map[string]string{"scope": "session"}}
	}
	total := 0
	for _, n := range s.pending {
		total += n
	}
	if total >= 64 {
		s.mu.Unlock()
		return nil, &Error{Code: CapacityExceeded, Message: "daemon diff request queue is full", Details: map[string]string{"scope": "daemon"}}
	}
	s.pending[session]++
	gate := s.gates[session]
	if gate == nil {
		gate = make(chan struct{}, 4)
		s.gates[session] = gate
	}
	s.mu.Unlock()
	finishPending := func() { s.mu.Lock(); s.pending[session]--; s.mu.Unlock() }
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		finishPending()
		return nil, ctx.Err()
	}
	select {
	case s.active <- struct{}{}:
		finishPending()
		return func() { <-s.active; <-gate }, nil
	case <-ctx.Done():
		<-gate
		finishPending()
		return nil, ctx.Err()
	}
}
func token(prefix string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte("kit-diff-v1\x00"))
	for _, p := range parts {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(len(p)))
		h.Write(b[:])
		h.Write([]byte(p))
	}
	return prefix + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Service) Observe(ctx context.Context, session, cwd string, in protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	if err := in.Validate(); err != nil {
		return protocol.WorkingTreePage{}, &Error{Code: InvalidPath, Message: "working-tree request is invalid"}
	}
	release, err := s.acquire(ctx, session)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	defer release()
	if in.Cursor != "" {
		return s.listCursor(session, in)
	}
	deadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		page, e := s.observeAttempt(ctx, session, cwd, in)
		if e == nil {
			return page, nil
		}
		if !errors.Is(e, errRaced) {
			return protocol.WorkingTreePage{}, e
		}
		last = e
	}
	_ = last
	return protocol.WorkingTreePage{}, &Error{Code: StaleTarget, Message: "repository changed while it was observed"}
}

var errRaced = errors.New("observation raced")

func (s *Service) observeAttempt(ctx context.Context, session, cwd string, in protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	ref := s.workspaces.Ref(session, cwd)
	if ref.WorkspaceID != in.WorkspaceID {
		return protocol.WorkingTreePage{}, &Error{Code: StaleWorkspace, Message: "the session workspace changed", Details: map[string]string{"currentWorkspaceId": ref.WorkspaceID}}
	}
	if ref.State != protocol.WorkspaceReady {
		return protocol.WorkingTreePage{}, &Error{Code: Unavailable, Message: "workspace is unavailable"}
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	if err = s.verifyRepository(ctx, repo); err != nil {
		return protocol.WorkingTreePage{}, err
	}
	c0, err := s.observeControl(ctx, repo)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	c1, err := s.observeControl(ctx, repo)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	if c0.digest != c1.digest {
		return protocol.WorkingTreePage{}, errRaced
	}
	aggregate := int64(0)
	live, err := s.workspaces.ObserveDiffPaths(ctx, session, cwd, in.WorkspaceID, c0.candidates, protocol.MaxDiffFileBytes, &aggregate)
	if err != nil {
		var we *workspace.Error
		if errors.As(err, &we) && we.Code == workspace.StaleFile {
			return protocol.WorkingTreePage{}, errRaced
		}
		return protocol.WorkingTreePage{}, projectWorkspace(err)
	}
	blobs, err := s.readBlobs(ctx, repo, c0.tree)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	c2, err := s.observeControl(ctx, repo)
	if err != nil {
		return protocol.WorkingTreePage{}, err
	}
	if c0.digest != c2.digest {
		return protocol.WorkingTreePage{}, errRaced
	}
	checkAggregate := int64(0)
	post, err := s.workspaces.ObserveDiffPaths(ctx, session, cwd, in.WorkspaceID, c0.candidates, protocol.MaxDiffFileBytes, &checkAggregate)
	if err != nil && ctx.Err() != nil {
		return protocol.WorkingTreePage{}, ctx.Err()
	}
	if err != nil || post.RootIdentity != live.RootIdentity || !sameLiveEntries(live.Entries, post.Entries) {
		return protocol.WorkingTreePage{}, errRaced
	}
	files, all, size, complete, trunc, omissions := s.classify(c0, live, blobs)
	targetID := token("difftarget_", session, in.WorkspaceID, cwd, "working_tree", "policy-1")
	manifest := manifestBytes(session, in.WorkspaceID, targetID, c0, files, live.RootIdentity, complete, trunc, omissions)
	revision := token("diffrev_", string(manifest))
	head := protocol.DiffHead{State: "commit", OID: c0.head}
	if c0.unborn {
		head = protocol.DiffHead{State: "unborn"}
	}
	obs := protocol.DiffObservation{SessionID: session, Target: protocol.DiffTarget{ID: targetID, WorkspaceID: in.WorkspaceID, Kind: "working_tree", RepositoryPath: ""}, Revision: revision, Head: head, IndexSummary: c0.indexSummary, Complete: complete, Truncation: trunc, Omissions: omissions}
	held := &observation{DiffObservation: obs, repo: repo, cwd: cwd, rootIdentity: live.RootIdentity, control: c0, files: files, allLive: all, touched: s.now(), expires: s.now().Add(observationTTL), size: estimateRetained(size, manifest, c0)}
	if held.size > 64<<20 {
		return protocol.WorkingTreePage{}, &Error{Code: CapacityExceeded, Message: "diff observation exceeds cache capacity", Details: map[string]string{"scope": "session"}}
	}
	s.store(held)
	return s.listPage(held, 0, pageSize(in.PageSize), ""), nil
}
func (s *Service) verifyRepository(ctx context.Context, r *repository) error {
	out, e := s.runner.run(ctx, r, nil, 4096, "rev-parse", "--show-toplevel", "--absolute-git-dir", "--git-common-dir", "--is-bare-repository")
	if e != nil {
		return commandFailure(ctx)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 || !samePath(lines[0], r.root) || !samePath(lines[1], r.gitdir) || !samePath(lines[2], r.common) {
		return unsupported("workspace_repository_mismatch")
	}
	if lines[3] != "false" {
		return unsupported("bare_repository")
	}
	return nil
}
func estimateRetained(content int64, manifest []byte, c control) int64 {
	total := content + int64(len(manifest)) + 4096
	for _, p := range c.candidates {
		total += int64(2048 + 8*len(p))
		if t, ok := c.tree[p]; ok {
			total += int64(len(t.oid) + len(t.kind))
		}
		if i, ok := c.index[p]; ok {
			total += int64(len(i.oid))
		}
		for k, v := range c.attrs[p] {
			total += int64(128 + len(k) + len(v))
		}
	}
	for k, v := range c.config {
		total += int64(256 + len(k) + len(v))
	}
	return total
}

func sameLiveEntries(a, b []workspace.DiffEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Kind != b[i].Kind || a[i].Mode != b[i].Mode || a[i].Identity != b[i].Identity || a[i].Digest != b[i].Digest || a[i].Unavailable != b[i].Unavailable || a[i].Limit != b[i].Limit {
			return false
		}
	}
	return true
}

func samePath(a, b string) bool {
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}
func (s *Service) observeControl(ctx context.Context, r *repository) (control, error) {
	fresh, err := s.discoverRepository(ctx, r.root)
	if err != nil && ctx.Err() != nil {
		return control{}, ctx.Err()
	}
	if err != nil || fresh.gitdirID != r.gitdirID || fresh.commonID != r.commonID || fresh.objectsID != r.objectsID || fresh.authorityDigest != r.authorityDigest {
		return control{}, repoUnavailable()
	}
	r = fresh
	c := control{tree: map[string]treeEntry{}, index: map[string]indexInfo{}, untracked: map[string]bool{}, attrs: map[string]map[string]string{}, config: map[string]string{}, indexSummary: "clean"}
	for k, v := range semanticConfig(r.config) {
		c.config[k] = v
	}
	head, e := s.runner.run(ctx, r, nil, 256, "rev-parse", "--verify", "HEAD^{commit}")
	if e != nil {
		if ctx.Err() != nil {
			return c, ctx.Err()
		}
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 128 {
			return c, repoUnavailable()
		}
		if _, rawErr := s.runner.run(ctx, r, nil, 256, "rev-parse", "--verify", "HEAD"); rawErr == nil {
			return c, repoUnavailable()
		} else if ctx.Err() != nil {
			return c, ctx.Err()
		}
		c.unborn = true
	} else {
		c.head = strings.TrimSpace(string(head))
		if !validOID(c.head) {
			return c, commandFailure(ctx)
		}
	}
	if !c.unborn {
		out, e := s.runner.run(ctx, r, nil, 16<<20, "ls-tree", "-r", "-z", "--full-tree", "--format=%(objectmode)%x09%(objecttype)%x09%(objectname)%x09%(path)", c.head)
		if e != nil {
			return c, commandFailure(ctx)
		}
		for _, rec := range splitNUL(out) {
			f := bytes.SplitN(rec, []byte{'\t'}, 4)
			if len(f) != 4 {
				return c, commandFailure(ctx)
			}
			p := string(f[3])
			if protocol.ValidateWorkspacePath(p, false) != nil {
				c.omitted++
				continue
			}
			m, e := gitMode(string(f[0]))
			if e != nil {
				return c, commandFailure(ctx)
			}
			c.tree[p] = treeEntry{m, string(f[2]), string(f[1])}
		}
	}
	idx, e := s.runner.run(ctx, r, nil, 16<<20, "ls-files", "--stage", "-v", "-z")
	if e != nil {
		return c, commandFailure(ctx)
	}
	for _, rec := range splitNUL(idx) {
		tab := bytes.IndexByte(rec, '\t')
		if tab < 0 {
			return c, commandFailure(ctx)
		}
		meta, p := string(rec[:tab]), string(rec[tab+1:])
		if protocol.ValidateWorkspacePath(p, false) != nil {
			c.omitted++
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 4 {
			return c, commandFailure(ctx)
		}
		mode, e := gitMode(f[1])
		if e != nil {
			return c, commandFailure(ctx)
		}
		stage := f[3]
		ii := c.index[p]
		ii.mode, ii.oid = mode, f[2]
		if stage != "0" {
			ii.conflict = true
			c.indexSummary = "conflicted"
		}
		if strings.HasPrefix(f[0], "S") {
			return c, unsupported("sparse_checkout")
		}
		if mode == 040000 {
			return c, unsupported("sparse_index")
		}
		c.index[p] = ii
	}
	itaVisible, e := s.runner.run(ctx, r, nil, 16<<20, "diff", "--cached", "--raw", "-z", "--no-renames", "--abbrev=64", "--no-ext-diff", "--no-textconv", "--ita-visible-in-index")
	if e != nil {
		return c, commandFailure(ctx)
	}
	itaInvisible, e := s.runner.run(ctx, r, nil, 16<<20, "diff", "--cached", "--raw", "-z", "--no-renames", "--abbrev=64", "--no-ext-diff", "--no-textconv", "--ita-invisible-in-index")
	if e != nil {
		return c, commandFailure(ctx)
	}
	visible, ok := rawDiffPaths(itaVisible)
	if !ok {
		return c, commandFailure(ctx)
	}
	invisible, ok := rawDiffPaths(itaInvisible)
	if !ok {
		return c, commandFailure(ctx)
	}
	for p := range visible {
		if !invisible[p] {
			ii := c.index[p]
			ii.ita = true
			c.index[p] = ii
		}
	}
	other, e := s.runner.run(ctx, r, nil, 16<<20, "-c", "core.excludesFile=/dev/null", "ls-files", "--others", "--exclude-standard", "-z")
	if e != nil {
		return c, commandFailure(ctx)
	}
	for _, rec := range splitNUL(other) {
		p := string(rec)
		if strings.HasSuffix(p, "/") && strings.Count(p, "/") >= 1 {
			p = strings.TrimSuffix(p, "/")
		}
		if protocol.ValidateWorkspacePath(p, false) != nil {
			c.omitted++
			continue
		}
		c.untracked[p] = true
	}
	set := map[string]bool{}
	for p := range c.tree {
		set[p] = true
	}
	for p := range c.index {
		set[p] = true
	}
	for p := range c.untracked {
		set[p] = true
	}
	for p := range set {
		c.candidates = append(c.candidates, p)
	}
	sort.Strings(c.candidates)
	if len(c.candidates) > protocol.MaxDiffCandidates {
		c.unexamined = len(c.candidates) - protocol.MaxDiffCandidates
		c.candidates = c.candidates[:protocol.MaxDiffCandidates]
	}
	var input bytes.Buffer
	for _, p := range c.candidates {
		input.WriteString(p)
		input.WriteByte(0)
	}
	attrs, e := s.runner.run(ctx, r, input.Bytes(), 16<<20, "-c", "core.attributesFile=/dev/null", "check-attr", "-z", "--all", "--stdin")
	if e != nil {
		return c, commandFailure(ctx)
	}
	parts := splitNUL(attrs)
	if len(parts)%3 != 0 {
		return c, commandFailure(ctx)
	}
	for i := 0; i < len(parts); i += 3 {
		p, a, v := string(parts[i]), string(parts[i+1]), string(parts[i+2])
		if !set[p] {
			return c, commandFailure(ctx)
		}
		if c.attrs[p] == nil {
			c.attrs[p] = map[string]string{}
		}
		c.attrs[p][a] = v
	}
	if c.indexSummary != "conflicted" {
		for p, i := range c.index {
			t, ok := c.tree[p]
			if !ok || i.mode != t.mode || i.oid != t.oid || i.ita {
				c.indexSummary = "diverged"
				break
			}
		}
		if c.indexSummary == "clean" {
			for p := range c.tree {
				if _, ok := c.index[p]; !ok {
					c.indexSummary = "diverged"
					break
				}
			}
		}
	}
	h := sha256.New()
	h.Write([]byte(c.head))
	h.Write(idx)
	h.Write(other)
	h.Write(itaVisible)
	h.Write(itaInvisible)
	h.Write(attrs)
	h.Write([]byte(r.gitdirID + "\x00" + r.commonID + "\x00" + r.objectsID))
	h.Write(r.authorityDigest[:])
	for _, k := range sortedConfig(c.config) {
		h.Write([]byte(k + "=" + c.config[k] + "\x00"))
	}
	copy(c.digest[:], h.Sum(nil))
	retained := make(map[string]bool, len(c.candidates))
	for _, p := range c.candidates {
		retained[p] = true
	}
	for p := range c.tree {
		if !retained[p] {
			delete(c.tree, p)
		}
	}
	for p := range c.index {
		if !retained[p] {
			delete(c.index, p)
		}
	}
	for p := range c.untracked {
		if !retained[p] {
			delete(c.untracked, p)
		}
	}
	for p := range c.attrs {
		if !retained[p] {
			delete(c.attrs, p)
		}
	}
	return c, nil
}
func semanticConfig(all map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range all {
		if k == "core.filemode" || k == "core.autocrlf" || k == "core.eol" || k == "core.sparsecheckout" || k == "core.sparsecheckoutcone" || k == "extensions.objectformat" || k == "extensions.worktreeconfig" || strings.HasSuffix(k, ".promisor") || strings.Contains(k, "partialclonefilter") {
			out[k] = v
		}
	}
	return out
}

func sortedConfig(m map[string]string) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func validOID(v string) bool {
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func rawDiffPaths(b []byte) (map[string]bool, bool) {
	out := map[string]bool{}
	parts := splitNUL(b)
	if len(parts)%2 != 0 {
		return nil, false
	}
	for i := 0; i < len(parts); i += 2 {
		if len(parts[i]) == 0 || parts[i][0] != ':' {
			return nil, false
		}
		path := string(parts[i+1])
		if protocol.ValidateWorkspacePath(path, false) != nil {
			continue
		}
		out[path] = true
	}
	return out, true
}

func splitNUL(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	if b[len(b)-1] != 0 {
		return [][]byte{b}
	}
	b = b[:len(b)-1]
	return bytes.Split(b, []byte{0})
}
