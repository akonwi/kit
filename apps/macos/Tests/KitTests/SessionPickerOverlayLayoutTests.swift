import Testing
@testable import Kit

struct SessionPickerOverlayLayoutTests {
    @Test func commandPaletteMovesLowerAsWindowGrows() {
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 640, pickerHeight: 460, palette: true) == 148)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 800, pickerHeight: 460, palette: true) == 240)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 1000, pickerHeight: 460, palette: true) == 300)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 1200, pickerHeight: 460, palette: true) == 360)
    }

    @Test func tallerPaletteScreenKeepsBottomMarginAtMinimumWindowHeight() {
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 640, pickerHeight: 520, palette: true) == 88)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 640, pickerHeight: 510, palette: true) == 98)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 400, pickerHeight: 520, palette: true) == 0)
    }

    @Test func filePickerRetainsItsOriginalPlacement() {
        #expect(abs(SessionPickerOverlayLayout.topOffset(availableHeight: 640, pickerHeight: 360, palette: false) - 89.6) < 0.001)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 800, pickerHeight: 360, palette: false) == 100)
    }
}
