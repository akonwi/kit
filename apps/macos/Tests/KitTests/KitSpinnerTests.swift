import Testing
@testable import Kit

struct KitSpinnerTests {
    @Test func framesMatchTuiBrailleAndCadence() {
        let glyphs = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
        #expect(KitSpinner.frames == glyphs.unicodeScalars.map { UInt8($0.value - 0x2800) })
        #expect(KitSpinner.frame(at: 0.079) == 0x0b)
        #expect(KitSpinner.frame(at: 0.081) == 0x19)
        #expect(KitSpinner.frame(at: 0.801) == 0x0b)
    }
}
