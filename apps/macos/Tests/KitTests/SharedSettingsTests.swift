import Foundation
import Testing
@testable import Kit

struct SharedSettingsTests {
    private func temporaryFile() throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-settings-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory.appendingPathComponent("settings.json")
    }

    @Test func missingFileUsesDefaultsAndRoundTripsChanges() async throws {
        let url = try temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let store = SharedSettingsStore(url: url)
        #expect(try await store.load() == .init())
        _ = try await store.update(.defaultModel("provider/model"))
        _ = try await store.update(.contextWindow("provider/model", 1_000_000))
        let restored = try await SharedSettingsStore(url: url).load()
        #expect(restored == .init(defaultModel: "provider/model", overrides: ["provider/model": 1_000_000]))
        _ = try await store.update(.defaultModel(""))
        #expect(try await store.update(.contextWindow("provider/model", nil)) == .init())
    }

    @Test func rereadsExternalEditsAndPreservesUnknownFields() async throws {
        let url = try temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let store = SharedSettingsStore(url: url)
        _ = try await store.load()
        try Data(#"{"theme":"flexoki","diffWrapLines":true,"future":{"nested":[1,2]},"modelOverrides":{"p/m":{"contextWindow":123,"future":true},"q/n":{"contextWindow":456}}}"#.utf8).write(to: url)
        _ = try await store.update(.contextWindow("p/m", 789))
        let object = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        #expect(object["theme"] as? String == "flexoki")
        #expect(object["diffWrapLines"] as? Bool == true)
        #expect((object["future"] as? [String: [Int]])?["nested"] == [1, 2])
        let overrides = try #require(object["modelOverrides"] as? [String: [String: Any]])
        #expect(overrides["p/m"]?["future"] as? Bool == true)
        #expect(overrides["p/m"]?["contextWindow"] as? Int == 789)
        #expect(overrides["q/n"]?["contextWindow"] as? Int == 456)
        _ = try await store.update(.contextWindow("p/m", nil))
        let removed = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        #expect((removed["modelOverrides"] as? [String: [String: Any]])?["p/m"]?["future"] as? Bool == true)
    }

    @Test func malformedFilesAreNotOverwritten() async throws {
        let url = try temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        for input in ["{", "[]", #"{"modelOverrides":false}"#, #"{"modelOverrides":{"p/m":{"contextWindow":true}}}"#, #"{"modelOverrides":{"p/m":{"contextWindow":1.5}}}"#] {
            let original = Data(input.utf8)
            try original.write(to: url)
            await #expect(throws: (any Error).self) { try await SharedSettingsStore(url: url).update(.defaultModel("p/m")) }
            #expect(try Data(contentsOf: url) == original)
        }
    }

    @Test func invalidEditsAndOversizedFilesAreRejected() async throws {
        let url = try temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let store = SharedSettingsStore(url: url)
        await #expect(throws: (any Error).self) { try await store.update(.defaultModel("model")) }
        await #expect(throws: (any Error).self) { try await store.update(.contextWindow("p/m", 0)) }
        try Data(repeating: 32, count: 1_048_577).write(to: url)
        await #expect(throws: (any Error).self) { try await store.load() }
    }

    @Test func newSessionsPreferSharedDefaultWithoutChangingFallback() {
        let defaults = SharedSettingsStore.Snapshot(defaultModel: "p/preferred")
        #expect(SharedSettingsStore.preferredModel(defaults, available: ["p/other", "p/preferred"], suggested: "p/other") == "p/preferred")
        #expect(SharedSettingsStore.preferredModel(defaults, available: ["p/other", "p/suggested"], suggested: "p/suggested") == "p/suggested")
        #expect(SharedSettingsStore.preferredModel(.init(), available: ["p/first"], suggested: nil) == "p/first")
    }
}
