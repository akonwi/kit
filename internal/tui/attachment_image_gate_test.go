package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// recordingUploadSession records each uploaded filename and fails the upload,
// so the staging flow completes with a "Could not attach file" toast.
type recordingUploadSession struct {
	*fakeSession
	uploads chan string
}

func (s recordingUploadSession) UploadAttachment(_ context.Context, filename string, _ io.Reader) (protocol.AttachmentInfo, error) {
	s.uploads <- filename
	return protocol.AttachmentInfo{}, errUploadRecorded
}

var errUploadRecorded = errors.New("upload recorded")

// receiveToasts returns the next count toasts shown through the override.
func receiveToasts(t *testing.T, toasts <-chan toastInput, count int) []toastInput {
	t.Helper()
	received := make([]toastInput, 0, count)
	for range count {
		select {
		case toast := <-toasts:
			received = append(received, toast)
		case <-time.After(time.Second):
			t.Fatalf("received toasts %+v, want %d", received, count)
		}
	}
	return received
}

func (recordingUploadSession) OpenAttachment(context.Context, string) (protocol.AttachmentInfo, io.ReadCloser, error) {
	return protocol.AttachmentInfo{}, nil, context.Canceled
}

var textOnlySession = protocol.SessionInfo{
	ID: "session-1", Name: "Attachments", Model: "opencode-go/kimi-k2.6",
	Inputs: []protocol.ModelInputKind{protocol.ModelInputText},
}

func TestComposerAttachmentRowMarksImagesTheModelCannotReceive(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultTheme()
	for _, test := range []struct {
		name       string
		attachment stagedAttachment
		rejecting  string
		want       string
		wantStyle  ui.Color
	}{
		{
			name:       "image rejected by the model",
			attachment: stagedAttachment{Filename: "shot.png", Info: protocol.AttachmentInfo{ID: "attachment-1", MediaType: "image/png", Size: 68}},
			rejecting:  "Kimi K2.6",
			want:       "attachment shot.png not supported by Kimi K2.6  " + glyphTimes,
			wantStyle:  theme.WarningText,
		},
		{
			name:       "uploading image rejected by the model",
			attachment: stagedAttachment{Filename: "shot.png", Uploading: true, Info: protocol.AttachmentInfo{MediaType: "image/png"}},
			rejecting:  "Kimi K2.6",
			want:       "attachment shot.png not supported by Kimi K2.6  " + glyphTimes,
			wantStyle:  theme.WarningText,
		},
		{
			name:       "text file with a model that rejects images",
			attachment: stagedAttachment{Filename: "notes.txt", Info: protocol.AttachmentInfo{ID: "attachment-2", MediaType: "text/plain", Size: 6}},
			rejecting:  "Kimi K2.6",
			want:       "attachment notes.txt 6 B " + glyphMiddleDot + " text/plain  " + glyphTimes,
			wantStyle:  theme.MutedForeground,
		},
		{
			name:       "image with a model that accepts images",
			attachment: stagedAttachment{Filename: "shot.png", Info: protocol.AttachmentInfo{ID: "attachment-1", MediaType: "image/png", Size: 68}},
			want:       "attachment shot.png 68 B " + glyphMiddleDot + " image/png  " + glyphTimes,
			wantStyle:  theme.MutedForeground,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const width = 60
			application := uitest.New(composerAttachmentRow(theme, test.attachment, 0, test.rejecting, func(ui.EventContext, int) {}))
			application.Pump(width, 1)
			rows := paintedRows(application, width, 1)
			if got := strings.TrimRight(rows[0], " "); got != test.want {
				t.Fatalf("row = %q, want %q", got, test.want)
			}
			// One padding cell precedes the "attachment " label.
			column := 1 + len("attachment ")
			if got := application.Cell(column, 0).Style.Foreground; got != test.wantStyle {
				t.Fatalf("detail foreground = %v, want %v", got, test.wantStyle)
			}
		})
	}
}

func TestComposerMarksStagedImagesFromSessionInputs(t *testing.T) {
	t.Parallel()
	attachments := []stagedAttachment{{
		Filename: "shot.png", Info: protocol.AttachmentInfo{ID: "attachment-1", Filename: "shot.png", MediaType: "image/png", Size: 68},
	}}
	for _, test := range []struct {
		name    string
		session protocol.SessionInfo
		want    string
	}{
		{"model rejects images", textOnlySession, "attachment shot.png not supported by Kimi K2.6"},
		{"model accepts images", protocol.SessionInfo{
			ID: "session-1", Name: "Attachments", Model: "anthropic/claude-haiku-4-5",
			Inputs: []protocol.ModelInputKind{protocol.ModelInputText, protocol.ModelInputImage},
		}, "attachment shot.png 68 B " + glyphMiddleDot + " image/png"},
		{"inputs unknown", protocol.SessionInfo{ID: "session-1", Name: "Attachments", Model: "test/model"}, "attachment shot.png 68 B " + glyphMiddleDot + " image/png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			application := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseReady, Session: test.session, ComposerAttachments: attachments}})
			application.Pump(80, 24)
			findTextCell(t, paintedRows(application, 80, 24), test.want)
		})
	}
}

func TestStagingRefusesImagesTheModelCannotReceive(t *testing.T) {
	directory := t.TempDir()
	image := filepath.Join(directory, "shot.png")
	notes := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(image, []byte("\x89PNG\r\n\x1a\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notes, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toasts := make(chan toastInput, 4)
	uploads := make(chan string, 2)
	state := &sessionMentionAppState{appState: appState{
		phase: phaseReady, session: textOnlySession, attachmentCtx: t.Context(),
		bound:             recordingUploadSession{fakeSession: &fakeSession{id: textOnlySession.ID}, uploads: uploads},
		showToastOverride: func(input toastInput) { toasts <- input },
	}}
	application := uitest.New(sessionMentionAppHarness{state})
	application.Pump(100, 24)

	state.stageAttachments([]string{image, notes}, "describe these")

	want := []toastInput{
		{Title: "Images not attached", Subtitle: "Kimi K2.6 doesn't accept images: shot.png.", Variant: toastWarning},
		{Title: "Could not attach file", Subtitle: errUploadRecorded.Error(), Variant: toastError},
	}
	if got := receiveToasts(t, toasts, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("toasts = %+v, want %+v", got, want)
	}
	close(uploads)
	var uploaded []string
	for filename := range uploads {
		uploaded = append(uploaded, filename)
	}
	if !reflect.DeepEqual(uploaded, []string{"notes.txt"}) {
		t.Fatalf("uploads = %v, want [notes.txt]", uploaded)
	}
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].Filename != "notes.txt" ||
		state.composerAttachments[0].Info.MediaType != "text/plain" || state.composer != "describe these" {
		t.Fatalf("staged = %+v, composer = %q; want only notes.txt staged", state.composerAttachments, state.composer)
	}
}

func TestStagingImagesWhenSessionInputsAreUnknown(t *testing.T) {
	image := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(image, []byte("\x89PNG\r\n\x1a\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}
	toasts := make(chan toastInput, 2)
	state := &sessionMentionAppState{appState: appState{
		phase: phaseReady, session: protocol.SessionInfo{ID: "session-1", Model: "test/model"}, attachmentCtx: t.Context(),
		bound:             recordingUploadSession{fakeSession: &fakeSession{id: "session-1"}, uploads: make(chan string, 1)},
		showToastOverride: func(input toastInput) { toasts <- input },
	}}
	application := uitest.New(sessionMentionAppHarness{state})
	application.Pump(100, 24)

	state.stageAttachments([]string{image}, "")

	want := []toastInput{{Title: "Could not attach file", Subtitle: errUploadRecorded.Error(), Variant: toastError}}
	if got := receiveToasts(t, toasts, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("toasts = %+v, want %+v", got, want)
	}
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].Filename != "shot.png" || state.composerAttachments[0].Info.MediaType != "image/png" {
		t.Fatalf("staged = %+v, want shot.png staged as an image", state.composerAttachments)
	}
}

func TestSubmitBlocksStagedImagesTheModelCannotReceive(t *testing.T) {
	var toasts []toastInput
	staged := []stagedAttachment{{
		Filename: "shot.png", Info: protocol.AttachmentInfo{ID: "attachment-1", Filename: "shot.png", MediaType: "image/png", Size: 68},
	}}
	state := &appState{
		phase: phaseReady, session: textOnlySession, composer: "what is this?",
		composerAttachments: append([]stagedAttachment(nil), staged...),
		bound:               &fakeSession{id: textOnlySession.ID},
		showToastOverride:   func(input toastInput) { toasts = append(toasts, input) },
	}

	state.submit(ui.EventContext{}, "what is this?")

	wantToasts := []toastInput{{Title: "Remove images before sending", Subtitle: "Kimi K2.6 doesn't accept images.", Variant: toastWarning}}
	if !reflect.DeepEqual(toasts, wantToasts) {
		t.Fatalf("toasts = %+v, want %+v", toasts, wantToasts)
	}
	if state.composer != "what is this?" || !reflect.DeepEqual(state.composerAttachments, staged) {
		t.Fatalf("draft = %q with %+v, want it retained", state.composer, state.composerAttachments)
	}
}
