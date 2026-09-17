import AppKit
import SwiftUI

private struct TranscriptAttachmentsKey: EnvironmentKey {
    static let defaultValue: TranscriptAttachmentStore? = nil
}
extension EnvironmentValues {
    var transcriptAttachments: TranscriptAttachmentStore? {
        get { self[TranscriptAttachmentsKey.self] }
        set { self[TranscriptAttachmentsKey.self] = newValue }
    }
}

struct TranscriptAttachments: View {
    let attachments: [TranscriptAttachment]
    var body: some View {
        ForEach(Array(attachments.enumerated()), id: \.offset) { _, reference in
            TranscriptAttachmentCard(reference: reference)
        }
    }
}

private struct TranscriptAttachmentCard: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptAttachments) private var store
    let reference: TranscriptAttachment
    @State private var content: TranscriptAttachmentStore.Content?
    @State private var error: String?
    @State private var attempt = 0
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label(content?.info.filename ?? reference.filename,
                      systemImage: reference.isImage || reference.mediaType?.hasPrefix("image/") == true ? "photo" : "doc")
                    .lineLimit(1).truncationMode(.middle)
                Spacer()
                if let content {
                    Text(ByteCountFormatter.string(fromByteCount: content.info.size, countStyle: .file))
                        .foregroundStyle(theme.muted)
                    Button("Save As…") { save(content) }.buttonStyle(.plain)
                }
            }.font(.kit(size: 12))
            if let image = content?.image {
                Image(nsImage: image).resizable().scaledToFit().frame(maxHeight: 240)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityLabel("Image preview: " + (content?.info.filename ?? reference.filename))
            } else if let text = content?.text {
                ScrollView {
                    Text(text).font(.kit(size: 11, design: .monospaced)).textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }.frame(height: min(160, CGFloat(text.split(separator: "\n", omittingEmptySubsequences: false).count) * 16 + 4))
                if content?.truncated == true { Text("Preview truncated · Save As to read the complete file").foregroundStyle(theme.muted) }
            } else if content != nil {
                Text("Preview unavailable · Save As to open this file").foregroundStyle(theme.muted)
            }
            if let error {
                HStack {
                    Text(error).foregroundStyle(theme.muted)
                    if store != nil && reference.id != nil { Button("Retry") { attempt += 1 }.buttonStyle(.plain) }
                }
            } else if content == nil {
                HStack { KitSpinner(); Text("Loading attachment…").foregroundStyle(theme.muted) }
            }
        }.font(.kit(size: 11)).foregroundStyle(theme.text).padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 8))
            .task(id: Request(reference: reference, server: store?.client.serverID, session: store?.session, attempt: attempt)) {
                content = nil; error = nil
                guard let store else { error = "Attachment unavailable"; return }
                do { content = try await store.load(reference) }
                catch is CancellationError {} catch { self.error = error.localizedDescription }
            }
    }
    private struct Request: Equatable { let reference: TranscriptAttachment; let server: String?; let session: String?; let attempt: Int }
    private func save(_ content: TranscriptAttachmentStore.Content) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = URL(fileURLWithPath: content.info.filename).lastPathComponent
        panel.begin { response in
            guard response == .OK, let url = panel.url else { return }
            do { try content.data.write(to: url, options: .atomic) }
            catch { self.error = "Couldn’t save attachment: " + error.localizedDescription }
        }
    }
}
