import AppKit
import SwiftUI
import Testing
@testable import Kit

struct CreatedSessionPresentationTests {
    private let id = "session_0123456789abcdef0123456789abcdef"
    private func tool(output: String, failed: Bool = false, status: String? = nil) -> ToolActivity {
        .init(id: "create", name: "create_session", summary: "create_session", output: output,
              arguments: #"{"name":"Investigate API","cwd":"/tmp/project","prompt":"Inspect the API and report findings."}"#,
              failed: failed, status: status)
    }
    @Test func successUsesAuthoritativeNameAndLinksOnlyCanonicalIdentity() {
        let recorded = tool(output: """
        {"session":{"id":"\(id)","cwd":"/private/tmp/project","name":"API investigation","model":"provider/model","thinkingLevel":"off","runId":"turn_test"}}
        """)
        let value = CreatedSessionPresentation(recorded)
        #expect(ToolPresentation(recorded).title == "Create session")
        #expect(value.summary == "API investigation · /private/tmp/project")
        #expect(value.prompt == "Inspect the API and report findings.")
        #expect(value.result == "Session created · Initial prompt started")
        #expect(value.sessionID == id)
    }
    @Test func failuresAndIncompleteResultsHaveUsefulTextWithoutNavigation() {
        let failed = CreatedSessionPresentation(tool(output: #"{"error":"Directory does not exist"}"#, failed: true))
        #expect(failed.result == "Directory does not exist")
        #expect(failed.summary == "Investigate API · /tmp/project")
        #expect(failed.sessionID == nil)
        let malformed = CreatedSessionPresentation(tool(output: #"{"session":{"id":"session_partial"}}"#))
        #expect(malformed.result == "Creation result unavailable")
        #expect(malformed.sessionID == nil)
        let running = CreatedSessionPresentation(tool(output: "", status: "Running…"))
        #expect(running.result == "Creating session…")
        #expect(running.sessionID == nil)
    }
    @Test func createdSessionWithoutInitialRunRemainsNavigable() {
        let value = CreatedSessionPresentation(tool(output: """
        {"session":{"id":"\(id)","cwd":"/tmp/project"}}
        """))
        #expect(value.result == "Session created")
        #expect(value.sessionID == id)
    }
    @MainActor @Test func rendersCreationAndFailureDetailsInBothThemes() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let created = tool(output: """
            {"session":{"id":"\(id)","cwd":"/tmp/project","name":"Investigate API","runId":"turn_test"}}
            """)
            let content = VStack(alignment: .leading, spacing: 16) {
                Text(ToolPresentation(created).summary)
                ToolCallDetail(tool: created, workspace: WorkspaceState())
                ToolCallDetail(tool: tool(output: #"{"error":"Directory does not exist"}"#, failed: true), workspace: WorkspaceState())
            }.padding(20).frame(width: 700).environment(\.mica, theme)
                .environment(\.transcriptSessionLink, TranscriptSessionLink(serverID: "test", open: { _ in }))
                .environment(\.colorScheme, dark ? .dark : .light).foregroundStyle(theme.text).background(theme.surface)
            let host = NSHostingView(rootView: content)
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 700, height: 400), styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(200))
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            // Flat tool details share the transcript surface in both appearances.
            let actual = try #require(bitmap.colorAt(x: bitmap.pixelsWide / 2, y: bitmap.pixelsHigh / 4)?.usingColorSpace(.deviceRGB))
            let expected = try #require(NSColor(theme.surface).usingColorSpace(.deviceRGB))
            #expect(abs(actual.redComponent - expected.redComponent) < 0.02)
            #expect(abs(actual.greenComponent - expected.greenComponent) < 0.02)
            #expect(abs(actual.blueComponent - expected.blueComponent) < 0.02)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-create-session-\(dark ? "dark" : "light").png"))
        }
    }
}
