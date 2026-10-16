import Foundation

/// Resolves the Kit home this process uses, so every consumer agrees on one
/// precedence order.
///
/// `KIT_HOME` wins when set. A launched GUI app does not inherit its launching
/// shell's environment, so a development bundle carries its isolated home in
/// `Info.plist` instead (`build_and_run.sh` writes `KitDevHome`); the app
/// prefers it over the shared `~/.kit`. A release bundle carries no key and
/// keeps resolving `~/.kit`.
enum DevHomeResolver {
    /// Info.plist key carrying a development build's isolated Kit home.
    static let infoKey = "KitDevHome"

    /// Resolves the home from process state.
    static func resolve() -> URL {
        resolve(environment: ProcessInfo.processInfo.environment, bundledHome: Bundle.main.object(forInfoDictionaryKey: infoKey) as? String)
    }

    /// Resolves the home from explicit inputs, so the precedence is testable.
    static func resolve(environment: [String: String], bundledHome: String?) -> URL {
        if let value = environment["KIT_HOME"], !value.isEmpty {
            return URL(fileURLWithPath: value, isDirectory: true).standardizedFileURL
        }
        if let value = bundledHome, !value.isEmpty {
            return URL(fileURLWithPath: value, isDirectory: true).standardizedFileURL
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".kit")
    }
}
