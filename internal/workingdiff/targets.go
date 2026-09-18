package workingdiff

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/akonwi/kit/internal/protocol"
)

const targetReferenceTTL = 5 * time.Minute

type targetReference struct {
	Session   string                `json:"s"`
	Workspace string                `json:"w"`
	Authority string                `json:"a"`
	Kind      string                `json:"k"`
	Base      protocol.DiffEndpoint `json:"b"`
	Head      protocol.DiffEndpoint `json:"h"`
	Expires   int64                 `json:"e"`
}

type commitInfo struct {
	oid, tree, parent, subject string
	committedAt                int64
}

type branchInfo struct{ name, oid string }

func authorityToken(repo *repository) string {
	h := sha256.New()
	for _, value := range []string{repo.root, repo.gitdirID, repo.commonID, repo.objectsID} {
		h.Write([]byte(value))
		h.Write([]byte{0})
	}
	keys := make([]string, 0, len(repo.config))
	for key := range repo.config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		h.Write([]byte(key))
		h.Write([]byte{'='})
		h.Write([]byte(repo.config[key]))
		h.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func targetID(session, workspace string, repo *repository, kind string, base, head protocol.DiffEndpoint) string {
	if kind == protocol.DiffTargetWorkingTree {
		return token("difftarget_", session, workspace, authorityToken(repo), "policy-2", kind)
	}
	return token("difftarget_", session, workspace, authorityToken(repo), "policy-2", kind, base.Kind, base.OID, head.Kind, head.OID)
}

func (s *Service) encodeTargetReference(ref targetReference) string {
	body, _ := json.Marshal(ref)
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	return "difftargetref_" + base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) decodeTargetReference(raw string) (targetReference, error) {
	var ref targetReference
	if len(raw) > protocol.MaxDiffTargetReference || !strings.HasPrefix(raw, "difftargetref_") {
		return ref, &Error{Code: StaleTarget, Message: "diff target reference is invalid"}
	}
	parts := strings.Split(strings.TrimPrefix(raw, "difftargetref_"), ".")
	if len(parts) != 2 {
		return ref, &Error{Code: StaleTarget, Message: "diff target reference is invalid"}
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(body) != parts[0] {
		return ref, &Error{Code: StaleTarget, Message: "diff target reference is invalid"}
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != parts[1] {
		return ref, &Error{Code: StaleTarget, Message: "diff target reference is invalid"}
	}
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(body, &ref) != nil || s.now().Unix() > ref.Expires {
		return targetReference{}, &Error{Code: StaleTarget, Message: "diff target reference is invalid or expired"}
	}
	validEndpoint := func(endpoint protocol.DiffEndpoint) bool {
		return endpoint.Kind == "empty_tree" && endpoint.OID == "" || endpoint.Kind == "commit" && validOID(endpoint.OID)
	}
	if !validEndpoint(ref.Base) || !validEndpoint(ref.Head) {
		return targetReference{}, &Error{Code: StaleTarget, Message: "diff target endpoints are invalid"}
	}
	switch ref.Kind {
	case protocol.DiffTargetWorkingTree:
		if ref.Base != ref.Head {
			return targetReference{}, &Error{Code: StaleTarget, Message: "working-tree endpoints do not match"}
		}
	case protocol.DiffTargetCommit:
		if ref.Head.Kind != "commit" {
			return targetReference{}, &Error{Code: StaleTarget, Message: "commit endpoint is invalid"}
		}
	case protocol.DiffTargetBranch:
		if ref.Base.Kind != "commit" || ref.Head.Kind != "commit" {
			return targetReference{}, &Error{Code: StaleTarget, Message: "branch endpoints are invalid"}
		}
	default:
		return targetReference{}, &Error{Code: StaleTarget, Message: "diff target kind is invalid"}
	}
	return ref, nil
}

// ListTargets snapshots the local repository and returns bounded selectable targets.
func (s *Service) ListTargets(ctx context.Context, session, cwd string, in protocol.ListDiffTargetsInput) (protocol.DiffTargetCatalog, error) {
	if err := in.Validate(); err != nil {
		return protocol.DiffTargetCatalog{}, &Error{Code: InvalidPath, Message: "diff target catalog request is invalid"}
	}
	release, err := s.acquire(ctx, session)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	defer release()
	ref := s.workspaces.Ref(session, cwd)
	if ref.WorkspaceID != in.WorkspaceID {
		return protocol.DiffTargetCatalog{}, &Error{Code: StaleWorkspace, Message: "the session workspace changed", Details: map[string]string{"currentWorkspaceId": ref.WorkspaceID}}
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	if err := s.verifyRepository(ctx, repo); err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	headOID, unborn, err := s.resolveHead(ctx, repo)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	endpoint := protocol.DiffEndpoint{Kind: "empty_tree"}
	if !unborn {
		endpoint = protocol.DiffEndpoint{Kind: "commit", OID: headOID}
	}
	catalog := protocol.DiffTargetCatalog{SessionID: session, WorkspaceID: in.WorkspaceID, Targets: []protocol.DiffTargetEntry{}, Diagnostics: []protocol.DiffTargetDiagnostic{}}
	expires := s.now().Add(targetReferenceTTL).Unix()
	seenTargets := map[string]bool{}
	add := func(kind string, base, head protocol.DiffEndpoint, metadata protocol.DiffTargetMetadata) {
		id := targetID(session, in.WorkspaceID, repo, kind, base, head)
		if seenTargets[id] {
			return
		}
		seenTargets[id] = true
		r := targetReference{Session: session, Workspace: in.WorkspaceID, Authority: authorityToken(repo), Kind: kind, Base: base, Head: head, Expires: expires}
		catalog.Targets = append(catalog.Targets, protocol.DiffTargetEntry{Reference: s.encodeTargetReference(r), TargetID: id, Kind: kind, Base: base, Head: head, Metadata: metadata})
	}
	add(protocol.DiffTargetWorkingTree, endpoint, endpoint, protocol.DiffTargetMetadata{Label: "Working tree"})
	if unborn {
		if err := s.verifyCatalogSnapshot(ctx, repo, headOID, unborn, nil, ""); err != nil {
			return protocol.DiffTargetCatalog{}, err
		}
		return catalog, nil
	}
	branches, current, diagnostics, err := s.listBranches(ctx, repo)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	catalog.Diagnostics = append(catalog.Diagnostics, diagnostics...)
	baseName := chooseBaseBranch(branches, current)
	branchTargets := make([]branchInfo, 0, len(branches))
	for _, branch := range branches {
		if branch.name != baseName {
			branchTargets = append(branchTargets, branch)
		}
	}
	sort.SliceStable(branchTargets, func(i, j int) bool {
		if branchTargets[i].name == current {
			return true
		}
		if branchTargets[j].name == current {
			return false
		}
		return branchTargets[i].name < branchTargets[j].name
	})
	if len(branchTargets) > 20 {
		catalog.Diagnostics = appendDiagnostic(catalog.Diagnostics, "branch_limit", len(branchTargets)-20)
		branchTargets = branchTargets[:20]
	}
	baseOID := ""
	for _, branch := range branches {
		if branch.name == baseName {
			baseOID = branch.oid
		}
	}
	for _, branch := range branchTargets {
		if baseOID == "" || branch.oid == baseOID {
			continue
		}
		mergeBase, err := s.mergeBase(ctx, repo, baseOID, branch.oid)
		if err != nil {
			catalog.Diagnostics = appendDiagnostic(catalog.Diagnostics, "missing_object", 1)
			continue
		}
		base := protocol.DiffEndpoint{Kind: "commit", OID: mergeBase}
		head := protocol.DiffEndpoint{Kind: "commit", OID: branch.oid}
		add(protocol.DiffTargetBranch, base, head, protocol.DiffTargetMetadata{Label: branch.name + " vs " + baseName, RefName: branch.name, BaseRefName: baseName, Abbreviated: abbreviate(branch.oid)})
	}
	commits, err := s.recentCommits(ctx, repo, headOID)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	for _, commit := range commits {
		base := protocol.DiffEndpoint{Kind: "empty_tree"}
		if commit.parent != "" {
			base = protocol.DiffEndpoint{Kind: "commit", OID: commit.parent}
		}
		head := protocol.DiffEndpoint{Kind: "commit", OID: commit.oid}
		add(protocol.DiffTargetCommit, base, head, protocol.DiffTargetMetadata{Label: abbreviate(commit.oid) + "  " + commit.subject, Subject: commit.subject, Abbreviated: abbreviate(commit.oid), CommittedAt: commit.committedAt})
	}
	if err := s.verifyCatalogSnapshot(ctx, repo, headOID, unborn, branches, current); err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	return catalog, nil
}

func (s *Service) verifyCatalogSnapshot(ctx context.Context, repo *repository, head string, unborn bool, branches []branchInfo, current string) error {
	checkHead, checkUnborn, err := s.resolveHead(ctx, repo)
	if err != nil {
		return err
	}
	if checkHead != head || checkUnborn != unborn {
		return &Error{Code: StaleTarget, Message: "repository changed while targets were listed"}
	}
	if unborn {
		return nil
	}
	checkBranches, checkCurrent, _, err := s.listBranches(ctx, repo)
	if err != nil {
		return err
	}
	if checkCurrent != current || len(checkBranches) != len(branches) {
		return &Error{Code: StaleTarget, Message: "repository changed while targets were listed"}
	}
	for i := range branches {
		if branches[i] != checkBranches[i] {
			return &Error{Code: StaleTarget, Message: "repository changed while targets were listed"}
		}
	}
	return nil
}

func appendDiagnostic(in []protocol.DiffTargetDiagnostic, reason string, count int) []protocol.DiffTargetDiagnostic {
	if count < 1 || len(in) >= protocol.MaxDiffTargetDiagnostics {
		return in
	}
	for i := range in {
		if in[i].Reason == reason {
			in[i].Count += count
			return in
		}
	}
	return append(in, protocol.DiffTargetDiagnostic{Reason: reason, Count: count})
}

func (s *Service) resolveHead(ctx context.Context, repo *repository) (string, bool, error) {
	out, err := s.runner.run(ctx, repo, nil, 256, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 128 {
			return "", true, nil
		}
		return "", false, repoUnavailable()
	}
	oid := strings.TrimSpace(string(out))
	if !validOID(oid) {
		return "", false, repoUnavailable()
	}
	return oid, false, nil
}

func (s *Service) listBranches(ctx context.Context, repo *repository) ([]branchInfo, string, []protocol.DiffTargetDiagnostic, error) {
	out, err := s.runner.run(ctx, repo, nil, 1<<20, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(objecttype)%00", "refs/heads/")
	if err != nil {
		return nil, "", nil, commandFailure(ctx)
	}
	parts := bytes.Split(out, []byte{0})
	branches := make([]branchInfo, 0)
	diagnostics := []protocol.DiffTargetDiagnostic{}
	for i := 0; i+2 < len(parts); i += 3 {
		refname, oid, kind := strings.TrimSpace(string(parts[i])), strings.TrimSpace(string(parts[i+1])), strings.TrimSpace(string(parts[i+2]))
		name := strings.TrimPrefix(refname, "refs/heads/")
		if refname == name || protocol.ValidateWorkspacePath(name, false) != nil || !safeDisplayText(name) || !validOID(oid) || kind != "commit" {
			diagnostics = appendDiagnostic(diagnostics, "invalid_ref", 1)
			continue
		}
		branches = append(branches, branchInfo{name: name, oid: oid})
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].name < branches[j].name })
	current := ""
	if sym, e := s.runner.run(ctx, repo, nil, 512, "symbolic-ref", "--quiet", "--short", "HEAD"); e == nil {
		candidate := strings.TrimSpace(string(sym))
		for _, branch := range branches {
			if branch.name == candidate {
				current = candidate
				break
			}
		}
	}
	return branches, current, diagnostics, nil
}

func chooseBaseBranch(branches []branchInfo, current string) string {
	for _, preferred := range []string{"main", "master"} {
		for _, branch := range branches {
			if branch.name == preferred && branch.name != current {
				return branch.name
			}
		}
	}
	for _, branch := range branches {
		if branch.name != current {
			return branch.name
		}
	}
	return ""
}

func (s *Service) mergeBase(ctx context.Context, repo *repository, a, b string) (string, error) {
	out, err := s.runner.run(ctx, repo, nil, 256, "merge-base", a, b)
	if err != nil {
		return "", commandFailure(ctx)
	}
	oid := strings.TrimSpace(string(out))
	if !validOID(oid) {
		return "", repoUnavailable()
	}
	return oid, nil
}

func (s *Service) recentCommits(ctx context.Context, repo *repository, head string) ([]commitInfo, error) {
	out, err := s.runner.run(ctx, repo, nil, 4096, "rev-list", "--max-count=20", "--date-order", head)
	if err != nil {
		return nil, commandFailure(ctx)
	}
	lines := strings.Fields(string(out))
	result := make([]commitInfo, 0, len(lines))
	for _, oid := range lines {
		if !validOID(oid) {
			return nil, repoUnavailable()
		}
		raw, err := s.runner.run(ctx, repo, nil, 64<<10, "cat-file", "commit", oid)
		if err != nil {
			return nil, commandFailure(ctx)
		}
		info, ok := parseCommit(oid, raw)
		if !ok {
			return nil, repoUnavailable()
		}
		result = append(result, info)
	}
	return result, nil
}

func parseCommit(oid string, raw []byte) (commitInfo, bool) {
	header, message, ok := bytes.Cut(raw, []byte("\n\n"))
	if !ok {
		return commitInfo{}, false
	}
	info := commitInfo{oid: oid}
	for _, line := range strings.Split(string(header), "\n") {
		if strings.HasPrefix(line, "tree ") {
			info.tree = strings.TrimPrefix(line, "tree ")
		}
		if strings.HasPrefix(line, "parent ") && info.parent == "" {
			info.parent = strings.TrimPrefix(line, "parent ")
		}
		if strings.HasPrefix(line, "committer ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				info.committedAt, _ = strconv.ParseInt(fields[len(fields)-2], 10, 64)
			}
		}
	}
	if !validOID(info.tree) || info.parent != "" && !validOID(info.parent) {
		return commitInfo{}, false
	}
	info.subject = strings.TrimSpace(strings.SplitN(string(message), "\n", 2)[0])
	info.subject = strings.ToValidUTF8(info.subject, "�")
	info.subject = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, info.subject)
	if len(info.subject) > 256 {
		info.subject = info.subject[:256]
		for !strings.HasSuffix(strings.ToValidUTF8(info.subject, ""), info.subject) {
			info.subject = info.subject[:len(info.subject)-1]
		}
	}
	if info.subject == "" || info.committedAt < 0 {
		return commitInfo{}, false
	}
	return info, true
}

func safeDisplayText(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func abbreviate(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	return oid
}

// ObserveTarget verifies a server-issued reference and observes its pinned evidence.
func (s *Service) ObserveTarget(ctx context.Context, session, cwd string, in protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	if err := in.Validate(); err != nil {
		return protocol.DiffPage{}, &Error{Code: InvalidPath, Message: "diff observation request is invalid"}
	}
	ref, err := s.decodeTargetReference(in.TargetReference)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if ref.Session != session || ref.Workspace != in.WorkspaceID {
		return protocol.DiffPage{}, &Error{Code: StaleWorkspace, Message: "diff target reference belongs to another workspace"}
	}
	if in.Cursor != "" {
		return s.listTargetCursor(session, in)
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if authorityToken(repo) != ref.Authority {
		return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "repository authority changed"}
	}
	if ref.Kind == protocol.DiffTargetWorkingTree {
		page, err := s.Observe(ctx, session, cwd, protocol.ObserveWorkingTreeInput{WorkspaceID: in.WorkspaceID, PageSize: in.PageSize})
		if err != nil {
			return protocol.DiffPage{}, err
		}
		observed := protocol.DiffEndpoint{Kind: "empty_tree"}
		if page.Observation.Head.State == "commit" {
			observed = protocol.DiffEndpoint{Kind: "commit", OID: page.Observation.Head.OID}
		}
		if observed != ref.Head {
			return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "working-tree head changed after target listing"}
		}
		s.mu.Lock()
		if held := s.observations[page.Observation.Revision]; held != nil && held.SessionID == session {
			held.Target.Base, held.Target.Head = ref.Base, ref.Head
			page.Observation = held.DiffObservation
		}
		s.mu.Unlock()
		return page, nil
	}
	return s.observeCommitted(ctx, session, cwd, in.WorkspaceID, ref, in.PageSize)
}

func (s *Service) listTargetCursor(session string, in protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	c, err := s.decodeCursor(in.Cursor)
	if err != nil || c.Operation != "list" || c.Session != session || c.Workspace != in.WorkspaceID || c.PageSize != pageSize(in.PageSize) {
		return protocol.DiffPage{}, staleCursor()
	}
	o, ok := s.get(c.Revision, session)
	if !ok || o.Target.ID != c.Target {
		return protocol.DiffPage{}, staleCursor()
	}
	ref, err := s.decodeTargetReference(in.TargetReference)
	if err != nil || ref.Session != session || ref.Workspace != in.WorkspaceID || ref.Authority != authorityToken(o.repo) || targetID(session, in.WorkspaceID, o.repo, ref.Kind, ref.Base, ref.Head) != o.Target.ID {
		return protocol.DiffPage{}, staleCursor()
	}
	return s.listPage(o, c.Offset, c.PageSize, in.Cursor), nil
}
