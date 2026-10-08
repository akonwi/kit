import SwiftUI

struct ToolActivityView: View {
    @Environment(\.mica) private var theme
    let tools: [ToolActivity]
    let workspace: WorkspaceState
    @Bindable var state: ToolDrawerState
    var inProgress = false
    var onExpand: () -> Void = {}

    var body: some View {
        let expanded = state.isExpanded(count: tools.count, inProgress: inProgress)
        VStack(alignment: .leading, spacing: 10) {
            Button {
                state.expanded = !expanded
                if !expanded { onExpand() }
            } label: {
                HStack(spacing: 9) {
                    if inProgress && !expanded {
                        KitSpinner().frame(width: 16)
                    } else {
                        Image(systemName: expanded ? "chevron.down" : "chevron.right")
                            .font(.kit(size: 10, weight: .semibold)).frame(width: 16)
                    }
                    Text("\(tools.count) tool \(tools.count == 1 ? "call" : "calls")")
                    if tools.contains(where: \.failed) {
                        Text("· \(tools.filter(\.failed).count) failed").foregroundStyle(theme.danger)
                    }
                }.padding(.vertical, 5).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .accessibilityLabel("\(expanded ? "Collapse" : "Expand") \(tools.count) recorded tool calls")
            if expanded {
                ForEach(tools) { tool in
                    if let thinking = tool.thinking, !thinking.isEmpty {
                        MarkdownView(source: thinking).padding(.leading, 25)
                    }
                    if tool.name == "subagent", let agent = ToolPresentation(tool).string("agent"), !agent.isEmpty {
                        SubagentToolChipRow(tool: tool, agent: agent, selected: state.selectedTool == tool.id,
                            inspect: { workspace.open(.agent(agent)) },
                            toggleDetails: { state.selectedTool = state.selectedTool == tool.id ? nil : tool.id })
                    } else {
                        ToolChipRow(tool: tool, selected: state.selectedTool == tool.id) {
                            state.selectedTool = state.selectedTool == tool.id ? nil : tool.id
                        }
                    }
                    if state.selectedTool == tool.id {
                        ToolCallDetail(tool: tool, workspace: workspace).padding(.leading, 25)
                    }
                }
            }
        }
        .font(.kit(size: 12))
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 4)
    }
}

private struct SubagentToolChipRow: View {
    @Environment(\.mica) private var theme
    let tool: ToolActivity
    let agent: String
    let selected: Bool
    let inspect: () -> Void
    let toggleDetails: () -> Void
    @State private var hovering = false

    var body: some View {
        HStack(spacing: 9) {
            Image(systemName: tool.failed ? "exclamationmark.circle" : "person.2")
                .font(.kit(size: 13)).frame(width: 16).foregroundStyle(tool.failed ? theme.danger : theme.muted)
            Text(ToolPresentation(tool).title).foregroundStyle(theme.text).fixedSize()
            if let status = tool.status, status != "Completed" {
                Text(status).font(.kit(size: 11)).foregroundStyle(theme.muted)
            }
            HStack(spacing: 0) {
                Button(action: inspect) {
                    Text(agent).font(.kit(size: 11, design: .monospaced))
                        .lineLimit(1).truncationMode(.middle)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, 5).contentShape(Rectangle())
                }.buttonStyle(.plain).foregroundStyle(hovering ? theme.accent : theme.text)
                    .onHover { hovering = $0 }.help("Open \(agent)’s conversation")
                    .accessibilityLabel("Inspect \(agent)")
                Button(action: toggleDetails) {
                    Image(systemName: selected ? "chevron.down" : "chevron.right")
                        .font(.kit(size: 8, weight: .semibold)).padding(10).contentShape(Rectangle())
                }.buttonStyle(.plain).foregroundStyle(theme.muted)
                    .help("Show tool details").accessibilityLabel("\(selected ? "Hide" : "Show") \(agent) tool details")
            }
        }
    }
}

private struct ToolChipRow: View {
    @Environment(\.mica) private var theme
    let tool: ToolActivity
    let selected: Bool
    let action: () -> Void
    @State private var hovering = false

    private var icon: String {
        if tool.failed { return "exclamationmark.circle" }
        switch tool.name.lowercased() {
        case "peer_session": return "arrow.left.arrow.right"
        case "create_session": return "plus.rectangle.on.rectangle"
        case "show_image": return "photo"
        case "read": return "doc"
        case "write", "edit", "apply_patch": return "pencil"
        case "bash", "shell", "exec", "exec_command": return "terminal"
        case "grep", "glob", "search", "find": return "magnifyingglass"
        case "ls", "change_cwd": return "folder"
        case "activate_skill": return "sparkles"
        case "subagent": return "person.2"
        case "edit_scratchpad": return "note.text"
        case "confirm_from_user", "input_from_user", "select_from_user", "guided_questions": return "questionmark.bubble"
        default: return "wrench.and.screwdriver"
        }
    }

    private var label: String { ToolPresentation(tool).title }

    var body: some View {
        Button(action: action) {
            DisclosureChipLabel(icon: icon, iconColor: tool.failed ? theme.danger : theme.muted, title: label,
                                status: tool.status == "Completed" ? nil : tool.status,
                                summary: ToolPresentation(tool).summary, expanded: selected, hovering: hovering)
        }
        .buttonStyle(.plain).onHover { hovering = $0 }
        .help(ToolPresentation(tool).summary)
        .accessibilityLabel("\(label): \(ToolPresentation(tool).summary)\(tool.failed ? ", failed" : "")")
        .accessibilityValue(selected ? "Expanded" : "Collapsed")
        .accessibilityHint("Show tool details")
    }
}

/// The row layout of a recorded tool call, shared by other transcript rows
/// that disclose details the same way: an icon in the indicator column, a
/// title, an optional status, and a muted one-line summary with a chevron.
struct DisclosureChipLabel: View {
    @Environment(\.mica) private var theme
    let icon: String
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
            Image(systemName: icon)
                .font(.kit(size: 13)).frame(width: 16)
                .foregroundStyle(iconColor)
            // The title is larger than the status and summary; share a
            // baseline rather than centring each run of text.
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
            Image(systemName: expanded ? "chevron.down" : "chevron.right")
                .font(.kit(size: 8, weight: .semibold))
                .foregroundStyle(detail)
                .padding(.leading, -1)
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
    }
}
