package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

const attachmentUploadTimeout = 45 * time.Second

func pastedAttachmentPaths(previous, next string) ([]string, string, bool) {
	start, oldEnd, newEnd := changedRange(previous, next)
	if oldEnd != start || newEnd <= start {
		return nil, next, false
	}
	pasted := next[start:newEnd]
	lines := strings.Split(strings.ReplaceAll(pasted, "\r\n", "\n"), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		linePaths, ok := pastedAttachmentPathsInLine(line)
		if !ok {
			return nil, next, false
		}
		paths = append(paths, linePaths...)
	}
	if len(paths) == 0 {
		return nil, next, false
	}
	return paths, next[:start] + next[newEnd:], true
}

func attachmentPathsForComposerChange(previous, next string) ([]string, string, bool) {
	if paths, composer, ok := pastedAttachmentPaths(previous, next); ok {
		return paths, composer, true
	}
	// Some terminals split one bracketed paste or drag-and-drop operation into
	// multiple field updates. On the final update the complete composer is the
	// only reliable attachment candidate.
	return pastedAttachmentPaths("", next)
}

// pastedAttachmentPathsInLine resolves one pasted line to the attachment paths
// it names. A line is either a single path that may contain unescaped spaces or
// a shell-style list of quoted and backslash-escaped paths, which is how
// terminals report drag-and-drop of names containing spaces.
func pastedAttachmentPathsInLine(line string) ([]string, bool) {
	if path, ok := pastedAttachmentPath(line); ok {
		return []string{path}, true
	}
	tokens, ok := splitPastedPathTokens(line)
	if !ok || len(tokens) == 0 {
		return nil, false
	}
	paths := make([]string, 0, len(tokens))
	for _, token := range tokens {
		path, ok := pastedAttachmentPath(token)
		if !ok {
			return nil, false
		}
		paths = append(paths, path)
	}
	return paths, true
}

// splitPastedPathTokens splits a line on unquoted whitespace, honouring single
// quotes, double quotes, and backslash escapes. It reports false when quoting
// is unterminated, because the paste is then not a well-formed path list.
func splitPastedPathTokens(line string) ([]string, bool) {
	var tokens []string
	var token strings.Builder
	quote := rune(0)
	escaped := false
	started := false
	for _, character := range line {
		switch {
		case escaped:
			token.WriteRune(character)
			escaped = false
		case character == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if character == quote {
				quote = 0
				break
			}
			token.WriteRune(character)
		case character == '\'' || character == '"':
			quote = character
			started = true
		case character == ' ' || character == '\t':
			if started {
				tokens = append(tokens, token.String())
				token.Reset()
				started = false
			}
		default:
			token.WriteRune(character)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if started {
		tokens = append(tokens, token.String())
	}
	return tokens, true
}

func pastedAttachmentPath(value string) (string, bool) {
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
	}
	if strings.HasPrefix(value, "file://") {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Host != "" && parsed.Host != "localhost") {
			return "", false
		}
		value = parsed.Path
	}
	if !filepath.IsAbs(value) {
		return "", false
	}
	value = filepath.Clean(value)
	info, err := os.Stat(value)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	file, err := os.Open(value)
	if err != nil {
		return "", false
	}
	header := make([]byte, 512)
	count, _ := file.Read(header)
	_ = file.Close()
	if count == 0 {
		return "", false
	}
	mediaType := strings.Split(http.DetectContentType(header[:count]), ";")[0]
	if mediaType != "text/plain" && mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" && mediaType != "image/webp" {
		return "", false
	}
	return value, true
}

func (s *appState) stageAttachments(paths []string, composer string) {
	s.SetState(s.closeSessionMention)
	attachments, ok := s.bound.(sessionclient.AttachmentSession)
	if !ok {
		s.showToast(toastInput{Title: "Attachments unavailable", Subtitle: fmt.Sprintf("Session client %T cannot upload files.", s.bound), Variant: toastError})
		return
	}
	pending := 0
	for _, item := range s.composerAttachments {
		if item.Info.ID == "" {
			pending++
		}
	}
	if len(s.composerAttachmentIDs)+pending+len(paths) > protocol.MaxAttachmentsPerPrompt {
		s.showToast(toastInput{Title: "Too many attachments", Subtitle: fmt.Sprintf("A prompt supports up to %d files.", protocol.MaxAttachmentsPerPrompt), Variant: toastError})
		return
	}
	tokens := make([]uint64, len(paths))
	s.SetState(func() {
		s.composer = composer
		s.composerCursorEndGeneration++
		s.composerDraftGeneration++
		for index, path := range paths {
			s.attachmentUploadGeneration++
			tokens[index] = s.attachmentUploadGeneration
			s.composerAttachments = append(s.composerAttachments, stagedAttachment{Filename: filepath.Base(path), Uploading: true, Token: tokens[index]})
		}
	})
	ctx, runtime, sessionID := s.attachmentCtx, s.Context().Runtime(), s.session.ID
	for index, path := range paths {
		token := tokens[index]
		go func() {
			file, err := os.Open(path)
			if err == nil {
				defer file.Close()
				requestContext, cancel := context.WithTimeout(ctx, attachmentUploadTimeout)
				defer cancel()
				var info protocol.AttachmentInfo
				info, err = attachments.UploadAttachment(requestContext, filepath.Base(path), file)
				runtime.Dispatch(func() { s.finishAttachmentUpload(sessionID, token, info, err) })
				return
			}
			runtime.Dispatch(func() { s.finishAttachmentUpload(sessionID, token, protocol.AttachmentInfo{}, err) })
		}()
	}
}

func (s *appState) finishAttachmentUpload(sessionID string, token uint64, info protocol.AttachmentInfo, err error) {
	matched := false
	s.SetState(func() {
		items := s.composerAttachments
		if s.session.ID != sessionID {
			items = s.sessionDraftAttachments[sessionID]
		}
		for index := range items {
			item := &items[index]
			if item.Token != token {
				continue
			}
			matched = true
			item.Uploading = false
			if err != nil {
				item.Error = err.Error()
				return
			}
			item.Info = info
			item.Filename = info.Filename
			if s.session.ID != sessionID {
				s.sessionDraftAttachments[sessionID] = items
			}
			return
		}
		if s.session.ID != sessionID {
			s.sessionDraftAttachments[sessionID] = items
		}
	})
	if matched && err != nil {
		s.showToast(toastInput{Title: "Could not attach file", Subtitle: err.Error(), Variant: toastError})
	}
}

func attachmentTranscriptContent(text string, items []stagedAttachment) []protocol.TranscriptContent {
	content := make([]protocol.TranscriptContent, 0, len(items)+1)
	if text != "" {
		content = append(content, protocol.TranscriptContent{Kind: protocol.TranscriptContentText, Text: text})
	}
	for _, item := range items {
		if item.Info.ID == "" {
			continue
		}
		kind := protocol.TranscriptContentFile
		if strings.HasPrefix(item.Info.MediaType, "image/") {
			kind = protocol.TranscriptContentImage
		}
		content = append(content, protocol.TranscriptContent{
			Kind: kind, Filename: item.Filename, MediaType: item.Info.MediaType, AttachmentID: item.Info.ID,
		})
	}
	return content
}

func resolveRestoredAttachments(ctx context.Context, bound any, messages []protocol.PromptInput) map[string]stagedAttachment {
	ids := make([]string, 0)
	seen := make(map[string]struct{})
	for _, message := range messages {
		for _, id := range message.AttachmentIDs {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows := make(map[string]stagedAttachment, len(ids))
	resolver, ok := bound.(sessionclient.AttachmentMetadataSession)
	if !ok {
		for _, id := range ids {
			rows[id] = stagedAttachment{Info: protocol.AttachmentInfo{ID: id}, Filename: "attachment", Error: "Attachment metadata is unavailable", PreserveID: true}
		}
		return rows
	}
	resolution, err := resolver.ResolveAttachments(ctx, ids)
	if err != nil {
		for _, id := range ids {
			rows[id] = stagedAttachment{Info: protocol.AttachmentInfo{ID: id}, Filename: "attachment", Error: "Could not restore attachment metadata: " + err.Error(), PreserveID: true}
		}
		return rows
	}
	for _, info := range resolution.Attachments {
		rows[info.ID] = stagedAttachment{Info: info, Filename: info.Filename}
	}
	for _, id := range resolution.MissingAttachmentIDs {
		rows[id] = stagedAttachment{Info: protocol.AttachmentInfo{ID: id}, Filename: "attachment", Error: "Attachment is unavailable"}
	}
	return rows
}

func (s *appState) composerPromptAttachmentIDs() []string {
	ids := append([]string(nil), s.composerAttachmentIDs...)
	for _, item := range s.composerAttachments {
		if item.Info.ID != "" && (item.Error == "" || item.PreserveID) {
			ids = append(ids, item.Info.ID)
		}
	}
	return ids
}

func (s *appState) removeComposerAttachment(index int) {
	if index < 0 || index >= len(s.composerAttachments) {
		return
	}
	removed := s.composerAttachments[index]
	s.SetState(func() {
		s.composerDraftGeneration++
		s.composerAttachments = append(s.composerAttachments[:index], s.composerAttachments[index+1:]...)
		if removed.Info.ID != "" {
			for idIndex, id := range s.composerAttachmentIDs {
				if id == removed.Info.ID {
					s.composerAttachmentIDs = append(s.composerAttachmentIDs[:idIndex], s.composerAttachmentIDs[idIndex+1:]...)
					break
				}
			}
		}
	})
}
