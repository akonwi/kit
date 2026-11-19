package sessiontool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/droids"
)

// DeleteToolName is the model tool that deletes the caller's child sessions.
const DeleteToolName = "delete_session"

const (
	// maxDeleteRequestIDs bounds the sessions one call names.
	maxDeleteRequestIDs = 20
	// maxDeletedSessions bounds the sessions one approval covers,
	// descendants included.
	maxDeletedSessions = 50
	// maxApprovalDetailBytes matches the session's interaction detail limit.
	maxApprovalDetailBytes = 8 << 10
	maxApprovalNameBytes   = 256
	// maxSessionIDBytes bounds an ID the call names, so a refusal that
	// echoes it stays small. Unknown IDs are refused by the ownership check.
	maxSessionIDBytes = 64
)

// SessionSummary is the stored state of one persistent session that the
// deletion tool reads to check ownership and to describe the session.
type SessionSummary struct {
	ID              string
	Name            string
	CWD             string
	ParentSessionID string
	Persistent      bool
	LastActivity    time.Time
}

// DeletionService owns the server state the deletion tool reads and changes.
type DeletionService interface {
	// ModelSessions lists the persistent sessions.
	ModelSessions(context.Context) ([]SessionSummary, error)
	// DeletionBlocker reports why the session can't be deleted now, or nil.
	DeletionBlocker(context.Context, string) error
	// DeleteModelSession permanently deletes one session, refusing one that
	// has child sessions, including one created while it runs.
	DeleteModelSession(context.Context, string) error
}

type deleteArguments struct {
	SessionIDs []string `json:"sessionIds"`
}

// DeletedSession names one deleted session in the tool's result.
type DeletedSession struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// FailedDeletion is one session the tool did not delete, with the reason.
type FailedDeletion struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error"`
}

// DeleteResponse is the bounded result projected to the model, as both the
// result's text and its details.
type DeleteResponse struct {
	Deleted  []DeletedSession `json:"deleted,omitempty"`
	Failed   []FailedDeletion `json:"failed,omitempty"`
	Declined bool             `json:"declined,omitempty"`
	// Resolution is how the approval ended: answered, negative_confirmation,
	// user_cancelled, run_abort, or unavailable.
	Resolution string `json:"resolution,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// deletionTarget is one session a deletion covers. Depth is 0 for a session
// the call named and grows by one for each generation below it.
type deletionTarget struct {
	SessionSummary
	Depth int
}

// deletionPlan lists the sessions a deletion covers, each requested child
// followed by its descendants, parents before children.
type deletionPlan []deletionTarget

func (s *ToolService) deleteTool(ownerSessionID string) (droids.AnyTool, error) {
	return droids.NewTool(droids.Tool[deleteArguments]{
		Name: DeleteToolName,
		Description: "Permanently delete child sessions of this session (sessions whose parentSessionId is this session; peer_session discover with children=true lists them), along with all of their descendants. " +
			"The user must approve every call; a declined approval returns declined=true and deletes nothing. Sessions that are busy cannot be deleted.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sessionIds": map[string]any{
					"type": "array", "minItems": 1, "maxItems": maxDeleteRequestIDs,
					"items":       map[string]any{"type": "string", "maxLength": maxSessionIDBytes},
					"description": "IDs of this session's child sessions to delete.",
				},
			},
			"required": []string{"sessionIds"}, "additionalProperties": false,
		},
		Mode: droids.ModeSequential,
		// The droid asks before the call runs, so the approval survives a
		// restart; Execute plans again from the stored sessions.
		Confirm: func(ctx context.Context, _ droids.ToolContext, args deleteArguments) (*droids.Confirmation, error) {
			return s.confirmDeletion(ctx, ownerSessionID, args)
		},
		Declined: func(ctx context.Context, _ droids.ToolContext, args deleteArguments, settlement droids.Settlement) droids.ToolResult {
			return deleteResult(s.declinedDeletion(ctx, ownerSessionID, args, settlement))
		},
		Execute: func(ctx context.Context, _ droids.ToolContext, args deleteArguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return deleteResult(s.executeDelete(ctx, ownerSessionID, args)), nil
		},
	})
}

// confirmDeletion writes the approval for a call, or refuses the call
// without asking: the IDs are invalid, none names a child, the sessions don't
// fit one approval, or one of them is busy.
func (s *ToolService) confirmDeletion(ctx context.Context, owner string, args deleteArguments) (*droids.Confirmation, error) {
	ids, err := requestedSessionIDs(args.SessionIDs)
	if err != nil {
		return nil, err
	}
	sessions, err := s.Deletion.ModelSessions(ctx)
	if err != nil {
		return nil, err
	}
	ids, missing := splitNonChildren(owner, ids, sessions)
	plan, err := planDeletion(owner, ids, sessions)
	if err != nil {
		return nil, errors.New("nothing was deleted: " + err.Error())
	}
	if len(plan) == 0 {
		named := make([]string, 0, len(missing))
		for _, failed := range missing {
			named = append(named, failed.ID)
		}
		return nil, fmt.Errorf("nothing was deleted: %s: %s", noChildReason, strings.Join(named, ", "))
	}
	var blocked []string
	for _, target := range plan {
		if err := s.Deletion.DeletionBlocker(ctx, target.ID); err != nil {
			blocked = append(blocked, fmt.Sprintf("%s (%s): %v", displayName(target.Name), target.ID, err))
		}
	}
	if len(blocked) > 0 {
		return nil, errors.New("nothing was deleted: " + strings.Join(blocked, "; "))
	}
	confirmation, err := deletionConfirmation(plan, s.now())
	if err != nil {
		return nil, errors.New("nothing was deleted: " + err.Error())
	}
	return &confirmation, nil
}

// declinedDeletion is the result of a call the user did not approve.
func (s *ToolService) declinedDeletion(ctx context.Context, owner string, args deleteArguments, settlement droids.Settlement) DeleteResponse {
	resolution := approvalResolution(settlement)
	message := "The user declined. Nothing was deleted."
	if resolution != "negative_confirmation" && resolution != "user_cancelled" {
		message = "The approval ended without an answer (" + resolution + "). Nothing was deleted."
	}
	response := DeleteResponse{Declined: true, Resolution: resolution, Message: message}
	if ids, err := requestedSessionIDs(args.SessionIDs); err == nil {
		if sessions, err := s.Deletion.ModelSessions(ctx); err == nil {
			_, response.Failed = splitNonChildren(owner, ids, sessions)
		}
	}
	return response
}

// approvalResolution names how an approval that was not given ended.
func approvalResolution(settlement droids.Settlement) string {
	switch settlement.Status {
	case droids.InteractionAnswered:
		return "negative_confirmation"
	case droids.InteractionDeclined:
		return "user_cancelled"
	case droids.InteractionCancelled:
		return "run_abort"
	default:
		return "unavailable"
	}
}

// executeDelete deletes an approved call's sessions. The approval may be
// from before a restart, so it plans again from the stored parents: only the
// owner's current children and their current descendants are deleted.
func (s *ToolService) executeDelete(ctx context.Context, owner string, args deleteArguments) DeleteResponse {
	ids, err := requestedSessionIDs(args.SessionIDs)
	if err != nil {
		return DeleteResponse{Error: err.Error()}
	}
	sessions, err := s.Deletion.ModelSessions(ctx)
	if err != nil {
		return DeleteResponse{Resolution: "answered", Error: "nothing was deleted: " + err.Error()}
	}
	ids, missing := splitNonChildren(owner, ids, sessions)
	plan, err := planDeletion(owner, ids, sessions)
	if err != nil {
		return DeleteResponse{Failed: missing, Resolution: "answered", Error: "nothing was deleted: " + err.Error()}
	}
	response := s.deletePlan(ctx, plan, missing)
	response.Resolution = "answered"
	return response
}

// noChildReason is the result for a named ID that is not one of the
// caller's children. It is the same whether the ID names no session or one
// the caller doesn't own, so the result doesn't tell them apart.
const noChildReason = "this session has no child with this ID"

// splitNonChildren separates the requested IDs that name no persistent child
// of owner, which the call reports as not deleted without refusing the rest.
func splitNonChildren(owner string, ids []string, sessions []SessionSummary) (children []string, others []FailedDeletion) {
	child := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		if session.Persistent && session.ParentSessionID == owner && session.ID != owner {
			child[session.ID] = true
		}
	}
	for _, id := range ids {
		if child[id] {
			children = append(children, id)
		} else {
			others = append(others, FailedDeletion{ID: id, Error: noChildReason})
		}
	}
	return children, others
}

// deletePlan deletes plan, descendants before their ancestors, and keeps
// the ancestors of any session it could not delete.
func (s *ToolService) deletePlan(ctx context.Context, plan deletionPlan, missing []FailedDeletion) DeleteResponse {
	var response DeleteResponse
	failed := map[string]string{}
	for index := len(plan) - 1; index >= 0; index-- {
		target := plan[index]
		reason := ""
		for _, child := range plan[index+1:] {
			if child.Depth <= target.Depth {
				break
			}
			if _, ok := failed[child.ID]; ok {
				reason = "a session below it was not deleted"
				break
			}
		}
		if reason == "" {
			if err := ctx.Err(); err != nil {
				reason = err.Error()
			} else if err := s.Deletion.DeletionBlocker(ctx, target.ID); err != nil {
				reason = err.Error()
			} else if err := s.Deletion.DeleteModelSession(ctx, target.ID); err != nil {
				reason = err.Error()
			}
		}
		if reason != "" {
			failed[target.ID] = reason
			continue
		}
		response.Deleted = append(response.Deleted, DeletedSession{ID: target.ID, Name: target.Name})
	}
	for _, target := range plan {
		if reason, ok := failed[target.ID]; ok {
			response.Failed = append(response.Failed, FailedDeletion{ID: target.ID, Name: target.Name, Error: reason})
		}
	}
	response.Failed = append(response.Failed, missing...)
	switch {
	case len(response.Failed) == 0:
		response.Message = "Deleted " + sessionCount(len(response.Deleted)) + "."
	case len(response.Deleted) == 0:
		response.Error = "nothing was deleted"
	default:
		response.Message = fmt.Sprintf("Deleted %s; %d not deleted.", sessionCount(len(response.Deleted)), len(response.Failed))
	}
	return response
}

func (s *ToolService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func requestedSessionIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("sessionIds must name at least one session")
	}
	if len(values) > maxDeleteRequestIDs {
		return nil, fmt.Errorf("sessionIds may name at most %d sessions", maxDeleteRequestIDs)
	}
	seen := make(map[string]bool, len(values))
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, errors.New("sessionIds must not contain empty IDs")
		}
		if len(id) > maxSessionIDBytes || !strings.HasPrefix(id, "session_") || strings.ContainsFunc(id, func(r rune) bool {
			return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) {
			return nil, errors.New("sessionIds must contain only session IDs")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// planDeletion resolves the requested children of owner and their
// descendants from stored parent links. Every requested session must be a
// persistent session whose stored parent is owner, and owner itself must be a
// persistent session without a parent.
func planDeletion(owner string, ids []string, sessions []SessionSummary) (deletionPlan, error) {
	byID := make(map[string]SessionSummary, len(sessions))
	children := map[string][]SessionSummary{}
	for _, session := range sessions {
		if !session.Persistent {
			continue
		}
		byID[session.ID] = session
		if session.ParentSessionID != "" {
			children[session.ParentSessionID] = append(children[session.ParentSessionID], session)
		}
	}
	if ownerSession, ok := byID[owner]; !ok || ownerSession.ParentSessionID != "" {
		return nil, errors.New("only a top-level persistent session can delete its child sessions")
	}
	var problems []string
	for _, id := range ids {
		session, ok := byID[id]
		switch {
		case id == owner:
			problems = append(problems, id+" is this session")
		case !ok:
			problems = append(problems, id+" is not a persistent session")
		case session.ParentSessionID != owner:
			problems = append(problems, id+" is not a child of this session")
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			left, right := children[parent][i], children[parent][j]
			if !left.LastActivity.Equal(right.LastActivity) {
				return left.LastActivity.After(right.LastActivity)
			}
			return left.ID < right.ID
		})
	}
	var plan deletionPlan
	seen := map[string]bool{}
	var visit func(SessionSummary, int) error
	visit = func(session SessionSummary, depth int) error {
		if seen[session.ID] {
			return fmt.Errorf("session %s appears twice in the session tree", session.ID)
		}
		seen[session.ID] = true
		if len(plan) == maxDeletedSessions {
			return fmt.Errorf("one call can delete at most %d sessions, descendants included", maxDeletedSessions)
		}
		plan = append(plan, deletionTarget{SessionSummary: session, Depth: depth})
		for _, child := range children[session.ID] {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range ids {
		if err := visit(byID[id], 0); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// deletionConfirmation writes the approval for plan: one line per session,
// indented below its parent.
func deletionConfirmation(plan deletionPlan, now time.Time) (droids.Confirmation, error) {
	title := "Delete " + sessionCount(len(plan)) + "?"
	if len(plan) == 1 {
		title = "Delete “" + displayName(plan[0].Name) + "”?"
	}
	var detail strings.Builder
	for _, target := range plan {
		if target.Depth > 0 {
			detail.WriteString(strings.Repeat("  ", target.Depth-1) + "↳ ")
		}
		detail.WriteString(displayName(target.Name))
		if cwd := safeText(target.CWD); cwd != "" {
			detail.WriteString(" · " + cwd)
		}
		if active := lastActive(target.LastActivity, now); active != "" {
			detail.WriteString(" · " + active)
		}
		detail.WriteString("\n")
	}
	detail.WriteString("\nThis can’t be undone.")
	if detail.Len() > maxApprovalDetailBytes || len(title) > 512 {
		return droids.Confirmation{}, errors.New("these sessions don't fit in one approval; delete fewer at a time")
	}
	return droids.Confirmation{Title: title, Detail: detail.String(), ConfirmLabel: "Delete", CancelLabel: "Keep"}, nil
}

func lastActive(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	elapsed := now.Sub(at)
	plural := func(count int, unit string) string {
		if count == 1 {
			return fmt.Sprintf("active 1 %s ago", unit)
		}
		return fmt.Sprintf("active %d %ss ago", count, unit)
	}
	switch {
	case elapsed < time.Minute:
		return "active just now"
	case elapsed < time.Hour:
		return plural(int(elapsed/time.Minute), "minute")
	case elapsed < 24*time.Hour:
		return plural(int(elapsed/time.Hour), "hour")
	case elapsed < 30*24*time.Hour:
		return plural(int(elapsed/(24*time.Hour)), "day")
	default:
		return "active " + at.Local().Format("Jan 2, 2006")
	}
}

func sessionCount(count int) string {
	if count == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", count)
}

func displayName(name string) string {
	if name = safeText(name); name == "" {
		return "Untitled session"
	}
	if len(name) <= maxApprovalNameBytes {
		return name
	}
	end := maxApprovalNameBytes
	for end > 0 && !utf8.RuneStart(name[end]) {
		end--
	}
	return name[:end] + "…"
}

// safeText puts a stored name or path on one renderer-safe line.
func safeText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
	return strings.Join(strings.Fields(value), " ")
}

func deleteResult(response DeleteResponse) droids.ToolResult {
	raw, err := json.Marshal(response)
	if err != nil {
		result := droids.ToolText(err.Error())
		result.IsError = true
		return result
	}
	result := droids.ToolText(string(raw))
	result.IsError = response.Error != ""
	if details, detailsErr := droids.EncodeDetails(response); detailsErr == nil {
		result.Details = details
	}
	return result
}
