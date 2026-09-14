package tui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
	_ "golang.org/x/image/webp"
)

const (
	attachmentPreviewColumns = 72
	attachmentPreviewRows    = 12
)

type attachmentPreview struct {
	Attachment protocol.TranscriptContent
	Loader     sessionclient.AttachmentSession
}

func (attachmentPreview) CreateState() ui.State { return &attachmentPreviewState{} }

type attachmentPreviewState struct {
	ui.StateBase
	ctx     context.Context
	cancel  context.CancelFunc
	raster  halfBlockRaster
	info    protocol.AttachmentInfo
	err     error
	openErr error
	loaded  bool
}

func (s *attachmentPreviewState) InitState() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	config := s.Widget().(attachmentPreview)
	runtimeUI := s.Context().Runtime()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		info, reader, err := config.Loader.OpenAttachment(ctx, config.Attachment.AttachmentID)
		if err == nil {
			defer reader.Close()
			var decoded image.Image
			decoded, _, err = image.Decode(reader)
			if err == nil {
				raster := renderHalfBlockImage(decoded, attachmentPreviewColumns, attachmentPreviewRows, color.RGBA{A: 255})
				s.dispatchIfActive(runtimeUI, func() { s.info, s.raster, s.loaded = info, raster, true })
				return
			}
		}
		s.dispatchIfActive(runtimeUI, func() { s.err, s.loaded = err, true })
	}()
}

func (s *attachmentPreviewState) Dispose() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *attachmentPreviewState) dispatchIfActive(runtimeUI ui.Runtime, update func()) {
	runtimeUI.Dispatch(func() {
		if s.ctx.Err() != nil {
			return
		}
		s.SetState(update)
	})
}

func (s *attachmentPreviewState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(attachmentPreview)
	theme := ui.MustDepend[ui.Theme](ctx)
	filename := config.Attachment.Filename
	if s.info.Filename != "" {
		filename = s.info.Filename
	}
	meta := filename
	if s.info.Width > 0 && s.info.Height > 0 {
		meta += fmt.Sprintf(" %s %d×%d", glyphMiddleDot, s.info.Width, s.info.Height)
	}
	var body ui.Widget
	if !s.loaded {
		body = ui.SizedBox{Width: attachmentPreviewColumns, Height: attachmentPreviewRows, Child: ui.Center(ui.Text{Value: "decoding preview…", Style: ui.Style{Foreground: theme.MutedForeground}})}
	} else if s.err != nil || len(s.raster.Rows) == 0 {
		body = ui.Text{Value: "preview unavailable", Style: ui.Style{Foreground: theme.DangerText}}
	} else {
		body = halfBlockRasterWidget(s.raster)
	}
	children := []ui.Widget{body, ui.Text{Value: meta, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}}
	if s.openErr != nil {
		children = append(children, ui.Text{Value: "could not open: " + s.openErr.Error(), Style: ui.Style{Foreground: theme.DangerText}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
	}
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: children})
	if s.loaded && s.err == nil {
		content = mouseActivator{Child: content, OnPressed: func(ui.EventContext) { s.open(config) }}
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: []ui.Widget{content}}
}

func (s *attachmentPreviewState) open(config attachmentPreview) {
	runtimeUI := s.Context().Runtime()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		info, reader, err := config.Loader.OpenAttachment(ctx, config.Attachment.AttachmentID)
		if err == nil {
			defer reader.Close()
			err = materializeAndOpenAttachment(info.Filename, reader)
		}
		if err != nil {
			s.dispatchIfActive(runtimeUI, func() { s.openErr = err })
		}
	}()
}

func materializeAndOpenAttachment(filename string, content io.Reader) error {
	directory := filepath.Join(os.TempDir(), "kit-v2-attachments")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if info, statErr := entry.Info(); statErr == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, "*-"+filepath.Base(filename))
	if err != nil {
		return err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err == nil {
		_, err = io.Copy(file, content)
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return openExternalPath(path)
}

func openExternalPath(path string) error {
	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "linux":
		command = "xdg-open"
	default:
		return fmt.Errorf("opening attachments is unsupported on %s", runtime.GOOS)
	}
	return startExternalCommand(command, path)
}
