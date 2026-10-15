import SwiftUI

/// The detail well of an open tool row. Content is laid out per tool; every
/// well stops at 14 lines and scrolls.
struct ToolCallDetail: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptToolContext) private var context
    let tool: ToolActivity
    let workspace: WorkspaceState
    var presentation: ToolPresentation? = nil

    /// Fourteen 18 pt lines and the well's vertical padding.
    static let maximumHeight: CGFloat = 14 * 18 + 18
    /// Content aligns with the row's title, past the icon column.
    static let inset: CGFloat = 34

    @State private var contentHeight: CGFloat = 1

    private var value: ToolPresentation { presentation ?? ToolPresentation(tool, servers: context.servers) }

    var body: some View {
        ScrollView(.vertical) {
            VStack(alignment: .leading, spacing: 8) {
                content
                TranscriptAttachments(attachments: (tool.attachments ?? []).filter { tool.name != "show_image" || !$0.isImage })
                    .padding(.horizontal, Self.inset)
                if tool.contentTruncated == true {
                    Text("Output truncated").font(.kit(size: 11)).foregroundStyle(theme.muted)
                        .padding(.leading, Self.inset)
                }
            }
            .font(.kit(size: 12)).foregroundStyle(theme.text)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.top, 8).padding(.bottom, 10)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
        }
        // A running command's well follows the end of its output.
        .defaultScrollAnchor(tool.running && tool.bashCommand != nil ? .bottom : .top)
        .frame(height: min(max(1, contentHeight), Self.maximumHeight))
    }

    @ViewBuilder private var content: some View {
        if tool.bashCommand != nil {
            command
        } else if value.server != nil {
            padded { structured(tool.output) }
        } else if tool.failed && !["create_session", "peer_session"].contains(tool.name) {
            padded { mono(output, color: theme.danger) }
        } else {
            switch tool.name {
            case "create_session": padded { created }
            case "peer_session": padded { PeerSessionDetail(value: PeerSessionPresentation(tool)) }
            case "read":
                if value.readContent.isEmpty { padded { note("Empty file") } }
                else { padded { numbered(value.readContent, from: value.startLine, language: value.language) } }
                if value.readTruncated { padded { note("Read output truncated") } }
            case "write":
                if let content = value.string("content") { padded { numbered(content, from: 1, language: value.language) } }
                else { padded { mono(output) } }
            case "edit", "edit_scratchpad":
                if value.edits.isEmpty { padded { mono(output) } }
                else { edits }
            case "subagent": padded { agent }
            case "grep":
                if let matches = value.matches, !matches.isEmpty { padded { searchResults(matches) } }
                else { padded { mono(output) } }
            case "find", "glob": padded { paths(base: nil) }
            case "ls": padded { paths(base: value.string("path")) }
            case "confirm_from_user", "input_from_user", "select_from_user", "guided_questions": padded { ask }
            case "show_image", "inspect_image": EmptyView()
            default:
                if value.pluginTool == nil, let arguments = tool.arguments, !arguments.isEmpty, arguments != "{}" {
                    padded { mono(Self.pretty(arguments), color: theme.muted) }
                }
                padded { structured(tool.output) }
            }
        }
    }

    private var output: String { tool.output.isEmpty ? "No text output recorded." : tool.output }

    private func padded(@ViewBuilder _ content: () -> some View) -> some View {
        content().padding(.leading, Self.inset).padding(.trailing, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func mono(_ text: String, color: Color? = nil) -> some View {
        Text(text).font(.kit(size: 11, design: .monospaced)).lineSpacing(4)
            .foregroundStyle(color ?? theme.text).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func note(_ text: String) -> some View {
        Text(text).font(.kit(size: 11)).foregroundStyle(theme.muted)
    }

    private func caption(_ text: String) -> some View {
        Text(text.uppercased()).font(.kit(size: 10, weight: .medium)).tracking(0.8).foregroundStyle(theme.muted)
    }

    // MARK: Commands

    @ViewBuilder private var command: some View {
        padded { mono("$ " + (tool.bashCommand ?? ""), color: theme.muted) }
        if !tool.output.isEmpty {
            if !tool.running, UnifiedDiff.matches(tool.output) {
                UnifiedDiffView(diff: UnifiedDiff(tool.output))
            } else {
                padded { mono(tool.output) }
            }
        }
    }

    // MARK: Files

    private func numbered(_ source: String, from start: Int, language: String) -> some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                Text(ToolPresentation.lines(source).indices.map { String(start + $0) }.joined(separator: "\n"))
                    .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.strongBorder)
                    .multilineTextAlignment(.trailing).accessibilityHidden(true)
                HighlightedCodeText(language: language, source: source, fontSize: 11)
                    .fixedSize(horizontal: true, vertical: true)
            }
        }.defaultScrollAnchor(.topLeading, for: .alignment).scrollIndicators(.never)
    }

    @ViewBuilder private var edits: some View {
        let all = value.edits
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(all.enumerated()), id: \.offset) { index, edit in
                if index > 0 {
                    DashedRule().padding(.leading, Self.inset).padding(.trailing, 12).frame(height: 18)
                }
                DiffRows(lines: ToolDiffLine.build(edit))
                if let notice = edit.newlineNotice {
                    note(notice).padding(.leading, Self.inset).padding(.top, 4)
                }
            }
        }
    }

    private func searchResults(_ matches: [ToolPresentation.Match]) -> some View {
        let paths = matches.reduce(into: [String]()) { if !$0.contains($1.path) { $0.append($1.path) } }
        return VStack(alignment: .leading, spacing: 2) {
            ForEach(Array(paths.enumerated()), id: \.element) { index, path in
                FileLink(text: path, path: path, monospaced: false, cwd: context.cwd, workspace: workspace)
                    .padding(.top, index == 0 ? 0 : 6)
                ForEach(Array(matches.filter { $0.path == path }.enumerated()), id: \.offset) { _, match in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(String(match.line)).foregroundStyle(theme.strongBorder)
                        Text(emphasized(match.text)).lineLimit(1).textSelection(.enabled)
                    }.font(.kit(size: 11, design: .monospaced))
                }
            }
        }
    }

    private func emphasized(_ text: String) -> AttributedString {
        var result = AttributedString(text)
        guard let pattern = value.string("pattern"),
              let regex = try? NSRegularExpression(pattern: pattern) else { return result }
        for match in regex.matches(in: text, range: NSRange(text.startIndex..., in: text)) {
            if let range = Range(match.range, in: text), let attributed = Range(range, in: result) {
                result[attributed].backgroundColor = theme.accent.opacity(0.18)
                result[attributed].font = .kit(size: 11, weight: .semibold, design: .monospaced)
            }
        }
        return result
    }

    /// Found or listed paths. Files open; folders, listed first, are muted.
    @ViewBuilder private func paths(base: String?) -> some View {
        let entries = ToolPresentation.lines(tool.output).filter { !$0.hasPrefix("[") && !$0.isEmpty }
        if tool.output.isEmpty || tool.output.hasPrefix("No ") {
            mono(output, color: theme.muted)
        } else {
            let folders = entries.filter { $0.hasSuffix("/") }, files = entries.filter { !$0.hasSuffix("/") }
            VStack(alignment: .leading, spacing: 0) {
                ForEach(folders, id: \.self) { folder in
                    Text(folder).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                        .frame(height: 18)
                }
                ForEach(files, id: \.self) { file in
                    FileLink(text: file, path: base.map { ($0 as NSString).appendingPathComponent(file) } ?? file,
                             monospaced: true, cwd: context.cwd, workspace: workspace)
                        .frame(height: 18)
                }
            }
        }
    }

    // MARK: Agents and sessions

    @ViewBuilder private var agent: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let task = value.string("message"), !task.isEmpty {
                VStack(alignment: .leading, spacing: 3) {
                    caption("Task")
                    MarkdownView(source: task)
                }
            }
            if !tool.output.isEmpty {
                if value.string("message")?.isEmpty == false { Rule() }
                VStack(alignment: .leading, spacing: 3) {
                    caption("Result")
                    MarkdownView(source: tool.output)
                }
            }
        }
    }

    @ViewBuilder private var created: some View {
        let session = CreatedSessionPresentation(tool)
        VStack(alignment: .leading, spacing: 6) {
            if !session.directory.isEmpty { field("Folder") { mono(session.directory) } }
            if let prompt = session.prompt { field("Prompt") { MarkdownView(source: prompt) } }
            if session.failed || session.sessionID == nil {
                Text(session.result).foregroundStyle(session.failed ? theme.danger : theme.muted).textSelection(.enabled)
            }
        }
    }

    private func field(_ label: String, @ViewBuilder _ value: () -> some View) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label).foregroundStyle(theme.muted).frame(width: 64, alignment: .leading)
            value()
        }
    }

    // MARK: Questions for the user

    @ViewBuilder private var ask: some View {
        let ask = value.ask
        VStack(alignment: .leading, spacing: 6) {
            if let detail = ask.detail { MarkdownView(source: detail).foregroundStyle(theme.muted) }
            if tool.name == "guided_questions" {
                ForEach(Array(ask.answers.enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        Text(item.question).foregroundStyle(theme.muted).frame(width: 200, alignment: .leading)
                        Text(item.answer ?? "Skipped").foregroundStyle(item.answer == nil ? theme.muted : theme.text)
                            .textSelection(.enabled)
                    }
                }
            } else if let answer = ask.answer {
                Text(answer).textSelection(.enabled)
            } else if let outcome = ask.outcome, ask.detail != nil {
                Text(outcome).foregroundStyle(outcome == "Cancelled" ? theme.muted : theme.text)
            }
        }
    }

    // MARK: Structured output

    /// Output that parses as JSON is pretty-printed; anything else is plain.
    private func structured(_ text: String) -> some View {
        mono(tool.output.isEmpty ? output : Self.pretty(text), color: tool.output.isEmpty ? theme.muted : nil)
    }

    static func pretty(_ text: String) -> String {
        guard let data = text.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]),
              object is [Any] || object is [String: Any],
              let formatted = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]),
              let result = String(data: formatted, encoding: .utf8) else { return text }
        return result
    }
}

/// Changed lines with a marker column under the row's icon.
private struct DiffRows: View {
    @Environment(\.mica) private var theme
    let lines: [ToolDiffLine]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                DiffLineRow(kind: line.kind, text: line.text)
            }
        }
    }
}

private struct DiffLineRow: View {
    @Environment(\.mica) private var theme
    let kind: ToolDiffLine.Kind
    let text: String

    var body: some View {
        HStack(spacing: 0) {
            Text(kind == .added ? "+" : kind == .removed ? "−" : "")
                .foregroundStyle(kind == .added ? theme.success : theme.danger)
                .frame(width: ToolCallDetail.inset)
                .accessibilityLabel(kind == .added ? "Added" : kind == .removed ? "Removed" : "")
            Text(text.isEmpty ? " " : text)
                .foregroundStyle(foreground).lineLimit(1).textSelection(.enabled)
            Spacer(minLength: 12)
        }
        .font(.kit(size: 11, design: .monospaced))
        .frame(height: 18)
        .background(background)
    }

    private var foreground: Color {
        switch kind {
        case .added: theme.text.mix(with: theme.success, by: 0.45)
        case .removed: theme.text.mix(with: theme.danger, by: 0.45)
        case .context: theme.text
        }
    }
    private var background: Color {
        switch kind {
        case .added: theme.token("diffAddedBg", fallback: theme.success.opacity(0.1))
        case .removed: theme.token("diffRemovedBg", fallback: theme.danger.opacity(0.1))
        case .context: .clear
        }
    }
}

private struct DashedRule: View {
    @Environment(\.mica) private var theme
    var body: some View {
        GeometryReader { geometry in
            Path { path in
                path.move(to: CGPoint(x: 0, y: geometry.size.height / 2))
                path.addLine(to: CGPoint(x: geometry.size.width, y: geometry.size.height / 2))
            }.stroke(theme.border, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
        }
    }
}

/// A path in a well that opens its file when it lies in the workspace.
private struct FileLink: View {
    @Environment(\.mica) private var theme
    let text: String
    let path: String
    let monospaced: Bool
    let cwd: String?
    let workspace: WorkspaceState
    @State private var hovering = false

    var body: some View {
        let label = Text(text)
            .font(monospaced ? .kit(size: 11, design: .monospaced) : .kit(size: 12, weight: .medium))
            .lineLimit(1).truncationMode(.head)
        if let file = ToolPresentation.workspacePath(path, cwd: cwd) {
            Button { workspace.open(.file(file)) } label: { label }
                .buttonStyle(.plain)
                .foregroundStyle(hovering ? theme.accent : theme.text)
                .onHover { hovering = $0 }
                .help("Open \(file)")
        } else {
            label.foregroundStyle(theme.text)
        }
    }
}

/// A unified diff split into file headers, hunks, and lines.
struct UnifiedDiff: Equatable {
    enum Line: Equatable {
        case header(String)
        case hunk(String)
        case line(ToolDiffLine.Kind, String)
    }
    let lines: [Line]

    /// Whether output reads as a unified diff: a file header and a hunk.
    static func matches(_ output: String) -> Bool {
        var header = false, previous = ""
        for line in output.split(separator: "\n", omittingEmptySubsequences: false) {
            if line.hasPrefix("diff --git ") || (line.hasPrefix("+++ ") && previous.hasPrefix("--- ")) { header = true }
            else if line.hasPrefix("@@ ") && header { return true }
            previous = String(line)
        }
        return false
    }

    init(_ output: String) {
        let source = ToolPresentation.lines(output)
        var result: [Line] = []
        var inHunk = false
        for (index, line) in source.enumerated() {
            let next = index + 1 < source.count ? source[index + 1] : ""
            let fileHeader = line.hasPrefix("diff --git ") || (line.hasPrefix("--- ") && next.hasPrefix("+++ "))
            if line.hasPrefix("@@") {
                inHunk = true
                result.append(.hunk(line))
            } else if fileHeader || !inHunk {
                inHunk = false
                // Name each file once, by its new path.
                if line.hasPrefix("+++ ") {
                    let name = String(line.dropFirst(4))
                    result.append(.header(name.hasPrefix("b/") ? String(name.dropFirst(2)) : name))
                }
            } else if line.hasPrefix("-") {
                result.append(.line(.removed, String(line.dropFirst())))
            } else if line.hasPrefix("+") {
                result.append(.line(.added, String(line.dropFirst())))
            } else {
                result.append(.line(.context, line.hasPrefix(" ") ? String(line.dropFirst()) : line))
            }
        }
        lines = result
    }
}

private struct UnifiedDiffView: View {
    @Environment(\.mica) private var theme
    let diff: UnifiedDiff

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(diff.lines.enumerated()), id: \.offset) { _, line in
                switch line {
                case .header(let name):
                    Text(name).font(.kit(size: 12, weight: .medium))
                        .padding(.leading, ToolCallDetail.inset).padding(.top, 6).padding(.bottom, 2)
                case .hunk(let text):
                    Text(text).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                        .lineLimit(1).padding(.leading, ToolCallDetail.inset).frame(height: 18)
                case .line(let kind, let text):
                    DiffLineRow(kind: kind, text: text)
                }
            }
        }
    }
}

extension View {
    /// The text style and leading rule of a disclosed transcript detail.
    func disclosureDetail() -> some View { modifier(DisclosureDetail()) }
}

private struct DisclosureDetail: ViewModifier {
    @Environment(\.mica) private var theme
    func body(content: Content) -> some View {
        content
            .font(.kit(size: 11)).foregroundStyle(theme.text)
            .padding(.leading, 12)
            .overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 1) }
    }
}
