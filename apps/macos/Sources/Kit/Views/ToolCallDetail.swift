import SwiftUI

struct ToolCallDetail: View {
    @Environment(\.mica) private var theme
    let tool: ToolActivity
    let workspace: WorkspaceState
    private var presentation: ToolPresentation { ToolPresentation(tool) }

    @State private var contentHeight: CGFloat = 1

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !presentation.header.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    if tool.bashCommand != nil {
                        HighlightedCodeText(language: "bash", source: presentation.header, fontSize: 11)
                    } else {
                        MarkdownView(source: presentation.header)
                    }

                }
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 8)
                Rule()
            }
            ScrollView(.vertical) {
                VStack(alignment: .leading, spacing: 12) {
                    content
                    TranscriptAttachments(attachments: (tool.attachments ?? []).filter { tool.name != "show_image" || !$0.isImage })
                    if tool.contentTruncated == true { Text("Tool output truncated").foregroundStyle(theme.muted) }
                }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 8)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
            }
            .defaultScrollAnchor(.topLeading, for: .alignment)
            .frame(height: min(max(1, contentHeight), tool.bashCommand != nil ? 180 : 280))
        }
        .font(.kit(size: 11)).foregroundStyle(theme.text)
        .padding(.leading, 12)
        .overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 1) }
    }

    @ViewBuilder private var content: some View {
        if tool.name == "create_session" {
            CreatedSessionDetail(value: CreatedSessionPresentation(tool))
        } else if tool.name == "peer_session" {
            PeerSessionDetail(value: PeerSessionPresentation(tool))
        } else if tool.failed {
            Label("Failed", systemImage: "exclamationmark.circle").foregroundStyle(theme.danger)
            plainOutput
        } else if tool.bashCommand != nil {
            ScrollView(.horizontal) {
                HighlightedCodeText(language: "bash", source: output, fontSize: 11)
            }.defaultScrollAnchor(.topLeading, for: .alignment)
        } else {
            switch tool.name {
            case "read":
                if presentation.readContent.isEmpty { Text("Empty file").foregroundStyle(theme.muted) }
                else { fileContent(presentation.readContent, start: presentation.startLine) }
                if presentation.readTruncated { Text("Read output truncated").foregroundStyle(theme.muted) }
            case "write":
                if let content = presentation.string("content") { fileContent(content, start: 1) }
                else { plainOutput }
            case "edit", "edit_scratchpad":
                if presentation.edits.isEmpty { plainOutput }
                else {
                    if tool.name == "edit_scratchpad" {
                        Button("Open scratchpad") { workspace.open(.scratchpad) }.buttonStyle(.link)
                    }
                    ForEach(Array(presentation.edits.enumerated()), id: \.offset) { index, edit in
                        if presentation.edits.count > 1 {
                            Text("Edit \(index + 1)").foregroundStyle(theme.muted)
                        }
                        ToolEditDiff(edit: edit)
                    }
                }
            case "activate_skill": MarkdownView(source: presentation.skill.body)
            case "subagent": MarkdownView(source: output)
            case "grep":
                if let matches = presentation.matches, !matches.isEmpty {
                    searchResults(matches)
                } else { plainOutput }
            case "ls", "find", "glob":
                if tool.output.isEmpty || tool.output.hasPrefix("No files") { plainOutput }
                else {
                    ForEach(Array(ToolPresentation.lines(tool.output).enumerated()), id: \.offset) { _, path in
                        Label(path, systemImage: path.hasSuffix("/") ? "folder" : "doc")
                            .font(.kit(size: 11, design: .monospaced)).textSelection(.enabled)
                    }
                }
            case "show_image":
                if !tool.output.isEmpty { MarkdownView(source: tool.output) }
            default: plainOutput
            }
        }
    }
    private var output: String { tool.output.isEmpty ? "No text output recorded." : tool.output }
    private var plainOutput: some View {
        Text(output).font(.kit(size: 11, design: .monospaced)).textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
    private func fileContent(_ source: String, start: Int) -> some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                Text(ToolPresentation.lines(source).indices.map { String(start + $0) }.joined(separator: "\n"))
                    .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                    .multilineTextAlignment(.trailing).accessibilityHidden(true)
                HighlightedCodeText(language: presentation.language, source: source, fontSize: 11)
                    .fixedSize(horizontal: true, vertical: true)
            }
        }.defaultScrollAnchor(.topLeading, for: .alignment)
    }
    private func searchResults(_ matches: [ToolPresentation.Match]) -> some View {
        let paths = matches.reduce(into: [String]()) { if !$0.contains($1.path) { $0.append($1.path) } }
        return VStack(alignment: .leading, spacing: 10) {
            ForEach(paths, id: \.self) { path in
                Text(path).fontWeight(.medium).textSelection(.enabled)
                ForEach(Array(matches.filter { $0.path == path }.enumerated()), id: \.offset) { _, match in
                    HStack(alignment: .top, spacing: 10) {
                        Text(String(match.line)).foregroundStyle(theme.muted)
                        Text(emphasized(match.text)).textSelection(.enabled)
                    }.font(.kit(size: 11, design: .monospaced))
                }
            }
        }
    }
    private func emphasized(_ text: String) -> AttributedString {
        var result = AttributedString(text)
        guard let pattern = presentation.string("pattern"),
              let regex = try? NSRegularExpression(pattern: pattern) else { return result }
        for match in regex.matches(in: text, range: NSRange(text.startIndex..., in: text)) {
            if let range = Range(match.range, in: text), let attributed = Range(range, in: result) {
                result[attributed].backgroundColor = theme.accent.opacity(0.18)
                result[attributed].font = .kit(size: 11, weight: .semibold, design: .monospaced)
            }
        }
        return result
    }
}

private struct ToolEditDiff: View {
    @Environment(\.mica) private var theme
    let edit: ToolPresentation.Edit
    private let rowHeight: CGFloat = 18

    var body: some View {
        let rows = ToolDiffLine.numbered(edit)
        let digits = String(max(ToolPresentation.lines(edit.old).count, ToolPresentation.lines(edit.new).count, 1)).count
        let gutterWidth = CGFloat(max(2, digits)) * 7
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text("−").frame(width: gutterWidth, alignment: .trailing).accessibilityLabel("Before")
                Text("+").frame(width: gutterWidth, alignment: .trailing).accessibilityLabel("After")
            }
            .font(.kit(size: 10)).foregroundStyle(theme.muted)
            .help("Line numbers are relative to the original and replacement text of this edit.")
            HStack(alignment: .top, spacing: 6) {
                VStack(spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                        HStack(spacing: 6) {
                            Text(row.oldNumber.map(String.init) ?? "").frame(width: gutterWidth, alignment: .trailing)
                            Text(row.newNumber.map(String.init) ?? "").frame(width: gutterWidth, alignment: .trailing)
                        }.frame(height: rowHeight).background(background(row.line.kind))
                    }
                }.foregroundStyle(theme.muted)
                ScrollView(.horizontal) {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                            let line = row.line
                            HStack(spacing: 6) {
                                Text(line.kind == .added ? "+" : line.kind == .removed ? "−" : " ")
                                Text(line.text.isEmpty ? " " : line.text).textSelection(.enabled)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading).frame(height: rowHeight)
                            .foregroundStyle(line.kind == .added ? theme.success : line.kind == .removed ? theme.danger : theme.text)
                            .background(background(line.kind))
                        }
                    }.fixedSize(horizontal: true, vertical: true)
                }.defaultScrollAnchor(.topLeading, for: .alignment)
            }.font(.kit(size: 11, design: .monospaced))
            if let notice = edit.newlineNotice {
                Text(notice).font(.kit(size: 10)).foregroundStyle(theme.muted)
            }
        }
    }
    private func background(_ kind: ToolDiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.token("diffAddedBg", fallback: theme.success.opacity(0.1))
        case .removed: theme.token("diffRemovedBg", fallback: theme.danger.opacity(0.1))
        case .context: .clear
        }
    }
}
