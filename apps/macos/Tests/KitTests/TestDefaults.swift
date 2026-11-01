import Foundation

/// Defaults that no other test shares.
///
/// A named suite leaves a property list in `~/Library/Preferences` even
/// after its domain is removed, so suites named per test pile up there run
/// after run. These defaults are property lists in a temporary directory
/// instead, given to the suite by absolute path, and the test process
/// removes the directory as it exits.
enum TestDefaults {
    private static let directory: URL = {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("KitTests-defaults-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        atexit { try? FileManager.default.removeItem(at: TestDefaults.directory) }
        return directory
    }()

    /// A new, empty set of defaults.
    static func make() -> UserDefaults {
        UserDefaults(suiteName: directory.appendingPathComponent(UUID().uuidString).path)!
    }
}
