import Foundation
import ImageIO
import KitClient
import Testing
import UniformTypeIdentifiers
@testable import Kit

@MainActor
struct ComposerAttachmentTests {
    private func wait(_ condition: () -> Bool) async throws {
        for _ in 0..<200 where !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }

    private func png(width: Int, height: Int) throws -> Data {
        let context = try #require(CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                                             space: CGColorSpaceCreateDeviceRGB(),
                                             bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        context.setFillColor(red: 0.2, green: 0.4, blue: 0.8, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        let image = try #require(context.makeImage())
        let data = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(data, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, nil)
        #expect(CGImageDestinationFinalize(destination))
        return data as Data
    }

    @Test func anAttachedFileGoesWithTheNextSendAndLeavesTheDraft() async throws {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        composer.attach(filename: "note.txt", data: Data("hello".utf8))
        #expect(composer.action == .send(enabled: false))
        try await wait { composer.uploads.ready }
        #expect(client.uploaded == ["note.txt"])
        #expect(composer.action == .send(enabled: true))

        composer.send()
        await SessionComposerTests.settle()
        #expect(client.submitted.map(\.attachmentIds) == [["attachment_1"]])
        #expect(composer.uploads.items.isEmpty)
        #expect(composer.action == .send(enabled: false))
    }

    @Test func aFailedUploadHoldsTheSendUntilItIsRemoved() async throws {
        let client = FakeComposerClient()
        client.uploadError = ClientError.http(503)
        let composer = SessionComposerTests.composer(client)
        composer.draft = "Look at this"
        composer.attach(filename: "note.txt", data: Data("hello".utf8))
        try await wait { composer.uploads.items.first?.error != nil }
        #expect(composer.action == .send(enabled: false))

        composer.uploads.remove(try #require(composer.uploads.items.first?.id))
        #expect(composer.action == .send(enabled: true))
    }

    @Test func anImageIsNotAttachedWhenTheModelTakesNone() async throws {
        let client = FakeComposerClient()
        var snapshot = SessionComposerTests.snapshot()
        snapshot.inputs = ["text"]
        let composer = SessionComposerTests.composer(client, snapshot: snapshot)
        composer.modelName = { "Kimi K2.6" }
        #expect(composer.imageRejectingModel == "Kimi K2.6")

        composer.attach(filename: "shot.png", data: try png(width: 8, height: 8))
        try await wait { composer.uploads.items.isEmpty }
        #expect(composer.uploads.error == "Kimi K2.6 doesn’t accept images, so shot.png wasn’t attached.")
        #expect(client.uploaded == [])
    }

    @Test func eightFilesFillTheDraft() {
        let composer = SessionComposerTests.composer()
        #expect(composer.attachmentRoom == 8)
        for index in 0..<3 { composer.attach(filename: "note\(index).txt", data: Data("hi".utf8)) }
        #expect(composer.attachmentRoom == 5)
    }

    @Test func aPhotoIsScaledToFitAsJPEG() throws {
        let data = try #require(PhotoPreparation.jpeg(try png(width: 5000, height: 2000)))
        let source = try #require(CGImageSourceCreateWithData(data as CFData, nil))
        #expect(CGImageSourceGetType(source) as String? == UTType.jpeg.identifier)
        let properties = try #require(CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any])
        #expect(properties[kCGImagePropertyPixelWidth] as? Int == 3072)
        #expect(properties[kCGImagePropertyPixelHeight] as? Int == 1229)
    }

    @Test func aSmallPhotoKeepsItsSize() throws {
        let data = try #require(PhotoPreparation.jpeg(try png(width: 640, height: 480)))
        let source = try #require(CGImageSourceCreateWithData(data as CFData, nil))
        let properties = try #require(CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any])
        #expect(properties[kCGImagePropertyPixelWidth] as? Int == 640)
        #expect(properties[kCGImagePropertyPixelHeight] as? Int == 480)
        #expect(PhotoPreparation.jpeg(Data("not an image".utf8)) == nil)
    }
}
