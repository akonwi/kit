import SwiftUI

struct AnnotationStrip: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    private func reveal(_ note: FileAnnotation) {
        if note.isDiff && !note.stale {
            state.ui.workspace.open(.review)
            state.ui.workspace.diff.reveal(note)
        } else {
            if !note.isDiff { state.ui.workspace.open(.file(note.path)) }
            state.annotationState.reveal(note)
        }
    }
    var body: some View {
        let annotations = state.annotationState
        VStack(alignment: .leading, spacing: 8) {
            if !annotations.records.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        ForEach(Array(annotations.records.prefix(3))) { note in
                            HStack(spacing: 6) {
                                Button {
                                    reveal(note)
                                } label: {
                                    HStack(spacing: 6) {
                                        Image(systemName: note.statusLabel != nil ? "exclamationmark.triangle" : "text.bubble")
                                        Text(note.label).font(.kit(size: 11, design: .monospaced))
                                        Text(note.statusLabel ?? note.body).lineLimit(1).frame(maxWidth: 160)
                                    }
                                }.buttonStyle(.plain)
                                Button {
                                    guard let client = state.catalogClient as? any AnnotationClient else { return }
                                    let session = state.selectedID
                                    Task { await annotations.remove(client: client, session: session, id: note.id) }
                                } label: { Image(systemName: "xmark") }
                                    .buttonStyle(.plain).accessibilityLabel("Delete annotation " + note.label)
                            }.font(.kit(size: 11)).padding(8)
                                .background(theme.raised, in: RoundedRectangle(cornerRadius: 6))
                                .disabled(annotations.pending)
                        }
                        if annotations.records.count > 3 {
                            Menu("\(annotations.records.count - 3) more") {
                                ForEach(Array(annotations.records.dropFirst(3))) { note in
                                    Button(note.label + " · " + (note.statusLabel ?? String(note.body.prefix(60)))) {
                                        reveal(note)
                                    }
                                }
                            }.menuStyle(.borderlessButton).fixedSize()
                        }
                    }
                }
            }
            if let error = annotations.error {
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.muted)
                if annotations.uncertain {
                    Button("I reviewed the synced annotations") { annotations.acknowledgeUncertainty() }
                        .font(.kit(size: 12))
                }
            }
        }.sheet(item: Binding(get: { annotations.inspected }, set: { annotations.inspected = $0 })) { note in
            StaleAnnotationView(note: note, annotations: annotations, client: state.catalogClient as? any AnnotationClient,
                session: state.selectedID, selectRange: {
                    annotations.replacement = $0
                    annotations.inspected = nil
                    if $0.isDiff {
                        state.ui.workspace.open(.review)
                        if let client = state.catalogClient as? any DiffClient {
                            let model = state.ui.workspace.diff, session = state.selectedID
                            Task { await model.refresh(client: client, session: session) }
                        }
                    } else {
                        state.ui.workspace.open(.file($0.path))
                        state.ui.workspace.filePreviews.invalidate($0.path)
                    }
                })
        }.padding(.horizontal, 16).padding(.vertical, annotations.records.isEmpty ? 0 : 8)
            .task(id: state.selectedID + "|" + (state.selected?.watchGeneration ?? "")) {
                guard let client = state.catalogClient as? any AnnotationClient else { return }
                try? await annotations.refresh(client: client, session: state.selectedID)
            }
    }
}
