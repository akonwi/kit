import Foundation

/// What a browser tab's address field accepts: web addresses only, with no
/// search. A host without a scheme gets one: http for local development
/// hosts, https for everything else.
enum BrowserAddress {
    /// The address `input` names, or nil when it isn't a web address.
    static func url(from input: String) -> URL? {
        let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !text.contains(where: \.isWhitespace) else { return nil }
        if let scheme = explicitScheme(text) {
            guard ["http", "https"].contains(scheme), let url = URL(string: text),
                  let host = url.host(), !host.isEmpty else { return nil }
            return url
        }
        guard let host = host(of: text), isHostLike(host) else { return nil }
        let scheme = isLocal(host) ? "http" : "https"
        guard let url = URL(string: scheme + "://" + text), url.host() != nil else { return nil }
        return url
    }

    /// Whether a link can open in a browser tab.
    static func isWeb(_ url: URL) -> Bool {
        ["http", "https"].contains(url.scheme?.lowercased() ?? "") && !(url.host() ?? "").isEmpty
    }

    /// The address without a user name or password, as it is saved and listed.
    static func withoutCredentials(_ url: URL) -> URL {
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              components.user != nil || components.password != nil else { return url }
        components.user = nil
        components.password = nil
        return components.url ?? url
    }

    /// The address as the field shows it at rest: the host, with its port,
    /// then the rest. The scheme and a bare "/" path are left out.
    static func display(_ url: URL) -> (host: String, rest: String) {
        guard isWeb(url), let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            return (url.absoluteString, "")
        }
        var host = components.percentEncodedHost ?? ""
        if let port = components.port { host += ":\(port)" }
        var rest = components.percentEncodedPath == "/" ? "" : components.percentEncodedPath
        if let query = components.percentEncodedQuery { rest += "?" + query }
        if let fragment = components.percentEncodedFragment { rest += "#" + fragment }
        return (host, rest)
    }

    /// The host and port, as a tab or message names a page.
    static func hostLabel(_ url: URL) -> String {
        let display = display(url)
        return display.host.isEmpty ? url.absoluteString : display.host
    }

    /// A scheme typed before "://". "localhost:3000" has none.
    private static func explicitScheme(_ text: String) -> String? {
        guard let range = text.range(of: "://") else { return nil }
        let scheme = text[..<range.lowerBound]
        guard let first = scheme.first, first.isLetter,
              scheme.allSatisfy({ $0.isLetter || $0.isNumber || "+.-".contains($0) }) else { return nil }
        return scheme.lowercased()
    }

    /// The host part of scheme-less input: before any path, query, or
    /// fragment, without credentials or a port.
    private static func host(of text: String) -> String? {
        let authority = text.prefix { !"/?#".contains($0) }
        guard !authority.contains("@") else { return nil }
        if authority.hasPrefix("[") {
            guard let close = authority.firstIndex(of: "]") else { return nil }
            let after = authority[authority.index(after: close)...]
            guard after.isEmpty || isPort(after) else { return nil }
            return String(authority[...close]).lowercased()
        }
        let parts = authority.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count <= 2, let host = parts.first, !host.isEmpty else { return nil }
        if parts.count == 2 { guard isPort(":" + parts[1]) else { return nil } }
        return host.lowercased()
    }

    private static func isPort(_ text: Substring) -> Bool {
        guard text.first == ":" else { return false }
        let digits = text.dropFirst()
        return !digits.isEmpty && digits.count <= 5 && digits.allSatisfy(\.isASCII) && digits.allSatisfy(\.isNumber)
            && Int(digits).map { $0 <= 65_535 } == true
    }

    /// localhost, an IP address, or a dotted name of valid labels, which may
    /// be international, whose last label isn't a number.
    private static func isHostLike(_ host: String) -> Bool {
        if host == "localhost" || isIPv4(host) || (host.hasPrefix("[") && host.hasSuffix("]")) { return true }
        let labels = host.split(separator: ".", omittingEmptySubsequences: false)
        guard labels.count >= 2, labels.allSatisfy({ label in
            !label.isEmpty && label.count <= 63 && label.first != "-" && label.last != "-"
                && label.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" }
        }) else { return false }
        return labels.last.map { !$0.allSatisfy(\.isNumber) } ?? false
    }

    private static func isIPv4(_ host: String) -> Bool {
        let parts = host.split(separator: ".", omittingEmptySubsequences: false)
        return parts.count == 4 && parts.allSatisfy { part in
            !part.isEmpty && part.count <= 3 && part.allSatisfy { $0.isASCII && $0.isNumber } && Int(part)! <= 255
        }
    }

    /// Development hosts that are usually served without TLS.
    private static func isLocal(_ host: String) -> Bool {
        host == "localhost" || host.hasSuffix(".localhost") || host.hasSuffix(".local")
            || host.hasPrefix("127.") && isIPv4(host) || host == "0.0.0.0" || host == "[::1]"
    }
}
