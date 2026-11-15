import AppKit
import Foundation
import GhosttyKit
import Testing
@testable import Kit

/// libghostty itself, without a window: a runtime following its own theme,
/// and terminals running a script that asks for the color scheme.
@MainActor @Suite(.serialized)
struct TerminalRuntimeTests {
    @Test func takesANewConfigurationWhenTheThemeChanges() async throws {
        let harness = TerminalThemeHarness(appearance: "light")
        let runtime = TerminalRuntime(theme: harness.theme)
        try #require(runtime.start())
        defer { runtime.stop() }
        #expect(runtime.appearance == harness.kit(dark: false))
        #expect(background(runtime) == harness.kit(dark: false).background)

        var configuration = ThemeConfiguration()
        configuration.light = ThemeConfiguration.kitDark
        harness.defaults.set(configuration.json, forKey: "themeConfiguration")
        try await eventually { runtime.appearance == harness.kit(dark: true) }
        #expect(background(runtime) == harness.kit(dark: true).background)

        harness.typography.monoFamily = "Menlo"
        try await eventually { runtime.appearance?.fontFamily == "Menlo" }
    }

    @Test func aSwitchWithNoTerminalOnScreenReachesATerminalShownLater() async throws {
        let harness = TerminalThemeHarness(appearance: "system")
        let runtime = TerminalRuntime(theme: harness.theme)
        try #require(runtime.start())
        defer { runtime.stop() }
        #expect(runtime.appearance == harness.kit(dark: false))

        harness.system.update(.dark)
        try await eventually { runtime.appearance == harness.kit(dark: true) }
        #expect(background(runtime) == harness.kit(dark: true).background)

        let probe = try SchemeProbe()
        let view = try #require(TerminalSurfaceView(directory: probe.directory.path, command: probe.command,
                                                    runtime: runtime))
        defer { view.close() }
        try await eventually { probe.answers.count == 1 }
        #expect(probe.answers == [SchemeProbe.dark])
    }

    @Test func programsInARunningTerminalAreAskedAndToldOfTheSwitch() async throws {
        let harness = TerminalThemeHarness(appearance: "system")
        let runtime = TerminalRuntime(theme: harness.theme)
        try #require(runtime.start())
        defer { runtime.stop() }
        let asking = try SchemeProbe(), listening = try SchemeProbe(listens: true)
        let views = try [asking, listening].map { probe in
            try #require(TerminalSurfaceView(directory: probe.directory.path, command: probe.command, runtime: runtime))
        }
        defer { views.forEach { $0.close() } }
        try await eventually { asking.answers.count == 1 && listening.answers.count == 1 }

        harness.defaults.set("dark", forKey: "appearance")
        try await eventually { runtime.appearance == harness.kit(dark: true) }
        asking.askAgain()
        try await eventually { asking.answers.count == 2 }
        #expect(asking.answers == [SchemeProbe.light, SchemeProbe.dark])
        // Told unasked (mode 2031), never in the old scheme.
        try await eventually { listening.answers.count >= 2 }
        #expect(listening.answers.first == SchemeProbe.light)
        #expect(listening.answers.dropFirst().allSatisfy { $0 == SchemeProbe.dark })
    }

    @Test func schemeThemesAreRewrittenPrivatelyForEachConfiguration() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("KitTests-schemes-\(UUID().uuidString)", isDirectory: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let appearance = TerminalThemeHarness().kit(dark: false)
        for _ in 0..<2 {
            let config = try #require(TerminalRuntime.makeConfig(appearance, schemes: directory))
            ghostty_config_free(config)
            #expect(try permissions(directory) == 0o700)
            #expect(try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted() == ["dark", "light"])
            for name in ["light", "dark"] {
                let file = directory.appendingPathComponent(name)
                #expect(try permissions(file) == 0o600)
                #expect(try Data(contentsOf: file).isEmpty)
            }
            // Cleaned away, as the system may do: the next configuration
            // writes them again.
            try FileManager.default.removeItem(at: directory)
        }
    }

    @Test func aTerminalStartsWithoutTheSchemeThemes() throws {
        // A file where the directory should be, so it can't be made.
        let blocker = FileManager.default.temporaryDirectory
            .appendingPathComponent("KitTests-schemes-\(UUID().uuidString)")
        try Data().write(to: blocker)
        defer { try? FileManager.default.removeItem(at: blocker) }
        let config = TerminalRuntime.makeConfig(TerminalThemeHarness().kit(dark: false), schemes: blocker)
        #expect(config != nil)
        if let config { ghostty_config_free(config) }
    }

    private func permissions(_ url: URL) throws -> Int {
        try #require(FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] as? Int)
    }

    /// The runtime's configured background, as `#RRGGBB`.
    private func background(_ runtime: TerminalRuntime) -> String? {
        runtime.configValue("background", as: ghostty_config_color_s.self)
            .map { String(format: "#%02X%02X%02X", $0.r, $0.g, $0.b) }
    }
}

/// A script that asks its terminal for the color scheme (CSI ? 996 n) and
/// records the answer in its working directory. Then it asks again each
/// time the test says so or, listening, asks to be told of changes (mode
/// 2031) and records each report.
private final class SchemeProbe {
    static let dark = "?997;1", light = "?997;2"
    let directory: URL
    private var asked = 0

    init(listens: Bool = false) throws {
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("KitTests-terminal-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let script = """
        stty -icanon -echo
        listen() {
          IFS= read -r -n 9 reply
          reply=${reply#*[}
          printf '%s\\n' "${reply%n}" >> answers
        }
        ask() { printf '\\033[?996n'; listen; }
        ask
        \(listens ? "printf '\\033[?2031h'; while :; do listen; done" : "")
        n=1
        while :; do
          if [ -e "go$n" ]; then ask; n=$((n + 1)); fi
          sleep 0.05
        done
        """
        try script.write(to: directory.appendingPathComponent("probe.sh"), atomically: true, encoding: .utf8)
    }

    deinit { try? FileManager.default.removeItem(at: directory) }

    var command: String { "/bin/bash " + directory.appendingPathComponent("probe.sh").path }

    var answers: [String] {
        let text = (try? String(contentsOf: directory.appendingPathComponent("answers"), encoding: .utf8)) ?? ""
        return text.split(separator: "\n").map(String.init)
    }

    func askAgain() {
        asked += 1
        FileManager.default.createFile(atPath: directory.appendingPathComponent("go\(asked)").path, contents: nil)
    }
}
