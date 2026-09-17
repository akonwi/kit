import SwiftUI
import AppKit

struct ReviewPane: View {
    @Environment(\.mica) private var theme
    @Bindable var workspace: WorkspaceState
    @State private var syntaxRows: [[SyntaxSpan]] = []
    @State private var highlightedPath: String?

    private var file: PreviewFile? { workspace.fixture.files.first { $0.path == workspace.selectedFile } }
    private var changed: [PreviewFile] { workspace.fixture.files.filter { !$0.patch.isEmpty } }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Button { workspace.changesVisible.toggle() } label: { Image(systemName: "sidebar.left") }
                    .buttonStyle(.plain).accessibilityLabel("Toggle changed files")
                Text("Changes").font(.kit(size: 12, weight: .medium))
                Spacer()
                Text("\(workspace.notes.count) notes").font(.kit(size: 11)).foregroundStyle(theme.muted)
                Button("Open file") { if let file { workspace.open(.file(file.path)) } }
                    .buttonStyle(MicaButtonStyle(compact: true)).disabled(file == nil)
            }.padding(12)
            Rule()
            if workspace.changesVisible {
                VStack(spacing: 0) {
                    ForEach(changed) { file in
                        Button { workspace.selectedFile = file.path; workspace.editingLine = nil } label: {
                            HStack {
                                Text("M").foregroundStyle(theme.warning).font(.kit(size: 12, design: .monospaced))
                                Text(file.path).lineLimit(1).truncationMode(.middle)
                                Spacer()
                                if file.path == workspace.selectedFile { Image(systemName: "checkmark").foregroundStyle(theme.accent) }
                            }.font(.kit(size: 11)).padding(.horizontal, 16).padding(.vertical, 10)
                                .background(file.path == workspace.selectedFile ? theme.hover : Color.clear).contentShape(Rectangle())
                        }.buttonStyle(.plain)
                    }
                }.background(theme.raised)
                Rule()
            }
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(Array((file?.patch ?? "No changes in the fixture.").components(separatedBy: "\n").enumerated()), id: \.offset) { index, line in
                        diffLine(line, index: index)
                        if workspace.editingLine == index { noteEditor }
                        else if let note = workspace.notes[workspace.noteKey(index)] {
                            HStack(alignment: .top) {
                                Image(systemName: "text.bubble").foregroundStyle(theme.accent)
                                Text(note).textSelection(.enabled)
                                Spacer()
                                Button("Edit") { workspace.editNote(line: index) }.buttonStyle(.plain)
                            }.font(.kit(size: 12)).padding(12).background(theme.raised).padding(.vertical, 8).padding(.horizontal, 16)
                        }
                    }
                }.padding(.vertical, 12)
            }
            Rule()
            HStack {
                Text("Click a diff row to leave a note")
                Spacer()
                Text("Notes attach to composer")
            }.font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
        }
        .task(id: workspace.selectedFile) {
            guard let file else { return }
            let language = (file.path as NSString).pathExtension
            let before = file.beforeContent ?? ""
            let old = (try? await CodeSyntaxHighlighter.shared.spans(for: .init(source: before, language: language))) ?? []
            let new = (try? await CodeSyntaxHighlighter.shared.spans(for: .init(source: file.content, language: language))) ?? []
            guard !Task.isCancelled else { return }
            syntaxRows = ReviewHighlighting.rows(patch: file.patch, before: before, after: file.content,
                                                beforeSpans: old, afterSpans: new)
            highlightedPath = file.path
        }
    }

    private func coloredLine(_ line: String, index: Int, fallback: Color) -> AttributedString {
        let result = NSMutableAttributedString(string: line.isEmpty ? " " : line,
            attributes: [.foregroundColor: NSColor(fallback)])
        if highlightedPath == workspace.selectedFile, syntaxRows.indices.contains(index) {
            for span in syntaxRows[index] {
                let color: Color = switch span.role {
                case "comment": theme.muted
                case "string", "escape": theme.success
                case "keyword", "keywordType", "function", "type", "builtin": theme.accent
                default: theme.text
                }
                result.addAttribute(.foregroundColor, value: NSColor(theme.syntax(span.role, fallback: color)), range: span.range)
            }
        }
        return AttributedString(result)
    }

    private func diffLine(_ line: String, index: Int) -> some View {
        let added = line.hasPrefix("+") && !line.hasPrefix("+++")
        let removed = line.hasPrefix("-") && !line.hasPrefix("---")
        let color = added ? theme.success : removed ? theme.danger : theme.text
        return Button { workspace.editNote(line: index) } label: {
            HStack(alignment: .top, spacing: 12) {
                Text("\(index + 1)").font(.kit(size: 10, design: .monospaced))
                    .foregroundStyle(theme.muted).frame(width: 30, alignment: .trailing)
                Text(coloredLine(line, index: index, fallback: color)).font(.kit(size: 11, design: .monospaced))
                    .foregroundStyle(color).frame(maxWidth: .infinity, alignment: .leading).multilineTextAlignment(.leading)
            }.padding(.horizontal, 12).padding(.vertical, 5)
                .background(workspace.editingLine == index ? theme.hover : added || removed ? color.opacity(0.07) : Color.clear)
                .contentShape(Rectangle())
        }.buttonStyle(.plain).accessibilityLabel("Diff row \(index + 1): \(line)")
    }

    private var noteEditor: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Review note").font(.kit(size: 12, weight: .semibold))
            TextField("What should change?", text: $workspace.noteDraft, axis: .vertical)
                .textFieldStyle(.plain).lineLimit(2...5).padding(10)
                .clipShape(RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.accent, lineWidth: 1))
                .accessibilityLabel("Review note editor")
            HStack {
                Button("Cancel") { workspace.editingLine = nil }.buttonStyle(MicaButtonStyle(compact: true))
                Spacer()
                Button("Save note") { workspace.saveNote() }.buttonStyle(MicaButtonStyle(primary: true, compact: true))
            }
        }.padding(16).background(theme.raised).padding(12)
    }
}
