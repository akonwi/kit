import AppKit
import Testing
import UniformTypeIdentifiers
@testable import Kit

@MainActor struct ComposerPasteTests {
    private func board() -> NSPasteboard {
        NSPasteboard(name: NSPasteboard.Name("kit-composer-paste-\(UUID().uuidString)"))
    }

    @Test func pastedFileURLStagesAttachmentWithoutInsertingPath() throws {
        let pasteboard = board()
        defer { pasteboard.releaseGlobally() }
        let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".txt")
        try Data("notes".utf8).write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }
        let item = NSPasteboardItem()
        item.setString(file.absoluteString, forType: .fileURL)
        pasteboard.writeObjects([item])
        let editor = ComposerTextView(frame: .zero)
        editor.string = "draft"
        editor.attachmentDrop = { providers in
            #expect(providers.count == 1)
            #expect(providers.first?.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) == true)
            return true
        }
        #expect(editor.pasteAttachments(from: pasteboard))
        #expect(editor.string == "draft")
    }

    @Test func pastedImageStagesUsableImageWithoutInsertingText() async throws {
        let pasteboard = board()
        defer { pasteboard.releaseGlobally() }
        let bitmap = try #require(NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1, pixelsHigh: 1,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        let png = try #require(bitmap.representation(using: .png, properties: [:]))
        let item = NSPasteboardItem()
        item.setData(Data([0x49, 0x49, 0x2a, 0x00]), forType: .tiff)
        item.setData(png, forType: .png)
        pasteboard.writeObjects([item])
        let editor = ComposerTextView(frame: .zero)
        editor.string = "draft"
        var image: NSItemProvider?
        editor.attachmentDrop = { providers in
            #expect(providers.count == 1)
            image = providers.first
            return true
        }
        #expect(editor.pasteAttachments(from: pasteboard))
        #expect(editor.string == "draft")
        let provider = try #require(image)
        #expect(provider.registeredTypeIdentifiers.first == UTType.png.identifier)
        let loaded = await withCheckedContinuation { continuation in
            provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, _ in
                continuation.resume(returning: data)
            }
        }
        #expect(loaded == png)
    }

    @Test func pastedPathsBecomeAttachmentsButProseAndUnavailableEditorsDoNot() throws {
        let pasteboard = board()
        defer { pasteboard.releaseGlobally() }
        let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + " notes.txt")
        try Data("notes".utf8).write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }
        pasteboard.setString("\"\(file.path)\"", forType: .string)
        let editor = ComposerTextView(frame: .zero)
        editor.attachmentDrop = { providers in
            #expect(providers.count == 1)
            #expect(providers.first?.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) == true)
            return true
        }
        #expect(editor.pasteAttachments(from: pasteboard))
        pasteboard.setString("Please inspect \(file.path)", forType: .string)
        #expect(!editor.pasteAttachments(from: pasteboard))
        let binary = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".bin")
        try Data([0, 1, 2]).write(to: binary)
        defer { try? FileManager.default.removeItem(at: binary) }
        pasteboard.setString(binary.path, forType: .string)
        #expect(!editor.pasteAttachments(from: pasteboard))
        pasteboard.setString(file.path, forType: .string)
        editor.attachmentDrop = { _ in false }
        #expect(!editor.pasteAttachments(from: pasteboard))
        editor.isEditable = false
        #expect(!editor.pasteAttachments(from: pasteboard))
        editor.isEditable = true
        editor.setDraftText("!ls")
        #expect(!editor.pasteAttachments(from: pasteboard))
    }
}
