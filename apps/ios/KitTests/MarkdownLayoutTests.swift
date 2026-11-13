import SwiftUI
import Testing
import UIKit
@testable import Kit

/// A message's text keeps every line: offered less height than it needs, as
/// a list item measured for a wider width than it gets is, it still draws its
/// lines instead of ending in an ellipsis.
@MainActor struct MarkdownLayoutTests {
    static let source = """
    - **What changed:**
      - If a run is still going, or the last message isn't a reply, the session opens at the end as before.
    """

    /// In a 353 pt column the outer item takes one line and the nested item
    /// three. Offered 40 pt, the list draws the same four lines.
    @Test func aNestedItemOfferedTooLittleHeightDrawsEveryLine() async throws {
        let size = CGSize(width: 353, height: 200)
        let roomy = try await AskRowLayoutTests.render(MarkdownBlocks(source: Self.source).frame(width: 353), size: size)
        let cramped = try await AskRowLayoutTests.render(MarkdownBlocks(source: Self.source).frame(width: 353, height: 40), size: size)
        #expect(Self.lines(roomy) == 4)
        #expect(Self.lines(cramped) == 4)
    }

    /// The bands of text, top to bottom, right of the bullets.
    static func lines(_ image: UIImage) -> Int {
        var count = 0, inLine = false
        for y in stride(from: CGFloat(0), to: image.size.height, by: 1) {
            let painted = AskRowLayoutTests.runs(image, y: y).contains { $0.lowerBound > 40 }
            if painted, !inLine { count += 1 }
            inLine = painted
        }
        return count
    }
}
