package tui

import (
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestToastControllerPreservesPersistentFeedbackAndDismissesByIdentity(t *testing.T) {
	t.Parallel()

	controller := toastController{}
	controller.Show(toastInput{Title: " Persistent ", Variant: toastInfo, Persistent: true})
	controller.Show(toastInput{Title: "Transient", Variant: toastWarning})
	toasts := controller.Snapshot()
	if len(toasts) != 2 || toasts[0].ID != 1 || toasts[1].ID != 2 || !toasts[0].Persistent {
		t.Fatalf("stored toasts = %+v", toasts)
	}
	if toasts[0].Title != "Persistent" {
		t.Fatalf("trimmed toast title = %q", toasts[0].Title)
	}
	if !controller.Dismiss(toasts[1].ID) || controller.Dismiss(999) {
		t.Fatal("toast dismissal result was incorrect")
	}
	if got := controller.Snapshot(); len(got) != 1 || !got[0].Persistent {
		t.Fatalf("toasts after dismiss = %+v", got)
	}
	for index := range toastStackMax {
		controller.Show(toastInput{Title: "Transient " + string(rune('A'+index))})
	}
	bounded := controller.Snapshot()
	if len(bounded) != toastStackMax || !bounded[0].Persistent || bounded[1].Title != "Transient B" {
		t.Fatalf("persistent-aware bounded toasts = %+v", bounded)
	}

	persistentOnly := toastController{}
	for range toastStackMax {
		persistentOnly.Show(toastInput{Title: "Persistent", Persistent: true})
	}
	dropped := persistentOnly.Show(toastInput{Title: "Transient"})
	if dropped.Retained || len(dropped.Evicted) != 1 || dropped.Evicted[0] != dropped.ID {
		t.Fatalf("transient overflow result = %+v", dropped)
	}
}

func TestToastEntryAnimatesOnlyTheFirstTimeItBecomesVisible(t *testing.T) {
	t.Parallel()

	displayed := make(map[uint64]bool)
	if !toastEntryShouldAnimate(displayed, 7, true) {
		t.Fatal("new toast did not animate")
	}
	if toastEntryShouldAnimate(displayed, 7, true) {
		t.Fatal("resurfacing toast replayed its entry animation")
	}
	if toastEntryShouldAnimate(displayed, 8, false) {
		t.Fatal("disabled toast animation ran")
	}
}

func TestVisibleToastRecordsKeepsNewestCardsWithinViewport(t *testing.T) {
	t.Parallel()

	records := []toastRecord{
		{ID: 1, toastInput: toastInput{Title: "Old", Subtitle: "Detail"}},
		{ID: 2, toastInput: toastInput{Title: "Middle", Subtitle: "Detail"}},
		{ID: 3, toastInput: toastInput{Title: "New", Subtitle: "Detail"}},
	}
	visible := visibleToastRecords(records, 10)
	if len(visible) != 2 || visible[0].ID != 2 || visible[1].ID != 3 {
		t.Fatalf("visible short-viewport toasts = %+v", visible)
	}
	if smallest := visibleToastRecords(records, 3); len(smallest) != 1 || smallest[0].ID != 3 {
		t.Fatalf("visible minimum-viewport toasts = %+v", smallest)
	}
}

func TestToastSlidesInFromTheRight(t *testing.T) {
	size := ui.Size{Width: 100, Height: 20}
	backend := &toastAnimationBackend{events: make(chan ui.Event), size: size}
	root := shellView{Snapshot: shellSnapshot{Phase: phaseReady, Toasts: []toastRecord{{
		ID: 1, toastInput: toastInput{Title: strings.Repeat("Sliding toast ", 8), Variant: toastInfo},
	}}}}
	runner := ui.NewRunner(ui.NewApp(root), backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	if err := runner.HandleFrame(now); err != nil {
		t.Fatal(err)
	}
	initial := strings.IndexAny(toastPainterRows(backend.painter)[2], "┌╭")
	if initial < 0 {
		t.Fatal("animated toast was completely hidden on its first frame")
	}
	if err := runner.HandleFrame(now.Add(350 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	settled := strings.IndexAny(toastPainterRows(backend.painter)[2], "┌╭")
	if settled < 0 || initial <= settled {
		t.Fatalf("toast horizontal positions initial=%d settled=%d, want right-to-left entry", initial, settled)
	}
}

func TestToastStackKeepsSettledAnimationWhenOverlaySiblingsChange(t *testing.T) {
	size := ui.Size{Width: 100, Height: 20}
	backend := &toastAnimationBackend{events: make(chan ui.Event), size: size}
	toast := toastStack{Toasts: []toastRecord{{
		ID: 1, toastInput: toastInput{Title: "Stable toast", Variant: toastInfo},
	}}, Animate: true}
	root := func(withSibling bool) ui.Widget {
		entries := make([]ui.OverlayEntry, 0, 2)
		if withSibling {
			entries = append(entries, ui.OverlayEntry{Child: ui.Text{Value: "Another overlay"}})
		}
		entries = append(entries, ui.OverlayEntry{Child: toast})
		return ui.Overlay{Child: ui.SizedBox{Width: size.Width, Height: size.Height}, Entries: entries}
	}
	application := ui.NewApp(root(false))
	runner := ui.NewRunner(application, backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	if err := runner.HandleFrame(now); err != nil {
		t.Fatal(err)
	}
	if err := runner.HandleFrame(now.Add(350 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	settled := strings.Index(toastPainterRows(backend.painter)[3], "Stable toast")
	if settled < 0 {
		t.Fatal("toast did not settle before overlay update")
	}

	application.UpdateRoot(root(true))
	if err := runner.HandleFrame(now.Add(400 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Index(toastPainterRows(backend.painter)[3], "Stable toast"); got != settled {
		t.Fatalf("toast horizontal position after sibling insertion = %d, want settled position %d", got, settled)
	}
}

func TestToastCardBlocksClicksFromReachingUnderlyingContent(t *testing.T) {
	t.Parallel()

	underlyingPressed := false
	application := uitest.New(ui.Overlay{
		Child: mouseActivator{
			Child:     ui.SizedBox{Width: 80, Height: 12},
			OnPressed: func(ui.EventContext) { underlyingPressed = true },
		},
		Entries: []ui.OverlayEntry{{Alignment: ui.TopLeft, Child: toastItem{Toast: toastRecord{
			ID: 1, toastInput: toastInput{Title: "Copied to clipboard", Variant: toastInfo},
		}}}},
	})
	application.Pump(80, 12)
	time.Sleep(350 * time.Millisecond)
	application.Pump(80, 12)
	rows := paintedRows(application, 80, 12)
	row, byteColumn := 1, strings.Index(rows[1], "Copied to clipboard")
	if byteColumn < 0 {
		t.Fatalf("toast not painted:\n%s", strings.Join(rows, "\n"))
	}
	column := len([]rune(rows[row][:byteColumn]))
	application.Click(column, row)
	if underlyingPressed {
		t.Fatalf("toast body click at %d,%d reached underlying content:\n%s", column, row, strings.Join(rows, "\n"))
	}
}

func TestToastStackMatchesTransientOverlayPresentationAndDismisses(t *testing.T) {
	t.Parallel()

	dismissed := uint64(0)
	size := ui.Size{Width: 100, Height: 20}
	backend := &toastAnimationBackend{events: make(chan ui.Event), size: size}
	root := shellView{
		Snapshot: shellSnapshot{Phase: phaseReady, Toasts: []toastRecord{
			{ID: 1, toastInput: toastInput{Title: "Copied to clipboard", Variant: toastInfo}},
			{ID: 2, toastInput: toastInput{Title: "Check setup", Subtitle: "A provider needs attention.", Variant: toastWarning, Persistent: true}},
			{ID: 3, toastInput: toastInput{Title: "Build failed", Subtitle: "The compiler returned an error.", Variant: toastError}},
		}},
		Callbacks: shellCallbacks{DismissToast: func(id uint64) { dismissed = id }},
	}
	runner := ui.NewRunner(ui.NewApp(root), backend, ui.NewFrameScheduler(time.Second/60))
	now := time.Now()
	runner.Start(now)
	if err := runner.HandleFrame(now); err != nil {
		t.Fatal(err)
	}
	if err := runner.HandleFrame(now.Add(350 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runner.HandleFrame(now.Add(700 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	rows := toastPainterRows(backend.painter)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Copied to clipboard", "Check setup", "A provider needs attention.",
		"Build failed", "The compiler returned an error.", glyphTimes,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("toast stack missing %q:\n%s", expected, text)
		}
	}
	firstBorder := strings.IndexAny(rows[2], "┌╭")
	if firstBorder < 60 {
		t.Fatalf("toast did not hug content at the right edge: border=%d\n%s", firstBorder, text)
	}
	titleByte := strings.Index(rows[3], "Copied to clipboard")
	titleCol := len([]rune(rows[3][:max(0, titleByte)]))
	if titleByte < 0 || backend.painter.Cell(titleCol, 3).Foreground != ui.DefaultTheme().AccentText {
		t.Fatalf("info toast title did not use info semantics")
	}
	if strings.Contains(rows[3], glyphTimes) || strings.Contains(rows[10], glyphTimes) {
		t.Fatalf("transient toast exposed explicit dismissal:\n%s", text)
	}
	closeByte := strings.LastIndex(rows[6], glyphTimes)
	if closeByte < 0 {
		t.Fatalf("persistent toast close control not painted: %q", rows[6])
	}
	closeCol := len([]rune(rows[6][:closeByte]))
	runner.HandleEvent(ui.Mouse{Col: closeCol, Row: 6, EventType: ui.EventMotion}, now.Add(700*time.Millisecond))
	if err := runner.HandleFrame(now.Add(716 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if got, want := backend.painter.Cell(closeCol, 6).Background, ui.DefaultTheme().SurfaceHovered; got != want {
		t.Fatalf("toast close hover background = %#v, want %#v", got, want)
	}
	runner.HandleEvent(ui.Mouse{Col: closeCol, Row: 6, Button: ui.MouseLeftButton, EventType: ui.EventPress}, now.Add(716*time.Millisecond))
	if dismissed != 2 {
		t.Fatalf("dismissed toast = %d, want 2", dismissed)
	}
}

type toastAnimationBackend struct {
	events  chan ui.Event
	size    ui.Size
	painter *ui.Painter
}

func (b *toastAnimationBackend) Events() <-chan ui.Event { return b.events }
func (b *toastAnimationBackend) Size() ui.Size           { return b.size }
func (b *toastAnimationBackend) Render(painter *ui.Painter) error {
	b.painter = painter
	return nil
}
func (*toastAnimationBackend) Dispatch(callback func())    { callback() }
func (*toastAnimationBackend) SetMouseShape(ui.MouseShape) {}
func (*toastAnimationBackend) Close() error                { return nil }

func toastPainterRows(painter *ui.Painter) []string {
	if painter == nil {
		return nil
	}
	size := painter.Size()
	rows := make([]string, size.Height)
	for row := range size.Height {
		var line strings.Builder
		for column := range size.Width {
			line.WriteString(painter.Cell(column, row).Grapheme)
		}
		rows[row] = line.String()
	}
	return rows
}
