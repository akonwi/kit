import SwiftUI

// MARK: Conversation

struct AgentPane: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tileID: UUID
    let focused: Bool
    @State private var toolsOpen = true

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    if session.title == "Tiling the native shell" {
                        tilingTranscript
                    } else {
                        UserBubble(text: "Pick up where we left off on \(session.title.lowercased()).")
                        Text("Reading the branch and the last review comments first.")
                            .font(Fonts.ui(13)).lineSpacing(4).foregroundStyle(theme.text)
                    }
                }
                .textSelection(.enabled)
                .padding(.horizontal, 28)
                .padding(.vertical, 22)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollIndicators(.automatic)

            VStack(spacing: 10) {
                if let activity = session.activity, session.question == nil {
                    ActivityCard(text: activity, expanded: Binding(
                        get: { session.thinkingExpanded }, set: { session.thinkingExpanded = $0 }))
                }
                if session.question != nil {
                    InteractionDock(session: session, focused: focused)
                } else {
                    Composer(session: session, tileID: tileID, focused: focused,
                             placeholder: "Ask about the diff, or reply to the reviewer…")
                }
            }
            .padding(.horizontal, 14)
            .padding(.bottom, 14)
        }
    }

    @ViewBuilder private var tilingTranscript: some View {
        UserBubble(text: "Split the review into its own tile and keep the agent on the left. The reviewer should be able to run beside it.")
        ToolGroup(tools: Fixtures.agentTools, open: $toolsOpen)
        VStack(alignment: .leading, spacing: 10) {
            Text("The workspace is now a split tree of tiles, and every tile holds tabs. New panes open as a tab in the focused tile; split or drag one to an edge to put it beside the conversation.")
            Text("I started a reviewer on the change. It will report back here when it's done.")
        }
        .font(Fonts.ui(13)).lineSpacing(4).foregroundStyle(theme.text)
        HStack(spacing: 10) {
            BrailleSpinner(color: theme.accent)
            Text("reviewer").font(Fonts.ui(13)).foregroundStyle(theme.text)
            Text("Reading WorkspaceSplitTree.swift").font(Fonts.ui(12)).foregroundStyle(theme.muted)
            Spacer()
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
    }
}

struct ReviewerPane: View {
    @Environment(\.theme) private var theme
    let session: Session
    let tileID: UUID
    let focused: Bool
    @State private var toolsOpen = true
    @State private var expanded = false
    @State private var text = ""

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    ToolGroup(tools: Fixtures.reviewerTools, open: $toolsOpen).padding(.leading, -8)
                    Text("The clamp looks right, but equalized() only resets the root split. Nested splits keep their old ratios, so a three-tile layout won't even out.")
                        .font(Fonts.ui(13)).lineSpacing(4).foregroundStyle(theme.text)
                }
                .textSelection(.enabled)
                .padding(.horizontal, 18).padding(.vertical, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            VStack(spacing: 8) {
                ActivityCard(text: "Checking whether resize preserves the zoomed node…", expanded: $expanded)
                HStack {
                    TextField("Message reviewer…", text: $text)
                        .textFieldStyle(.plain).font(Fonts.ui(13)).foregroundStyle(theme.text)
                    Button {} label: {
                        Icon(glyph: .stop, size: 10, color: theme.surface, filled: true)
                            .frame(width: 22, height: 22)
                            .background(RoundedRectangle(cornerRadius: 3).fill(theme.text))
                    }.buttonStyle(.plain)
                }
                .padding(.leading, 12).padding(.trailing, 6)
                .frame(height: 34)
                .background(RoundedRectangle(cornerRadius: 4).fill(theme.raised))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            }
            .padding(.horizontal, 12).padding(.bottom, 12)
        }
    }
}

struct UserBubble: View {
    @Environment(\.theme) private var theme
    let text: String

    var body: some View {
        HStack {
            Spacer(minLength: 60)
            Text(text)
                .font(Fonts.ui(13)).lineSpacing(3).foregroundStyle(theme.text)
                .padding(.horizontal, 14).padding(.vertical, 10)
                .background(RoundedRectangle(cornerRadius: 4).fill(theme.userTint))
                .frame(maxWidth: 440, alignment: .trailing)
        }
    }
}

struct ToolGroup: View {
    @Environment(\.theme) private var theme
    let tools: [Fixtures.ToolCall]
    @Binding var open: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Button { withAnimation(.easeOut(duration: 0.15)) { open.toggle() } } label: {
                HStack(spacing: 10) {
                    Icon(glyph: open ? .caretDown : .caretRight, size: 10, color: theme.muted).frame(width: 16)
                    Text("\(tools.count) tool calls").font(Fonts.ui(12)).foregroundStyle(theme.muted)
                }
                .padding(.leading, 8).frame(height: 24).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                ForEach(tools) { ToolRow(tool: $0) }
            }
        }
    }
}

struct ToolRow: View {
    @Environment(\.theme) private var theme
    let tool: Fixtures.ToolCall
    @State private var hover = false

    var body: some View {
        HStack(spacing: 10) {
            Icon(glyph: tool.icon, size: 14, color: theme.muted).frame(width: 16)
            Text(tool.verb).font(Fonts.ui(13)).foregroundStyle(theme.text).frame(width: 112, alignment: .leading)
            CodeChip(text: tool.chip).frame(maxWidth: .infinity, alignment: .leading)
            if let added = tool.added { Text("+\(added)").font(Fonts.mono(11)).foregroundStyle(theme.success) }
            if let removed = tool.removed { Text("−\(removed)").font(Fonts.mono(11)).foregroundStyle(theme.danger) }
            if let meta = tool.meta { Text(meta).font(Fonts.mono(11)).foregroundStyle(theme.muted) }
            Icon(glyph: .caretRight, size: 10, color: hover ? theme.text : theme.muted)
        }
        .padding(.horizontal, 8)
        .frame(height: 28)
        .background(RoundedRectangle(cornerRadius: 3).fill(hover ? theme.raised : .clear))
        .onHover { hover = $0 }
    }
}

/// The activity line: one card above the composer, expands upward.
struct ActivityCard: View {
    @Environment(\.theme) private var theme
    let text: String
    @Binding var expanded: Bool
    @State private var hover = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if expanded {
                VStack(alignment: .leading, spacing: 8) {
                    inlineCode("`equalized()` only touches `root.withRatio(0.5)`, so the nested split keeps whatever ratio it had after the last drag.", theme: theme, size: 12, color: theme.muted)
                    inlineCode("Recursing would fix it, but a zoomed tree swaps the root for the zoomed leaf.", theme: theme, size: 12, color: theme.muted)
                    inlineCode(text, theme: theme, size: 12)
                }
                .padding(.horizontal, 13).padding(.top, 10).padding(.bottom, 6)
                .textSelection(.enabled)
                .transition(.opacity)
            }
            HStack(spacing: 8) {
                BrailleSpinner(color: theme.accent)
                inlineCode(text, theme: theme, size: 13, color: theme.muted).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 4)
                Icon(glyph: expanded ? .caretDown : .caretUp, size: 10, color: hover ? theme.text : theme.muted)
            }
            .padding(.leading, 13).padding(.trailing, 10)
            .frame(height: 30)
        }
        .background(RoundedRectangle(cornerRadius: 4).fill(hover ? theme.frameHover : theme.frame))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture { withAnimation(.easeOut(duration: 0.15)) { expanded.toggle() } }
    }
}

// MARK: Composer

struct Composer: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tileID: UUID
    let focused: Bool
    let placeholder: String
    @FocusState private var fieldFocused: Bool
    @State private var modelName = "Opus 4.6 · high"

    var body: some View {
        @Bindable var session = session
        VStack(alignment: .leading, spacing: 10) {
            if !session.commentChips.isEmpty {
                HStack(spacing: 6) {
                    ForEach(session.commentChips, id: \.self) { chip in
                        HStack(spacing: 6) {
                            Icon(glyph: .chatCenteredText, size: 12, color: theme.accent)
                            Text(chip).font(Fonts.mono(11)).foregroundStyle(theme.text)
                            Button { session.commentChips.removeAll { $0 == chip } } label: {
                                Icon(glyph: .x, size: 10, color: theme.muted)
                            }.buttonStyle(.plain)
                        }
                        .padding(.horizontal, 8).frame(height: 24)
                        .background(RoundedRectangle(cornerRadius: 3).fill(theme.userTint))
                    }
                }
            }
            TextField(placeholder, text: $session.draft, axis: .vertical)
                .textFieldStyle(.plain)
                .font(Fonts.ui(13))
                .foregroundStyle(theme.text)
                .lineLimit(1...10)
                .focused($fieldFocused)
                .onSubmit(send)
            HStack {
                Menu {
                    ForEach(["Opus 4.6 · high", "Sonnet 4.6 · medium", "GPT-5.1 · high"], id: \.self) { name in
                        Button(name) { modelName = name }
                    }
                } label: {
                    Text(modelName).font(Fonts.ui(12)).foregroundStyle(theme.muted)
                        .padding(.horizontal, 8).frame(height: 24)
                        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                }
                .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden).fixedSize()
                Spacer()
                Button(action: send) {
                    Icon(glyph: session.status == .running ? .stop : .arrowUp, size: 12, color: theme.surface,
                         filled: session.status == .running)
                        .frame(width: 26, height: 26)
                        .background(RoundedRectangle(cornerRadius: 3).fill(theme.text))
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .background(RoundedRectangle(cornerRadius: 4).fill(theme.raised))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
        .onAppear { syncFocus() }
        .onChange(of: model.focusGeneration) { syncFocus() }
        .onChange(of: fieldFocused) { _, now in if now && session.focusedTile != tileID { model.focus(tile: tileID) } }
    }

    private func syncFocus() {
        guard !model.paletteOpen else { return }
        if session.focusedTile == tileID && model.current === session { fieldFocused = true }
    }

    private func send() {
        guard !session.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        session.draft = ""
        session.status = .running
        session.activity = "Working…"
        model.toast(Toast(variant: .info, title: "Follow-ups sent", detail: "Promoted 1 queued message."))
    }
}

// MARK: Interaction dock

struct InteractionDock: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let focused: Bool
    @FocusState private var keyFocus: Bool

    var body: some View {
        if let q = session.question {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 8) {
                    Icon(glyph: .question, size: 14, color: theme.warning)
                    Text(q.title).font(Fonts.ui(13, weight: .semibold)).foregroundStyle(theme.text)
                    Spacer()
                    HStack(spacing: 4) {
                        ForEach(0..<q.steps, id: \.self) { i in
                            Circle()
                                .fill(i == q.step ? theme.accent : (i < q.step ? theme.text : .clear))
                                .overlay(Circle().strokeBorder(i > q.step ? theme.muted : .clear))
                                .frame(width: 6, height: 6)
                        }
                    }
                    IconButton(glyph: .x, size: 20, iconSize: 10, help: "Cancel") { model.answer(nil) }
                }
                .padding(.horizontal, 12).frame(height: 38)
                Rectangle().fill(theme.border).frame(height: 1)
                VStack(alignment: .leading, spacing: 4) {
                    Text(q.prompt).font(Fonts.ui(13, weight: .medium)).foregroundStyle(theme.text)
                        .padding(.bottom, 6)
                    ForEach(Array(q.options.enumerated()), id: \.offset) { index, option in
                        let on = index == q.highlighted
                        HStack(spacing: 14) {
                            Text(String(UnicodeScalar(65 + index)!)).font(Fonts.mono(11)).foregroundStyle(theme.muted)
                            Text(option.title).font(Fonts.ui(13)).foregroundStyle(theme.text).frame(width: 64, alignment: .leading)
                            Text(option.detail).font(Fonts.ui(12)).foregroundStyle(theme.muted)
                            Spacer()
                        }
                        .padding(.horizontal, 10).frame(height: 28)
                        .background(RoundedRectangle(cornerRadius: 2).fill(on ? theme.frameHover : .clear))
                        .overlay(alignment: .leading) { if on { Rectangle().fill(theme.accent).frame(width: 2) } }
                        .contentShape(Rectangle())
                        .onTapGesture { model.answer(index) }
                    }
                }
                .padding(12)
                HStack {
                    if q.step > 0 { SmallButton(title: "Back") {} }
                    Spacer()
                    SmallButton(title: q.step + 1 == q.steps ? "Submit" : "Next", primary: true) { model.answer(q.highlighted) }
                }
                .padding(.horizontal, 12).padding(.bottom, 12)
            }
            .background(RoundedRectangle(cornerRadius: 4).fill(theme.frame))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .focusable()
            .focusEffectDisabled()
            .focused($keyFocus)
            .onKeyPress(.upArrow) { move(-1); return .handled }
            .onKeyPress(.downArrow) { move(1); return .handled }
            .onKeyPress(.return) { model.answer(q.highlighted); return .handled }
            .onKeyPress(.escape) { model.answer(nil); return .handled }
            .onKeyPress(characters: .letters) { press in
                let index = Int(press.characters.uppercased().unicodeScalars.first!.value) - 65
                guard q.options.indices.contains(index) else { return .ignored }
                model.answer(index)
                return .handled
            }
            .onAppear { if focused { keyFocus = true } }
            .onChange(of: model.focusGeneration) { if focused && !model.paletteOpen { keyFocus = true } }
        }
    }

    private func move(_ delta: Int) {
        guard var q = session.question else { return }
        q.highlighted = (q.highlighted + delta + q.options.count) % q.options.count
        session.question = q
    }
}

// MARK: Diff

struct DiffPane: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    @State private var collapsed: Set<String> = ["Views/WorkspaceLayout.swift", "Views/TerminalSurfaceView.swift"]
    @State private var comments: [UUID: String] = [:]
    @State private var editing: UUID?

    var body: some View {
        ScrollView([.vertical]) {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Fixtures.diff) { file in
                    fileHeader(file)
                    if !collapsed.contains(file.path) {
                        ForEach(file.lines) { line in
                            DiffLineRow(line: line, commented: comments[line.id] != nil || line.new == 38 && file.path.hasPrefix("Models")) {
                                editing = line.id
                            }
                            if line.new == 38 && file.path.hasPrefix("Models") {
                                CommentCard(text: "Clamp before resizing the children, not after. Otherwise a collapsed tile can't be dragged back open.")
                            }
                            if let text = comments[line.id] { CommentCard(text: text) }
                            if editing == line.id {
                                CommentEditor { text in
                                    comments[line.id] = text
                                    editing = nil
                                    let path = (file.path as NSString).lastPathComponent
                                    session.commentChips.append("\(path):\(line.new ?? line.old ?? 0)")
                                } cancel: { editing = nil }
                            }
                        }
                    }
                }
            }
        }
    }

    private func fileHeader(_ file: Fixtures.DiffFile) -> some View {
        let open = !collapsed.contains(file.path)
        return HStack(spacing: 8) {
            Icon(glyph: open ? .caretDown : .caretRight, size: 10, color: theme.muted)
            Text(file.path).font(Fonts.mono(12)).foregroundStyle(theme.text)
            Spacer()
            Text("+\(file.added)").font(Fonts.mono(12)).foregroundStyle(theme.success)
            Text("−\(file.removed)").font(Fonts.mono(12)).foregroundStyle(theme.danger)
        }
        .padding(.horizontal, 16).frame(height: 34)
        .background(theme.raised)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.border).frame(height: 1) }
        .contentShape(Rectangle())
        .onTapGesture {
            if open { collapsed.insert(file.path) } else { collapsed.remove(file.path) }
        }
    }
}

private struct DiffLineRow: View {
    @Environment(\.theme) private var theme
    let line: Fixtures.DiffLine
    let commented: Bool
    let addComment: () -> Void
    @State private var hover = false

    var body: some View {
        let (bg, fg, gutter, marker): (Color, Color, Color, String) = switch line.kind {
        case .context: (.clear, theme.text, theme.strongBorder, "")
        case .added: (theme.addedBg, theme.addedText, theme.addedGutter, "+")
        case .removed: (theme.removedBg, theme.removedText, theme.removedGutter, "−")
        }
        HStack(spacing: 0) {
            Text(line.old.map(String.init) ?? "").frame(width: 30, alignment: .trailing).padding(.trailing, 6)
                .foregroundStyle(gutter)
            Text(line.new.map(String.init) ?? "").frame(width: 30, alignment: .trailing).padding(.trailing, 6)
                .foregroundStyle(gutter)
            ZStack {
                if hover {
                    Button(action: addComment) {
                        Text("+").font(Fonts.mono(12, weight: .semibold)).foregroundStyle(theme.surface)
                            .frame(width: 16, height: 16)
                            .background(RoundedRectangle(cornerRadius: 2).fill(theme.accent))
                    }.buttonStyle(.plain)
                } else {
                    Text(marker).foregroundStyle(line.kind == .added ? theme.success : theme.danger)
                }
            }
            .frame(width: 16)
            Text(line.text).foregroundStyle(fg).fixedSize().textSelection(.enabled)
            Spacer(minLength: 0)
        }
        .font(Fonts.mono(12))
        .frame(height: 20)
        .background(bg)
        .overlay(alignment: .leading) { if commented { Rectangle().fill(theme.accent).frame(width: 2) } }
        .onHover { hover = $0 }
    }
}

private struct CommentCard: View {
    @Environment(\.theme) private var theme
    let text: String
    var body: some View {
        Text(text).font(Fonts.ui(13)).lineSpacing(3).foregroundStyle(theme.text)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12).padding(.vertical, 10)
            .background(RoundedRectangle(cornerRadius: 4).fill(theme.raised))
            .padding(.leading, 76).padding(.trailing, 16).padding(.top, 8).padding(.bottom, 10)
            .textSelection(.enabled)
    }
}

private struct CommentEditor: View {
    @Environment(\.theme) private var theme
    let save: (String) -> Void
    let cancel: () -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .trailing, spacing: 8) {
            TextField("Comment on this line…", text: $text, axis: .vertical)
                .textFieldStyle(.plain).font(Fonts.ui(13)).foregroundStyle(theme.text)
                .focused($focused).onSubmit { if !text.isEmpty { save(text) } }
                .onKeyPress(.escape) { cancel(); return .handled }
            HStack {
                SmallButton(title: "Cancel", action: cancel)
                SmallButton(title: "Comment", primary: true) { if !text.isEmpty { save(text) } }
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 4).fill(theme.raised))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.accent))
        .padding(.leading, 76).padding(.trailing, 16).padding(.vertical, 8)
        .onAppear { focused = true }
    }
}

// MARK: File + scratchpad

struct FilePane: View {
    @Environment(\.theme) private var theme
    let path: String

    var body: some View {
        let url = URL(fileURLWithPath: Fixtures.workingDirectory).appendingPathComponent("apps/macos/Sources/Kit/Support/WorkspaceSplitDivider.swift")
        let text = (try? String(contentsOf: url, encoding: .utf8)) ?? "// \(path)"
        ScrollView([.vertical, .horizontal]) {
            HStack(alignment: .top, spacing: 12) {
                VStack(alignment: .trailing, spacing: 0) {
                    ForEach(1...max(text.split(separator: "\n", omittingEmptySubsequences: false).count, 1), id: \.self) {
                        Text("\($0)").frame(height: 18)
                    }
                }
                .foregroundStyle(theme.strongBorder)
                Text(text).lineSpacing(18 - 14.5).foregroundStyle(theme.text).textSelection(.enabled).fixedSize()
            }
            .font(Fonts.mono(12))
            .padding(16)
        }
    }
}

struct ScratchpadPane: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let tileID: UUID
    let focused: Bool
    @State private var text = "# Tiling\n\n- clamp divider ratio 0.15–0.85\n- equalize nested splits\n- zoom keeps native views alive\n"
    @FocusState private var editorFocused: Bool

    var body: some View {
        TextEditor(text: $text)
            .font(Fonts.mono(12))
            .foregroundStyle(theme.text)
            .scrollContentBackground(.hidden)
            .padding(12)
            .focused($editorFocused)
            .onAppear { if focused { editorFocused = true } }
            .onChange(of: model.focusGeneration) { if focused && !model.paletteOpen { editorFocused = true } }
    }
}
