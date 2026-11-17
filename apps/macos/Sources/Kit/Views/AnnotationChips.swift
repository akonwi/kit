import SwiftUI

/// The session's line annotations as composer chips, in the composer's one
/// row of chips (ADR 0070): each reveals its lines, and its × deletes it.
/// Past three, the rest sit behind a menu.
struct LineAnnotationChips: View {
    @Environment(\.mica) private var theme
    let state: SessionStore

    var body: some View {
        let annotations = state.annotationState
        ForEach(Array(annotations.records.prefix(3))) { note in
            CommentChipFrame(help: note.statusLabel ?? note.body, label: "Annotation on \(note.label): \(note.body)") {
                Button {
                    AnnotationNotices.reveal(note, state: state)
                } label: {
                    HStack(spacing: 6) {
                        IconView(note.statusLabel != nil ? .warning : .chatCenteredText, size: 12)
                            .foregroundStyle(note.statusLabel != nil ? theme.warning : theme.muted)
                        Text(note.label)
                            .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                            .lineLimit(1).truncationMode(.middle).frame(maxWidth: 160, alignment: .leading).fixedSize()
                        Text(note.statusLabel ?? note.body).lineLimit(1).truncationMode(.tail)
                            .frame(maxWidth: 180, alignment: .leading)
                    }.contentShape(Rectangle())
                }.buttonStyle(.plain)
            } remove: {
                guard let client = state.catalogClient as? any AnnotationClient else { return }
                let session = state.selectedID
                Task { await annotations.remove(client: client, session: session, id: note.id) }
            }
            .disabled(annotations.pending)
        }
        if annotations.records.count > 3 {
            Menu("\(annotations.records.count - 3) more") {
                ForEach(Array(annotations.records.dropFirst(3))) { note in
                    Button(note.label + " · " + (note.statusLabel ?? String(note.body.prefix(60)))) {
                        AnnotationNotices.reveal(note, state: state)
                    }
                }
            }.menuStyle(.borderlessButton).fixedSize().font(.kit(size: 12))
        }
    }
}

/// A composer chip's frame: its content, then a × that removes it, on the
/// surface with a hairline, as every chip in the composer's row is drawn.
struct CommentChipFrame<Content: View>: View {
    @Environment(\.mica) private var theme
    let help: String
    let label: String
    @ViewBuilder let content: Content
    let remove: () -> Void
    @State private var hovering = false

    var body: some View {
        HStack(spacing: 6) {
            content
            Button(action: remove) {
                IconView(.x, size: 10).frame(width: 18, height: 18).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .accessibilityLabel("Remove " + label)
        }
        .font(.kit(size: 12))
        .padding(.leading, 6).padding(.trailing, 4).padding(.vertical, 4)
        .background(hovering ? theme.hover : theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.border, lineWidth: 1))
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(label)
    }
}

/// What the composer says about its line annotations under its row of
/// chips: a failed or unconfirmed change, and the sheet a stale one opens.
/// It keeps them synchronized with the session.
struct AnnotationNotices: View {
    @Environment(\.mica) private var theme
    let state: SessionStore

    static func reveal(_ note: FileAnnotation, state: SessionStore) {
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
        }.padding(.horizontal, 16).padding(.vertical, annotations.error == nil ? 0 : 8)
            .task(id: state.selectedID + "|" + (state.selected?.watchGeneration ?? "")) {
                guard let client = state.catalogClient as? any AnnotationClient else { return }
                try? await annotations.refresh(client: client, session: state.selectedID)
            }
    }
}
