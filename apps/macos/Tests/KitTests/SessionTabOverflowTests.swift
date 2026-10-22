import Foundation
import Testing
@testable import Kit

struct SessionTabOverflowTests {
    private let ids = (0..<5).map { _ in UUID() }
    /// Five 220 pt tabs, 2 pt apart, in a 500 pt strip.
    private func overflow(offset: CGFloat) -> TabStripOverflow<UUID> {
        let frames = Dictionary(uniqueKeysWithValues: ids.enumerated().map { index, id in
            let minX = CGFloat(index) * 222
            return (id, minX...(minX + 220))
        })
        return TabStripOverflow(frames: frames, offset: offset, viewport: 500)
    }

    @Test func tabsPastTheTrailingEdgeAreHidden() {
        let strip = overflow(offset: 0)
        #expect(ids.map(strip.hides) == [false, false, true, true, true])
        #expect(!strip.hiddenBefore)
        #expect(strip.hiddenAfter)
    }

    @Test func scrolledTabsAreHiddenOnBothSides() {
        let strip = overflow(offset: 300)
        #expect(ids.map(strip.hides) == [true, true, false, true, true])
        #expect(strip.hiddenBefore)
        #expect(strip.hiddenAfter)
    }

    @Test func tabsThatFitAreNotHidden() {
        let strip = TabStripOverflow(frames: [ids[0]: 0...220], offset: 0, viewport: 500)
        #expect(!strip.hides(ids[0]))
        #expect(!strip.hiddenBefore && !strip.hiddenAfter)
    }
}
