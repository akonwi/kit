import AppKit
import Testing
@testable import Kit

@MainActor struct IconTests {
    @Test func everyIconLoadsItsBundledPhosphorVectorAsATemplate() {
        for icon in Icon.allCases {
            let image = icon.image
            #expect(!image.representations.isEmpty, "\(icon.rawValue).svg is missing from Resources/Icons")
            #expect(image.isTemplate)
            #expect(image.size == NSSize(width: 16, height: 16))
        }
    }
}
