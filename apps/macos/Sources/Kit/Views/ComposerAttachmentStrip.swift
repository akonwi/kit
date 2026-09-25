import SwiftUI

struct ComposerAttachmentStrip: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    var body: some View {
        let uploads = state.ui.uploads
        if let error = uploads.error {
            HStack { Text(error); Button("Dismiss") { uploads.error = nil } }.font(.kit(size: 12)).padding(12)
        }
        if !uploads.items.isEmpty {
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: 8) {
                    ForEach(uploads.items) { item in
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                if item.busy { KitSpinner() }
                                Text(item.filename).lineLimit(1).truncationMode(.middle)
                                Button { uploads.remove(item.id) } label: { Image(systemName: "xmark") }
                                    .accessibilityLabel("Remove \(item.filename)")
                            }
                            if let preview = item.preview {
                                Image(nsImage: preview).resizable().scaledToFit().frame(width: 120, height: 76)
                                    .accessibilityLabel("Preview of \(item.filename)")
                            }
                            if let error = item.error {
                                Text(error).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
                                if item.data != nil, let client = state.composerClient {
                                    Button("Retry upload") { uploads.retry(item.id, client: client, session: state.selectedID) }
                                }
                            }
                        }.font(.kit(size: 12)).buttonStyle(.plain).padding(10).frame(width: 200, alignment: .leading)
                            .background(theme.raised, in: RoundedRectangle(cornerRadius: 6))
                    }
                }.padding(12)
            }.scrollIndicators(.hidden)
            Rule()
        }
    }
}

struct RestoredAttachmentStrip: View {
    @Bindable var state: SessionStore
    @State private var metadata: [String: WireAttachmentInfo] = [:]
    @State private var missing: Set<String> = []
    @State private var error: String?
    var body: some View {
        if !state.ui.serverAttachmentIDs.isEmpty {
            VStack(alignment: .leading) {
                ForEach(state.ui.serverAttachmentIDs, id: \.self) { id in
                    HStack {
                        Label(metadata[id]?.filename ?? (missing.contains(id) ? "Attachment unavailable" : "Loading attachment…"), systemImage: "paperclip")
                        Button { state.ui.serverAttachmentIDs.removeAll { $0 == id } } label: { Image(systemName: "xmark") }
                            .buttonStyle(.plain).accessibilityLabel("Remove \(metadata[id]?.filename ?? "attachment")")
                    }
                }
                if let error { Text(error); Button("Retry") { Task { await load() } } }
            }.font(.kit(size: 12)).padding(12)
                .task(id: state.ui.serverAttachmentIDs) { await load() }
        }
    }
    private func load() async {
        guard let client = state.composerClient else { return }
        let session = state.selectedID, ids = state.ui.serverAttachmentIDs
        do {
            let result = try await client.resolveAttachments(session, ids: Array(Set(ids)))
            try Task.checkCancellation()
            guard session == state.selectedID else { return }
            metadata = Dictionary(uniqueKeysWithValues: (result.attachments ?? []).map { ($0.id, $0) })
            missing = Set(result.missingAttachmentIds ?? []); error = nil
        } catch is CancellationError {} catch { self.error = error.localizedDescription }
    }
}
