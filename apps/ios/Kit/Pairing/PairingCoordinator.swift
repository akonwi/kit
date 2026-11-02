import Foundation
import KitClient
import Observation

/// Presents pairing: the full-screen scanner and the pair sheet. One is
/// shown only after the other has gone, so switching between them waits
/// for the dismissal.
@MainActor @Observable
final class PairingCoordinator {
    /// How a pairing link arrived.
    enum Origin: Equatable { case scan, paste }

    /// One showing of the pair sheet. Without a link it asks for one.
    struct Request: Identifiable, Equatable {
        let id = UUID()
        var link: PairingLink?
        let origin: Origin
        /// The saved server being paired again, if any.
        let target: String?
    }

    var scanning = false
    var sheet: Request?

    private var target: String?
    private var afterScanner: Request?
    private var scanAfterSheet = false

    /// Opens the scanner, for a new server or to pair `target` again.
    func scan(target: String? = nil) {
        self.target = target
        scanning = true
    }

    /// Opens the pair sheet asking for a link.
    func paste(target: String? = nil) {
        self.target = target
        sheet = Request(link: nil, origin: .paste, target: target)
    }

    func scanned(_ link: PairingLink) {
        afterScanner = Request(link: link, origin: .scan, target: target)
        scanning = false
    }

    func pasteInstead() {
        afterScanner = Request(link: nil, origin: .paste, target: target)
        scanning = false
    }

    func scannerDismissed() {
        sheet = afterScanner
        afterScanner = nil
    }

    /// Closes the sheet and scans again for the same server.
    func scanAgain() {
        scanAfterSheet = true
        sheet = nil
    }

    func sheetDismissed() {
        guard scanAfterSheet else { return }
        scanAfterSheet = false
        scanning = true
    }
}
