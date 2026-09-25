import Testing
@testable import Kit

struct TranscriptScrollFollowTests {
    @Test func currentDrawerResumesFollowingAfterManualScroll() {
        var state = TranscriptScrollFollow()
        state.userScrolled(distanceFromBottom: 120)
        #expect(state.followsBottom == false)
        let shouldScroll = state.expandedDrawer(isCurrent: true)
        #expect(shouldScroll)
        #expect(state.followsBottom)
        state.userScrolled(distanceFromBottom: 45)
        #expect(state.followsBottom == false)
        state.userScrolled(distanceFromBottom: 2)
        #expect(state.followsBottom)
    }

    @Test func historicalDrawerDoesNotJumpToBottom() {
        var state = TranscriptScrollFollow()
        let shouldScroll = state.expandedDrawer(isCurrent: false)
        #expect(shouldScroll == false)
        #expect(state.followsBottom == false)
        state.resume()
        #expect(state.followsBottom)
    }
}
