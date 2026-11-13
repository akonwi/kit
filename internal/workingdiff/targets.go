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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	protocol "github.com/akonwi/kit/api/contract"
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
	parents                    []string
	committedAt                int64
}

// branchInfo is one local branch. upstream is its configured upstream when
// that is a remote-tracking ref (refs/remotes/...), and remote names the
// upstream's remote.
type branchInfo struct{ name, oid, upstream, remote string }

// remoteRef is one remote-tracking ref, named without its refs/remotes/ prefix
// (for example "origin/main").
type remoteRef struct{ name, oid string }

// baseChoice is one branch a branch target may compare against: a local branch
// with its remote-tracking counterpart when one is fetched, or a remote-only
// default base. name is empty for a remote-only base.
type baseChoice struct {
	name, oid string
	remote    remoteRef
}

// refSnapshot is the ref state one catalog is built from. Listing it twice and
// comparing detects a ref update (such as a fetch) while the catalog was built.
type refSnapshot struct {
	branches []branchInfo
	current  string
	choices  []baseChoice
	omitted  int
}

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
func (s *Service) ListTargets(ctx context.Context, session, cwd string, in protocol.ListDiffTargetsInput) (_ protocol.DiffTargetCatalog, err error) {
	defer projectDeadline(ctx, &err)
	if err := in.Validate(); err != nil {
		return protocol.DiffTargetCatalog{}, &Error{Code: InvalidPath, Message: "diff target catalog request is invalid"}
	}
	release, err := s.acquire(ctx, session)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	defer release()
	ctx, cancel := s.boundedContext(ctx)
	defer cancel()
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
		if err := s.verifyCatalogSnapshot(ctx, repo, headOID, unborn, refSnapshot{}); err != nil {
			return protocol.DiffTargetCatalog{}, err
		}
		return catalog, nil
	}
	refs, diagnostics, err := s.listRefs(ctx, repo)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	catalogStorage, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	catalog.Diagnostics = append(catalog.Diagnostics, diagnostics...)
	catalog.Diagnostics = appendDiagnostic(catalog.Diagnostics, "branch_limit", refs.omitted)
	currentOID := ""
	for _, branch := range refs.branches {
		if branch.name == refs.current {
			currentOID = branch.oid
		}
	}
	if currentOID != "" && currentOID == headOID {
		for _, choice := range refs.choices {
			baseName, baseOID, mergeBase, err := s.resolveBaseChoice(ctx, repo, choice, currentOID)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return protocol.DiffTargetCatalog{}, ctxErr
				}
				catalog.Diagnostics = appendDiagnostic(catalog.Diagnostics, "missing_object", 1)
				continue
			}
			if baseOID == currentOID {
				continue
			}
			base := protocol.DiffEndpoint{Kind: "commit", OID: mergeBase}
			head := protocol.DiffEndpoint{Kind: "commit", OID: currentOID}
			add(protocol.DiffTargetBranch, base, head, protocol.DiffTargetMetadata{Label: branchTargetLabel(refs.current, baseName), RefName: refs.current, BaseRefName: baseName, Abbreviated: abbreviate(currentOID)})
		}
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
		add(protocol.DiffTargetCommit, base, head, protocol.DiffTargetMetadata{Label: truncateTargetLabel(abbreviate(commit.oid)+"  "+commit.subject, protocol.MaxDiffTargetLabelBytes), Subject: commit.subject, Abbreviated: abbreviate(commit.oid), CommittedAt: commit.committedAt})
	}
	if err := s.verifyCatalogSnapshot(ctx, repo, headOID, unborn, refs); err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	catalogStorageAfter, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return protocol.DiffTargetCatalog{}, err
	}
	if catalogStorage != catalogStorageAfter {
		return protocol.DiffTargetCatalog{}, &Error{Code: StaleTarget, Message: "object storage changed while targets were listed"}
	}
	return catalog, nil
}

func (s *Service) verifyCatalogSnapshot(ctx context.Context, repo *repository, head string, unborn bool, refs refSnapshot) error {
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
	check, _, err := s.listRefs(ctx, repo)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(check, refs) {
		return &Error{Code: StaleTarget, Message: "repository changed while targets were listed"}
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
	out, err := s.runner.run(ctx, repo, nil, 256, "rev-parse", "--verify", "HEAD")
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

// maxBaseChoices bounds the branches a catalog compares the current branch against.
const maxBaseChoices = 20

// listRefs snapshots the local branches, the current branch, and the bounded
// base choices with their already-fetched remote-tracking counterparts. It
// reads refs/heads and refs/remotes only and never contacts a remote.
func (s *Service) listRefs(ctx context.Context, repo *repository) (refSnapshot, []protocol.DiffTargetDiagnostic, error) {
	branches, current, diagnostics, err := s.listBranches(ctx, repo)
	if err != nil {
		return refSnapshot{}, nil, err
	}
	defaultRemote := ""
	for _, branch := range branches {
		if branch.name == current && branch.upstream != "" {
			defaultRemote = branch.remote
		}
	}
	heads, defaults, err := s.listRemoteDefaults(ctx, repo, defaultRemote, &diagnostics)
	if err != nil {
		return refSnapshot{}, nil, err
	}
	if defaultRemote == "" {
		if _, ok := heads["origin"]; ok {
			defaultRemote = "origin"
		} else if len(heads) == 1 {
			for remote := range heads {
				defaultRemote = remote
			}
		}
	}

	baseName, remoteOnly := chooseBaseBranch(branches, current, defaultRemote, heads, defaults)
	choices := make([]baseChoice, 0, len(branches))
	if remoteOnly.name != "" {
		choices = append(choices, baseChoice{remote: remoteOnly})
	}
	locals := make([]branchInfo, 0, len(branches))
	for _, branch := range branches {
		if branch.name != current {
			locals = append(locals, branch)
		}
	}
	sort.SliceStable(locals, func(i, j int) bool {
		if locals[i].name == baseName {
			return true
		}
		if locals[j].name == baseName {
			return false
		}
		return locals[i].name < locals[j].name
	})
	for _, branch := range locals {
		choices = append(choices, baseChoice{name: branch.name, oid: branch.oid})
	}
	omitted := 0
	if len(choices) > maxBaseChoices {
		omitted = len(choices) - maxBaseChoices
		choices = choices[:maxBaseChoices]
	}

	// Look up the remote-tracking counterpart of each retained local base: its
	// configured upstream, otherwise the same name on the default remote.
	wanted := make([]string, len(choices))
	upstreams := map[string]string{}
	for _, branch := range branches {
		upstreams[branch.name] = branch.upstream
	}
	patterns := make([]string, 0, 2*len(choices))
	for i, choice := range choices {
		if choice.name == "" {
			continue
		}
		if upstream := upstreams[choice.name]; upstream != "" {
			wanted[i] = upstream
			patterns = append(patterns, upstream)
		}
		if defaultRemote != "" {
			fallback := "refs/remotes/" + defaultRemote + "/" + choice.name
			if wanted[i] == "" {
				wanted[i] = fallback
			}
			patterns = append(patterns, fallback)
		}
	}
	if len(patterns) > 0 {
		found, err := s.listRemoteRefs(ctx, repo, patterns, &diagnostics)
		if err != nil {
			return refSnapshot{}, nil, err
		}
		for i, choice := range choices {
			if choice.name == "" {
				continue
			}
			ref, ok := found[wanted[i]]
			if !ok && defaultRemote != "" {
				// A configured upstream that was never fetched falls back to the
				// default remote's branch of the same name.
				ref, ok = found["refs/remotes/"+defaultRemote+"/"+choice.name]
			}
			if ok {
				choices[i].remote = ref
			}
		}
	}
	return refSnapshot{branches: branches, current: current, choices: choices, omitted: omitted}, diagnostics, nil
}

func (s *Service) listBranches(ctx context.Context, repo *repository) ([]branchInfo, string, []protocol.DiffTargetDiagnostic, error) {
	out, err := s.runner.run(ctx, repo, nil, 1<<20, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(upstream:remotename)%00", "refs/heads/")
	if err != nil {
		return nil, "", nil, commandFailure(ctx)
	}
	parts := bytes.Split(out, []byte{0})
	branches := make([]branchInfo, 0)
	diagnostics := []protocol.DiffTargetDiagnostic{}
	for i := 0; i+3 < len(parts); i += 4 {
		refname, oid := strings.TrimSpace(string(parts[i])), strings.TrimSpace(string(parts[i+1]))
		upstream, remote := strings.TrimSpace(string(parts[i+2])), strings.TrimSpace(string(parts[i+3]))
		name := strings.TrimPrefix(refname, "refs/heads/")
		if refname == name || !validRefName(name) || !validOID(oid) {
			diagnostics = appendDiagnostic(diagnostics, "invalid_ref", 1)
			continue
		}
		// Only an upstream on a named remote is a remote-tracking ref; a
		// branch tracking another local branch has no remote counterpart.
		if !strings.HasPrefix(upstream, "refs/remotes/"+remote+"/") || remote == "" || remote == "." || !validRefName(strings.TrimPrefix(upstream, "refs/remotes/")) {
			upstream, remote = "", ""
		}
		branches = append(branches, branchInfo{name: name, oid: oid, upstream: upstream, remote: remote})
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

// listRemoteDefaults reads each remote's HEAD and its main and master
// remote-tracking refs. heads maps a remote to the branch its HEAD names;
// defaults holds the refs by full refname. A ref glob's * does not match "/",
// so tracked, the current branch's upstream remote, is also read by exact
// name when its name contains a slash.
func (s *Service) listRemoteDefaults(ctx context.Context, repo *repository, tracked string, diagnostics *[]protocol.DiffTargetDiagnostic) (map[string]string, map[string]remoteRef, error) {
	args := []string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00", "refs/remotes/*/HEAD", "refs/remotes/*/main", "refs/remotes/*/master"}
	exact := map[string]bool{}
	if strings.Contains(tracked, "/") {
		for _, branch := range []string{"HEAD", "main", "master"} {
			refname := "refs/remotes/" + tracked + "/" + branch
			exact[refname] = true
			args = append(args, refname)
		}
	}
	out, err := s.runner.run(ctx, repo, nil, 1<<20, args...)
	if err != nil {
		return nil, nil, commandFailure(ctx)
	}
	parts := bytes.Split(out, []byte{0})
	heads := map[string]string{}
	defaults := map[string]remoteRef{}
	for i := 0; i+2 < len(parts); i += 3 {
		refname, oid, symref := strings.TrimSpace(string(parts[i])), strings.TrimSpace(string(parts[i+1])), strings.TrimSpace(string(parts[i+2]))
		name := strings.TrimPrefix(refname, "refs/remotes/")
		// An exact name also matches refs below it; keep only the named refs.
		if strings.Count(name, "/") > 1 && !exact[refname] {
			continue
		}
		if remote, ok := strings.CutSuffix(name, "/HEAD"); ok && refname != name {
			target, isRemote := strings.CutPrefix(symref, "refs/remotes/"+remote+"/")
			if remote == "" || !isRemote || !validRefName(target) || !validRefName(remote+"/"+target) {
				*diagnostics = appendDiagnostic(*diagnostics, "invalid_ref", 1)
				continue
			}
			heads[remote] = target
			if validOID(oid) {
				defaults[symref] = remoteRef{name: remote + "/" + target, oid: oid}
			}
			continue
		}
		if refname == name || symref != "" || !validRefName(name) || !validOID(oid) {
			*diagnostics = appendDiagnostic(*diagnostics, "invalid_ref", 1)
			continue
		}
		defaults[refname] = remoteRef{name: name, oid: oid}
	}
	return heads, defaults, nil
}

// listRemoteRefs reads the named remote-tracking refs that exist, by full refname.
func (s *Service) listRemoteRefs(ctx context.Context, repo *repository, refnames []string, diagnostics *[]protocol.DiffTargetDiagnostic) (map[string]remoteRef, error) {
	want := map[string]bool{}
	for _, refname := range refnames {
		want[refname] = true
	}
	args := append([]string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00"}, refnames...)
	out, err := s.runner.run(ctx, repo, nil, 1<<20, args...)
	if err != nil {
		return nil, commandFailure(ctx)
	}
	parts := bytes.Split(out, []byte{0})
	found := map[string]remoteRef{}
	for i := 0; i+2 < len(parts); i += 3 {
		refname, oid, symref := strings.TrimSpace(string(parts[i])), strings.TrimSpace(string(parts[i+1])), strings.TrimSpace(string(parts[i+2]))
		// for-each-ref also matches refs below a pattern; keep exact names.
		if !want[refname] {
			continue
		}
		name := strings.TrimPrefix(refname, "refs/remotes/")
		if refname == name || symref != "" || !validRefName(name) || !validOID(oid) {
			*diagnostics = appendDiagnostic(*diagnostics, "invalid_ref", 1)
			continue
		}
		found[refname] = remoteRef{name: name, oid: oid}
	}
	return found, nil
}

func validRefName(name string) bool {
	return protocol.ValidateWorkspacePath(name, false) == nil && safeDisplayText(name)
}

// chooseBaseBranch picks the default base: main, then master, then the branch
// the default remote's HEAD names, then the first other local branch. A local
// branch wins over the remote-tracking ref of the same name; when only the
// remote-tracking ref exists it is returned as remoteOnly.
func chooseBaseBranch(branches []branchInfo, current, defaultRemote string, heads map[string]string, defaults map[string]remoteRef) (string, remoteRef) {
	preferred := []string{"main", "master"}
	if target := heads[defaultRemote]; target != "" {
		preferred = append(preferred, target)
	}
	for _, name := range preferred {
		if name == current {
			continue
		}
		for _, branch := range branches {
			if branch.name == name {
				return branch.name, remoteRef{}
			}
		}
		if defaultRemote != "" {
			if ref, ok := defaults["refs/remotes/"+defaultRemote+"/"+name]; ok {
				return "", ref
			}
		}
	}
	for _, branch := range branches {
		if branch.name != current {
			return branch.name, remoteRef{}
		}
	}
	return "", remoteRef{}
}

// resolveBaseChoice picks which ref a base choice compares against and
// returns that ref's display name and OID with its merge base with head.
// When a local branch and its remote-tracking ref differ, the ref whose merge
// base is closer to head wins, so the comparison holds only the current
// branch's own changes; when both give the same merge base, the newer ref wins.
func (s *Service) resolveBaseChoice(ctx context.Context, repo *repository, choice baseChoice, head string) (string, string, string, error) {
	if choice.name == "" || choice.remote.name == "" || choice.remote.oid == choice.oid {
		name, oid := choice.name, choice.oid
		if choice.name == "" {
			name, oid = choice.remote.name, choice.remote.oid
		}
		mergeBase, err := s.resolveMergeBase(ctx, repo, oid, head)
		return name, oid, mergeBase, err
	}
	localBase, localErr := s.resolveMergeBase(ctx, repo, choice.oid, head)
	remoteBase, remoteErr := s.resolveMergeBase(ctx, repo, choice.remote.oid, head)
	switch {
	case localErr != nil && remoteErr != nil:
		return "", "", "", localErr
	case remoteErr != nil:
		return choice.name, choice.oid, localBase, nil
	case localErr != nil:
		return choice.remote.name, choice.remote.oid, remoteBase, nil
	}
	useRemote := false
	if localBase != remoteBase {
		remoteCloser, err := s.isAncestor(ctx, repo, localBase, remoteBase)
		if err != nil {
			return "", "", "", err
		}
		localCloser := false
		if !remoteCloser {
			if localCloser, err = s.isAncestor(ctx, repo, remoteBase, localBase); err != nil {
				return "", "", "", err
			}
		}
		useRemote = remoteCloser
		if !remoteCloser && !localCloser {
			// Unrelated merge bases: fall back to the newer ref.
			if useRemote, err = s.isAncestor(ctx, repo, choice.oid, choice.remote.oid); err != nil {
				return "", "", "", err
			}
		}
	} else {
		var err error
		if useRemote, err = s.isAncestor(ctx, repo, choice.oid, choice.remote.oid); err != nil {
			return "", "", "", err
		}
	}
	if useRemote {
		return choice.remote.name, choice.remote.oid, remoteBase, nil
	}
	return choice.name, choice.oid, localBase, nil
}

// isAncestor reports whether commit a is an ancestor of (or equal to) commit b.
func (s *Service) isAncestor(ctx context.Context, repo *repository, a, b string) (bool, error) {
	_, err := s.runner.run(ctx, repo, nil, 256, "merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, commandFailure(ctx)
}

func (s *Service) loadCommitExact(ctx context.Context, repo *repository, oid string) (commitInfo, error) {
	storageBefore, err := validateObjectStorage(repo, []string{oid})
	if err != nil {
		return commitInfo{}, err
	}
	raw, err := s.runner.run(ctx, repo, nil, 64<<10, "cat-file", "commit", oid)
	if err != nil {
		return commitInfo{}, commandFailure(ctx)
	}
	if !matchesObjectOID("commit", raw, oid) {
		return commitInfo{}, repoUnavailable()
	}
	info, ok := parseCommit(oid, raw)
	if !ok {
		return commitInfo{}, repoUnavailable()
	}
	storageAfter, err := validateObjectStorage(repo, []string{oid})
	if err != nil {
		return commitInfo{}, err
	}
	if storageBefore != storageAfter {
		return commitInfo{}, repoUnavailable()
	}
	return info, nil
}

func (s *Service) resolveMergeBase(ctx context.Context, repo *repository, a, b string) (string, error) {
	out, err := s.runner.run(ctx, repo, nil, 256, "merge-base", a, b)
	if err != nil {
		return "", commandFailure(ctx)
	}
	oid := strings.TrimSpace(string(out))
	if !validOID(oid) {
		return "", repoUnavailable()
	}
	if _, err := s.loadCommitExact(ctx, repo, oid); err != nil {
		return "", err
	}
	return oid, nil
}

func (s *Service) mergeBase(ctx context.Context, repo *repository, a, b string) (string, error) {
	storageBefore, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return "", err
	}
	oid, err := s.resolveMergeBase(ctx, repo, a, b)
	if err != nil {
		return "", err
	}
	storageAfter, err := validateObjectStoreAuthority(repo)
	if err != nil {
		return "", err
	}
	if storageBefore != storageAfter {
		return "", repoUnavailable()
	}
	return oid, nil
}

func (s *Service) recentCommits(ctx context.Context, repo *repository, head string) ([]commitInfo, error) {
	first, err := s.loadCommitExact(ctx, repo, head)
	if err != nil {
		return nil, err
	}
	frontier := []commitInfo{first}
	seen := map[string]bool{}
	result := make([]commitInfo, 0, 20)
	for len(frontier) > 0 && len(result) < 20 {
		sort.Slice(frontier, func(i, j int) bool {
			if frontier[i].committedAt != frontier[j].committedAt {
				return frontier[i].committedAt > frontier[j].committedAt
			}
			return frontier[i].oid < frontier[j].oid
		})
		current := frontier[0]
		frontier = frontier[1:]
		if seen[current.oid] {
			continue
		}
		seen[current.oid] = true
		result = append(result, current)
		for _, parent := range current.parents {
			if seen[parent] {
				continue
			}
			info, err := s.loadCommitExact(ctx, repo, parent)
			if err != nil {
				return nil, err
			}
			frontier = append(frontier, info)
		}
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
		if strings.HasPrefix(line, "parent ") {
			parent := strings.TrimPrefix(line, "parent ")
			info.parents = append(info.parents, parent)
			if info.parent == "" {
				info.parent = parent
			}
		}
		if strings.HasPrefix(line, "committer ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				info.committedAt, _ = strconv.ParseInt(fields[len(fields)-2], 10, 64)
			}
		}
	}
	if !validOID(info.tree) {
		return commitInfo{}, false
	}
	for _, parent := range info.parents {
		if !validOID(parent) {
			return commitInfo{}, false
		}
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

// truncateTargetLabel bounds display text in bytes without splitting a UTF-8 rune.
func truncateTargetLabel(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const ellipsis = "…"
	end := 0
	for end < len(value) {
		_, size := utf8.DecodeRuneInString(value[end:])
		if end+size > limit-len(ellipsis) {
			break
		}
		end += size
	}
	return value[:end] + ellipsis
}

func branchTargetLabel(current, base string) string {
	const separator = " vs "
	limit := protocol.MaxDiffTargetLabelBytes
	if len(current)+len(separator)+len(base) <= limit {
		return current + separator + base
	}
	available := limit - len(separator)
	currentLimit := min(len(current), available/2)
	baseLimit := min(len(base), available-currentLimit)
	currentLimit = min(len(current), available-baseLimit)
	return truncateTargetLabel(current, currentLimit) + separator + truncateTargetLabel(base, baseLimit)
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
func (s *Service) ObserveTarget(ctx context.Context, session, cwd string, in protocol.ObserveDiffInput) (_ protocol.DiffPage, err error) {
	defer projectDeadline(ctx, &err)
	if err := in.Validate(); err != nil {
		return protocol.DiffPage{}, &Error{Code: InvalidPath, Message: "diff observation request is invalid"}
	}
	ctx, cancel := s.boundedContext(ctx)
	defer cancel()
	ref, err := s.decodeTargetReference(in.TargetReference)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if ref.Session != session || ref.Workspace != in.WorkspaceID {
		return protocol.DiffPage{}, &Error{Code: StaleWorkspace, Message: "diff target reference belongs to another workspace"}
	}
	if in.Cursor != "" {
		release, err := s.acquire(ctx, session)
		if err != nil {
			return protocol.DiffPage{}, err
		}
		defer release()
		return s.listTargetCursor(session, in)
	}
	repo, err := s.discoverRepository(ctx, cwd)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if authorityToken(repo) != ref.Authority {
		return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "repository authority changed"}
	}
	resolvedTargetID := targetID(session, in.WorkspaceID, repo, ref.Kind, ref.Base, ref.Head)
	if in.ExpectedTargetID != resolvedTargetID {
		return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "diff target identity does not match reference"}
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
		if page.Observation.Target.ID != in.ExpectedTargetID {
			return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "working-tree result identity does not match request"}
		}
		return page, nil
	}
	page, err := s.observeCommitted(ctx, session, cwd, in.WorkspaceID, ref, in.PageSize)
	if err != nil {
		return protocol.DiffPage{}, err
	}
	if page.Observation.Target.ID != in.ExpectedTargetID {
		return protocol.DiffPage{}, &Error{Code: StaleTarget, Message: "committed result identity does not match request"}
	}
	return page, nil
}

func (s *Service) listTargetCursor(session string, in protocol.ObserveDiffInput) (protocol.DiffPage, error) {
	c, err := s.decodeCursor(in.Cursor)
	if err != nil || c.Operation != "list" || c.Session != session || c.Workspace != in.WorkspaceID || c.Revision != in.ExpectedTargetRevision || c.PageSize != pageSize(in.PageSize) {
		return protocol.DiffPage{}, staleCursor()
	}
	o, ok := s.get(c.Revision, session)
	if !ok || o.Target.ID != c.Target {
		return protocol.DiffPage{}, staleCursor()
	}
	ref, err := s.decodeTargetReference(in.TargetReference)
	if err != nil || ref.Session != session || ref.Workspace != in.WorkspaceID || ref.Authority != authorityToken(o.repo) || targetID(session, in.WorkspaceID, o.repo, ref.Kind, ref.Base, ref.Head) != o.Target.ID || in.ExpectedTargetID != o.Target.ID || in.ExpectedTargetRevision != o.Revision {
		return protocol.DiffPage{}, staleCursor()
	}
	return s.listPage(o, c.Offset, c.PageSize, in.Cursor), nil
}
