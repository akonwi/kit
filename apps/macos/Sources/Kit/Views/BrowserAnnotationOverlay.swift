import AppKit
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

    var body: some View {
        GeometryReader { geometry in
            let size = geometry.size
            ZStack(alignment: .topLeading) {
                AnnotationPointerLayer(session: session, page: page, focusRequest: pointerFocus, escape: escape)
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

    private func escape() {
        if session.page.selected != nil {
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
/// comment, and whether its screenshot goes with it.
struct BrowserAnnotationCommentBox: View {
    @Environment(\.mica) private var theme
    @Bindable var session: BrowserAnnotationSession
    let add: () -> Void
    let cancel: () -> Void
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            crumbs
            TextField("", text: $session.comment, prompt: Text("Comment on this element…").foregroundStyle(theme.muted),
                      axis: .vertical)
                .textFieldStyle(.plain)
                .font(.kit(size: 13))
                .foregroundStyle(theme.text)
                .lineLimit(2...8)
                .focused($focused)
                .onSubmit(add)
                .onExitCommand(perform: cancel)
                // An empty comment has no lines to move through, so the
                // arrows step out to the parent and back in.
                .onKeyPress(.upArrow) { stepIfEmpty(.parent) }
                .onKeyPress(.downArrow) { stepIfEmpty(.child) }
                .padding(.horizontal, 10).padding(.vertical, 8)
                .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 4)
                    .strokeBorder(focused ? theme.accent.opacity(0.6) : theme.border, lineWidth: 1))
                .accessibilityLabel("Comment on the selected element")
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
                Button("Add to message", action: add)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(session.comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || session.adding)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.strongBorder, lineWidth: 1))
        .shadow(color: .black.opacity(0.14), radius: 12, y: 4)
        .onAppear { focused = true }
        .onChange(of: session.page.selected?.label) { focused = true }
        .onChange(of: session.clicks) { if session.page.selected != nil { focused = true } }
    }

    private func stepIfEmpty(_ step: BrowserAnnotationSession.Step) -> KeyPress.Result {
        guard session.comment.isEmpty else { return .ignored }
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
private struct AnnotationPointerLayer: NSViewRepresentable {
    let session: BrowserAnnotationSession
    let page: NSView
    let focusRequest: Int
    let escape: () -> Void

    func makeNSView(context: Context) -> PointerView {
        let view = PointerView()
        view.session = session
        view.page = page
        view.escape = escape
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        return view
    }

    func updateNSView(_ view: PointerView, context: Context) {
        view.session = session
        view.page = page
        view.escape = escape
        if view.focusRequest != focusRequest {
            view.focusRequest = focusRequest
            DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        }
    }

    final class PointerView: NSView {
        weak var session: BrowserAnnotationSession?
        weak var page: NSView?
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
            case 126 where session?.page.selected != nil: Task { @MainActor [weak self] in await self?.session?.step(.parent) }
            case 125 where session?.page.selected != nil: Task { @MainActor [weak self] in await self?.session?.step(.child) }
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
