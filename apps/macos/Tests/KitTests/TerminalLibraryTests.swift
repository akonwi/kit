import Testing
@testable import Kit

@Suite struct TerminalLibraryTests {
    /// Fails when Vendor/Ghostty is stale: update with the pin in build_ghosttykit.sh.
    @Test func linksTheOptimizedPinnedLibghostty() {
        #expect(TerminalLibrary.isOptimized)
        #expect(TerminalLibrary.version == "1.3.2-HEAD+83edd49")
    }
}
