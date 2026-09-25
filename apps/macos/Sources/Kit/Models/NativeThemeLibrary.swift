import Darwin
import Foundation
import Observation
import SwiftUI

struct InstalledTheme: Identifiable, Equatable, Sendable {
    let name: String
    let definition: NativeThemeDefinition

    var id: String { "global:\(name)" }
}

/// Themes shared with terminal clients through `$KIT_HOME/themes`.
@MainActor @Observable
final class NativeThemeLibrary {
    static let maxFileBytes = 256 * 1024

    let directory: URL
    private(set) var themes: [InstalledTheme] = []
    private(set) var issues: [String] = []

    init(directory: URL = SharedSettingsStore.home.appendingPathComponent("themes", isDirectory: true)) {
        self.directory = directory
        reload()
    }

    func reload() {
        do {
            let result = try Self.discover(in: directory)
            themes = result.themes
            issues = result.issues
        } catch {
            themes = []
            issues = [error.localizedDescription]
        }
    }

    static func discover(in directory: URL) throws -> (themes: [InstalledTheme], issues: [String]) {
        let keys: [URLResourceKey] = [.isRegularFileKey, .isSymbolicLinkKey]
        guard FileManager.default.fileExists(atPath: directory.path) else { return ([], []) }
        let urls: [URL]
        do {
            urls = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: keys,
                                                                options: [.skipsHiddenFiles])
        } catch {
            let value = error as NSError
            if value.domain == NSCocoaErrorDomain && value.code == NSFileNoSuchFileError { return ([], []) }
            throw error
        }
        var themes: [InstalledTheme] = []
        var issues: [String] = []
        for url in urls {
            guard url.pathExtension == "json" else { continue }
            let name = url.deletingPathExtension().lastPathComponent
            guard validName(name), name != "system" else { continue }
            do {
                let values = try url.resourceValues(forKeys: Set(keys))
                guard values.isRegularFile == true, values.isSymbolicLink != true else { continue }
                let definition = try NativeThemeDefinition.parse(readThemeFile(url))
                themes.append(InstalledTheme(name: name, definition: definition))
            } catch {
                issues.append("\(url.lastPathComponent): \(error.localizedDescription)")
            }
        }
        themes.sort { $0.name < $1.name }
        issues.sort()
        return (themes, issues)
    }

    static func validName(_ name: String) -> Bool {
        guard !name.isEmpty, name != ".", name != "..", !name.contains("/"), !name.contains("\0") else { return false }
        return !name.unicodeScalars.contains { scalar in
            CharacterSet.controlCharacters.contains(scalar) || scalar.value == 0x061c ||
                scalar.value == 0x200e || scalar.value == 0x200f ||
                (0x202a...0x202e).contains(scalar.value) || (0x2066...0x2069).contains(scalar.value)
        }
    }

    private static func readThemeFile(_ url: URL) throws -> Data {
        let descriptor = Darwin.open(url.path, O_RDONLY | O_NONBLOCK | O_NOFOLLOW)
        guard descriptor >= 0 else {
            throw LibraryError.invalid("Couldn’t open \(url.lastPathComponent): \(String(cString: strerror(errno))).")
        }
        let file = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? file.close() }
        var info = stat()
        guard fstat(descriptor, &info) == 0, info.st_mode & S_IFMT == S_IFREG else {
            throw LibraryError.invalid("\(url.lastPathComponent) must be a regular file.")
        }
        let data = try file.read(upToCount: maxFileBytes + 1) ?? Data()
        guard data.count <= maxFileBytes else {
            throw LibraryError.invalid("\(url.lastPathComponent) exceeds 256 KB.")
        }
        return data
    }

    struct LibraryError: LocalizedError {
        let message: String
        static func invalid(_ message: String) -> Self { Self(message: message) }
        var errorDescription: String? { message }
    }
}

private struct InstalledThemesKey: EnvironmentKey {
    static let defaultValue: [InstalledTheme] = []
}

extension EnvironmentValues {
    var installedThemes: [InstalledTheme] {
        get { self[InstalledThemesKey.self] }
        set { self[InstalledThemesKey.self] = newValue }
    }
}
