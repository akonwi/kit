package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

type moveInteractionOptionIntent struct{ Delta int }

func (moveInteractionOptionIntent) IntentType() ui.IntentType { return "kit.interaction-option.move" }

type chooseInteractionOptionIntent struct{}

func (chooseInteractionOptionIntent) IntentType() ui.IntentType {
	return "kit.interaction-option.choose"
}

type chooseInteractionBooleanIntent struct{ Value bool }

func (chooseInteractionBooleanIntent) IntentType() ui.IntentType {
	return "kit.interaction-boolean.choose"
}

type continueInteractionIntent struct{}

func (continueInteractionIntent) IntentType() ui.IntentType { return "kit.interaction.continue" }

type interactionDock struct {
	Suspended   bool
	Request     protocol.InteractionRequest
	QueueLength int
	OnRespond   func(ui.EventContext, protocol.InteractionResponse, func(error))
}

func (interactionDock) CreateState() ui.State { return &interactionDockState{} }

type interactionDockState struct {
	ui.StateBase
	requestID    string
	value        string
	step         int
	answers      map[string]protocol.InteractionAnswer
	selected     map[string]bool
	optionOffset int
	optionCursor int
	submitting   bool
}

func (s *interactionDockState) DidUpdateWidget(ui.Widget) {
	request := s.Widget().(interactionDock).Request
	if request.ID != s.requestID {
		s.requestID, s.value, s.step, s.optionOffset, s.optionCursor, s.submitting = request.ID, "", 0, 0, 0, false
		s.answers = nil
		s.selected = make(map[string]bool)
	}
}

func (s *interactionDockState) Build(ctx ui.BuildContext) ui.Widget {
	widget := s.Widget().(interactionDock)
	request := widget.Request
	if s.requestID != request.ID {
		s.requestID = request.ID
		s.selected = make(map[string]bool)
	}
	theme := ui.MustDepend[ui.Theme](ctx)
	children := []ui.Widget{s.header(theme, request, widget.QueueLength)}
	if request.Detail != "" {
		children = append(children, ui.Text{Value: request.Detail, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true, MaxLines: 2})
	}
	children = append(children, ui.SizedBox{Height: 1}, s.body(theme, request, widget.OnRespond), ui.SizedBox{Height: 1})
	cancel := func(event ui.EventContext) {
		s.respond(event, widget.OnRespond, protocol.InteractionResponse{RequestID: request.ID, Cancelled: true})
	}
	hint := "enter submit · esc cancel"
	if request.Kind == protocol.InteractionConfirm {
		hint = "y yes · n no · esc cancel"
	}
	if request.Kind == protocol.InteractionSelect {
		hint = "↑↓ move · enter/space select · esc cancel"
	}
	if request.Kind == protocol.InteractionGuided && s.step < len(request.Questions) {
		switch request.Questions[s.step].Kind {
		case protocol.InteractionQuestionBoolean:
			hint = "y yes · n no · esc cancel"
		case protocol.InteractionQuestionSelect:
			hint = "↑↓ move · enter/space select · esc cancel"
		case protocol.InteractionQuestionMultiselect:
			hint = "↑↓ move · space toggle · enter continue · esc cancel"
		}
	}
	children = append(children, ui.Flex{Axis: ui.Horizontal, MainAxisAlignment: ui.MainAxisEnd, Children: []ui.Widget{
		ui.Text{Value: hint, Style: ui.Style{Foreground: theme.MutedForeground}},
		ui.SizedBox{Width: 2}, plainButton{Label: "Cancel", OnPressed: cancel},
	}})
	actions := map[ui.IntentType]ui.ActionFunc{
		inputTargetIntent{}.IntentType(): inputTargetAction(inputInteraction),
		insertPasteIntent{}.IntentType(): func(event ui.EventContext, intent ui.Intent) ui.EventResult {
			if s.acceptsPaste() {
				event.Invoke(ui.InsertTextIntent{Text: intent.(insertPasteIntent).Text})
			}
			return ui.EventHandled
		},
		"vaxis.dismiss": func(event ui.EventContext, _ ui.Intent) ui.EventResult { cancel(event); return ui.EventHandled },
	}
	if request.Kind == protocol.InteractionGuided {
		actions["vaxis.next-focus"] = func(event ui.EventContext, _ ui.Intent) ui.EventResult {
			s.submitCurrentGuided(event, request, widget.OnRespond)
			return ui.EventHandled
		}
		actions["vaxis.previous-focus"] = func(ui.EventContext, ui.Intent) ui.EventResult {
			s.movePreviousGuided(request)
			return ui.EventHandled
		}
	}
	shortcuts := ui.ShortcutMap{}
	var options []protocol.InteractionOption
	optionWindow := 3
	var choose func(ui.EventContext, string)
	if request.Kind == protocol.InteractionSelect {
		options = request.Options
		choose = func(event ui.EventContext, id string) {
			s.respond(event, widget.OnRespond, protocol.InteractionResponse{RequestID: request.ID, SelectedOptionID: id})
		}
	} else if request.Kind == protocol.InteractionGuided && s.step < len(request.Questions) && request.Questions[s.step].Kind == protocol.InteractionQuestionSelect {
		question := request.Questions[s.step]
		options = question.Options
		if question.Required {
			optionWindow = 2
		} else {
			optionWindow = 1
		}
		choose = func(event ui.EventContext, id string) {
			s.advanceGuided(event, request, widget.OnRespond, question.ID, protocol.InteractionAnswer{OptionIDs: []string{id}})
		}
	}
	if len(options) > 0 {
		actions[moveInteractionOptionIntent{}.IntentType()] = func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			s.moveOptionCursor(len(options), optionWindow, intent.(moveInteractionOptionIntent).Delta)
			return ui.EventHandled
		}
		actions[chooseInteractionOptionIntent{}.IntentType()] = func(event ui.EventContext, _ ui.Intent) ui.EventResult {
			choose(event, options[min(max(0, s.optionCursor), len(options)-1)].ID)
			return ui.EventHandled
		}
		shortcuts["Up"], shortcuts["k"] = moveInteractionOptionIntent{Delta: -1}, moveInteractionOptionIntent{Delta: -1}
		shortcuts["Down"], shortcuts["j"] = moveInteractionOptionIntent{Delta: 1}, moveInteractionOptionIntent{Delta: 1}
		shortcuts["Enter"] = chooseInteractionOptionIntent{}
		shortcuts["Space"] = chooseInteractionOptionIntent{}
	}
	content := ui.FocusScope{Trap: !widget.Suspended, AutoFocus: !widget.Suspended, ReclaimFocus: !widget.Suspended, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Background}},
		ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}),
	)}
	if widget.Suspended {
		actions = nil
		shortcuts = nil
	}
	return ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: shortcuts, Child: content}}
}

func (s *interactionDockState) header(theme ui.Theme, request protocol.InteractionRequest, queueLength int) ui.Widget {
	meta := ""
	if queueLength > 1 {
		meta = fmt.Sprintf("1 of %d", queueLength)
	}
	return ui.Flex{Axis: ui.Horizontal, MainAxisAlignment: ui.MainAxisSpaceBetween, Children: []ui.Widget{
		ui.Text{Value: request.Title, Style: ui.Style{Foreground: theme.Foreground, Attribute: ui.AttrBold}},
		ui.Text{Value: meta, Style: ui.Style{Foreground: theme.MutedForeground}},
	}}
}

func (s *interactionDockState) body(theme ui.Theme, request protocol.InteractionRequest, respond func(ui.EventContext, protocol.InteractionResponse, func(error))) ui.Widget {
	switch request.Kind {
	case protocol.InteractionConfirm:
		choose := func(event ui.EventContext, value bool) {
			s.respond(event, respond, protocol.InteractionResponse{RequestID: request.ID, Confirmed: &value})
		}
		return booleanChoices(choose)
	case protocol.InteractionInput:
		return ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{textInput(theme, textInputConfig{Value: s.value, Placeholder: "Type a response", AutoFocus: !s.Widget().(interactionDock).Suspended,
			OnChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.value = value }) },
			OnSubmitted: func(event ui.EventContext, value string) {
				if strings.TrimSpace(value) == "" {
					return
				}
				s.respond(event, respond, protocol.InteractionResponse{RequestID: request.ID, Value: &value})
			},
		})}}
	case protocol.InteractionSelect:
		return s.optionButtons(theme, request.Options, func(event ui.EventContext, value string) {
			s.respond(event, respond, protocol.InteractionResponse{RequestID: request.ID, SelectedOptionID: value})
		})
	case protocol.InteractionGuided:
		return s.guidedBody(theme, request, respond)
	default:
		return ui.Text{Value: "Unsupported interaction", Style: ui.Style{Foreground: theme.DangerText}}
	}
}

func (s *interactionDockState) optionButtons(theme ui.Theme, options []protocol.InteractionOption, choose func(ui.EventContext, string)) ui.Widget {
	return s.optionButtonsWindow(theme, options, 3, choose)
}

func (s *interactionDockState) optionButtonsWindow(theme ui.Theme, options []protocol.InteractionOption, windowSize int, choose func(ui.EventContext, string)) ui.Widget {
	if len(options) == 0 {
		return ui.SizedBox{}
	}
	cursor := min(max(0, s.optionCursor), len(options)-1)
	start := min(max(0, s.optionOffset), max(0, len(options)-windowSize))
	if cursor < start {
		start = cursor
	} else if cursor >= start+windowSize {
		start = cursor - windowSize + 1
	}
	end := min(len(options), start+windowSize)
	children := make([]ui.Widget, 0, windowSize+2)
	if start > 0 {
		children = append(children, ui.Text{Value: fmt.Sprintf("  ▲ %d more", start), Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	for index := start; index < end; index++ {
		option, index := options[index], index
		children = append(children, interactionOptionRow{Marker: interactionOptionMarker(index), Label: option.Label, Detail: option.Detail, Selected: index == cursor, OnPressed: func(event ui.EventContext) {
			s.SetState(func() { s.optionCursor = index })
			choose(event, option.ID)
		}})
	}
	if end < len(options) {
		children = append(children, ui.Text{Value: fmt.Sprintf("  ▼ %d more", len(options)-end), Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	move := func(delta int) {
		s.SetState(func() {
			s.optionCursor = min(max(0, s.optionCursor+delta), len(options)-1)
			if s.optionCursor < s.optionOffset {
				s.optionOffset = s.optionCursor
			}
			if s.optionCursor >= s.optionOffset+windowSize {
				s.optionOffset = s.optionCursor - windowSize + 1
			}
		})
	}
	actions := map[ui.IntentType]ui.ActionFunc{
		moveInteractionOptionIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
			move(intent.(moveInteractionOptionIntent).Delta)
			return ui.EventHandled
		},
		chooseInteractionOptionIntent{}.IntentType(): func(event ui.EventContext, _ ui.Intent) ui.EventResult {
			choose(event, options[min(max(0, s.optionCursor), len(options)-1)].ID)
			return ui.EventHandled
		},
	}
	return ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"Up": moveInteractionOptionIntent{Delta: -1}, "k": moveInteractionOptionIntent{Delta: -1},
		"Down": moveInteractionOptionIntent{Delta: 1}, "j": moveInteractionOptionIntent{Delta: 1},
		"Enter": chooseInteractionOptionIntent{},
	}, Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}}}
}

func (s *interactionDockState) guidedBody(theme ui.Theme, request protocol.InteractionRequest, respond func(ui.EventContext, protocol.InteractionResponse, func(error))) ui.Widget {
	if s.step >= len(request.Questions) {
		return ui.SizedBox{}
	}
	question := request.Questions[s.step]
	prefix := fmt.Sprintf("Question %d of %d · ", s.step+1, len(request.Questions))
	children := []ui.Widget{ui.Text{Value: prefix + question.Prompt, Style: ui.Style{Foreground: theme.Foreground}, SoftWrap: true, MaxLines: 2}}
	advance := func(event ui.EventContext, questionID string, answer protocol.InteractionAnswer) {
		s.advanceGuided(event, request, respond, questionID, answer)
	}
	switch question.Kind {
	case protocol.InteractionQuestionText:
		children = append(children, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{textInput(theme, textInputConfig{Value: s.value, Placeholder: "Type an answer", AutoFocus: !s.Widget().(interactionDock).Suspended,
			OnChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.value = value }) },
			OnSubmitted: func(event ui.EventContext, value string) {
				if strings.TrimSpace(value) == "" {
					if !question.Required {
						advance(event, question.ID, protocol.InteractionAnswer{Skipped: true})
					}
					return
				}
				advance(event, question.ID, protocol.InteractionAnswer{Text: &value})
			},
		})}})
	case protocol.InteractionQuestionBoolean:
		children = append(children, booleanChoices(func(event ui.EventContext, value bool) {
			advance(event, question.ID, protocol.InteractionAnswer{Boolean: &value})
		}))
	case protocol.InteractionQuestionSelect:
		windowSize := 1
		if question.Required {
			windowSize = 2
		}
		children = append(children, s.optionButtonsWindow(theme, question.Options, windowSize, func(event ui.EventContext, value string) {
			advance(event, question.ID, protocol.InteractionAnswer{OptionIDs: []string{value}})
		}))
	case protocol.InteractionQuestionMultiselect:
		const windowSize = 1
		cursor := min(max(0, s.optionCursor), len(question.Options)-1)
		start := min(max(0, s.optionOffset), max(0, len(question.Options)-windowSize))
		if cursor < start {
			start = cursor
		} else if cursor >= start+windowSize {
			start = cursor - windowSize + 1
		}
		end := min(len(question.Options), start+windowSize)
		rows := make([]ui.Widget, 0, windowSize+3)
		if start > 0 {
			rows = append(rows, ui.Text{Value: fmt.Sprintf("  ▲ %d more", start), Style: ui.Style{Foreground: theme.MutedForeground}})
		}
		for index := start; index < end; index++ {
			option, index := question.Options[index], index
			label := map[bool]string{true: "[x] ", false: "[ ] "}[s.selected[option.ID]] + option.Label
			gap := 0
			if question.Required {
				gap = 1
			}
			rows = append(rows, interactionOptionRow{Index: index, Label: label, Detail: option.Detail, Selected: index == cursor, Gap: gap, OnPressed: func(ui.EventContext) {
				s.SetState(func() { s.optionCursor = index; s.selected[option.ID] = !s.selected[option.ID] })
			}})
		}
		if end < len(question.Options) {
			rows = append(rows, ui.Text{Value: fmt.Sprintf("  ▼ %d more", len(question.Options)-end), Style: ui.Style{Foreground: theme.MutedForeground}})
		}
		complete := func(event ui.EventContext) {
			ids := make([]string, 0, len(s.selected))
			for _, option := range question.Options {
				if s.selected[option.ID] {
					ids = append(ids, option.ID)
				}
			}
			if len(ids) == 0 {
				if !question.Required {
					advance(event, question.ID, protocol.InteractionAnswer{Skipped: true})
				}
				return
			}
			advance(event, question.ID, protocol.InteractionAnswer{OptionIDs: ids})
		}
		move := func(delta int) {
			s.SetState(func() {
				s.optionCursor = min(max(0, s.optionCursor+delta), len(question.Options)-1)
				if s.optionCursor < s.optionOffset {
					s.optionOffset = s.optionCursor
				}
				if s.optionCursor >= s.optionOffset+windowSize {
					s.optionOffset = s.optionCursor - windowSize + 1
				}
			})
		}
		actions := map[ui.IntentType]ui.ActionFunc{
			moveInteractionOptionIntent{}.IntentType(): func(_ ui.EventContext, intent ui.Intent) ui.EventResult {
				move(intent.(moveInteractionOptionIntent).Delta)
				return ui.EventHandled
			},
			chooseInteractionOptionIntent{}.IntentType(): func(_ ui.EventContext, _ ui.Intent) ui.EventResult {
				option := question.Options[min(max(0, s.optionCursor), len(question.Options)-1)]
				s.SetState(func() { s.selected[option.ID] = !s.selected[option.ID] })
				return ui.EventHandled
			},
			continueInteractionIntent{}.IntentType(): func(event ui.EventContext, _ ui.Intent) ui.EventResult { complete(event); return ui.EventHandled },
		}
		rows = append(rows, plainButton{Label: "Continue", OnPressed: complete})
		children = append(children, ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{"Up": moveInteractionOptionIntent{Delta: -1}, "Down": moveInteractionOptionIntent{Delta: 1}, "k": moveInteractionOptionIntent{Delta: -1}, "j": moveInteractionOptionIntent{Delta: 1}, "Space": chooseInteractionOptionIntent{}, "Enter": continueInteractionIntent{}}, Child: ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}}})
	}
	if !question.Required {
		children = append(children, plainButton{Label: "Skip", OnPressed: func(event ui.EventContext) {
			advance(event, question.ID, protocol.InteractionAnswer{Skipped: true})
		}})
	}
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (s *interactionDockState) advanceGuided(event ui.EventContext, request protocol.InteractionRequest, respond func(ui.EventContext, protocol.InteractionResponse, func(error)), questionID string, answer protocol.InteractionAnswer) {
	answers := make(map[string]protocol.InteractionAnswer, len(s.answers)+1)
	for id, existing := range s.answers {
		answers[id] = existing
	}
	answers[questionID] = answer
	if s.step+1 == len(request.Questions) {
		s.respond(event, respond, protocol.InteractionResponse{RequestID: request.ID, Answers: answers})
		return
	}
	s.SetState(func() {
		s.answers, s.step, s.value, s.selected, s.optionOffset, s.optionCursor = answers, s.step+1, "", make(map[string]bool), 0, 0
	})
}

func (s *interactionDockState) submitCurrentGuided(event ui.EventContext, request protocol.InteractionRequest, respond func(ui.EventContext, protocol.InteractionResponse, func(error))) {
	if s.step < 0 || s.step >= len(request.Questions) {
		return
	}
	question := request.Questions[s.step]
	answer := protocol.InteractionAnswer{}
	switch question.Kind {
	case protocol.InteractionQuestionText:
		value := strings.TrimSpace(s.value)
		if value == "" {
			if question.Required {
				return
			}
			answer.Skipped = true
		} else {
			answer.Text = &value
		}
	case protocol.InteractionQuestionSelect:
		if len(question.Options) == 0 {
			return
		}
		answer.OptionIDs = []string{question.Options[min(max(0, s.optionCursor), len(question.Options)-1)].ID}
	case protocol.InteractionQuestionMultiselect:
		for _, option := range question.Options {
			if s.selected[option.ID] {
				answer.OptionIDs = append(answer.OptionIDs, option.ID)
			}
		}
		if len(answer.OptionIDs) == 0 {
			if question.Required {
				return
			}
			answer.Skipped = true
		}
	case protocol.InteractionQuestionBoolean:
		if existing, ok := s.answers[question.ID]; ok && existing.Boolean != nil {
			value := *existing.Boolean
			answer.Boolean = &value
		} else {
			return
		}
	}
	s.advanceGuided(event, request, respond, question.ID, answer)
}

func (s *interactionDockState) movePreviousGuided(request protocol.InteractionRequest) {
	if s.step <= 0 {
		return
	}
	s.SetState(func() {
		s.step--
		s.value, s.optionCursor, s.optionOffset = "", 0, 0
		s.selected = make(map[string]bool)
		question := request.Questions[s.step]
		answer, ok := s.answers[question.ID]
		if !ok {
			return
		}
		if answer.Text != nil {
			s.value = *answer.Text
		}
		if len(answer.OptionIDs) > 0 {
			for _, id := range answer.OptionIDs {
				s.selected[id] = true
			}
			for index, option := range question.Options {
				if option.ID == answer.OptionIDs[0] {
					s.optionCursor = index
					break
				}
			}
		}
	})
}

func (s *interactionDockState) moveOptionCursor(optionCount, windowSize, delta int) {
	s.SetState(func() {
		s.optionCursor = min(max(0, s.optionCursor+delta), optionCount-1)
		if s.optionCursor < s.optionOffset {
			s.optionOffset = s.optionCursor
		}
		if s.optionCursor >= s.optionOffset+windowSize {
			s.optionOffset = s.optionCursor - windowSize + 1
		}
	})
}

func booleanChoices(choose func(ui.EventContext, bool)) ui.Widget {
	activate := func(event ui.EventContext, intent ui.Intent) ui.EventResult {
		choose(event, intent.(chooseInteractionBooleanIntent).Value)
		return ui.EventHandled
	}
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		chooseInteractionBooleanIntent{}.IntentType(): activate,
	}, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"y": chooseInteractionBooleanIntent{Value: true}, "Y": chooseInteractionBooleanIntent{Value: true},
		"n": chooseInteractionBooleanIntent{Value: false}, "N": chooseInteractionBooleanIntent{Value: false},
	}, Child: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		plainButton{Label: "Yes", OnPressed: func(event ui.EventContext) { choose(event, true) }},
		ui.SizedBox{Width: 1},
		plainButton{Label: "No", OnPressed: func(event ui.EventContext) { choose(event, false) }},
	}}}}
}

func interactionOptionMarker(index int) string {
	marker := ""
	for index >= 0 {
		marker = string(rune('A'+index%26)) + marker
		index = index/26 - 1
	}
	return marker
}

type interactionOptionRow struct {
	Index     int
	Marker    string
	Label     string
	Detail    string
	Selected  bool
	Gap       int
	OnPressed ui.VoidCallback
}

func (interactionOptionRow) CreateState() ui.State { return &interactionOptionRowState{} }

type interactionOptionRowState struct {
	ui.StateBase
	hovered bool
	focus   ui.FocusNode
}

func (s *interactionOptionRowState) Build(ctx ui.BuildContext) ui.Widget {
	row := s.Widget().(interactionOptionRow)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := ui.Style{Background: theme.Background}
	if row.Selected || s.hovered {
		style.Background = theme.SurfaceHovered
	}
	prefix := row.Marker
	if prefix == "" {
		prefix = fmt.Sprintf("%d", row.Index+1)
	}
	prefix += ". "
	labelStyle := ui.Style{Foreground: theme.Foreground, Background: style.Background}
	if row.Selected {
		labelStyle.Attribute = ui.AttrBold
	}
	content := []ui.Widget{ui.RichText{Spans: []ui.TextSpan{
		{Text: prefix, Style: ui.Style{Foreground: theme.MutedForeground, Background: style.Background}},
		{Text: row.Label, Style: labelStyle},
	}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}}
	if row.Detail != "" {
		content = append(content, ui.Text{Value: "   " + row.Detail, Style: ui.Style{Foreground: theme.MutedForeground, Background: style.Background}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis})
	}
	return ui.Focus(&s.focus, ui.Padding(ui.Insets{Bottom: row.Gap}, mouseActivator{
		OnPressed: row.OnPressed,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
		Child: ui.DecoratedBox(ui.Decoration{Style: style}, ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: content})),
	}))
}

func (s *interactionDockState) respond(event ui.EventContext, callback func(ui.EventContext, protocol.InteractionResponse, func(error)), response protocol.InteractionResponse) {
	if callback == nil || s.submitting || s.Widget().(interactionDock).Suspended {
		return
	}
	s.SetState(func() { s.submitting = true })
	callback(event, response, func(err error) {
		if err != nil {
			s.SetState(func() { s.submitting = false })
		}
	})
}

func (s *interactionDockState) acceptsPaste() bool {
	w := s.Widget().(interactionDock)
	if w.Suspended || s.submitting {
		return false
	}
	return w.Request.Kind == protocol.InteractionInput || (w.Request.Kind == protocol.InteractionGuided && s.step < len(w.Request.Questions) && w.Request.Questions[s.step].Kind == protocol.InteractionQuestionText)
}
func (s *interactionDockState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	if key, ok := event.(ui.Key); ok && key.EventType == vaxis.EventPaste {
		if s.acceptsPaste() {
			insertPastedText(ctx, pastedKeyText(key))
		}
		return ui.EventHandled
	}
	return ui.EventIgnored
}
