import AppKit
import SwiftUI

/// What tool rows need from the session around them: the working directory
/// that names workspace files, and the MCP servers whose tools are titled
/// after them.
struct TranscriptToolContext: Equatable, Sendable {
    var cwd: String?
    var servers: [String] = []
}
private struct TranscriptToolContextKey: EnvironmentKey {
    static let defaultValue = TranscriptToolContext()
}
extension EnvironmentValues {
    var transcriptToolContext: TranscriptToolContext {
        get { self[TranscriptToolContextKey.self] }
        set { self[TranscriptToolContextKey.self] = newValue }
    }
}

/// One batch of tool calls: a disclosure header, then a row per call. Rows
/// share a title column as wide as the batch's longest title, within bounds,
/// as in the terminal client.
struct ToolActivityView: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptToolContext) private var context
    let tools: [ToolActivity]
    let workspace: WorkspaceState
    @Bindable var state: ToolDrawerState
    var inProgress = false
    var onExpand: () -> Void = {}

    static let minimumTitleWidth: CGFloat = 64
    static let maximumTitleWidth: CGFloat = 180

    var body: some View {
        let expanded = state.isExpanded(count: tools.count, inProgress: inProgress)
        let presentations = tools.map { ToolPresentation($0, servers: context.servers) }
        VStack(alignment: .leading, spacing: 2) {
            Button {
                state.expanded = !expanded
                if !expanded { onExpand() }
            } label: {
                HStack(spacing: 10) {
                    Group {
                        if inProgress && !expanded {
                            KitSpinner().foregroundStyle(theme.accent)
                        } else {
                            IconView(expanded ? .caretDown : .caretRight, size: 10)
                        }
                    }.frame(width: 16)
                    Text("\(tools.count) tool \(tools.count == 1 ? "call" : "calls")")
                    let failed = tools.filter(\.failed).count
                    if failed > 0 {
                        Text("·")
                        Text("\(failed) failed").foregroundStyle(theme.danger)
                    }
                }
                .font(.kit(size: 12))
                .padding(.leading, 8).frame(height: 24).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .accessibilityLabel("\(expanded ? "Collapse" : "Expand") \(tools.count) recorded tool calls")
            if expanded {
                let titleWidth = Self.titleWidth(presentations.map(\.title))
                ForEach(Array(zip(tools, presentations)), id: \.0.id) { tool, presentation in
                    if let thinking = tool.thinking, !thinking.isEmpty {
                        MarkdownView(source: thinking).padding(.leading, 34).padding(.vertical, 4)
                    }
                    ToolCallRow(tool: tool, presentation: presentation, titleWidth: titleWidth,
                                open: state.selectedTool == tool.id, workspace: workspace) {
                        state.selectedTool = state.selectedTool == tool.id ? nil : tool.id
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 4)
    }

    /// The width of the title column shared by rows with these titles.
    @MainActor static func titleWidth(_ titles: [String]) -> CGFloat {
        let font = Typography.shared.font(size: 13, weight: .regular, mono: false)
        let widest = titles.map { ceil(($0 as NSString).size(withAttributes: [.font: font]).width) }.max() ?? 0
        return min(maximumTitleWidth, max(minimumTitleWidth, widest))
    }
}

/// A tool call's row: state icon, title, chip, result facts, and a chevron
/// when it has a detail well. An open row becomes a raised block with the
/// well beneath a divider.
private struct ToolCallRow: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptToolContext) private var context
    @Environment(\.transcriptSessionLink) private var sessionLink
    let tool: ToolActivity
    let presentation: ToolPresentation
    let titleWidth: CGFloat
    let open: Bool
    let workspace: WorkspaceState
    let toggle: () -> Void
    @State private var hovering = false

    private var expandable: Bool { presentation.expandable }
    private var shown: Bool { open && expandable }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            row.padding(.horizontal, 8)
            if shown {
                Rule()
                ToolCallDetail(tool: tool, workspace: workspace, presentation: presentation)
            }
        }
        .background {
            if shown {
                RoundedRectangle(cornerRadius: 4).fill(theme.raised)
            } else if hovering && expandable {
                RoundedRectangle(cornerRadius: 4).fill(theme.hover)
            }
        }
        .overlay {
            if shown { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
        }
        .clipShape(RoundedRectangle(cornerRadius: 4))
    }

    private var row: some View {
        let muted = tool.notRun == true
        return HStack(spacing: 10) {
            icon.frame(width: 16)
            Text(presentation.title)
                .font(.kit(size: 13)).lineLimit(1)
                .foregroundStyle(muted ? theme.muted : theme.text)
                .frame(width: titleWidth, alignment: .leading)
            Group {
                if let chip = presentation.chip {
                    ToolChip(text: chip, path: presentation.chipIsPath, muted: muted,
                             fill: shown ? theme.frameHover : theme.frame, action: linkAction)
                }
            }.frame(maxWidth: .infinity, alignment: .leading)
            facts
            if expandable {
                IconView(shown ? .caretDown : .caretRight, size: 10)
                    .foregroundStyle(shown ? theme.text : theme.muted)
                    .frame(width: 10)
            } else {
                Color.clear.frame(width: 10, height: 1)
            }
        }
        .frame(height: 28)
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
        .onTapGesture { if expandable { toggle() } }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityAddTraits(expandable ? .isButton : [])
        .accessibilityValue(expandable ? (shown ? "Expanded" : "Collapsed") : "")
        .accessibilityAction { if expandable { toggle() } }
    }

    @ViewBuilder private var icon: some View {
        if tool.running {
            KitSpinner().foregroundStyle(theme.accent)
        } else if tool.notRun == true {
            IconView(.prohibit).foregroundStyle(theme.muted)
        } else if tool.failed {
            IconView(.warningCircle).foregroundStyle(theme.danger)
        } else {
            IconView(Self.icon(for: tool, presentation: presentation)).foregroundStyle(theme.muted)
        }
    }

    @ViewBuilder private var facts: some View {
        if tool.notRun == true {
            Text("Not run").font(.kit(size: 11)).foregroundStyle(theme.muted)
        } else {
            ForEach(Array(presentation.facts.enumerated()), id: \.offset) { _, fact in
                Text(fact.text)
                    .font(.kit(size: 11, design: fact.monospaced ? .monospaced : .default))
                    .foregroundStyle(color(fact.tone)).lineLimit(1).fixedSize()
            }
            if presentation.showsDuration, presentation.facts.isEmpty, let started = tool.startedAt {
                if tool.running {
                    TimelineView(.periodic(from: started, by: 1)) { timeline in
                        duration(timeline.date.timeIntervalSince(started))
                    }
                } else if let finished = tool.finishedAt {
                    duration(finished.timeIntervalSince(started))
                }
            }
        }
    }

    private func duration(_ interval: TimeInterval) -> some View {
        Text(ToolTiming.format(interval))
            .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).fixedSize()
    }

    private func color(_ tone: ToolFact.Tone) -> Color {
        switch tone {
        case .muted: theme.muted
        case .text: theme.text
        case .success: theme.success
        case .danger: theme.danger
        }
    }

    /// Opens what the chip names, or nil when it names nothing that opens.
    private var linkAction: (() -> Void)? {
        switch presentation.link {
        case .file(let path)?:
            guard let file = ToolPresentation.workspacePath(path, cwd: context.cwd) else { return nil }
            return { workspace.open(.file(file)) }
        case .agent(let name)?: return { workspace.open(.agent(name)) }
        case .scratchpad?: return { workspace.open(.scratchpad) }
        case .session(let id)?:
            guard let sessionLink else { return nil }
            return { sessionLink.open(id) }
        case nil: return nil
        }
    }

    private var accessibilityLabel: String {
        var parts = [presentation.title]
        if let chip = presentation.chip { parts.append(chip) }
        parts += presentation.facts.map(\.text)
        if tool.notRun == true { parts.append("not run") }
        else if tool.failed { parts.append("failed") }
        else if tool.running { parts.append("running") }
        return parts.joined(separator: ", ")
    }

    static func icon(for tool: ToolActivity, presentation: ToolPresentation) -> Icon {
        if presentation.server != nil { return .plugsConnected }
        if presentation.pluginTool != nil { return .plug }
        if tool.bashCommand != nil { return .terminalWindow }
        switch tool.name.lowercased() {
        case "peer_session": return .arrowsLeftRight
        case "create_session": return .plusSquare
        case "show_image", "inspect_image": return .image
        case "read": return .file
        case "write": return .filePlus
        case "edit", "apply_patch": return .pencilSimple
        case "grep", "search": return .magnifyingGlass
        case "find", "glob": return .files
        case "ls", "change_cwd": return .folder
        case "activate_skill": return .sparkle
        case "subagent", "subagent_send", "subagent_reply", "subagent_inspect", "subagent_inbox": return .robot
        case "edit_scratchpad": return .notePencil
        case "read_scratchpad": return .note
        case "confirm_from_user", "input_from_user", "select_from_user", "guided_questions": return .chatCircleDots
        default: return .wrench
        }
    }
}

/// A tool call's arguments in a mono chip. A chip that opens something
/// carries ↗ and turns blue on hover.
private struct ToolChip: View {
    @Environment(\.mica) private var theme
    let text: String
    let path: Bool
    let muted: Bool
    let fill: Color
    let action: (() -> Void)?
    @State private var hovering = false

    var body: some View {
        if let action {
            Button(action: action) { label }
                .buttonStyle(.plain)
                .onHover { hovering = $0 }
                .help("Open \(text)")
        } else {
            label
        }
    }

    private var label: some View {
        let linked = action != nil
        let tint = linked && hovering ? theme.accent : (muted ? theme.muted : theme.text)
        return HStack(spacing: 5) {
            Text(text)
                .font(.kit(size: 11, design: .monospaced))
                .lineLimit(1).truncationMode(path ? .head : .tail)
            if linked {
                IconView(.arrowUpRight, size: 10)
                    .foregroundStyle(hovering ? theme.accent : theme.muted)
            }
        }
        .foregroundStyle(tint)
        .padding(.horizontal, 6).padding(.vertical, 2)
        .background(linked && hovering ? fill.mix(with: theme.border, by: 0.35) : fill,
                    in: RoundedRectangle(cornerRadius: 3))
        .contentShape(RoundedRectangle(cornerRadius: 3))
    }
}

/// The row layout of a disclosed transcript item other than a tool call,
/// such as a plugin message: an icon in the indicator column, a title, an
/// optional status, and a muted one-line summary with a chevron.
struct DisclosureChipLabel: View {
    @Environment(\.mica) private var theme
    let icon: Icon
    let iconColor: Color
    let title: String
    var status: String? = nil
    let summary: String
    /// Monospaced summaries (paths, commands) truncate in the middle; prose
    /// summaries truncate at the end.
    var monospacedSummary = true
    let expanded: Bool
    let hovering: Bool

    var body: some View {
        let detail = expanded || hovering ? theme.text : theme.muted
        HStack(spacing: 9) {
            IconView(icon).frame(width: 16)
                .foregroundStyle(iconColor)
            HStack(alignment: .firstTextBaseline, spacing: 9) {
                Text(title).foregroundStyle(theme.text).fixedSize()
                if let status {
                    Text(status).font(.kit(size: 11)).foregroundStyle(theme.muted)
                }
                Text(summary)
                    .font(.kit(size: 11, design: monospacedSummary ? .monospaced : .default))
                    .lineLimit(1).truncationMode(monospacedSummary ? .middle : .tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .foregroundStyle(detail)
            }
            IconView(expanded ? .caretDown : .caretRight, size: 10)
                .foregroundStyle(detail)
                .padding(.leading, -1)
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
    }
}
