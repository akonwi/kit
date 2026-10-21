import CryptoKit
import Foundation
import IOKit

/// This Mac's Kit-scoped machine identifiers, derived exactly as a Kit server
/// on this Mac derives the `machineId` its health reports (ADR 0045). A
/// server runs on this Mac only when its identifier is one of these; its
/// address never says, because SSH forwarding makes a remote server answer
/// on loopback.
enum LocalMachine {
    /// Separates Kit machine identifiers from other uses of the platform one.
    private static let domain = "kit-machine-id-v1\u{0}"

    /// The identifiers a server sharing the Kit home `home` may report: the
    /// hashed hardware UUID, and the hash of the home's own random machine
    /// value, which a server uses when it couldn't read the hardware UUID.
    /// That file is shared only by processes using the home, so it still
    /// means this Mac. It is read, never created; empty when neither exists.
    static func identifiers(home: URL, platform: () -> String? = platformUUID) -> Set<String> {
        var identifiers: Set<String> = []
        if let raw = platform(), !raw.isEmpty { identifiers.insert(hash(raw)) }
        let file = home.appendingPathComponent("machine-id")
        if let body = try? String(contentsOf: file, encoding: .utf8) {
            let value = body.trimmingCharacters(in: .whitespacesAndNewlines)
            if ServerIdentity.validMachine(value) { identifiers.insert(hash("home:" + value)) }
        }
        return identifiers
    }

    /// The Kit-scoped hash of a raw platform identifier.
    static func hash(_ raw: String) -> String {
        SHA256.hash(data: Data((domain + raw).utf8)).map { String(format: "%02x", $0) }.joined()
    }

    /// The hardware UUID, the `IOPlatformUUID` the server reads through ioreg.
    static func platformUUID() -> String? {
        let service = IOServiceGetMatchingService(kIOMainPortDefault, IOServiceMatching("IOPlatformExpertDevice"))
        guard service != 0 else { return nil }
        defer { IOObjectRelease(service) }
        let property = IORegistryEntryCreateCFProperty(service, "IOPlatformUUID" as CFString, kCFAllocatorDefault, 0)
        return property?.takeRetainedValue() as? String
    }
}
