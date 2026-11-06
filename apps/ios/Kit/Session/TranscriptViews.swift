import KitClient
import KitTranscript
import SwiftUI

/// One transcript message, drawn for its kind.
struct TranscriptRow: View {
    let message: TranscriptMessage
    /// Whether this tool batch belongs to the run in progress.
    var live = false

    var body: some View {
        if let bash = message.bash {
            BashRow(execution: bash)
        } else if let marker = message.marker {
            MarkerRow(marker: marker)
        } else if let inbox = message.inbox {
            NoteRow(title: inbox.kind == .request ? "Request from \(inbox.from ?? "an agent")" : "Reply from \(inbox.from ?? "an agent")",
                    text: inbox.text, failed: inbox.failed)
                .modifier(MessageMenu(text: inbox.text))
        } else if message.role == "tools" {
            VStack(alignment: .leading, spacing: 12) {
                ToolBatch(tools: message.tools, live: live)
                // Shown images stay in view, as on the Mac.
                ForEach(message.tools.filter { $0.name == "show_image" }) { tool in
                    TranscriptAttachments(attachments: (tool.attachments ?? []).filter(\.isImage))
                }
            }
        } else if message.role == "user" {
            UserMessage(message: message)
                .modifier(MessageMenu(text: message.text))
        } else if message.plugin != nil {
            VStack(alignment: .leading, spacing: 6) {
                Text("From \(message.plugin!.pluginID)")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                MarkdownBlocks(source: message.text)
            }
            .modifier(MessageMenu(text: message.text))
        } else if !message.text.isEmpty || !(message.attachments ?? []).isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                if !message.text.isEmpty { MarkdownBlocks(source: message.text) }
                TranscriptAttachments(attachments: message.attachments ?? [])
            }
            .modifier(MessageMenu(text: message.text))
        }
    }
}

/// Copy and Select Text on a long press of a message. Copy takes the whole
/// message as written; Select Text opens it to pick out part of it.
private struct MessageMenu: ViewModifier {
    let text: String
    @Environment(\.selectText) private var selectText

    func body(content: Content) -> some View {
        content
            .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: Theme.cardRadius))
            .contextMenu {
                Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
                Button("Select Text", systemImage: "character.cursor.ibeam") { selectText(text) }
            }
    }
}

/// The user's message on the accent wash.
private struct UserMessage: View {
    let message: TranscriptMessage

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !message.text.isEmpty {
                Prose(text: AttributedString(message.text), lineHeight: 23)
            }
            TranscriptAttachments(attachments: message.attachments ?? [])
            if let notes = message.annotations, !notes.isEmpty {
                SentComments(notes: notes)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.userWash, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
    }
}

// MARK: Tool calls

/// A batch of tool calls. One call is just its row; several have a header
/// that opens and closes them, closed when there are more than five.
struct ToolBatch: View {
    let tools: [ToolActivity]
    let live: Bool
    @State private var expanded: Bool?
    @State private var open: String?

    static let collapseThreshold = 5

    private var isExpanded: Bool { expanded ?? (tools.count <= Self.collapseThreshold) }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if tools.count > 1 {
                Button {
                    withAnimation(.snappy) { expanded = !isExpanded }
                } label: {
                    HStack(spacing: 8) {
                        Group {
                            if live && !isExpanded {
                                LaneSpinner()
                            } else {
                                Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                                    .font(.system(size: 11, weight: .semibold))
                            }
                        }
                        .frame(width: 16)
                        Text("\(tools.count) tool calls")
                        let failed = tools.filter(\.failed).count
                        if failed > 0 {
                            Text("· \(failed) failed").foregroundStyle(Theme.danger)
                        }
                    }
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .frame(height: 28)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(tools.count) tool calls")
                .accessibilityValue(isExpanded ? "Expanded" : "Collapsed")
            }
            if tools.count == 1 || isExpanded {
                ForEach(tools) { tool in
                    ToolRow(tool: tool, open: open == tool.id) {
                        withAnimation(.snappy) { open = open == tool.id ? nil : tool.id }
                    }
                }
            }
        }
    }
}

/// A tool call: icon, title, chip, and results. A row with details opens
/// beneath itself.
private struct ToolRow: View {
    let tool: ToolActivity
    let open: Bool
    let toggle: () -> Void

    var body: some View {
        let presentation = ToolPresentation(tool)
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                icon(presentation).frame(width: 16)
                Text(presentation.title)
                    .font(.system(size: 15))
                    .foregroundStyle(tool.notRun == true ? Theme.muted : Theme.text)
                    .lineLimit(1)
                    .layoutPriority(1)
                if let chip = presentation.chip {
                    Text(chip)
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                        .truncationMode(presentation.chipIsPath ? .head : .tail)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 3)
                        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.controlRadius))
                        .overlay { RoundedRectangle(cornerRadius: Theme.controlRadius).strokeBorder(Theme.border, lineWidth: 0.5) }
                }
                Spacer(minLength: 0)
                facts(presentation)
            }
            .frame(minHeight: 32)
            .contentShape(Rectangle())
            .onTapGesture { if presentation.expandable { toggle() } }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(presentation.expandable ? .isButton : [])
            if open && presentation.expandable {
                ToolDetail(tool: tool, presentation: presentation)
                    .padding(.leading, 24)
                    .padding(.bottom, 6)
            }
        }
    }

    @ViewBuilder
    private func icon(_ presentation: ToolPresentation) -> some View {
        if tool.running {
            LaneSpinner()
        } else if tool.notRun == true {
            Image(systemName: "nosign").foregroundStyle(Theme.muted).font(.system(size: 13))
        } else if tool.failed {
            Image(systemName: "exclamationmark.circle").foregroundStyle(Theme.danger).font(.system(size: 14))
        } else {
            Image(systemName: Self.symbol(for: tool, presentation: presentation))
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
        }
    }

    @ViewBuilder
    private func facts(_ presentation: ToolPresentation) -> some View {
        if tool.notRun == true {
            Text("Not run").font(.system(size: 12)).foregroundStyle(Theme.muted)
        } else {
            HStack(spacing: 6) {
                ForEach(Array(presentation.facts.enumerated()), id: \.offset) { _, fact in
                    Text(fact.text)
                        .font(.system(size: 12, design: fact.monospaced ? .monospaced : .default))
                        .foregroundStyle(color(fact.tone))
                        .lineLimit(1)
                        .fixedSize()
                }
            }
        }
    }

    private func color(_ tone: ToolFact.Tone) -> Color {
        switch tone {
        case .muted: Theme.muted
        case .text: Theme.text
        case .success: Theme.success
        case .danger: Theme.danger
        }
    }

    /// The Mac's tool icons, as SF Symbols.
    static func symbol(for tool: ToolActivity, presentation: ToolPresentation) -> String {
        if presentation.server != nil { return "powerplug" }
        if presentation.pluginTool != nil { return "puzzlepiece.extension" }
        if tool.bashCommand != nil { return "apple.terminal" }
        switch tool.name.lowercased() {
        case "peer_session": return "arrow.left.arrow.right"
        case "create_session": return "plus.square"
        case "show_image", "inspect_image": return "photo"
        case "read": return "doc"
        case "write": return "doc.badge.plus"
        case "edit", "apply_patch": return "pencil"
        case "grep", "search": return "magnifyingglass"
        case "find", "glob": return "doc.on.doc"
        case "ls", "change_cwd": return "folder"
        case "activate_skill": return "sparkles"
        case "subagent", "subagent_send", "subagent_reply", "subagent_inspect", "subagent_inbox": return "person.2"
        case "edit_scratchpad": return "square.and.pencil"
        case "read_scratchpad": return "note.text"
        case "confirm_from_user", "input_from_user", "select_from_user", "guided_questions": return "bubble.left.and.text.bubble.right"
        default: return "wrench.and.screwdriver"
        }
    }
}

/// What an opened tool row shows: an edit's changes, a read or written
/// file colored by its language, or the call's output.
private struct ToolDetail: View {
    let tool: ToolActivity
    let presentation: ToolPresentation

    /// The most output lines shown.
    static let lineLimit = 120

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !presentation.header.isEmpty {
                Text(presentation.header)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.muted)
            }
            let name = tool.name.lowercased()
            let edits = presentation.edits
            if failure {
                CodeBlock(language: "", source: tool.output.isEmpty ? "No text output recorded." : tool.output,
                          tone: Theme.danger)
            } else if ["edit", "edit_scratchpad"].contains(name), !edits.isEmpty {
                ForEach(Array(edits.enumerated()), id: \.offset) { _, edit in
                    EditDiffBlock(lines: ToolDiffLine.build(edit), syntax: presentation.language)
                }
            } else if name == "read", !presentation.readContent.isEmpty {
                output(presentation.readContent, syntax: presentation.language)
            } else if name == "write", let content = presentation.string("content"), !content.isEmpty {
                output(content, syntax: presentation.language)
            } else if !tool.output.isEmpty {
                output(tool.output, syntax: nil)
            }
            // A shown image sits under its batch instead.
            TranscriptAttachments(attachments: (tool.attachments ?? []).filter { tool.name != "show_image" || !$0.isImage })
        }
    }

    /// Whether the call failed and shows its error, as on the Mac. Commands,
    /// MCP tools, and session tools show their usual view either way.
    private var failure: Bool {
        tool.failed && tool.bashCommand == nil && presentation.server == nil
            && !["create_session", "peer_session"].contains(tool.name)
    }

    /// Up to `lineLimit` lines of text, then how many more there are.
    @ViewBuilder private func output(_ text: String, syntax: String?) -> some View {
        let lines = ToolPresentation.lines(text)
        CodeBlock(language: "", source: lines.prefix(Self.lineLimit).joined(separator: "\n"), syntax: syntax)
        if lines.count > Self.lineLimit {
            Text("\(lines.count - Self.lineLimit) more lines")
                .font(.system(size: 12))
                .foregroundStyle(Theme.muted)
        }
    }
}

// MARK: Other rows

/// A shell command the user ran in the session.
private struct BashRow: View {
    let execution: BashExecution

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                if execution.running { LaneSpinner() } else {
                    Image(systemName: "apple.terminal").font(.system(size: 13)).foregroundStyle(Theme.muted)
                }
                Text(execution.command)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .lineLimit(2)
                Spacer(minLength: 0)
                Text(execution.statusLabel)
                    .font(.system(size: 12))
                    .foregroundStyle(execution.running || execution.exitCode == 0 ? Theme.muted : Theme.danger)
            }
            if !execution.output.isEmpty {
                CodeBlock(language: "", source: execution.output)
            }
        }
    }
}

/// An event in the timeline rather than a message.
private struct MarkerRow: View {
    let marker: TranscriptMarker
    @State private var expanded = false

    var body: some View {
        switch marker {
        case .compaction(let summary):
            VStack(alignment: .leading, spacing: 8) {
                Button { withAnimation(.snappy) { expanded.toggle() } } label: {
                    line("Earlier context was summarized", symbol: expanded ? "chevron.down" : "chevron.right")
                }
                .buttonStyle(.plain)
                if expanded { MarkdownBlocks(source: summary) }
            }
        case .fork:
            line("Forked into a new session", symbol: "arrow.triangle.branch")
        case .workingDirectory(let path, _):
            line("Working directory is now \(SessionFormat.directory(path))", symbol: "folder")
        case .turnEnded(let end, let message):
            VStack(alignment: .leading, spacing: 4) {
                line(end == .stopped ? "Stopped" : end == .interrupted ? "Interrupted" : "The turn failed",
                     symbol: end == .failed ? "exclamationmark.circle" : "stop.circle",
                     color: end == .failed ? Theme.danger : Theme.muted)
                if !message.isEmpty {
                    Text(message).font(.system(size: 13)).foregroundStyle(Theme.muted).padding(.leading, 24)
                }
            }
        }
    }

    private func line(_ text: String, symbol: String, color: Color = Theme.muted) -> some View {
        HStack(spacing: 8) {
            Image(systemName: symbol).font(.system(size: 12, weight: .semibold)).frame(width: 16)
            Text(text).font(.system(size: 14))
        }
        .foregroundStyle(color)
        .frame(minHeight: 24)
    }
}

/// A note from another agent.
private struct NoteRow: View {
    let title: String
    let text: String
    let failed: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(failed ? Theme.danger : Theme.muted)
            MarkdownBlocks(source: text)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
    }
}
