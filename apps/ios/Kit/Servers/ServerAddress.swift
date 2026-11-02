import Foundation

/// How a server's saved URL is shown and talked about.
enum ServerAddress {
    /// An HTTPS server by its host (and port, if any); a plain HTTP server
    /// by its whole URL, so the missing encryption is visible.
    static func display(_ url: String) -> String {
        guard let parts = URLComponents(string: url), let host = parts.host, !isPlainHTTP(url) else { return url }
        return host + (parts.port.map { ":\($0)" } ?? "")
    }

    static func isPlainHTTP(_ url: String) -> Bool {
        url.lowercased().hasPrefix("http://")
    }

    /// Whether a server is reached through Tailscale: a MagicDNS name or an
    /// address in Tailscale's range.
    static func isTailnet(_ url: String) -> Bool {
        guard let host = URLComponents(string: url)?.host?.lowercased() else { return false }
        if host.hasSuffix(".ts.net") { return true }
        let octets = host.split(separator: ".").compactMap { Int($0) }
        return octets.count == 4 && octets[0] == 100 && (64...127).contains(octets[1])
    }

    /// What to check when a server doesn't answer.
    static func reachabilityHint(_ url: String) -> String {
        isTailnet(url)
            ? "Check that Tailscale is connected on this iPhone and that the server is running."
            : "Check that this iPhone is on the server’s network and that the server is running."
    }
}
