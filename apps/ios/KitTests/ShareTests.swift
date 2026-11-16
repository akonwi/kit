import Foundation
import ImageIO
import KitClient
import KitDictation
import Testing
import UniformTypeIdentifiers
@testable import Kit

/// What's shared from other apps waits in the app group, then goes to the
/// session's draft when the app comes to the front.
@MainActor
struct ShareTests {
    let session = SessionIdentity(server: "studio", session: "s")
    let drafts = SessionDrafts(defaults: UserDefaults(suiteName: UUID().uuidString)!)

    func inbox() -> ShareInbox {
        ShareInbox(folder: FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString))
    }

    func dictations() -> SessionDictation {
        SessionDictation(engine: SharedMicrophone(ScriptedDictationEngine(transcript: "", delay: .zero)), drafts: drafts)
    }

    @Test func aSharedLinkFollowsItsNote() {
        #expect(SharedItem.text(note: "  Read this first. \n", shared: ["https://swift.org/blog"])
            == "Read this first.\n\nhttps://swift.org/blog")
        #expect(SharedItem.text(note: "", shared: ["https://swift.org/blog"]) == "https://swift.org/blog")
    }

    @Test func sharedWordsStartANewParagraphInTheDraft() {
        #expect(SharedItem.join("Look at", "https://swift.org") == "Look at\n\nhttps://swift.org")
        #expect(SharedItem.join("  \n", "https://swift.org") == "https://swift.org")
    }

    @Test func sharesWaitInOrderUntilTheyreLetGo() throws {
        let inbox = inbox()
        defer { try? FileManager.default.removeItem(at: inbox.folder) }
        let later = SharedItem(server: "studio", session: "s", text: "second", date: Date(timeIntervalSince1970: 200))
        let earlier = SharedItem(server: "vps", session: "t", text: "first", date: Date(timeIntervalSince1970: 100))
        try inbox.put(later)
        try inbox.put(earlier)
        #expect(inbox.waiting() == [earlier, later])

        inbox.remove(earlier.id)
        #expect(inbox.waiting() == [later])
    }

    @Test func aShareGoesToAClosedSessionsDraft() throws {
        let inbox = inbox()
        defer { try? FileManager.default.removeItem(at: inbox.folder) }
        drafts.save(KeptDraft(text: "Look at", attachmentIDs: ["attachment_1"]), for: session)
        try inbox.put(SharedItem(server: "studio", session: "s", text: "https://swift.org", attachmentIDs: ["attachment_2"]))

        dictations().receiveShares(from: inbox)
        #expect(drafts.draft(for: session)
            == KeptDraft(text: "Look at\n\nhttps://swift.org", attachmentIDs: ["attachment_1", "attachment_2"]))
        #expect(inbox.waiting().isEmpty)
    }

    @Test func aShareGoesToAnOpenSessionsComposer() throws {
        let inbox = inbox()
        defer { try? FileManager.default.removeItem(at: inbox.folder) }
        let dictations = dictations()
        let composer = SessionComposerTests.composer()
        dictations.opened([session])
        dictations.attach(composer, for: session) {}
        composer.draft = "Look at"
        try inbox.put(SharedItem(server: "studio", session: "s", text: "https://swift.org"))

        dictations.receiveShares(from: inbox)
        #expect(composer.draft == "Look at\n\nhttps://swift.org")
        #expect(drafts.draft(for: session).text == "Look at\n\nhttps://swift.org")
        #expect(inbox.waiting().isEmpty)
    }

    @Test func sessionsFromEveryServerListMostRecentFirst() {
        let studio = ShareDestination.listed(.ordered([ListedSession(session: SessionTesting.session("a", at: 100), status: .idle),
                                                      ListedSession(session: SessionTesting.session("b", at: 300), status: .running)]),
                                             on: "studio")
        let vps = ShareDestination.listed(.unordered([SessionTesting.session("c", at: 200)]), on: "vps")
        let ordered = ShareDestination.ordered(studio + vps)
        #expect(ordered.map(\.id) == ["studio\u{1F}b", "vps\u{1F}c", "studio\u{1F}a"])
        #expect(ordered[0] == ShareDestination(server: "studio", session: "b", title: "Session b", folder: "~/kit",
                                               changed: Date(timeIntervalSince1970: 1_800_000_300)))
    }

    @Test func searchFindsEveryWordInTitleFolderOrServer() {
        let destination = ShareDestination(server: "vps", session: "e", title: "Eval runner drops rows",
                                           folder: "~/src/evals", changed: nil)
        #expect(destination.matches(""))
        #expect(destination.matches("eval vps"))
        #expect(destination.matches("SRC rows"))
        #expect(!destination.matches("eval studio"))
    }

    /// A session whose model takes no images says so, by the model's name.
    @Test func aModelWithoutImagesIsNamed() {
        var excerpt = SessionTesting.session("a")
        excerpt.inputs = ["text"]
        #expect(ShareDestination(server: "studio", excerpt: excerpt).imageRejectingModel == "m")
        excerpt.inputs = ["text", "image"]
        #expect(ShareDestination(server: "studio", excerpt: excerpt).imageRejectingModel == nil)
        excerpt.inputs = nil
        #expect(ShareDestination(server: "studio", excerpt: excerpt).imageRejectingModel == nil)
    }

    @Test func anImageTheServerTakesUploadsAsItIs() throws {
        let png = try image(width: 640, height: 480, type: .png)
        let upload = PhotoPreparation.forUpload(png, filename: "Screenshot.png")
        #expect(upload.data == png)
        #expect(upload.filename == "Screenshot.png")
    }

    @Test func otherImagesUploadAsJPEG() throws {
        let tiff = PhotoPreparation.forUpload(try image(width: 640, height: 480, type: .tiff), filename: "Scan.tiff")
        #expect(tiff.filename == "Scan.jpg")
        #expect(type(of: tiff.data) == UTType.jpeg.identifier)

        let large = PhotoPreparation.forUpload(try image(width: 5000, height: 3000, type: .png), filename: "Pano.png")
        #expect(large.filename == "Pano.jpg")
        #expect(type(of: large.data) == UTType.jpeg.identifier)
    }

    @Test func otherFilesUploadAsTheyAre() {
        let text = Data("notes".utf8)
        let upload = PhotoPreparation.forUpload(text, filename: "notes.txt")
        #expect(upload.data == text)
        #expect(upload.filename == "notes.txt")
    }

    private func type(of data: Data) -> String? {
        CGImageSourceCreateWithData(data as CFData, nil).flatMap(CGImageSourceGetType) as String?
    }

    private func image(width: Int, height: Int, type: UTType) throws -> Data {
        let context = try #require(CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                                             space: CGColorSpaceCreateDeviceRGB(),
                                             bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        context.setFillColor(red: 0.2, green: 0.4, blue: 0.8, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        let image = try #require(context.makeImage())
        let data = NSMutableData()
        let destination = try #require(CGImageDestinationCreateWithData(data, type.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, nil)
        #expect(CGImageDestinationFinalize(destination))
        return data as Data
    }
}
