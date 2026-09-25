import SwiftUI

struct StaleAnnotationView: View {
    @Environment(\.mica) private var theme
    let note: FileAnnotation
    @Bindable var annotations: AnnotationState
    let client: (any AnnotationClient)?
    let session: String
    let selectRange: (FileAnnotation) -> Void
    @State private var full: FileAnnotation?
    @State private var error: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Label(note.statusLabel ?? "Captured diff annotation", systemImage: note.statusLabel != nil ? "exclamationmark.triangle" : "text.bubble").font(.kit(size: 16, weight: .semibold))
            Text(note.path + ":" + note.rangeLabel).font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.muted)
            if let context = note.diffContext {
                Text(context).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).textSelection(.enabled)
            }
            if let full {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        Text(full.body).textSelection(.enabled)
                        Text(full.source).font(.kit(size: 12, design: .monospaced)).textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if full.truncated { Text("Captured source truncated").foregroundStyle(theme.muted) }
                    }
                }.frame(maxHeight: 360)
            } else if let error { Text(error).foregroundStyle(theme.muted) }
            else { ProgressView("Loading captured source…") }
            HStack {
                Button("Close") { annotations.inspected = nil }
                Spacer()
                Button("Delete", role: .destructive) {
                    guard let client else { return }
                    Task {
                        await annotations.remove(client: client, session: session, id: note.id)
                        if !annotations.records.contains(where: { $0.id == note.id }) { annotations.inspected = nil }
                    }
                }.disabled(annotations.pending)
                if note.stale {
                    Button("Select new range") { if let full { selectRange(full) } }
                        .disabled(full == nil || annotations.editor != nil || annotations.pending)
                }
            }
        }.font(.kit(size: 14)).padding(24).frame(width: 600).foregroundStyle(theme.text).background(theme.surface)
            .task {
                guard let client else { error = "Annotations unavailable"; return }
                do {
                    try await annotations.refresh(client: client, session: session)
                    guard let value = annotations.records.first(where: { $0.id == note.id }), value.complete else {
                        error = "This annotation no longer exists."; return
                    }
                    full = value
                } catch { self.error = error.localizedDescription }
            }
    }
}
