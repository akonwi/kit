import Foundation
import Network

/// What `kit server status --json` prints about a running Kit server, read
/// over SSH before the app knows the server's protocol (ADR 0045). Its fields
/// are stable across Kit versions: they may be added to but never renamed,
/// retyped, or removed, so this reader ignores fields it doesn't know.
struct SSHStatusReport: Decodable, Equatable, Sendable {
    /// The largest report a reader accepts.
    static let maxBytes = 16 << 10

    /// The absolute path of the server's Kit home on its host.
    let home: String
    let pid: Int
    /// The server's HTTP origin on its host: plain HTTP on a loopback address.
    let url: String
    let kitVersion: String
    let protocolVersion: Int
    /// Identifies this run of the server.
    let instanceId: String
    let databaseReady: Bool
    /// The server's local token, equivalent to access to its Kit home.
    let token: String

    /// Reads one report and validates it.
    static func decode(_ data: Data) throws -> SSHStatusReport {
        guard data.count <= maxBytes else { throw SSHReportError("the server status report is too large") }
        let report: SSHStatusReport
        do { report = try JSONDecoder().decode(SSHStatusReport.self, from: data) }
        catch { throw SSHReportError("the server status report couldn’t be read") }
        try report.validate()
        return report
    }

    func validate() throws {
        guard pid > 0 else { throw SSHReportError("the server status report has no pid") }
        guard protocolVersion > 0 else { throw SSHReportError("the server status report has no protocol version") }
        guard Self.printable(kitVersion, limit: 128), !kitVersion.isEmpty else {
            throw SSHReportError("the server status report has an invalid Kit version")
        }
        guard Self.printable(instanceId, limit: 128), !instanceId.isEmpty else {
            throw SSHReportError("the server status report has an invalid instance id")
        }
        guard Self.printable(token, limit: 512), !token.isEmpty else {
            throw SSHReportError("the server status report has an invalid token")
        }
        guard Self.printable(home, limit: 4096) else { throw SSHReportError("the server status report has an invalid home") }
        _ = try address()
    }

    /// The host and port of the report's URL, which must be a plain HTTP
    /// origin on a loopback address, as a server publishes it.
    func address() throws -> (host: String, port: Int) {
        let invalid = SSHReportError("the server status report has an invalid url")
        guard let parts = URLComponents(string: url), parts.scheme == "http", parts.user == nil, parts.password == nil,
              parts.path.isEmpty || parts.path == "/", parts.query == nil, parts.fragment == nil,
              let host = parts.host, let port = parts.port else { throw invalid }
        guard (1...65535).contains(port) else { throw SSHReportError("the server status report url has no port") }
        let bare = host.hasPrefix("[") && host.hasSuffix("]") ? String(host.dropFirst().dropLast()) : host
        guard Self.loopback(bare) else { throw SSHReportError("the server status report url is not on a loopback address") }
        return (bare, port)
    }

    /// Whether `host` is a literal loopback address, as Go's `IsLoopback` decides.
    private static func loopback(_ host: String) -> Bool {
        if let v4 = IPv4Address(host) { return v4.rawValue.first == 127 }
        if let v6 = IPv6Address(host) {
            if v6.isLoopback { return true }
            // An IPv4-mapped loopback address, such as ::ffff:127.0.0.1.
            let bytes = Array(v6.rawValue)
            return bytes[0..<10].allSatisfy { $0 == 0 } && bytes[10] == 0xff && bytes[11] == 0xff && bytes[12] == 127
        }
        return false
    }

    private static func printable(_ value: String, limit: Int) -> Bool {
        value.utf8.count <= limit && !value.unicodeScalars.contains { $0.properties.generalCategory == .control }
    }
}

/// Why a server status report was refused.
struct SSHReportError: LocalizedError, Equatable {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
