import CoreServices
import Foundation

/// Where browser downloads are saved: the user's Downloads folder, with a
/// number added when a file of the same name is already there.
enum BrowserDownloads {
    static var folder: URL {
        FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
            ?? URL.homeDirectory.appending(path: "Downloads", directoryHint: .isDirectory)
    }

    /// A free destination in `folder` for `suggested`: "report.pdf", then
    /// "report 2.pdf", "report 3.pdf", and so on.
    static func destination(for suggested: String, in folder: URL = folder,
                            exists: (URL) -> Bool = { FileManager.default.fileExists(atPath: $0.path) }) -> URL {
        let name = sanitized(suggested)
        let base = (name as NSString).deletingPathExtension
        let ext = (name as NSString).pathExtension
        var candidate = folder.appending(path: name, directoryHint: .notDirectory)
        var number = 2
        while exists(candidate) {
            let numbered = base + " \(number)" + (ext.isEmpty ? "" : "." + ext)
            candidate = folder.appending(path: numbered, directoryHint: .notDirectory)
            number += 1
        }
        return candidate
    }

    /// The last path component of a server's suggestion, never empty or hidden.
    static func sanitized(_ suggested: String) -> String {
        let last = suggested.replacingOccurrences(of: ":", with: "-")
            .split(separator: "/").last.map(String.init) ?? ""
        let trimmed = last.trimmingCharacters(in: .whitespacesAndNewlines.union(.controlCharacters))
        let visible = String(trimmed.drop { $0 == "." })
        guard !visible.isEmpty else { return "download" }
        // File names are limited to 255 bytes; shorten the name, not its extension.
        guard visible.utf8.count > maximumNameBytes else { return visible }
        let ext = (visible as NSString).pathExtension
        let suffix = ext.isEmpty || ext.utf8.count > 16 ? "" : "." + ext
        var base = suffix.isEmpty ? visible : String(visible.dropLast(suffix.count))
        while base.utf8.count + suffix.utf8.count > maximumNameBytes { base.removeLast() }
        return base + suffix
    }

    /// Leaves room for the number added to a name already taken.
    static let maximumNameBytes = 240

    /// Marks a downloaded file as coming from the web, so Gatekeeper checks
    /// it when opened, unless the engine already did.
    static func quarantine(_ file: URL, source: URL?) {
        var url = file
        if let existing = try? url.resourceValues(forKeys: [.quarantinePropertiesKey]).quarantineProperties,
           !existing.isEmpty { return }
        var properties: [String: Any] = [
            kLSQuarantineAgentNameKey as String: "Kit",
            kLSQuarantineTypeKey as String: kLSQuarantineTypeWebDownload as String,
        ]
        if let source { properties[kLSQuarantineDataURLKey as String] = source }
        var values = URLResourceValues()
        values.quarantineProperties = properties
        try? url.setResourceValues(values)
    }
}
