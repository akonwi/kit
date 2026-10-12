import Testing
@testable import Kit

struct SessionPickerOverlayLayoutTests {
    @Test func commandPaletteSitsAQuarterOfTheWindowBelowItsTop() {
        // Paper's 900 pt window, with the overlay under a 44 pt session bar: 225 pt.
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 850, pickerHeight: 468, palette: true,
                                                     originY: 44, windowHeight: 900) == 181)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 1374, pickerHeight: 468, palette: true,
                                                     originY: 44, windowHeight: 1424) == 312)
    }

    @Test func shortWindowsKeepTheBottomMargin() {
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 550, pickerHeight: 468, palette: true,
                                                     originY: 44, windowHeight: 600) == 50)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 400, pickerHeight: 520, palette: true,
                                                     originY: 44, windowHeight: 450) == 0)
    }

    @Test func commandPaletteCentresOnTheWindowInsideTheOverlay() {
        // Paper: a 640 pt palette in a 1440 pt window, beside a 248 pt sidebar.
        #expect(SessionPickerOverlayLayout.leadingOffset(availableWidth: 1186, pickerWidth: 640, originX: 248, windowWidth: 1440) == 152)
        #expect(SessionPickerOverlayLayout.leadingOffset(availableWidth: 1428, pickerWidth: 640, originX: 6, windowWidth: 1440) == 394)
        #expect(SessionPickerOverlayLayout.leadingOffset(availableWidth: 646, pickerWidth: 640, originX: 248, windowWidth: 900) == 0)
        #expect(SessionPickerOverlayLayout.leadingOffset(availableWidth: 700, pickerWidth: 640, originX: 0, windowWidth: 2000) == 60)
    }

    @Test func filePickerRetainsItsOriginalPlacement() {
        #expect(abs(SessionPickerOverlayLayout.topOffset(availableHeight: 640, pickerHeight: 360, palette: false) - 89.6) < 0.001)
        #expect(SessionPickerOverlayLayout.topOffset(availableHeight: 800, pickerHeight: 360, palette: false) == 100)
    }
}
