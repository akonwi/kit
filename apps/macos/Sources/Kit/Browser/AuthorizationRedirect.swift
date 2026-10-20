import Foundation

/// The loopback address an authorization page sends the browser back to when
/// the user approves access. A browser tab watching for it receives the
/// redirect instead of loading it, so the user never pastes it.
struct AuthorizationRedirect: Equatable {
    /// The redirect address, without a query.
    let callback: URL
    /// The authorization's state, which its redirect carries back.
    let state: String

    /// The redirect of the authorization page at `authorization`, which must
    /// carry a state, back to the loopback HTTP address `redirectURI`.
    init?(authorization: URL, redirectURI: String?) {
        guard let redirectURI, var callback = URLComponents(string: redirectURI),
              callback.scheme?.lowercased() == "http", let host = callback.host, Self.isLoopback(host),
              let state = URLComponents(url: authorization, resolvingAgainstBaseURL: false)?
                  .queryItems?.first(where: { $0.name == "state" })?.value, !state.isEmpty else { return nil }
        callback.query = nil
        callback.fragment = nil
        guard let url = callback.url else { return nil }
        self.callback = url
        self.state = state
    }

    /// Whether `url` is the redirect for this authorization: its address and
    /// state, so another page can't answer it.
    func matches(_ url: URL) -> Bool {
        guard let candidate = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let expected = URLComponents(url: callback, resolvingAgainstBaseURL: false) else { return false }
        let path = { (components: URLComponents) in components.path.isEmpty ? "/" : components.path }
        return candidate.scheme?.lowercased() == expected.scheme?.lowercased()
            && candidate.host?.lowercased() == expected.host?.lowercased()
            && candidate.port == expected.port
            && path(candidate) == path(expected)
            && candidate.queryItems?.first(where: { $0.name == "state" })?.value == state
    }

    private static func isLoopback(_ host: String) -> Bool {
        let host = host.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if host == "localhost" || host == "::1" { return true }
        let octets = host.split(separator: ".", omittingEmptySubsequences: false).map { UInt8($0) }
        return octets.count == 4 && !octets.contains(nil) && octets[0] == 127
    }
}
