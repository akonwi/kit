import SwiftUI

/// A one-line timeline event in the tool-row lane: a muted icon, muted words,
/// and the subject. Compaction opens like a tool row, with the summary in a well.
struct TranscriptMarkerRow: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptSessionLink) private var sessionLink
    let marker: TranscriptMarker
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}
    @State private var hovering = false
    @State private var contentHeight: CGFloat = 1

    var body: some View {
        switch marker {
        case .compaction(let summary): compaction(summary)
        case .fork(let session): fork(session)
        case .workingDirectory(let path, let previous): workingDirectory(path, previous: previous)
        }
    }

    private func compaction(_ summary: String) -> some View {
        let open = state.expanded ?? false
        return VStack(alignment: .leading, spacing: 0) {
            line(icon: .arrowsInLineVertical, iconColor: open ? theme.text : theme.muted) {
                Text("Context compacted").foregroundStyle(open ? theme.text : theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                IconView(open ? .caretDown : .caretRight, size: 10)
                    .foregroundStyle(open ? theme.text : theme.muted).frame(width: 10)
            }
            .contentShape(Rectangle())
            .onHover { hovering = $0 }
            .onTapGesture { toggle(open) }
            .accessibilityElement(children: .combine)
            .accessibilityLabel("Context compacted")
            .accessibilityAddTraits(.isButton)
            .accessibilityValue(open ? "Expanded" : "Collapsed")
            .accessibilityHint("Show the summary that replaced earlier context")
            .accessibilityAction { toggle(open) }
            if open {
                Rule()
                ScrollView(.vertical) {
                    MarkdownView(source: summary, size: 12, compactHeadings: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.leading, ToolCallDetail.inset).padding(.trailing, 16)
                        .padding(.top, 8).padding(.bottom, 10)
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
                }
                .frame(height: min(max(1, contentHeight), ToolCallDetail.maximumHeight))
                .background(MessageCopyRegion(markdown: summary))
            }
        }
        .background {
            if open {
                RoundedRectangle(cornerRadius: 4).fill(theme.raised)
            } else if hovering {
                RoundedRectangle(cornerRadius: 4).fill(theme.hover)
            }
        }
        .overlay {
            if open { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
        }
        .clipShape(RoundedRectangle(cornerRadius: 4))
    }

    private func toggle(_ open: Bool) {
        state.expanded = !open
        if !open { onExpand() }
    }

    @ViewBuilder private func fork(_ session: String) -> some View {
        let title = sessionLink?.title(session)
        line(icon: .gitFork) {
            Text("Forked into").foregroundStyle(theme.muted).fixedSize()
            if let sessionLink {
                Button { sessionLink.open(session) } label: {
                    Text(title ?? "another session").foregroundStyle(theme.text)
                        .underline(hovering).lineLimit(1)
                }
                .buttonStyle(.plain)
                .onHover { hovering = $0 }
                .help("Open the fork")
                .accessibilityHint("Opens the forked session")
            } else {
                Text(title ?? "another session").foregroundStyle(theme.text).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
    }

    private func workingDirectory(_ path: String, previous: String?) -> some View {
        line(icon: .folderSimple) {
            Text("Working directory changed to").foregroundStyle(theme.muted).fixedSize()
            Text(WorkspaceLocation.homeRelative(path))
                .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.text)
                .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                .help(previous.map { "Was " + WorkspaceLocation.homeRelative($0) } ?? path)
            Spacer(minLength: 0)
        }
    }

    private func line<Content: View>(icon: Icon, iconColor: Color? = nil, @ViewBuilder content: () -> Content) -> some View {
        HStack(spacing: 10) {
            IconView(icon).foregroundStyle(iconColor ?? theme.muted).frame(width: 16)
            HStack(spacing: 6) { content() }
        }
        .font(.kit(size: 12))
        .frame(height: 28)
        .padding(.horizontal, 8)
    }
}

/// A request or reply from another subagent, in the receiving subagent's pane.
struct SubagentInboxRow: View {
    @Environment(\.mica) private var theme
    let message: SubagentInboxMessage

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                IconView(message.failed ? .warningCircle : .robot)
                    .foregroundStyle(message.failed ? theme.danger : theme.muted)
                HStack(spacing: 4) {
                    Text(label).foregroundStyle(theme.muted)
                    if let from = message.from { Text(from).foregroundStyle(theme.text) }
                    if message.failed { Text("failed").foregroundStyle(theme.muted) }
                }
                .lineLimit(1)
            }
            .font(.kit(size: 12))
            .frame(height: 16)
            .accessibilityElement(children: .combine)
            if message.failed {
                Text(message.text).font(.kit(size: 14)).foregroundStyle(theme.danger).textSelection(.enabled)
            } else {
                MarkdownView(source: message.text)
            }
        }
        .padding(.horizontal, 16).padding(.top, 12).padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
        .background(MessageCopyRegion(markdown: message.text))
    }

    private var label: String {
        if message.failed { return message.from == nil ? "Request" : "Request to" }
        switch message.kind {
        case .request: return message.from == nil ? "Request from another subagent" : "Request from"
        case .reply: return message.from == nil ? "Reply from another subagent" : "Reply from"
        }
    }
}
