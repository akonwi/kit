import Foundation

enum AppIdentity {
    /// The running bundle's identifier. Development bundles use their own,
    /// so they never share preferences or window restoration with an
    /// installed release.
    static let bundleIdentifier = Bundle.main.bundleIdentifier ?? "com.akonwi.kit"
    static let tabbingIdentifier = bundleIdentifier + ".sessions"
}
