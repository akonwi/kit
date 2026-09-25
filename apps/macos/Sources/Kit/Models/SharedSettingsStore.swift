import Foundation
import CoreFoundation
import Darwin

/// Machine-wide defaults shared with the CLI. Session configuration remains server-owned.
actor SharedSettingsStore {
    static let shared = SharedSettingsStore(url: home.appendingPathComponent("settings.json"))
    static var home: URL {
        if let value = ProcessInfo.processInfo.environment["KIT_HOME"], !value.isEmpty {
            return URL(fileURLWithPath: value, isDirectory: true).standardizedFileURL
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".kit")
    }
    struct Snapshot: Equatable, Sendable {
        var defaultModel = ""
        var overrides: [String: Int] = [:]
    }
    enum Change: Sendable {
        case defaultModel(String)
        case contextWindow(String, Int?)
    }
    struct SettingsError: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }
    let url: URL
    init(url: URL) { self.url = url }

    func load() throws -> Snapshot { try project(read()) }

    /// Re-read before every mutation, preserving settings and override fields we don't own.
    /// Like the CLI store, independent processes remain last-writer-wins.
    func update(_ change: Change) throws -> Snapshot {
        var fields = try read()
        switch change {
        case .defaultModel(let selector):
            guard selector.isEmpty || Self.validSelector(selector) else {
                throw SettingsError(message: "Choose a model with an exact provider/model identifier.")
            }
            fields["defaultModel"] = selector.isEmpty ? nil : selector
        case .contextWindow(let selector, let tokens):
            guard Self.validSelector(selector), tokens.map({ $0 > 0 }) ?? true else {
                throw SettingsError(message: "Enter a positive whole number of tokens.")
            }
            guard fields["modelOverrides"] == nil || fields["modelOverrides"] is [String: Any] else {
                throw SettingsError(message: "modelOverrides must be a JSON object. Fix settings.json before saving.")
            }
            var overrides = fields["modelOverrides"] as? [String: Any] ?? [:]
            var model = overrides[selector] as? [String: Any] ?? [:]
            model["contextWindow"] = tokens
            overrides[selector] = model.isEmpty ? nil : model
            fields["modelOverrides"] = overrides.isEmpty ? nil : overrides
        }
        let snapshot = try project(fields)
        var data = try JSONSerialization.data(withJSONObject: fields, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        data.append(0x0a)
        guard data.count <= 1_048_576 else { throw SettingsError(message: "settings.json exceeds the 1 MB limit.") }
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: url, options: .atomic)
        return snapshot
    }

    private func read() throws -> [String: Any] {
        let fd = Darwin.open(url.path, O_RDONLY | O_NONBLOCK | O_NOFOLLOW)
        if fd < 0 {
            if errno == ENOENT { return [:] }
            throw SettingsError(message: "Couldn’t open settings.json: \(String(cString: strerror(errno)))")
        }
        let file = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        defer { try? file.close() }
        var info = stat()
        guard fstat(fd, &info) == 0, info.st_mode & S_IFMT == S_IFREG else {
            throw SettingsError(message: "settings.json must be a regular file.")
        }
        let data = try file.read(upToCount: 1_048_577) ?? Data()
        guard data.count <= 1_048_576 else { throw SettingsError(message: "settings.json exceeds the 1 MB limit.") }
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw SettingsError(message: "settings.json must contain a JSON object.")
        }
        return object
    }

    private func project(_ fields: [String: Any]) throws -> Snapshot {
        var result = Snapshot()
        if let value = fields["defaultModel"] {
            guard let selector = value as? String, Self.validSelector(selector) else {
                throw SettingsError(message: "defaultModel must be an exact provider/model identifier. Fix settings.json and reload.")
            }
            result.defaultModel = selector
        }
        if let value = fields["modelOverrides"] {
            guard let overrides = value as? [String: Any] else {
                throw SettingsError(message: "modelOverrides must be a JSON object. Fix settings.json and reload.")
            }
            for (selector, value) in overrides {
                guard Self.validSelector(selector), let model = value as? [String: Any] else {
                    throw SettingsError(message: "Invalid model override for \(selector). Fix settings.json and reload.")
                }
                guard let raw = model["contextWindow"] else { continue }
                guard let number = raw as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(),
                      let count = Int(number.stringValue), count > 0 else {
                    throw SettingsError(message: "Context window for \(selector) must be a positive integer.")
                }
                result.overrides[selector] = count
            }
        }
        return result
    }

    static func validSelector(_ value: String) -> Bool {
        let parts = value.split(separator: "/", maxSplits: 1, omittingEmptySubsequences: false)
        return parts.count == 2 && parts.allSatisfy { !$0.isEmpty && $0.trimmingCharacters(in: .whitespacesAndNewlines) == $0 }
    }

    static func preferredModel(_ settings: Snapshot, available: [String], suggested: String?) -> String {
        [settings.defaultModel, suggested ?? ""].first { available.contains($0) } ?? available.first ?? ""
    }
}
