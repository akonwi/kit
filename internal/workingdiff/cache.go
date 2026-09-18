package workingdiff

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

type cursorRecord struct {
	Session      string `json:"s"`
	Workspace    string `json:"w"`
	Target       string `json:"t"`
	Revision     string `json:"r"`
	Operation    string `json:"o"`
	Path         string `json:"p,omitempty"`
	FileRevision string `json:"f,omitempty"`
	Offset       int    `json:"n"`
	PageSize     int    `json:"z"`
	MaxHunks     int    `json:"h,omitempty"`
	Expires      int64  `json:"e"`
}

func boundPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Service) cursor(c cursorRecord) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(b)
	return "diffcursor_" + base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) decodeCursor(raw string) (cursorRecord, error) {
	var c cursorRecord
	if len(raw) > protocol.MaxDiffCursorBytes || len(raw) < 12 {
		return c, staleCursor()
	}
	p, sig, ok := cutCursor(raw)
	if !ok {
		return c, staleCursor()
	}
	b, e := base64.RawURLEncoding.DecodeString(p)
	if e != nil || base64.RawURLEncoding.EncodeToString(b) != p {
		return c, staleCursor()
	}
	got, e := base64.RawURLEncoding.DecodeString(sig)
	if e != nil || base64.RawURLEncoding.EncodeToString(got) != sig {
		return c, staleCursor()
	}
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write(b)
	if !hmac.Equal(got, mac.Sum(nil)) || json.Unmarshal(b, &c) != nil || s.now().Unix() > c.Expires {
		return c, staleCursor()
	}
	return c, nil
}
func cutCursor(v string) (string, string, bool) {
	if len(v) < 11 || v[:11] != "diffcursor_" {
		return "", "", false
	}
	for i := 11; i < len(v); i++ {
		if v[i] == '.' {
			return v[11:i], v[i+1:], true
		}
	}
	return "", "", false
}
func staleCursor() error { return &Error{Code: StaleCursor, Message: "diff cursor is unavailable"} }
func (s *Service) store(o *observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	s.observations[o.Revision] = o
	for {
		sessionCount, totalCount := 0, len(s.observations)
		var sessionBytes, totalBytes int64
		for _, x := range s.observations {
			totalBytes += x.size
			if x.SessionID == o.SessionID {
				sessionCount++
				sessionBytes += x.size
			}
		}
		if sessionCount <= 2 && sessionBytes <= 64<<20 && totalCount <= 8 && totalBytes <= 256<<20 {
			break
		}
		key := ""
		var at time.Time
		prefer := sessionCount > 2 || sessionBytes > 64<<20
		for k, x := range s.observations {
			if k == o.Revision || prefer && x.SessionID != o.SessionID {
				continue
			}
			if key == "" || x.touched.Before(at) {
				key, at = k, x.touched
			}
		}
		if key == "" {
			delete(s.observations, o.Revision)
			break
		}
		delete(s.observations, key)
	}
}
func (s *Service) evictLocked() {
	now := s.now()
	for k, o := range s.observations {
		if !now.Before(o.expires) {
			delete(s.observations, k)
		}
	}
}
func (s *Service) get(rev, session string) (*observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	o := s.observations[rev]
	if o == nil || o.SessionID != session {
		return nil, false
	}
	o.touched = s.now()
	return o, true
}
func (s *Service) listPage(o *observation, offset, size int, cursor string) protocol.WorkingTreePage {
	end := min(len(o.files), offset+size)
	p := protocol.WorkingTreePage{Observation: o.DiffObservation, Files: make([]protocol.DiffFileSummary, 0, end-offset)}
	for _, f := range o.files[offset:end] {
		p.Files = append(p.Files, f.summary)
	}
	for {
		p.NextCursor = ""
		if end < len(o.files) {
			p.NextCursor = s.cursor(cursorRecord{Session: o.SessionID, Workspace: o.Target.WorkspaceID, Target: o.Target.ID, Revision: o.Revision, Operation: "list", Offset: end, PageSize: size, Expires: o.expires.Unix()})
		}
		encoded, _ := json.Marshal(p)
		if len(encoded) <= protocol.MaxDiffResponseBytes || end == offset {
			break
		}
		end--
		p.Files = p.Files[:len(p.Files)-1]
	}
	return p
}
func (s *Service) listCursor(session string, in protocol.ObserveWorkingTreeInput) (protocol.WorkingTreePage, error) {
	c, e := s.decodeCursor(in.Cursor)
	if e != nil || c.Operation != "list" || c.Session != session || c.Workspace != in.WorkspaceID || c.PageSize != pageSize(in.PageSize) {
		return protocol.WorkingTreePage{}, staleCursor()
	}
	o, ok := s.get(c.Revision, session)
	if !ok || o.Target.ID != c.Target {
		return protocol.WorkingTreePage{}, staleCursor()
	}
	return s.listPage(o, c.Offset, c.PageSize, in.Cursor), nil
}
func (s *Service) ReadFile(ctx context.Context, session, cwd string, in protocol.ReadFileDiffInput) (protocol.FileDiffPage, error) {
	if e := in.Validate(); e != nil {
		return protocol.FileDiffPage{}, &Error{Code: InvalidPath, Message: "file diff request is invalid"}
	}
	release, e := s.acquire(ctx, session)
	if e != nil {
		return protocol.FileDiffPage{}, e
	}
	defer release()
	offset := 0
	cursorFileRevision := ""
	lineLimit := in.PageSize
	if lineLimit == 0 {
		lineLimit = protocol.DefaultDiffLinePageSize
	}
	hunkLimit := in.MaxHunks
	if hunkLimit == 0 {
		hunkLimit = protocol.DefaultDiffHunkPageSize
	}
	if in.Cursor != "" {
		c, e := s.decodeCursor(in.Cursor)
		if e != nil || c.Operation != "file" || c.Session != session || c.Target != in.TargetID || c.Revision != in.TargetRevision || c.Path != boundPath(in.Path) || c.PageSize != lineLimit || c.MaxHunks != hunkLimit {
			return protocol.FileDiffPage{}, staleCursor()
		}
		offset = c.Offset
		cursorFileRevision = c.FileRevision
	}
	o, ok := s.get(in.TargetRevision, session)
	if !ok {
		return protocol.FileDiffPage{}, &Error{Code: StaleTarget, Message: "diff observation is unavailable"}
	}
	if o.Target.ID != in.TargetID || o.Target.WorkspaceID != s.workspaces.Ref(session, cwd).WorkspaceID {
		return protocol.FileDiffPage{}, &Error{Code: StaleWorkspace, Message: "the session workspace changed"}
	}
	i := sort.Search(len(o.files), func(i int) bool { return o.files[i].summary.Path >= in.Path })
	if i == len(o.files) || o.files[i].summary.Path != in.Path {
		return protocol.FileDiffPage{}, &Error{Code: NotFound, Message: "changed file is not in the observation"}
	}
	f := o.files[i]
	if cursorFileRevision != "" && cursorFileRevision != f.summary.FileRevision {
		return protocol.FileDiffPage{}, staleCursor()
	}
	if in.ExpectedFileRevision != "" && in.ExpectedFileRevision != f.summary.FileRevision {
		return protocol.FileDiffPage{}, &Error{Code: StaleFile, Message: "file revision does not match"}
	}
	rediscovered, e := s.discoverRepository(ctx, cwd)
	if e != nil && ctx.Err() != nil {
		return protocol.FileDiffPage{}, ctx.Err()
	}
	if e != nil || rediscovered.gitdirID != o.repo.gitdirID || rediscovered.commonID != o.repo.commonID || rediscovered.objectsID != o.repo.objectsID || o.committed && authorityToken(rediscovered) != authorityToken(o.repo) || !o.committed && rediscovered.authorityDigest != o.repo.authorityDigest {
		return protocol.FileDiffPage{}, &Error{Code: StaleTarget, Message: "repository authority changed"}
	}
	if o.committed {
		ref := targetReference{Kind: o.Target.Kind, Base: o.Target.Base, Head: o.Target.Head}
		if e := s.verifyPinnedTarget(ctx, rediscovered, ref); e != nil {
			return protocol.FileDiffPage{}, e
		}
	} else {
		current, e := s.observeControl(ctx, o.repo)
		if e != nil {
			return protocol.FileDiffPage{}, e
		}
		if current.digest != o.control.digest {
			return protocol.FileDiffPage{}, &Error{Code: StaleTarget, Message: "diff target changed"}
		}
		aggregate := int64(0)
		snap, e := s.workspaces.ObserveDiffPaths(ctx, session, cwd, o.Target.WorkspaceID, o.control.candidates, protocol.MaxDiffFileBytes, &aggregate)
		if e != nil {
			return protocol.FileDiffPage{}, projectWorkspace(e)
		}
		if snap.RootIdentity != o.rootIdentity {
			return protocol.FileDiffPage{}, &Error{Code: StaleWorkspace, Message: "workspace root changed"}
		}
		for _, entry := range snap.Entries {
			old := o.allLive[entry.Path]
			if entry.Path == in.Path {
				if entry.Identity != old.Identity || entry.Digest != old.Digest || entry.Mode != old.Mode || entry.Kind != old.Kind {
					return protocol.FileDiffPage{}, &Error{Code: StaleFile, Message: "requested file changed"}
				}
			} else if entry.Identity != old.Identity || entry.Mode != old.Mode || entry.Kind != old.Kind {
				return protocol.FileDiffPage{}, &Error{Code: StaleTarget, Message: "another target file changed"}
			}
		}
	}
	result := protocol.FileDiffPage{Observation: o.DiffObservation, File: f.summary, Computation: protocol.DiffComputation{State: "complete"}, Hunks: []protocol.DiffHunk{}}
	if f.summary.ContentState != "text" || f.summary.Change == "mode_changed" {
		return result, nil
	}
	oldLines, reason := splitLines(f.old)
	if reason != "" {
		return result, nil
	}
	newLines, _ := splitLines(f.new)
	hunks, e := semanticHunks(ctx, oldLines, newLines)
	if errors.Is(e, errTooComplex) {
		result.Computation = protocol.DiffComputation{State: "too_complex", Reason: "diff_work"}
		return result, nil
	}
	if e != nil {
		return protocol.FileDiffPage{}, e
	}
	flat := 0
	usedLines := 0
	for _, h := range hunks {
		if flat+len(h.Lines) <= offset {
			flat += len(h.Lines)
			continue
		}
		start := max(0, offset-flat)
		take := min(len(h.Lines)-start, lineLimit-usedLines)
		if take <= 0 {
			break
		}
		frag := h
		frag.Lines = append([]protocol.DiffLine(nil), h.Lines[start:start+take]...)
		frag.ContinuedBefore = start > 0
		frag.ContinuedAfter = start+take < len(h.Lines)
		result.Hunks = append(result.Hunks, frag)
		usedLines += take
		flat += len(h.Lines)
		if usedLines >= lineLimit || len(result.Hunks) >= hunkLimit {
			break
		}
	}
	for {
		encoded, _ := json.Marshal(result)
		if len(encoded) <= protocol.MaxDiffResponseBytes || usedLines == 0 {
			break
		}
		last := len(result.Hunks) - 1
		fragment := &result.Hunks[last]
		fragment.Lines = fragment.Lines[:len(fragment.Lines)-1]
		fragment.ContinuedAfter = true
		usedLines--
		if len(fragment.Lines) == 0 {
			result.Hunks = result.Hunks[:last]
		}
	}
	next := offset + usedLines
	total := 0
	for _, h := range hunks {
		total += len(h.Lines)
	}
	if next < total {
		result.NextCursor = s.cursor(cursorRecord{Session: session, Workspace: o.Target.WorkspaceID, Target: o.Target.ID, Revision: o.Revision, Operation: "file", Path: boundPath(in.Path), FileRevision: f.summary.FileRevision, Offset: next, PageSize: lineLimit, MaxHunks: hunkLimit, Expires: o.expires.Unix()})
	}
	return result, nil
}
func projectWorkspace(err error) error {
	var e *workspace.Error
	if !errors.As(err, &e) {
		return err
	}
	switch e.Code {
	case workspace.StaleWorkspace:
		return &Error{Code: StaleWorkspace, Message: "the session workspace changed"}
	case workspace.PermissionDenied, workspace.OutsideWorkspace:
		return &Error{Code: PermissionDenied, Message: "workspace read was denied"}
	case workspace.InvalidPath:
		return &Error{Code: InvalidPath, Message: "workspace path is invalid"}
	case workspace.LimitExceeded:
		limit := "path_bytes"
		if candidate := e.Details["limit"]; candidate == "path_bytes" || candidate == "path_components" || candidate == "path_name_checks" {
			limit = candidate
		}
		return &Error{Code: LimitExceeded, Message: "workspace limit was exceeded", Details: map[string]string{"limit": limit}}
	default:
		return &Error{Code: RepositoryUnavailable, Message: "workspace content is unavailable"}
	}
}
