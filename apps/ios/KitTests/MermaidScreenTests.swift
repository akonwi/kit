import KitMermaid
import KitTranscript
import SwiftUI
import Testing
import UIKit
@testable import Kit

/// A diagram opened on its own is drawn whole, at its full size, and
/// fitted to the screen's width.
@MainActor
struct MermaidScreenTests {
    nonisolated private static let wide = """
    sequenceDiagram
      participant Operator
      participant one_off
      participant Server
      participant Store
      participant Queue
      participant Worker
      participant Cache
      participant Mailer
      Operator->>one_off: [commit?, limit]
      one_off->>Server: forward the request to server
      Server->>Store: forward the request to store
      Store->>Queue: forward the request to queue
      Queue->>Worker: forward the request to worker
      Worker->>Cache: forward the request to cache
      Cache->>Mailer: forward the request to mailer
      Mailer-->>one_off: done
      one_off-->>Operator: report
    """

    nonisolated private static let tall = "flowchart TD\n" + (0..<14).map { "  N\($0)[Step \($0)] --> N\($0 + 1)[Step \($0 + 1)]" }
        .joined(separator: "\n")

    /// Shows a diagram on its own in a window of `size` and returns its scroll view once laid out.
    private func open(_ source: String, in size: CGSize) async throws -> (MermaidScrollView, MermaidImage, UIWindow) {
        let diagram = MermaidDiagram(source: source)
        let drawing = try diagram.drawing.get()
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        window.rootViewController = UIHostingController(rootView: MermaidScreen(diagram: diagram, drawing: drawing))
        window.makeKeyAndVisible()
        var scroll: MermaidScrollView?
        for _ in 0..<40 where scroll?.imageView.image == nil {
            window.layoutIfNeeded()
            try await Task.sleep(for: .milliseconds(25))
            scroll = find(in: window)
        }
        let found = try #require(scroll)
        found.layoutIfNeeded()
        let image = MermaidImage(diagram: diagram, drawing: drawing, scheme: .light, displayScale: window.screen.scale)
        return (found, image, window)
    }

    /// Whether an image is `size` in points, to its last whole pixel.
    private func covers(_ image: UIImage, _ size: CGSize) -> Bool {
        abs(image.size.width - size.width) <= 1 / image.scale && abs(image.size.height - size.height) <= 1 / image.scale
    }

    private func find(in view: UIView) -> MermaidScrollView? {
        if let scroll = view as? MermaidScrollView { return scroll }
        return view.subviews.lazy.compactMap { self.find(in: $0) }.first
    }

    @Test(arguments: [(wide, CGSize(width: 393, height: 852)), (wide, CGSize(width: 852, height: 393)),
                      (tall, CGSize(width: 393, height: 852)), (tall, CGSize(width: 852, height: 393))])
    func opensWholeAndFittedToTheWidth(source: String, window: CGSize) async throws {
        let (scroll, image, host) = try await open(source, in: window)
        defer { host.isHidden = true }
        let size = image.size
        // The whole scene and its margin, at full size, drawn in one image.
        #expect(scroll.imageView.bounds.size == size)
        let drawn = try #require(scroll.imageView.image)
        #expect(drawn.scale == image.scale(zoom: 1))
        #expect(covers(drawn, size))
        // Fitted to the width, or at full size when narrower, and centred.
        let fit = min(1, scroll.bounds.width / size.width)
        #expect(abs(scroll.zoomScale - fit) < 0.001)
        #expect(abs(scroll.contentSize.width - size.width * fit) < 0.5)
        #expect(abs(scroll.contentSize.height - size.height * fit) < 0.5)
        #expect(scroll.minimumZoomScale <= fit)
        #expect(scroll.minimumZoomScale <= scroll.bounds.height / size.height + 0.001)
        #expect(abs(scroll.contentInset.left - max(0, (scroll.bounds.width - size.width * fit) / 2)) < 0.5)
    }

    @Test func zoomingInDrawsItSharperWithinTheImageLimits() async throws {
        let (scroll, image, host) = try await open(Self.wide, in: CGSize(width: 393, height: 852))
        defer { host.isHidden = true }
        let screen = host.screen.scale
        #expect(image.scale(zoom: 0.5) == screen)
        #expect(image.scale(zoom: 1) == screen)
        #expect(image.scale(zoom: 3) == min(3 * screen, image.scale(zoom: 100)))
        let size = image.size
        let limit = min(MermaidImage.maximumSide / max(size.width, size.height),
                        (MermaidImage.maximumPixels / (size.width * size.height)).squareRoot()).rounded(.down)
        #expect(image.scale(zoom: 100) == limit)

        scroll.setZoomScale(2, animated: false)
        scroll.delegate?.scrollViewDidEndZooming?(scroll, with: scroll.imageView, atScale: 2)
        let drawn = try #require(scroll.imageView.image)
        #expect(drawn.scale == image.scale(zoom: 2))
        #expect(covers(drawn, size))
        #expect(scroll.imageView.bounds.size == size)
    }
}
