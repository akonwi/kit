import AppKit
import KitDictation
import SwiftUI

/// Annotate mode over a browser tab's page: Kit takes the pointer, draws
/// the outlines in the manner of a design tool, and opens a comment box
/// on the selection. The page gets no clicks or hovers meanwhile; the
/// scroll wheel still scrolls it.
///
/// - Hover: a thin outline and a name tag on what a click would pick.
/// - Selection: a solid box with corner handles and its size below.
/// - The group holding the selection: a dashed frame with its name, which
///   is what parent widens to.
struct BrowserAnnotationOverlay: View {
    @Environment(\.mica) private var theme
    let session: BrowserAnnotationSession
    let page: NSView
    /// Sends the finished annotation to the session's composer.
    let attach: (BrowserElementAnnotation) -> Void
    let done: () -> Void
    /// Asks the pointer layer for the keyboard.
    @State private var pointerFocus = 0
    @State private var boxHeight: CGFloat = 150
    /// The comment box's dictation, which the page's keys defer to.
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    var body: some View {
        GeometryReader { geometry in
            let size = geometry.size
            ZStack(alignment: .topLeading) {
                AnnotationPointerLayer(session: session, page: page, focusRequest: pointerFocus,
                                       dictation: dictationSlot, escape: escape)
                outlines(in: size)
                    .allowsHitTesting(false)
                if let selected = session.page.selected {
                    commentBox
                        .frame(width: Self.boxWidth)
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { boxHeight = $0 }
                        .offset(boxOrigin(for: session.viewRect(selected), in: size))
                }
            }
            .onAppear { session.viewSize = size }
            .onChange(of: size) { session.viewSize = size }
        }
        .task {
            // Elements scroll, move, and resize; the outlines follow.
            while !Task.isCancelled {
                try? await Task.sleep(for: BrowserAnnotationSession.followInterval)
                await session.follow()
            }
        }
        .onChange(of: session.page.selected == nil) { _, none in if none { pointerFocus += 1 } }
    }

    static let boxWidth: CGFloat = 340
    /// Space between the selection and the comment box, past its size badge.
    private static let boxGap: CGFloat = 26

    /// Below the selection, or above when it doesn't fit, kept in the pane.
    private func boxOrigin(for rect: CGRect, in size: CGSize) -> CGSize {
        let x = min(max(rect.minX, 8), max(8, size.width - Self.boxWidth - 8))
        var y = rect.maxY + Self.boxGap
        if y + boxHeight > size.height - 8 {
            let above = rect.minY - Self.boxGap - boxHeight
            y = above >= 8 ? above : max(8, size.height - boxHeight - 8)
        }
        return CGSize(width: x, height: y)
    }

    private var dictationSlot: DictationSlot { givenDictation ?? ownDictation }

    private func escape() {
        if dictationSlot.isActive {
            dictationSlot.reset()
        } else if session.page.selected != nil {
            Task { await session.deselect() }
        } else {
            done()
        }
    }

    // MARK: Outlines

    @ViewBuilder private func outlines(in size: CGSize) -> some View {
        ZStack(alignment: .topLeading) {
            if let scope = session.page.scope {
                let rect = session.viewRect(scope)
                Rectangle()
                    .strokeBorder(theme.accent.opacity(0.55), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                    .frame(width: rect.width, height: rect.height)
                    .offset(x: rect.minX, y: rect.minY)
                nameTag(scope, filled: false)
                    .offset(tagOffset(rect))
            }
            if let hovered = session.page.hovered {
                let rect = session.viewRect(hovered)
                Rectangle()
                    .strokeBorder(theme.accent, lineWidth: 1)
                    .frame(width: rect.width, height: rect.height)
                    .offset(x: rect.minX, y: rect.minY)
                nameTag(hovered, filled: session.page.selected == nil)
                    .offset(tagOffset(rect))
            }
            if let selected = session.page.selected {
                let rect = session.viewRect(selected)
                SelectionBox(color: theme.accent)
                    .frame(width: rect.width, height: rect.height)
                    .offset(x: rect.minX, y: rect.minY)
                nameTag(selected, filled: true)
                    .offset(tagOffset(rect))
                Text("\(Self.dimension(selected.rect.width)) × \(Self.dimension(selected.rect.height))")
                    .font(.kit(size: 10.5, weight: .medium)).monospacedDigit()
                    .foregroundStyle(.white)
                    .padding(.horizontal, 5).padding(.vertical, 1)
                    .background(theme.accent, in: RoundedRectangle(cornerRadius: 3))
                    .fixedSize()
                    // Centred under the box; a frame, not an alignment guide,
                    // so it can't move the stack's origin and the outlines.
                    .frame(width: max(rect.width, 1))
                    .offset(x: rect.minX, y: rect.maxY + 6)
            }
        }
        .frame(width: size.width, height: size.height, alignment: .topLeading)
        .clipped()
    }

    /// A tag above the box's top-left corner, or inside it at the top edge.
    private func tagOffset(_ rect: CGRect) -> CGSize {
        CGSize(width: max(rect.minX, 0), height: rect.minY >= 18 ? rect.minY - 17 : max(rect.minY, 0) + 2)
    }

    private func nameTag(_ target: BrowserAnnotationTarget, filled: Bool) -> some View {
        HStack(spacing: 4) {
            Text(target.label).font(.kit(size: 10.5, weight: .medium, design: .monospaced))
            if let hint = target.hint {
                Text(hint).font(.kit(size: 10.5)).opacity(0.8)
            }
        }
        .lineLimit(1)
        .foregroundStyle(filled ? Color.white : theme.accent)
        .padding(.horizontal, 4).frame(height: 15)
        .background(filled ? theme.accent : theme.surface.opacity(0.9), in: RoundedRectangle(cornerRadius: 2))
        .fixedSize()
    }

    static func dimension(_ value: Double) -> String {
        value.rounded() == value ? String(Int(value)) : String(format: "%.1f", value)
    }

    // MARK: Comment

    private var commentBox: some View {
        BrowserAnnotationCommentBox(session: session, add: add, cancel: { Task { await session.deselect() } })
            .environment(\.dictationSlot, dictationSlot)
    }

    private func add() {
        Task {
            guard let annotation = await session.capture() else { return }
            attach(annotation)
            await session.deselect()
        }
    }
}

/// The selection's box: a solid outline with a square handle at each corner.
private struct SelectionBox: View {
    let color: Color

    var body: some View {
        Rectangle().strokeBorder(color, lineWidth: 1.5)
            .overlay(alignment: .topLeading) { handle.offset(x: -3, y: -3) }
            .overlay(alignment: .topTrailing) { handle.offset(x: 3, y: -3) }
            .overlay(alignment: .bottomLeading) { handle.offset(x: -3, y: 3) }
            .overlay(alignment: .bottomTrailing) { handle.offset(x: 3, y: 3) }
    }

    private var handle: some View {
        Rectangle().fill(.white).frame(width: 6, height: 6)
            .overlay(Rectangle().strokeBorder(color, lineWidth: 1.5))
    }
}

/// The comment box on a selection: its ancestors to step through, the
/// comment, and whether its screenshot goes with it. The comment can be
/// dictated, as code comments are; a recording goes when the box closes
/// or another element is picked.
struct BrowserAnnotationCommentBox: View {
    @Environment(\.mica) private var theme
    @Bindable var session: BrowserAnnotationSession
    let add: () -> Void
    let cancel: () -> Void
    @FocusState private var focused: Bool
    @State private var selection: TextSelection?
    @State private var caretToEnd = false
    /// The field's height before the strip took its place, which the strip keeps.
    @State private var fieldHeight: CGFloat?
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            crumbs.disabled(dictating)
            field
            if #available(macOS 26, *), dictation.hasNotice { DictationNotice(dictation: dictation) }
            if let error = session.error {
                Text(error).font(.kit(size: 11)).foregroundStyle(theme.danger)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 8) {
                Toggle("Attach screenshot", isOn: $session.attachScreenshot)
                    .toggleStyle(.checkbox)
                    .font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .lineLimit(1).fixedSize()
                Spacer(minLength: 4)
                if session.adding { KitSpinner() }
                Button("Cancel", action: cancel).buttonStyle(SmallButtonStyle(bordered: true))
                Button("Save", action: add)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(session.comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || session.adding
                              || dictating)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.strongBorder, lineWidth: 1))
        .shadow(color: .black.opacity(0.14), radius: 12, y: 4)
        .onAppear { focused = true }
        .onDisappear { dictationSlot.reset() }
        // Words said about one element don't go to another.
        .onChange(of: session.page.selected?.label) {
            dictationSlot.reset()
            focused = true
        }
        .onChange(of: session.clicks) {
            dictationSlot.reset()
            if session.page.selected != nil { focused = true }
        }
        .onChange(of: focused) {
            guard focused, caretToEnd else { return }
            caretToEnd = false
            selection = TextSelection(insertionPoint: session.comment.endIndex)
        }
        .onChange(of: session.comment) {
            if #available(macOS 26, *) { dictation.dismissProblem() }
        }
    }

    /// The comment, or the recording strip in its place while dictating.
    private var field: some View {
        HStack(alignment: .top, spacing: 4) {
            if #available(macOS 26, *), dictation.listening {
                DictationStrip(dictation: dictation, done: finishDictation, cancelled: focusAtEnd, size: 13)
                DictationSquare(dictation: dictation, size: 20, done: finishDictation)
            } else {
                TextField("", text: $session.comment, selection: $selection,
                          prompt: Text("Comment on this element…").foregroundStyle(theme.muted), axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(.kit(size: 13))
                    .foregroundStyle(theme.text)
                    .lineLimit(2...8)
                    .focused($focused)
                    .onSubmit { if !dictating { add() } }
                    .onExitCommand { if dictating { dictationSlot.reset() } else { cancel() } }
                    // An empty comment has no lines to move through, so the
                    // arrows step out to the parent and back in.
                    .onKeyPress(.upArrow) { stepIfEmpty(.parent) }
                    .onKeyPress(.downArrow) { stepIfEmpty(.child) }
                    .padding(.vertical, 2)
                    .accessibilityLabel("Comment on the selected element")
                if #available(macOS 26, *) {
                    DictationMic(dictation: dictation, size: 20, iconSize: 13)
                }
            }
        }
        .frame(height: dictationListening ? fieldHeight : nil, alignment: .top)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height in
            if !dictationListening { fieldHeight = height }
        }
        .padding(.leading, 10).padding(.trailing, DictationSupport.available ? 4 : 10).padding(.vertical, 6)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4)
            .strokeBorder(focused || dictating ? theme.accent.opacity(0.6) : theme.border, lineWidth: 1))
    }

    // MARK: Dictation

    private var dictationSlot: DictationSlot { givenDictation ?? ownDictation }

    @available(macOS 26, *)
    private var dictation: Dictation { dictationSlot.dictation }

    private var dictating: Bool {
        if #available(macOS 26, *) { return dictation.isActive }
        return false
    }

    /// Whether the recording strip stands in the comment's place.
    private var dictationListening: Bool {
        if #available(macOS 26, *) { return dictation.listening }
        return false
    }

    /// Stops recording and puts the words after the comment, with the
    /// field focused and the caret after them.
    private func finishDictation() {
        guard #available(macOS 26, *) else { return }
        let session = session
        dictation.finish { text in
            let current = session.comment
            session.comment = DictationText.join(
                current.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : current, text)
            focusAtEnd()
        }
    }

    /// Back to the comment with the caret at its end, as the strip goes.
    /// Focus selects the whole comment, so the caret moves once it has.
    private func focusAtEnd() {
        caretToEnd = true
        focused = true
    }

    private func stepIfEmpty(_ step: BrowserAnnotationSession.Step) -> KeyPress.Result {
        guard session.comment.isEmpty, !dictating else { return .ignored }
        Task { await session.step(step) }
        return .handled
    }

    /// The selection's nearest ancestors that fit, each in full, after an
    /// ellipsis when the outer ones don't.
    private var crumbs: some View {
        let crumbs = session.page.crumbs
        return HStack(spacing: 4) {
            ViewThatFits(in: .horizontal) {
                ForEach(Array(stride(from: crumbs.count, through: 1, by: -1)), id: \.self) { shown in
                    crumbTrail(crumbs, from: crumbs.count - shown)
                }
                // A selection whose own name is too long shortens it.
                Text(crumbs.last ?? "").foregroundStyle(theme.text)
                    .lineLimit(1).truncationMode(.middle)
            }
            .font(.kit(size: 11, design: .monospaced))
            .frame(maxWidth: .infinity, alignment: .leading)
            StepButton(icon: .arrowUp, help: "Select Parent") { Task { await session.step(.parent) } }
                .disabled(!session.page.canParent)
            StepButton(icon: .arrowDown, help: "Select Child") { Task { await session.step(.child) } }
                .disabled(!session.page.canChild)
        }
    }

    private func crumbTrail(_ crumbs: [String], from first: Int) -> some View {
        HStack(spacing: 3) {
            if first > 0 {
                Text("…").foregroundStyle(theme.muted)
                Text("›").foregroundStyle(theme.muted)
            }
            ForEach(first..<crumbs.count, id: \.self) { index in
                if index > first { Text("›").foregroundStyle(theme.muted) }
                let crumb = crumbs[index], current = index == crumbs.count - 1
                Button { Task { await session.selectCrumb(index) } } label: {
                    Text(crumb)
                        .foregroundStyle(current ? theme.text : theme.muted)
                        .padding(.horizontal, 4).padding(.vertical, 1)
                        .background(current ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(current)
                .help(current ? "Selected" : "Select \(crumb)")
            }
        }
        .lineLimit(1)
        .fixedSize()
    }
}

private struct StepButton: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let icon: Icon
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            IconView(icon, size: 11)
                .foregroundStyle(theme.text)
                .frame(width: 20, height: 18)
                .background(hovering && enabled ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.4)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
    }
}

/// Takes the pointer over the page while annotating: moves outline, clicks
/// select, ⌘ picks the innermost element, the wheel scrolls the page, Esc
/// leaves the selection and then the mode, and the arrows step out and in.
/// While the comment is dictated the arrows wait, Esc throws the recording
/// away, and a click throws it away before picking anything.
private struct AnnotationPointerLayer: NSViewRepresentable {
    let session: BrowserAnnotationSession
    let page: NSView
    let focusRequest: Int
    let dictation: DictationSlot
    let escape: () -> Void

    func makeNSView(context: Context) -> PointerView {
        let view = PointerView()
        view.session = session
        view.page = page
        view.dictation = dictation
        view.escape = escape
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        return view
    }

    func updateNSView(_ view: PointerView, context: Context) {
        view.session = session
        view.page = page
        view.dictation = dictation
        view.escape = escape
        if view.focusRequest != focusRequest {
            view.focusRequest = focusRequest
            DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        }
    }

    final class PointerView: NSView {
        weak var session: BrowserAnnotationSession?
        weak var page: NSView?
        var dictation: DictationSlot?
        var escape: () -> Void = {}
        var focusRequest = 0
        private var tracking: NSTrackingArea?

        override var isFlipped: Bool { true }
        override var acceptsFirstResponder: Bool { true }
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            if let tracking { removeTrackingArea(tracking) }
            let area = NSTrackingArea(rect: .zero, options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow,
                                                             .inVisibleRect, .cursorUpdate], owner: self)
            addTrackingArea(area)
            tracking = area
        }

        override func cursorUpdate(with event: NSEvent) { NSCursor.arrow.set() }

        private func point(_ event: NSEvent) -> CGPoint { convert(event.locationInWindow, from: nil) }

        /// Where the pointer is now, when it's over the layer.
        private var pointer: CGPoint? {
            guard let window else { return nil }
            let location = convert(window.mouseLocationOutsideOfEventStream, from: nil)
            return bounds.contains(location) ? location : nil
        }

        override func mouseMoved(with event: NSEvent) {
            session?.hover(at: point(event), deep: event.modifierFlags.contains(.command))
        }

        override func mouseExited(with event: NSEvent) { session?.hoverEnded() }

        override func flagsChanged(with event: NSEvent) {
            if let pointer { session?.hover(at: pointer, deep: event.modifierFlags.contains(.command)) }
            super.flagsChanged(with: event)
        }

        override func mouseDown(with event: NSEvent) {
            window?.makeFirstResponder(self)
            // Before the click picks anything, so words said about this
            // element can't land on the next.
            dictation?.reset()
            let location = point(event), deep = event.modifierFlags.contains(.command)
            Task { @MainActor [weak self] in await self?.session?.click(at: location, deep: deep) }
        }

        override func mouseDragged(with event: NSEvent) {}
        override func mouseUp(with event: NSEvent) {}
        override func rightMouseDown(with event: NSEvent) {}
        override func otherMouseDown(with event: NSEvent) {}

        override func scrollWheel(with event: NSEvent) {
            page?.scrollWheel(with: event)
            if let pointer { session?.hover(at: pointer, deep: event.modifierFlags.contains(.command)) }
        }

        override func magnify(with event: NSEvent) { page?.magnify(with: event) }

        override func keyDown(with event: NSEvent) {
            switch event.keyCode {
            case 53: escape()
            case 126 where session?.page.selected != nil && dictation?.isActive != true: Task { @MainActor [weak self] in await self?.session?.step(.parent) }
            case 125 where session?.page.selected != nil && dictation?.isActive != true: Task { @MainActor [weak self] in await self?.session?.step(.child) }
            default: super.keyDown(with: event)
            }
        }
    }
}

/// The strip under the toolbar while annotating: how to select, what v1
/// can't reach, and Done.
struct BrowserAnnotationStrip: View {
    @Environment(\.mica) private var theme
    let added: Int
    let done: () -> Void

    /// What Annotate mode can't select inside, in full.
    static let limits = """
        Frames from other sites select as a whole; their contents can’t be read. \
        Closed shadow roots select as their host. \
        Kit never captures field values, cookies, or storage.
        """

    var body: some View {
        HStack(spacing: 10) {
            Circle().fill(theme.accent).frame(width: 7, height: 7)
            (Text("Annotating").foregroundColor(theme.text).fontWeight(.medium)
             + Text("  Click to select · ↑↓ parent and child · ⌘ for the innermost").foregroundColor(theme.muted))
                .lineLimit(1).truncationMode(.tail)
            Spacer(minLength: 8)
            if added > 0 {
                Text("\(added) in message").foregroundStyle(theme.muted).fixedSize()
            }
            HStack(spacing: 4) {
                IconView(.info, size: 12)
                Text("Other sites’ frames select whole").lineLimit(1)
            }
            .foregroundStyle(theme.muted)
            .help(Self.limits)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(Self.limits)
            Button("Done", action: done).buttonStyle(SmallButtonStyle(bordered: true))
        }
        .font(.kit(size: 11.5))
        .padding(.horizontal, 10)
        .frame(height: 30)
        .background(theme.raised)
        .overlay(alignment: .bottom) { Rule() }
    }
}
