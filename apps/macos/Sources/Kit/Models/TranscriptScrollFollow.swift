import Foundation

/// Content growth preserves following; only user scrolling changes the pin.
struct TranscriptScrollFollow {
    private(set) var followsBottom = true

    mutating func userScrolled(distanceFromBottom: CGFloat) {
        followsBottom = distanceFromBottom <= 8
    }

    mutating func expandedDrawer(isCurrent: Bool) -> Bool {
        followsBottom = isCurrent
        return isCurrent
    }

    mutating func resume() { followsBottom = true }
}
