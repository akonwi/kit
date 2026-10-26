import Foundation

/// Web addresses a session asked this app to open: with the model's open_url
/// tool, or with a plugin's url.open.requested event.
///
/// The server records the validated address in the call's result details, or
/// sends it on the event stream, and does not wait for any client. The app
/// opens an address only for a call it saw complete live or a request it
/// received live, and at most once, so loading a transcript, replaying events
/// from before it attached, or catching up after a reconnect never opens a
/// page.
enum OpenURLRequest {
    static let toolName = "open_url"
    static let presentation = "open-url"
    static let maxURLBytes = 2 << 10
    /// Remembered calls beyond which those no longer shown are forgotten.
    static let handledLimit = 256

    /// The addresses of live open_url calls in `messages` and of live page
    /// `requests` not yet in `handled`, oldest first within each. Every
    /// address returned is added to `handled`.
    static func pending(in messages: [TranscriptMessage], requests: [RequestedPage] = [], handled: inout Set<String>) -> [URL] {
        var urls: [URL] = []
        var visible: Set<String> = []
        for row in messages where row.role == "tools" {
            for tool in row.tools where tool.name == toolName && tool.completedLive == true {
                visible.insert(tool.id)
                guard handled.insert(tool.id).inserted, !tool.failed, let url = url(details: tool.details) else { continue }
                urls.append(url)
            }
        }
        for request in requests {
            visible.insert(request.id)
            if handled.insert(request.id).inserted { urls.append(request.url) }
        }
        // A call or request still shown stays remembered, so it can never
        // open twice.
        if handled.count > handledLimit { handled.formIntersection(visible) }
        return urls
    }

    /// The address in an open_url result's details, re-validated on this side
    /// of the boundary: an absolute http or https URL with a host and no
    /// credentials.
    static func url(details: String?) -> URL? {
        guard let details, let data = details.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              object["presentation"] as? String == presentation,
              let raw = object["url"] as? String else { return nil }
        return url(raw)
    }

    /// `raw` as a web address, validated on this side of the boundary: an
    /// absolute http or https URL with a host and no credentials.
    static func url(_ raw: String) -> URL? {
        guard !raw.isEmpty, raw.utf8.count <= maxURLBytes,
              !raw.contains(where: { $0.isWhitespace || $0.unicodeScalars.contains { CharacterSet.controlCharacters.contains($0) } }),
              let url = URL(string: raw), BrowserAddress.isWeb(url),
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              components.user == nil, components.password == nil else { return nil }
        return url
    }
}
