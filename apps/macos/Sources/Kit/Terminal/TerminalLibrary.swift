import Foundation
import GhosttyKit

/// The libghostty build that Kit's terminal panes run on. Only files under
/// `Terminal/` import GhosttyKit.
enum TerminalLibrary {
    /// The libghostty version Kit was built with, such as `1.3.2-dev+83edd491`.
    static var version: String {
        let info = ghostty_info()
        guard let pointer = info.version else { return "" }
        let bytes = UnsafeRawBufferPointer(start: pointer, count: Int(info.version_len))
        return String(decoding: bytes, as: UTF8.self)
    }

    /// Whether libghostty was built with optimizations and without safety checks.
    static var isOptimized: Bool {
        ghostty_info().build_mode == GHOSTTY_BUILD_MODE_RELEASE_FAST
    }
}
