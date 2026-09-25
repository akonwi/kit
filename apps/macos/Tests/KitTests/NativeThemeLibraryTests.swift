import Foundation
import Testing
@testable import Kit

@MainActor
struct NativeThemeLibraryTests {
    private func temporaryDirectory() throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-themes-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    @Test func discoversSharedThemesAndReportsInvalidFiles() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        try Data(##"{"tokens":{"bg":"#100f0f"}}"##.utf8).write(to: directory.appendingPathComponent("z-dark.json"))
        try Data(##"{"tokens":{"bg":"#fffaf0"}}"##.utf8).write(to: directory.appendingPathComponent("a-light.json"))
        try Data("{".utf8).write(to: directory.appendingPathComponent("broken.json"))
        try Data(##"{"tokens":{"bg":"#000000"}}"##.utf8).write(to: directory.appendingPathComponent("system.json"))
        try FileManager.default.createSymbolicLink(at: directory.appendingPathComponent("linked.json"),
                                                    withDestinationURL: directory.appendingPathComponent("a-light.json"))

        let result = try NativeThemeLibrary.discover(in: directory)
        #expect(result.themes.map(\.name) == ["a-light", "z-dark"])
        #expect(result.issues.count == 1)
        #expect(result.issues[0].contains("broken.json"))
    }

    @Test func sharedThemeConfigurationResolvesDiscoveredFile() throws {
        let directory = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        try Data(##"{"tokens":{"bg":"#100f0f","textPrimary":"#ceddc3"}}"##.utf8)
            .write(to: directory.appendingPathComponent("Flexoki.json"))
        let library = NativeThemeLibrary(directory: directory)
        let item = try #require(library.themes.first)

        var configuration = ThemeConfiguration()
        configuration.assign(item, fallbackDark: false)
        let theme = configuration.theme(dark: true, installed: library.themes)
        #expect(theme.surface == NativeThemeDefinition.hexColor("#100f0f"))
    }

    @Test func missingDirectoryIsAnEmptyLibrary() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("kit-themes-missing-\(UUID().uuidString)")
        let result = try NativeThemeLibrary.discover(in: root)
        #expect(result.themes.isEmpty)
        #expect(result.issues.isEmpty)
    }
}
