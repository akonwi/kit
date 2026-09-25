import SwiftUI

struct AnnotationNote: View {
    @Environment(\.mica) private var theme
    let annotation: FileAnnotation
    var edit: () -> Void = {}
    var delete: () -> Void = {}
    @State private var hovered = false
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if annotation.stale {
                Label("File changed", systemImage: "exclamationmark.triangle")
                    .font(.kit(size: 11)).foregroundStyle(theme.muted)
            }
            HStack(alignment: .top, spacing: 12) {
                Text(annotation.body).font(.kit(size: 14)).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                HStack(spacing: 12) {
                    Button(annotation.stale ? "Select new range" : "Edit", action: edit)
                    Button("Delete", action: delete)
                }.font(.kit(size: 11)).buttonStyle(.plain).opacity(hovered ? 1 : 0)
            }
        }.padding(12).foregroundStyle(theme.text)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 10))
            .onHover { hovered = $0 }
            .contextMenu {
                Button(annotation.stale ? "Select new range" : "Edit", action: edit)
                Button("Delete", action: delete)
            }
    }
}

/// Isolated design specimen; source rows here are not the production editor.
struct AnnotationDesignPreview: View {
    @Environment(\.mica) private var theme
    let note: FileAnnotation
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("File · SessionStore.swift").font(.kit(size: 12)).foregroundStyle(theme.muted)
            VStack(alignment: .leading, spacing: 0) {
                sourceRow(41, "func send() {")
                sourceRow(42, "    submit(draft)")
                sourceRow(43, "}")
                AnnotationNote(annotation: note).padding(.leading, 56).padding(.vertical, 8)
                sourceRow(44, "")
                sourceRow(45, "func cancel() {")
                sourceRow(46, "    operation.cancel()")
                sourceRow(47, "}")
            }
            Rule()
            HStack(spacing: 8) {
                Image(systemName: "text.bubble")
                Text(note.label).font(.kit(size: 11, design: .monospaced))
                Text(note.body).lineLimit(1)
                Image(systemName: "xmark")
            }.font(.kit(size: 11)).padding(8).background(theme.raised, in: RoundedRectangle(cornerRadius: 6))
            HStack {
                Text("Ask Kit…").foregroundStyle(theme.muted)
                Spacer()
                Image(systemName: "arrow.up.circle.fill").foregroundStyle(theme.accent)
            }.padding(16).background(theme.raised, in: RoundedRectangle(cornerRadius: 10))
            Rule()
            Text("Accepted message").font(.kit(size: 12)).foregroundStyle(theme.muted)
            VStack(alignment: .leading, spacing: 12) {
                Text("Keep this change focused.")
                TranscriptAnnotations(annotations: [note], expanded: true)
            }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
                .background(theme.accent.opacity(theme.dark ? 0.18 : 0.12), in: RoundedRectangle(cornerRadius: 10))
        }.font(.kit(size: 14)).padding(24).frame(width: 720)
            .foregroundStyle(theme.text).background(theme.surface)
    }

    private func sourceRow(_ line: Int, _ source: String) -> some View {
        let anchored = !note.stale && (note.startLine...note.endLine).contains(line)
        return HStack(spacing: 12) {
            Text(String(line))
                .foregroundStyle(anchored ? theme.accent : theme.muted)
                .frame(width: 44, height: 20, alignment: .trailing)
                .background(anchored ? theme.accent.opacity(0.12) : .clear)
                .overlay(alignment: .leading) {
                    if anchored { Rectangle().fill(theme.accent).frame(width: 2) }
                }
                .accessibilityLabel(anchored ? "Line \(line), annotated" : "Line \(line)")
            Text(source).foregroundStyle(theme.text)
        }.font(.kit(size: 12, design: .monospaced))
    }

}
