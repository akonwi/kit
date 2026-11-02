import KitClient
import SwiftUI
import UIKit

extension EnvironmentValues {
    /// The open session's attachment downloads, shared by its transcript.
    @Entry var transcriptAttachments: TranscriptAttachmentStore? = nil
}

/// A message's or tool call's attachments: images as previews that open
/// full screen, other files as cards to share.
struct TranscriptAttachments: View {
    let attachments: [TranscriptAttachment]

    var body: some View {
        if !attachments.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(Array(attachments.enumerated()), id: \.offset) { _, reference in
                    AttachmentCard(reference: reference)
                }
            }
        }
    }
}

/// One attachment, downloaded when it appears.
private struct AttachmentCard: View {
    @Environment(\.transcriptAttachments) private var store
    let reference: TranscriptAttachment
    @State private var content: TranscriptAttachmentStore.Content?
    /// The downloaded bytes as a file, to share.
    @State private var file: URL?
    @State private var error: String?
    @State private var attempt = 0
    @State private var viewing = false

    private var filename: String { content?.info.filename ?? reference.filename }
    private var isImage: Bool { reference.isImage || reference.mediaType?.hasPrefix("image/") == true }

    var body: some View {
        Group {
            if let image = content?.image {
                Button { viewing = true } label: {
                    Image(decorative: image, scale: 1)
                        .resizable()
                        .scaledToFit()
                        .clipShape(RoundedRectangle(cornerRadius: Theme.controlRadius))
                        .overlay {
                            RoundedRectangle(cornerRadius: Theme.controlRadius).strokeBorder(Theme.border, lineWidth: 0.5)
                        }
                        .frame(maxHeight: 320, alignment: .leading)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Image: \(filename)")
                .accessibilityHint("Opens the image full screen")
                .accessibilityIdentifier("attachment-image")
            } else {
                card
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .fullScreenCover(isPresented: $viewing) {
            if let content { ImageViewer(content: content, file: file) }
        }
        .task(id: Request(reference: reference, session: store?.session, attempt: attempt)) {
            content = nil; file = nil; error = nil
            guard let store else { error = "Attachment unavailable"; return }
            do {
                let loaded = try await store.load(reference)
                content = loaded
                file = await Self.write(loaded)
            } catch is CancellationError {
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    /// A file's name, size, and preview, or the download's progress or failure.
    private var card: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: isImage ? "photo" : "doc")
                    .foregroundStyle(Theme.muted)
                Text(filename)
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                if let content {
                    Text(ByteCountFormatter.string(fromByteCount: content.info.size, countStyle: .file))
                        .foregroundStyle(Theme.muted)
                }
                if let file {
                    ShareLink(item: file) { Image(systemName: "square.and.arrow.up") }
                        .accessibilityLabel("Share \(filename)")
                }
            }
            .font(.system(size: 13))
            if let text = content?.text {
                Text(text.split(separator: "\n", omittingEmptySubsequences: false).prefix(8).joined(separator: "\n"))
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.text)
                    .lineLimit(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if let error {
                HStack(spacing: 8) {
                    Text(error).foregroundStyle(Theme.muted)
                    if store != nil, reference.id != nil {
                        Button("Retry") { attempt += 1 }.foregroundStyle(Theme.accent)
                    }
                }
                .font(.system(size: 13))
            } else if content == nil {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text(isImage ? "Loading image…" : "Loading attachment…").foregroundStyle(Theme.muted)
                }
                .font(.system(size: 13))
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.controlRadius))
        .overlay { RoundedRectangle(cornerRadius: Theme.controlRadius).strokeBorder(Theme.border, lineWidth: 0.5) }
    }

    private struct Request: Equatable {
        let reference: TranscriptAttachment
        let session: String?
        let attempt: Int
    }

    /// Writes an attachment to a temporary file named as it was attached.
    private static func write(_ content: TranscriptAttachmentStore.Content) async -> URL? {
        let info = content.info, data = content.data
        return await Task.detached(priority: .utility) {
            let folder = FileManager.default.temporaryDirectory
                .appendingPathComponent("attachments", isDirectory: true)
                .appendingPathComponent(info.id, isDirectory: true)
            let name = URL(fileURLWithPath: info.filename).lastPathComponent
            let url = folder.appendingPathComponent(name.isEmpty ? info.id : name)
            if FileManager.default.fileExists(atPath: url.path) { return url }
            do {
                try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
                try data.write(to: url, options: .atomic)
                return url
            } catch {
                return nil
            }
        }.value
    }
}

/// An image full screen: pinch or double-tap to zoom, drag to pan, and
/// share the original file.
private struct ImageViewer: View {
    let content: TranscriptAttachmentStore.Content
    let file: URL?
    @Environment(\.dismiss) private var dismiss
    @State private var image: UIImage?
    @State private var scale: CGFloat = 1
    @State private var settledScale: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var settledOffset: CGSize = .zero

    var body: some View {
        NavigationStack {
            GeometryReader { _ in
                Group {
                    if let image {
                        Image(uiImage: image).resizable().scaledToFit()
                    } else if let preview = content.image {
                        Image(decorative: preview, scale: 1).resizable().scaledToFit()
                    }
                }
                .scaleEffect(scale)
                .offset(offset)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .contentShape(Rectangle())
                .gesture(zoom.simultaneously(with: pan))
                .onTapGesture(count: 2) {
                    withAnimation(.snappy) {
                        scale = scale > 1 ? 1 : 2.5
                        settledScale = scale
                        offset = .zero
                        settledOffset = .zero
                    }
                }
            }
            .background(.black)
            .ignoresSafeArea(edges: .bottom)
            .navigationTitle(content.info.filename)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(.visible, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", role: .confirm) { dismiss() }
                }
                if let file {
                    ToolbarItem(placement: .topBarLeading) {
                        ShareLink(item: file) { Image(systemName: "square.and.arrow.up") }
                            .accessibilityLabel("Share image")
                    }
                }
            }
        }
        .accessibilityIdentifier("image-viewer")
        .task {
            let data = content.data
            image = await Task.detached(priority: .userInitiated) { UIImage(data: data) }.value
        }
    }

    private var zoom: some Gesture {
        MagnifyGesture()
            .onChanged { scale = min(max(settledScale * $0.magnification, 1), 8) }
            .onEnded { _ in
                settledScale = scale
                if scale == 1 { withAnimation(.snappy) { offset = .zero; settledOffset = .zero } }
            }
    }

    private var pan: some Gesture {
        DragGesture()
            .onChanged { value in
                guard scale > 1 else { return }
                offset = CGSize(width: settledOffset.width + value.translation.width,
                                height: settledOffset.height + value.translation.height)
            }
            .onEnded { _ in settledOffset = offset }
    }
}
