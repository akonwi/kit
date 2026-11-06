import Foundation
import KitClient
import Testing
import UIKit
@testable import Kit

@MainActor
struct ScratchpadFormatTests {
    @Test func theSubtitleNamesWhoSharesThePad() {
        #expect(ScratchpadFormat.sharing(owner: "root", session: "root", title: "Fix SSE reconnect")
            == "Shared with forks of Fix SSE reconnect")
        #expect(ScratchpadFormat.sharing(owner: nil, session: "root", title: "") == "Shared with this session’s forks")
        #expect(ScratchpadFormat.sharing(owner: "root", session: "fork", title: "Fork of it")
            == "Shared with the session this was forked from")
    }

    @Test func aReviewShowsEachLineAsSameAddedOrRemovedWithoutItsHeader() {
        let shared = "## Done\n- [x] One\n## Questions"
        let draft = "## Done\n- [x] One\n- [x] Two"
        let lines = ScratchpadFormat.reviewLines(ScratchpadState.unified(shared: shared, draft: draft))
        #expect(lines == [
            .init(kind: .same, text: "## Done"),
            .init(kind: .same, text: "- [x] One"),
            .init(kind: .removed, text: "## Questions"),
            .init(kind: .added, text: "- [x] Two"),
        ])
        #expect(lines.map(\.marker) == [" ", " ", "-", "+"])
        #expect(lines[2].accessibilityLabel == "Only in the shared copy: ## Questions")
        #expect(lines[3].accessibilityLabel == "Only in your draft: - [x] Two")
    }

    @Test func headingsAreBoldAndTheRestIsRegular() {
        #expect(ScratchpadEditor.isHeading("# Plan"))
        #expect(ScratchpadEditor.isHeading("###### Deep"))
        #expect(ScratchpadEditor.isHeading("##"))
        #expect(!ScratchpadEditor.isHeading("####### Too deep"))
        #expect(!ScratchpadEditor.isHeading("#tag"))
        #expect(!ScratchpadEditor.isHeading("- # not a heading"))

        let text = ScratchpadEditor.styled("# Plan\n\n- [ ] One\n## Next")
        func bold(_ index: Int) -> Bool {
            let font = text.attribute(.font, at: index, effectiveRange: nil) as? UIFont
            return font?.fontDescriptor.symbolicTraits.contains(.traitBold) == true
        }
        let string = text.string as NSString
        #expect(bold(string.range(of: "Plan").location))
        #expect(!bold(string.range(of: "One").location))
        #expect(bold(string.range(of: "Next").location))
    }
}
