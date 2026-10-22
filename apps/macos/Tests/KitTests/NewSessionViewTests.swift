import AppKit
import SwiftUI
import Testing
@testable import Kit

/// A server that records creation requests and answers with the session.
private final class CreationServer: SessionCreationClient, @unchecked Sendable {
    let serverID = "test"
    let isDemo = false
    var catalog: [WireModelCapability]
    var created: [WireCreateSessionInput] = []

    init(models: [WireModelCapability]) { catalog = models }

    func models() async throws -> [WireModelCapability] { catalog }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        created.append(input)
        return SessionExcerpt(id: input.id ?? "", title: "Untitled session", sourceTitle: "Kit server", model: input.model,
                              thinking: input.thinkingLevel ?? "off", workspace: "kit", cwd: input.cwd, date: "2026-09-15T12:00:00Z", messages: [])
    }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
}

@MainActor
struct NewSessionViewTests {
    static func model(_ id: String, _ name: String, levels: [WireThinkingLevel]?) -> WireModelCapability {
        WireModelCapability(id: id, name: name, provider: "anthropic", api: "anthropic", contextWindow: 200_000,
                            maxInputTokens: nil, maxOutputTokens: nil, thinkingLevels: levels, inputs: nil, available: true)
    }

    private func host(_ server: CreationServer, close: @escaping () -> Void, open: @escaping (SessionExcerpt) -> Void) -> NSWindow {
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let view = NewSessionOverlay(client: server, suggestedModel: "anthropic/sonnet", close: close,
                                     loadDefaults: { ServerSettings() }, open: open)
            .environment(\.mica, theme).environment(\.colorScheme, .dark)
            .frame(width: 900, height: 700)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: view)
        window.orderFront(nil)
        return window
    }

    private func press(_ key: UInt16, _ text: String, in window: NSWindow) async throws {
        let event = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: window.windowNumber, context: nil, characters: text, charactersIgnoringModifiers: text,
            isARepeat: false, keyCode: key))
        window.sendEvent(event)
        try await Task.sleep(for: .milliseconds(60))
    }

    @Test func returnCreatesInTheHomeFolderWithTheSuggestedModel() async throws {
        let server = CreationServer(models: [Self.model("anthropic/opus", "Opus 4.6", levels: [.value2, .value4]),
                                             Self.model("anthropic/sonnet", "Sonnet 4.6", levels: [.value2, .value3, .value4])])
        var closed = 0
        var opened: [String] = []
        let window = host(server, close: { closed += 1 }, open: { opened.append($0.model) })
        defer { window.close() }
        // Return does nothing until the form is shown and the models load, so
        // keep pressing it. It is the default action, so it needs no focus.
        try await eventually {
            if server.created.isEmpty { try? await press(36, "\r", in: window) }
            return !server.created.isEmpty
        }
        try await eventually { !opened.isEmpty }
        #expect(server.created.count == 1)
        let input = try #require(server.created.first)
        #expect(input.cwd == FileManager.default.homeDirectoryForCurrentUser.path)
        #expect(input.model == "anthropic/sonnet")
        #expect(input.thinkingLevel == "medium")
        #expect(input.temporary == false)
        #expect(input.name == "")
        #expect(opened == ["anthropic/sonnet"])
        #expect(closed == 1)
    }

    @Test func escCancelsWithoutCreating() async throws {
        let server = CreationServer(models: [Self.model("anthropic/sonnet", "Sonnet 4.6", levels: nil)])
        var closed = 0
        let window = host(server, close: { closed += 1 }, open: { _ in })
        defer { window.close() }
        // Esc does nothing until the form is shown, so keep pressing it.
        try await eventually {
            if closed == 0 { try? await press(53, "\u{1b}", in: window) }
            return closed > 0
        }
        #expect(closed == 1)
        #expect(server.created.isEmpty)
    }

    @Test func aModelKeepsTheThinkingLevelItOffers() {
        #expect(NewSessionView.defaultThinking(["low", "medium", "high"]) == "medium")
        #expect(NewSessionView.defaultThinking(["low", "high"]) == "low")
        #expect(NewSessionView.defaultThinking([]) == "")
    }
}
