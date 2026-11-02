import ImageIO
import KitClient
import PhotosUI
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// The capsule's + button: a menu of Photo Library, Take Photo, and Choose
/// File. Each picked file joins the draft and uploads at once.
struct AttachMenu: View {
    let composer: SessionComposer
    @State private var choosingPhotos = false
    @State private var photos: [PhotosPickerItem] = []
    @State private var takingPhoto = false
    @State private var choosingFiles = false

    var body: some View {
        Menu {
            Button("Photo Library", systemImage: "photo.on.rectangle") { choosingPhotos = true }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button("Take Photo", systemImage: "camera") { takingPhoto = true }
            }
            Button("Choose File", systemImage: "folder") { choosingFiles = true }
        } label: {
            Image(systemName: "plus")
                .font(.system(size: 18, weight: .medium))
                .foregroundStyle(composer.attachmentRoom > 0 ? Theme.text : Theme.muted)
                .frame(width: 40, height: 40)
                .contentShape(Circle())
        }
        .menuOrder(.fixed)
        .disabled(!composer.editable || composer.attachmentRoom == 0)
        .accessibilityLabel("Attach")
        .accessibilityIdentifier("attach-menu")
        .photosPicker(isPresented: $choosingPhotos, selection: $photos,
                      maxSelectionCount: max(1, composer.attachmentRoom), matching: .images)
        .onChange(of: photos) {
            let picked = photos
            photos = []
            for item in picked { load(item) }
        }
        .fileImporter(isPresented: $choosingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            for url in urls.prefix(composer.attachmentRoom) { composer.attach(url: url) }
        }
        .fullScreenCover(isPresented: $takingPhoto) {
            CameraPicker { image in
                guard let data = image.jpegData(compressionQuality: 0.95) else { return }
                attachPhoto(data, name: "Photo")
            }
            .ignoresSafeArea()
        }
    }

    private func load(_ item: PhotosPickerItem) {
        Task {
            guard let data = try? await item.loadTransferable(type: Data.self) else { return }
            attachPhoto(data, name: "Photo")
        }
    }

    /// Attaches a photo scaled to fit what models take.
    private func attachPhoto(_ data: Data, name: String) {
        Task {
            let prepared = await Task.detached(priority: .userInitiated) { PhotoPreparation.jpeg(data) }.value
            composer.attach(filename: name + ".jpg", data: prepared ?? data)
        }
    }
}

/// Phone photos are larger than an attachment may be, so they go as JPEG at
/// most 3072 pixels on the longer side.
enum PhotoPreparation {
    static let maxPixels = 3072

    /// The photo as a JPEG that fits, or nil when it isn't an image.
    nonisolated static func jpeg(_ data: Data) -> Data? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                  kCGImageSourceCreateThumbnailFromImageAlways: true,
                  kCGImageSourceCreateThumbnailWithTransform: true,
                  kCGImageSourceThumbnailMaxPixelSize: maxPixels
              ] as CFDictionary) else { return nil }
        let output = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(output, UTType.jpeg.identifier as CFString, 1, nil) else {
            return nil
        }
        CGImageDestinationAddImage(destination, image, [kCGImageDestinationLossyCompressionQuality: 0.85] as CFDictionary)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return output as Data
    }
}

/// The camera, for Take Photo.
private struct CameraPicker: UIViewControllerRepresentable {
    let take: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ picker: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(_ parent: CameraPicker) { self.parent = parent }

        func imagePickerController(_ picker: UIImagePickerController,
                                   didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let image = info[.originalImage] as? UIImage { parent.take(image) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) { parent.dismiss() }
    }
}

/// The draft's attachments above the capsule: images as tiles, other files
/// as chips, and a notice when one couldn't be attached. Scrolls sideways
/// past four.
struct AttachmentStrip: View {
    let composer: SessionComposer

    var body: some View {
        let uploads = composer.uploads
        VStack(alignment: .leading, spacing: 8) {
            if let error = uploads.error {
                AttachmentNotice(text: error) { uploads.error = nil }
            } else if composer.hasRejectedImages, let model = composer.imageRejectingModel {
                AttachmentNotice(text: "\(model) doesn’t accept images. Remove them to send.", dismiss: nil)
            }
            if !uploads.items.isEmpty || !composer.restoredAttachmentIDs.isEmpty {
                ScrollView(.horizontal) {
                    HStack(alignment: .bottom, spacing: 10) {
                        ForEach(composer.restoredAttachmentIDs, id: \.self) { id in
                            Removable(label: composer.restoredNames[id] ?? "attachment") {
                                composer.removeRestored(id)
                            } content: {
                                FileChip(name: composer.restoredNames[id] ?? "Attachment", detail: .text("Attached before"))
                            }
                        }
                        ForEach(uploads.items) { item in
                            Removable(label: item.filename) { uploads.remove(item.id) } content: {
                                tile(item)
                            }
                        }
                    }
                    .padding(.leading, 4)
                    .padding(.trailing, 6)
                    .padding(.top, 6)
                }
                .scrollIndicators(.hidden)
                .accessibilityIdentifier("attachment-strip")
                .task(id: composer.restoredAttachmentIDs) { await composer.loadRestoredNames() }
            }
        }
    }

    @ViewBuilder
    private func tile(_ item: ComposerAttachments.Item) -> some View {
        if let preview = item.preview {
            ImageTile(image: preview, busy: item.busy, failed: item.error != nil) {
                composer.retryUpload(item.id)
            }
            .accessibilityLabel(item.error == nil ? item.filename : "\(item.filename), didn’t upload")
        } else {
            FileChip(name: item.filename, detail: item.error != nil
                ? (item.data != nil ? .failed { composer.retryUpload(item.id) } : .text(item.error ?? ""))
                : item.busy ? .uploading : .text(item.data.map { ByteCountFormatter.string(fromByteCount: Int64($0.count), countStyle: .file) } ?? ""))
        }
    }
}

/// A tile with the × that removes it at its top right.
private struct Removable<Content: View>: View {
    let label: String
    let remove: () -> Void
    @ViewBuilder let content: Content

    var body: some View {
        content
            .overlay(alignment: .topTrailing) {
                Button(action: remove) {
                    Image(systemName: "xmark")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(Theme.surface)
                        .frame(width: 22, height: 22)
                        .background(Theme.text, in: Circle())
                        .overlay { Circle().strokeBorder(Theme.surface, lineWidth: 2) }
                        .contentShape(Circle().inset(by: -8))
                }
                .buttonStyle(.plain)
                .offset(x: 6, y: -6)
                .accessibilityLabel("Remove \(label)")
            }
    }
}

/// An attached image: its thumbnail, dimmed under a spinner while it
/// uploads, or with Retry when the upload failed.
private struct ImageTile: View {
    let image: CGImage
    let busy: Bool
    let failed: Bool
    let retry: () -> Void

    var body: some View {
        Image(decorative: image, scale: 1)
            .resizable()
            .scaledToFill()
            .frame(width: 64, height: 64)
            .overlay {
                if busy {
                    Color.black.opacity(0.35)
                    ProgressView().tint(.white)
                } else if failed {
                    Color.black.opacity(0.45)
                    Button(action: retry) {
                        Image(systemName: "arrow.clockwise")
                            .font(.system(size: 17, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: 64, height: 64)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Retry upload")
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay { RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.border, lineWidth: 0.5) }
            .shadow(color: .black.opacity(0.14), radius: 7, y: 4)
            .accessibilityElement(children: .contain)
    }
}

/// An attached file: its extension, name, and size, or what went wrong.
private struct FileChip: View {
    enum Detail {
        case text(String)
        case uploading
        case failed(retry: () -> Void)
    }

    let name: String
    let detail: Detail

    var body: some View {
        HStack(spacing: 10) {
            Text(kind)
                .font(.system(size: 9, weight: .semibold, design: .monospaced))
                .foregroundStyle(Theme.muted)
                .padding(.bottom, 6)
                .frame(width: 36, height: 44, alignment: .bottom)
                .background(Theme.raised, in: RoundedRectangle(cornerRadius: 6))
                .overlay { RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.border, lineWidth: 0.5) }
            VStack(alignment: .leading, spacing: 2) {
                Text(name)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                switch detail {
                case .text(let text):
                    Text(text).foregroundStyle(Theme.muted).lineLimit(1)
                case .uploading:
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.mini)
                        Text("Uploading").foregroundStyle(Theme.muted)
                    }
                case .failed(let retry):
                    HStack(spacing: 6) {
                        Text("Didn’t upload").foregroundStyle(Theme.danger)
                        Button("Retry", action: retry)
                            .buttonStyle(.plain)
                            .foregroundStyle(Theme.accent)
                    }
                }
            }
            .font(.system(size: 13))
            .frame(maxWidth: 160, alignment: .leading)
        }
        .padding(.leading, 10)
        .padding(.trailing, 14)
        .frame(height: 64)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    /// The file's extension as a short label.
    private var kind: String {
        let ext = (name as NSString).pathExtension.uppercased()
        return ext.isEmpty ? "FILE" : String(ext.prefix(4))
    }
}

/// Why an attachment wasn't attached, with Dismiss when it can be.
private struct AttachmentNotice: View {
    let text: String
    let dismiss: (() -> Void)?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 12))
                .foregroundStyle(Theme.warning)
            Text(text)
                .font(.system(size: 13))
                .foregroundStyle(Theme.text)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let dismiss {
                Button("Dismiss", action: dismiss)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(Theme.accent)
                    .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("attachment-notice")
    }
}
