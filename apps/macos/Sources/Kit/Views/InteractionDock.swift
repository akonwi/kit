import AppKit
import SwiftUI

/// A pending request in the composer's place until it is answered or
/// cancelled: a header with what's being asked, the detail and answer, then
/// buttons. Keys follow the TUI's dock; only keys a person wouldn't guess,
/// y and n, are shown.
struct InteractionDock: View {
    @Environment(\.mica) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.openURL) private var openURL
    @Bindable var flow: InteractionFlow
    let pendingCount: Int
    /// Whether the dock takes keyboard focus when it appears. It waits while
    /// another tile is focused.
    var takesFocus = true
    /// The tallest the dock may grow; past it the body scrolls.
    var maxHeight: CGFloat = .infinity
    let finish: (Bool) -> Void
    @FocusState private var focused: Bool
    @State private var bodyHeight: CGFloat = 0
    /// Which way the last move went, so pages slide the way the questions run.
    @State private var direction = Direction.forward

    enum Direction { case forward, backward }

    /// Header and footer, which never scroll.
    private static let chrome: CGFloat = 36 + 40 + 2

    /// What the dock shows: a request and its question. Changing it swaps
    /// the body and animates the height.
    private struct Page: Hashable {
        let flow: UUID
        let step: Int
    }
    private var page: Page { Page(flow: flow.id, step: flow.step) }
    private var motion: Animation? { reduceMotion ? nil : .easeOut(duration: 0.2) }

    var body: some View {
        VStack(spacing: 0) {
            header
            Rule()
            ScrollView {
                ZStack(alignment: .top) {
                    content
                        .id(page)
                        // Forward slides in from the trailing edge; Back the other way.
                        .transition(reduceMotion ? .opacity
                                    : .push(from: direction == .forward ? .trailing : .leading).combined(with: .opacity))
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height in
                            // The first measurement sets the size; later ones animate to it.
                            if bodyHeight == 0 { bodyHeight = height }
                            else { withAnimation(motion) { bodyHeight = height } }
                        }
                }
            }
            .scrollBounceBehavior(.basedOnSize)
            .frame(height: min(bodyHeight, max(96, maxHeight - Self.chrome)))
            if let error = flow.error {
                Rule()
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).lineLimit(3)
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if hasFooter {
                Rule()
                footer
            }
        }
        .foregroundStyle(theme.text)
        .background(theme.frame, in: RoundedRectangle(cornerRadius: 4))
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
        // A text question focuses its field instead.
        .focusable(flow.mode != .text)
        .focusEffectDisabled()
        .focused($focused)
        .onKeyPress(phases: .down, action: handle)
        .onExitCommand { finish(true) }
        .animation(motion, value: page)
        .task(id: page) {
            guard takesFocus, flow.mode != .text else { return }
            await Task.yield()
            focused = true
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(flow.title)
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 10) {
            IconView(.question).foregroundStyle(theme.warning)
            ZStack(alignment: .leading) {
                Text(flow.title).font(.kit(size: 13, weight: .semibold)).lineLimit(1)
                    .id(flow.id).transition(.opacity)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 8) {
                if let plugin = flow.plugin {
                    Text(plugin).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                }
                if flow.guided { StepTrail(flow: flow) }
                if pendingCount > 1 {
                    Text("1 of " + String(pendingCount)).font(.kit(size: 11)).foregroundStyle(theme.muted)
                }
            }
            Button { finish(true) } label: {
                IconView(.x, size: 11)
                    .frame(width: 24, height: 24).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .disabled(flow.submitting)
            .help("Cancel request").accessibilityLabel("Cancel request")
        }
        .padding(.leading, 12).padding(.trailing, 6).frame(height: 36)
    }

    // MARK: Body

    private var content: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let unsupported = flow.unsupported {
                HStack(spacing: 8) {
                    IconView(.warningCircle, size: 12)
                    Text(unsupported).font(.kit(size: 12))
                }.foregroundStyle(theme.danger)
            } else {
                if !flow.detail.isEmpty { DockMarkdown(source: flow.detail) }
                if let url = flow.authorizationURL {
                    Link(url.absoluteString, destination: url)
                        .font(.kit(size: 12)).lineLimit(1).truncationMode(.middle)
                    Text(flow.authorizationRedirect == nil
                         ? "Approve access in your browser, then paste the address it ends up at."
                         : "Approve access at this address in Kit's browser, and Kit continues. In another browser, paste the address it ends up at.")
                        .font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
                if flow.guided {
                    Text(flow.question.prompt).font(.kit(size: 13, weight: .medium))
                    if !flow.question.detail.isEmpty { DockMarkdown(source: flow.question.detail) }
                }
                answer
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var answer: some View {
        switch flow.mode {
        case .text:
            InteractionTextInput(text: $flow.answers[flow.step].text, prompt: placeholder,
                                 submit: { forward { flow.submitText() } }, previous: { backward() },
                                 cancel: { finish(true) }, autofocus: takesFocus)
                .frame(height: 32)
                .background(theme.surface, in: RoundedRectangle(cornerRadius: 3))
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.focusBorder))
                .disabled(flow.submitting)
        case .choice, .multiple:
            DockOptionList(flow: flow) { index in
                focused = true
                forward { flow.choose(index) }
            }
        case .confirm, .unsupported:
            EmptyView()
        }
    }

    private var placeholder: String {
        if flow.guided { return "Type an answer" }
        if flow.authorizationURL != nil { return "Paste the address your browser ended up at" }
        return flow.request?.placeholder.flatMap { $0.isEmpty ? nil : $0 } ?? "Type a response"
    }

    // MARK: Footer

    /// A plain single choice answers on click or Return, so it has no footer.
    private var hasFooter: Bool { !(flow.mode == .choice && !flow.guided) }

    private var footer: some View {
        HStack(spacing: 12) {
            HStack {
                if flow.guided && flow.step > 0 {
                    Button { backward() } label: {
                        HStack(spacing: 4) {
                            IconView(.caretLeft, size: 10)
                            Text("Back")
                        }
                    }
                    .buttonStyle(DockButtonStyle(kind: .plain)).disabled(flow.submitting)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 6) {
                if flow.guided && !flow.question.required && flow.mode != .unsupported {
                    Button("Skip") { forward { flow.skip() } }
                        .buttonStyle(DockButtonStyle(kind: .plain)).disabled(flow.submitting)
                }
                actions
            }
        }
        .padding(.leading, 12).padding(.trailing, 8).frame(height: 40)
    }

    @ViewBuilder private var actions: some View {
        switch flow.mode {
        case .unsupported:
            Button("Cancel") { finish(true) }.buttonStyle(DockButtonStyle(kind: .bordered))
        case .confirm:
            // Without a default choice the affirmative answer leads.
            let affirmative = !flow.offersChoice || flow.confirming
            confirmButton(false, primary: !affirmative)
            confirmButton(true, primary: affirmative)
        case .text:
            if let url = flow.authorizationURL {
                Button("Open") { openURL(url) }
                    .buttonStyle(DockButtonStyle(kind: .bordered)).disabled(flow.submitting)
            }
            primary(flow.guided && !flow.isLastStep ? "Next" : "Submit") { forward { flow.submitText() } }
        case .choice, .multiple:
            primary(flow.isLastStep ? "Submit" : "Next") { forward { flow.next() } }
        }
    }

    private func confirmButton(_ value: Bool, primary: Bool) -> some View {
        let label = flow.question.optionLabels[value ? "true" : "false"] ?? (value ? "Yes" : "No")
        return Button { forward { flow.confirm(value) } } label: {
            HStack(spacing: 4) {
                if primary && flow.submitting { KitSpinner() }
                Text(label)
                Text(value ? "y" : "n").font(.kit(size: 11, design: .monospaced))
                    .foregroundStyle(primary ? theme.surface.opacity(0.5) : theme.muted)
                    .padding(.leading, 4)
            }
        }
        .buttonStyle(DockButtonStyle(kind: primary ? .primary : .bordered))
        .disabled(flow.submitting && !primary)
        .allowsHitTesting(!flow.submitting)
    }

    private func primary(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if flow.submitting { KitSpinner() }
                Text(title)
            }
        }
        .buttonStyle(DockButtonStyle(kind: .primary))
        .allowsHitTesting(!flow.submitting)
    }

    // MARK: Keys

    private func handle(_ press: KeyPress) -> KeyPress.Result {
        guard !flow.submitting, !press.modifiers.contains(.command) else { return .ignored }
        let key = press.characters.lowercased()
        if flow.guided && press.key == .tab {
            if press.modifiers.contains(.shift) { backward() } else { forward { flow.next() } }
            return .handled
        }
        switch flow.mode {
        case .confirm:
            if key == "y" { forward { flow.confirm(true) }; return .handled }
            if key == "n" { forward { flow.confirm(false) }; return .handled }
            guard flow.offersChoice else { return .ignored }
            if [.tab, .leftArrow, .rightArrow].contains(press.key) { flow.confirming.toggle(); return .handled }
            if press.key == .return { forward { flow.confirm(flow.confirming) }; return .handled }
        case .choice, .multiple:
            if press.key == .upArrow { flow.move(-1); return .handled }
            if press.key == .downArrow { flow.move(1); return .handled }
            // An option's letter answers with it, or toggles it in a multiple choice.
            if let index = DockOptionList.index(forKey: key, count: flow.question.options.count) {
                forward { flow.choose(index) }; return .handled
            }
            if flow.mode == .choice, press.key == .return || press.key == .space {
                forward { flow.choose(flow.cursor) }; return .handled
            }
            if press.key == .space { _ = flow.choose(flow.cursor); return .handled }
            if press.key == .return { forward { flow.continueMultiple() }; return .handled }
        case .text, .unsupported:
            break
        }
        return .ignored
    }

    /// Answers, skips, and Next move forward; a plugin's next request also
    /// arrives as a forward move.
    private func forward(_ action: @escaping () -> Bool) {
        go(.forward) { if action() { finish(false) } }
    }

    private func backward() {
        go(.backward) { flow.back() }
    }

    /// An outgoing page keeps the transition it last rendered with, so a
    /// reversal renders the new direction once before the page changes.
    private func go(_ direction: Direction, _ perform: @escaping () -> Void) {
        guard direction != self.direction else { perform(); return }
        self.direction = direction
        DispatchQueue.main.async { perform() }
    }
}

/// Answered, current, and remaining questions: ● ◉ ○ in the TUI. Long
/// questionnaires show a count instead.
private struct StepTrail: View {
    @Environment(\.mica) private var theme
    let flow: InteractionFlow

    var body: some View {
        Group {
            if flow.questions.count > 8 {
                Text(String(flow.step + 1) + "/" + String(flow.questions.count))
                    .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
            } else {
                HStack(spacing: 5) {
                    ForEach(flow.questions.indices, id: \.self) { index in
                        if index == flow.step {
                            Circle().strokeBorder(theme.accent, lineWidth: 1.5).frame(width: 8, height: 8)
                                .overlay(Circle().fill(theme.accent).frame(width: 3, height: 3))
                        } else if flow.answered(index) {
                            Circle().fill(theme.text).frame(width: 6, height: 6)
                        } else {
                            Circle().strokeBorder(theme.strongBorder, lineWidth: 1).frame(width: 6, height: 6)
                        }
                    }
                }
            }
        }
        .accessibilityElement()
        .accessibilityLabel("Question " + String(flow.step + 1) + " of " + String(flow.questions.count))
    }
}

/// Lettered options, each with its whole label and up to three lines of
/// detail below it. A list taller than `maxScrollHeight` scrolls, with the
/// hidden count at each edge.
struct DockOptionList: View {
    @Environment(\.mica) private var theme
    let flow: InteractionFlow
    let choose: (Int) -> Void
    @State private var hovered: Int?
    /// The scrolled-to part of the options.
    @State private var visible = CGRect.zero
    @State private var opened = false
    /// Each row's vertical extent within the scrolled content.
    @State private var frames: [Int: ClosedRange<CGFloat>] = [:]

    static let minimumRowHeight: CGFloat = 30
    static let spacing: CGFloat = 2
    /// The tallest a scrolling list grows before it scrolls.
    static let maxScrollHeight: CGFloat = 280
    /// Detail lines a row shows; the rest is in its tooltip.
    static let detailLines = 3
    private nonisolated static let space = "dock.options"

    /// Options hidden above and below a list scrolled this far: those whose
    /// middle is outside the viewport.
    static func hidden(frames: [ClosedRange<CGFloat>], offset: CGFloat, viewport: CGFloat) -> (above: Int, below: Int) {
        guard viewport > 0 else { return (0, 0) }
        let middles = frames.map { ($0.lowerBound + $0.upperBound) / 2 }
        return (middles.filter { $0 < offset }.count, middles.filter { $0 > offset + viewport }.count)
    }

    /// A–Z, then AA, AB, and so on, as in the TUI.
    static func marker(_ index: Int) -> String {
        let letters = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
        var remaining = index, marker = ""
        repeat {
            marker = String(letters[remaining % 26]) + marker
            remaining = remaining / 26 - 1
        } while remaining >= 0
        return marker
    }

    /// The option a typed letter picks. Only single letters are keys, so
    /// options past Z are reached with the arrows.
    static func index(forKey key: String, count: Int) -> Int? {
        guard key.count == 1, let scalar = key.lowercased().unicodeScalars.first,
              ("a"..."z").contains(scalar) else { return nil }
        let index = Int(scalar.value - UnicodeScalar("a").value)
        return index < count ? index : nil
    }

    var body: some View {
        let options = flow.question.options
        let hidden = Self.hidden(frames: options.indices.compactMap { frames[$0] },
                                 offset: visible.minY, viewport: visible.height)
        VStack(spacing: Self.spacing) {
            if hidden.above > 0 { overflow(hidden.above, up: true) }
            ScrollViewReader { proxy in
                ScrollView {
                    rows(options)
                }
                .scrollIndicators(.never)
                .scrollBounceBehavior(.basedOnSize)
                // As tall as the options, up to the limit.
                .frame(maxHeight: Self.maxScrollHeight)
                .fixedSize(horizontal: false, vertical: true)
                .onScrollGeometryChange(for: CGRect.self) { $0.visibleRect } action: { _, rect in visible = rect }
                .onChange(of: flow.cursor) { _, cursor in proxy.scrollTo(cursor) }
                // Each question mounts its own list; one answered earlier
                // opens on its answer once the options are laid out.
                .onScrollGeometryChange(for: Bool.self) { $0.contentSize.height > 0 } action: { _, laidOut in
                    guard laidOut, !opened else { return }
                    opened = true
                    proxy.scrollTo(flow.cursor)
                }
            }
            if hidden.below > 0 { overflow(hidden.below, up: false) }
        }
        .padding(.horizontal, -6)
    }

    private func rows(_ options: [String]) -> some View {
        VStack(spacing: Self.spacing) {
            ForEach(Array(options.enumerated()), id: \.offset) { index, option in
                DockOptionRow(flow: flow, index: index, option: option, hovered: hovered == index,
                              hover: { hovered = $0 ? index : (hovered == index ? nil : hovered) }, choose: choose)
                    .onGeometryChange(for: ClosedRange<CGFloat>.self) { proxy in
                        let frame = proxy.frame(in: .named(Self.space))
                        return frame.minY...max(frame.minY, frame.maxY)
                    } action: { frames[index] = $0 }
                    .id(index)
            }
        }
        .coordinateSpace(.named(Self.space))
    }

    private func overflow(_ count: Int, up: Bool) -> some View {
        HStack(spacing: 6) {
            IconView(up ? .caretUp : .caretDown, size: 10)
            Text(String(count) + " more").font(.kit(size: 11))
        }
        .foregroundStyle(theme.muted)
        // Under the labels.
        .padding(.leading, DockOptionRow.padding.leading + DockOptionRow.markerWidth + DockOptionRow.spacing)
        .frame(maxWidth: .infinity, minHeight: 22, alignment: .leading)
    }
}

/// One lettered option. The letter and checkbox sit on the label's first
/// line, and the label is never cut short. The highlight is a fill, so moving
/// it never rewraps a row.
struct DockOptionRow: View {
    @Environment(\.mica) private var theme
    let flow: InteractionFlow
    let index: Int
    let option: String
    var hovered = false
    var hover: (Bool) -> Void = { _ in }
    let choose: (Int) -> Void

    static let padding = EdgeInsets(top: 6, leading: 12, bottom: 6, trailing: 10)
    static let markerWidth: CGFloat = 12
    static let spacing: CGFloat = 10
    /// Between the label and its detail.
    static let detailSpacing: CGFloat = 2

    private static var capHeight: CGFloat { Typography.shared.font(size: 13, weight: .medium).capHeight }

    var body: some View {
        let highlighted = index == flow.cursor
        let checked = flow.answers[flow.step].choices.contains(option)
        let detail = flow.question.optionDetails[option] ?? ""
        let capHeight = Self.capHeight
        HStack(alignment: .firstTextBaseline, spacing: Self.spacing) {
            Text(DockOptionList.marker(index)).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .frame(width: Self.markerWidth, alignment: .leading)
            if flow.mode == .multiple {
                // Centred on the label's capitals.
                DockCheckbox(checked: checked).alignmentGuide(.firstTextBaseline) { $0.height / 2 + capHeight / 2 }
            }
            VStack(alignment: .leading, spacing: Self.detailSpacing) {
                Text(flow.question.optionLabels[option] ?? option)
                    .font(.kit(size: 13, weight: .medium))
                    .fixedSize(horizontal: false, vertical: true)
                if !detail.isEmpty { DockOptionDetail(text: detail) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(Self.padding)
        .frame(minHeight: DockOptionList.minimumRowHeight)
        .background(highlighted ? theme.accent.opacity(0.18) : hovered ? theme.hover : .clear,
                    in: RoundedRectangle(cornerRadius: 3))
        .contentShape(Rectangle())
        .onHover(perform: hover)
        .onTapGesture { if !flow.submitting { choose(index) } }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(checked ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction { choose(index) }
    }
}

/// An option's detail, up to `DockOptionList.detailLines` lines. Detail cut
/// short is in full in its tooltip.
private struct DockOptionDetail: View {
    @Environment(\.mica) private var theme
    let text: String
    @State private var shown: CGFloat = 0
    @State private var full: CGFloat = 0

    var body: some View {
        let detail = Text(text).font(.kit(size: 12))
        detail.foregroundStyle(theme.muted)
            .lineLimit(DockOptionList.detailLines).truncationMode(.tail)
            .fixedSize(horizontal: false, vertical: true)
            // The column's width, for the measurement and the tooltip's target.
            .frame(maxWidth: .infinity, alignment: .leading)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { shown = $0 }
            // The whole detail at this width, measured to tell if it's cut.
            .background(alignment: .top) {
                detail.fixedSize(horizontal: false, vertical: true).hidden()
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { full = $0 }
            }
            .help(full > shown + 0.5 ? text : "")
            // The row already reads the whole detail.
            .accessibilityHint("")
    }
}

struct DockCheckbox: View {
    @Environment(\.mica) private var theme
    let checked: Bool
    static let size: CGFloat = 14

    var body: some View {
        if checked {
            RoundedRectangle(cornerRadius: 2).fill(theme.accent).frame(width: Self.size, height: Self.size)
                .overlay(IconView(.check, size: 10).foregroundStyle(theme.surface))
        } else {
            RoundedRectangle(cornerRadius: 2).strokeBorder(theme.strongBorder).frame(width: Self.size, height: Self.size)
        }
    }
}

/// Muted Markdown for request and question details; code blocks keep their
/// formatting.
private struct DockMarkdown: View {
    @Environment(\.mica) private var theme
    let source: String

    var body: some View {
        MarkdownView(source: source, size: 12).foregroundStyle(theme.muted)
            .environment(\.compactCodeBlocks, true)
    }
}

/// The dock's 24 pt buttons: the text-coloured primary, like Stop; bordered;
/// and plain for Back and Skip.
private struct DockButtonStyle: ButtonStyle {
    enum Kind { case primary, bordered, plain }
    let kind: Kind

    func makeBody(configuration: Configuration) -> some View {
        Label(configuration: configuration, kind: kind)
    }

    private struct Label: View {
        @Environment(\.mica) private var theme
        @Environment(\.isEnabled) private var enabled
        @State private var hovering = false
        let configuration: Configuration
        let kind: Kind

        var body: some View {
            configuration.label
                .font(.kit(size: 12)).lineLimit(1)
                .foregroundStyle(kind == .primary ? theme.surface : kind == .plain ? theme.muted : theme.text)
                .padding(.horizontal, 10).frame(height: 24)
                .background(fill, in: RoundedRectangle(cornerRadius: 3))
                .overlay {
                    if kind == .bordered { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border) }
                }
                .contentShape(Rectangle())
                .opacity(enabled ? 1 : 0.45)
                .onHover { hovering = enabled && $0 }
        }

        private var fill: Color {
            if kind == .primary { return theme.text.opacity(configuration.isPressed ? 0.85 : 1) }
            return hovering || configuration.isPressed ? theme.hover : .clear
        }
    }
}
