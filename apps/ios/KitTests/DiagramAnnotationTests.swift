import Foundation
import KitClient
import KitMermaid
import KitMermaidScene
import KitTranscript
import SwiftUI
import Testing
import UIKit
@testable import Kit

/// Comments on parts of a diagram: picked in the diagram's own screen,
/// waiting in the composer, and sent as text after the message.
@MainActor
struct DiagramAnnotationTests {
    /// Drawn as testdata/mermaid/flowchart-labeled-branches-merge-again.txt.
    nonisolated static let flowchart = """
        flowchart TD
        A{Valid?} -- yes --> B[Save]
        A -- no --> C[Reject]
        B --> D[Done]
        C --> D
        """

    // MARK: Sending

    @Test func diagramCommentsGoAfterTheMessageOnceTheServerHasIt() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        composer.diagrams.add(comment: "Retry first.", source: Self.flowchart, part: Part(.node, 2), nonce: "n1")
        composer.draft = "Revise the flow."
        composer.send()
        await SessionComposerTests.settle()
        let text = client.submitted.first?.text ?? ""
        #expect(text.hasPrefix("Revise the flow.\n\n<diagram-annotation n=\"1\">\nComment: Retry first.\n"
            + "[UNTRUSTED_DIAGRAM_CONTENT nonce=n1]\nDiagram: flowchart TD\nTarget: node C \"Reject\" (box)\n"))
        #expect(DiagramAnnotationText.split(text).text == "Revise the flow.")
        #expect(composer.diagrams.isEmpty)
        #expect(composer.draft == "")
    }

    @Test func diagramCommentsAloneMakeAMessage() async {
        let client = FakeComposerClient()
        let composer = SessionComposerTests.composer(client)
        #expect(composer.action == .send(enabled: false))
        composer.diagrams.add(comment: "Rename.", source: Self.flowchart, part: Part(.node, 3), nonce: "n1")
        #expect(composer.action == .send(enabled: true))
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.submitted.first?.text.hasPrefix("<diagram-annotation n=\"1\">\nComment: Rename.\n") == true)
    }

    @Test func aRefusedSendKeepsTheDiagramComments() async {
        let client = FakeComposerClient()
        client.submitError = ClientError.http(429)
        let composer = SessionComposerTests.composer(client)
        composer.diagrams.add(comment: "Rename.", source: Self.flowchart, part: Part(.node, 3))
        composer.send()
        await SessionComposerTests.settle()
        #expect(composer.diagrams.items.map(\.annotation.comment) == ["Rename."])
    }

    @Test func aPromptCommandTakesNoDiagramComments() async throws {
        let client = FakeComposerClient()
        let review = try JSONDecoder().decode(PromptCommand.self, from: Data(
            #"{"name":"review","description":"Review the diff","source":"project","location":"/r.md"}"#.utf8))
        let composer = SessionComposerTests.composer(client, snapshot: SessionComposerTests.snapshot(commands: [review]))
        composer.diagrams.add(comment: "Rename.", source: Self.flowchart, part: Part(.node, 3))
        composer.draft = "/review"
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.isEmpty && client.submitted.isEmpty)
        #expect(composer.notice == "Didn’t send. Prompt commands do not accept attachments or comments.")
    }

    // MARK: The diagram's screen

    /// Shows the diagram on its own with the session's comments, and
    /// returns its scroll view once drawn.
    private func open(_ draft: DiagramAnnotationDraft, annotator: DiagramAnnotator = DiagramAnnotator()) async throws
        -> (MermaidScrollView, MermaidImage, UIWindow) {
        let diagram = MermaidDiagram(source: Self.flowchart)
        let drawing = try diagram.drawing.get()
        // In the host app's scene, so SwiftUI keeps updating it.
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 393, height: 852)
        window.overrideUserInterfaceStyle = .light
        window.rootViewController = UIHostingController(
            rootView: MermaidScreen(diagram: diagram, drawing: drawing, annotations: draft, annotator: annotator))
        window.makeKeyAndVisible()
        var scroll: MermaidScrollView?
        for _ in 0..<40 where scroll?.imageView.image == nil {
            window.layoutIfNeeded()
            try await Task.sleep(for: .milliseconds(25))
            scroll = Self.find(MermaidScrollView.self, in: window)
        }
        let found = try #require(scroll)
        let image = MermaidImage(diagram: diagram, drawing: drawing, scheme: .light, displayScale: window.screen.scale)
        return (found, image, window)
    }

    private static func find<View: UIView>(_ type: View.Type, in view: UIView) -> View? {
        if let found = view as? View { return found }
        return view.subviews.lazy.compactMap { find(type, in: $0) }.first
    }

    /// The image point over the middle of a cell of the scene.
    private func point(_ image: MermaidImage, _ x: Int, _ y: Int) -> CGPoint {
        CGPoint(x: MermaidImage.margin + (CGFloat(x) + 0.5) * image.metrics.cell.width,
                y: MermaidImage.margin + (CGFloat(y) + 0.5) * image.metrics.cell.height)
    }

    /// A tap lands on the part drawn under it, past the image's margin: node
    /// C, "Reject", in cells 11…20 of rows 6…8; edge 1's own line; nothing
    /// where the edges into Done merge, or in the margin.
    @Test func aPointInTheImageFindsThePartDrawnThere() async throws {
        let (_, image, window) = try await open(DiagramAnnotationDraft())
        defer { window.isHidden = true }
        #expect(image.part(at: point(image, 15, 7)) == Part(.node, 2))
        #expect(image.part(at: point(image, 16, 4)) == Part(.edge, 1))
        #expect(image.part(at: point(image, 10, 10)) == nil)
        #expect(image.part(at: CGPoint(x: MermaidImage.margin / 2, y: MermaidImage.margin / 2)) == nil)
    }

    /// The overlay rides in the image, at the image's size, so it zooms
    /// with it. Until Annotate is on it outlines nothing; marks show either way.
    @Test func theOverlayOutlinesThePickedPartAndMarksCommentedOnes() async throws {
        let draft = DiagramAnnotationDraft()
        draft.add(comment: "Rename.", source: Self.flowchart, part: Part(.node, 3))
        let (scroll, image, window) = try await open(draft)
        defer { window.isHidden = true }
        let overlay = scroll.overlay
        #expect(overlay.superview === scroll.imageView)
        #expect(overlay.frame == scroll.imageView.bounds)
        let margin = CGAffineTransform(translationX: MermaidImage.margin, y: MermaidImage.margin)
        let done = image.highlight(Part(.node, 3))
        #expect(overlay.marks.path?.boundingBox == MermaidAnnotationOverlay.markRect(done).applying(margin))
        #expect(overlay.pickedBoxes.path == nil && overlay.hoveredBoxes.path == nil)

        let picked = image.highlight(Part(.node, 2))
        let hovered = image.highlight(Part(.edge, 1))
        overlay.show(MermaidMarks(annotating: true, hovered: Part(.edge, 1), picked: Part(.node, 2),
                                  marked: [Part(.node, 3)]), in: image)
        #expect(overlay.pickedBoxes.path?.boundingBox == picked.bounds.applying(margin))
        #expect(overlay.pickedWash.path?.boundingBox == picked.bounds.applying(margin))
        // A node has no lines of its own.
        #expect(overlay.pickedLines.path == nil)
        #expect(overlay.hoveredLines.path?.boundingBox == hovered.linePath.applying(margin).boundingRect)
        #expect(overlay.pickedBoxes.strokeColor == UIColor(Theme.accent).resolvedColor(with: overlay.traitCollection).cgColor)

        // Off, the outlines go and the marks stay.
        overlay.show(MermaidMarks(annotating: false, hovered: Part(.edge, 1), picked: Part(.node, 2),
                                  marked: [Part(.node, 3)]), in: image)
        #expect(overlay.pickedBoxes.path == nil && overlay.hoveredLines.path == nil)
        #expect(overlay.marks.path?.boundingBox == MermaidAnnotationOverlay.markRect(done).applying(margin))
    }

    /// With Annotate on, a tap picks the part under it and opens the comment
    /// box on it; Comment adds the comment to the message, marks the part,
    /// and closes the box.
    @Test func annotatingPicksAPartAndCommentAddsIt() async throws {
        let draft = DiagramAnnotationDraft()
        let annotator = DiagramAnnotator()
        let (scroll, image, window) = try await open(draft, annotator: annotator)
        defer { window.isHidden = true }
        let tap = try #require(scroll.gestureRecognizers?.compactMap { $0 as? UITapGestureRecognizer }.first)
        let coordinator = try #require(scroll.delegate as? MermaidZoomingView.Coordinator)
        // Taps pan and zoom until Annotate is on.
        #expect(!tap.isEnabled)
        annotator.start()
        try await settle(window)
        #expect(tap.isEnabled)

        coordinator.tapped(at: point(image, 15, 7))
        try await settle(window)
        #expect(annotator.selected == Part(.node, 2))
        let margin = CGAffineTransform(translationX: MermaidImage.margin, y: MermaidImage.margin)
        #expect(scroll.overlay.pickedBoxes.path?.boundingBox == image.highlight(Part(.node, 2)).bounds.applying(margin))
        annotator.comment = "Say why it was rejected."
        try await settle(window)
        // The comment box opens under the diagram with the comment in it.
        let field = try #require(Self.find(UITextView.self, in: window))
        #expect(field.text == "Say why it was rejected.")
        #expect(field.window != nil && field.convert(field.bounds, to: window).maxY <= window.bounds.maxY)
        try snapshot(window, "ios-diagram-comment")

        #expect(annotator.commit(to: draft, source: Self.flowchart))
        try await settle(window)
        #expect(draft.items.map(\.summary) == ["node C \"Reject\" (box)"])
        #expect(scroll.overlay.pickedBoxes.path == nil)
        #expect(scroll.overlay.marks.path?.boundingBox
            == MermaidAnnotationOverlay.markRect(image.highlight(Part(.node, 2))).applying(margin))
        try snapshot(window, "ios-diagram-marked")

        // A tap on nothing lets go of the picked part.
        coordinator.tapped(at: point(image, 15, 7))
        coordinator.tapped(at: point(image, 10, 10))
        #expect(annotator.selected == nil)
    }

    private func settle(_ window: UIWindow) async throws {
        for _ in 0..<6 {
            window.layoutIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
    }

    /// Keeps a picture of the window for review when the runner is given a
    /// folder for them.
    private func snapshot(_ window: UIWindow, _ name: String) throws {
        guard let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] else { return }
        let image = UIGraphicsImageRenderer(bounds: window.bounds).image { context in
            window.layer.render(in: context.cgContext)
        }
        try #require(image.pngData()).write(to: URL(fileURLWithPath: directory).appendingPathComponent("\(name).png"))
    }
}
