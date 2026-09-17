import AppKit
import SwiftUI
import Testing
@testable import Kit

struct PeerSessionPresentationTests {
    private let id = "session_0123456789abcdef0123456789abcdef"
    private func tool(_ action: String, _ output: String, failed: Bool = false, status: String? = nil) -> ToolActivity {
        .init(id: "peer", name: "peer_session", summary: "peer_session", output: output,
              arguments: """
              {"action":"\(action)","sessionId":"\(id)","message":"What is the **API contract**?"}
              """, failed: failed, status: status)
    }
    @Test func discoveryShowsNamesDirectoriesAndAvailability() {
        let value = PeerSessionPresentation(tool("discover", """
        {"action":"discover","sessions":[{"id":"\(id)","name":"API design","cwd":"/tmp/api","availability":"available"}]}
        """))
        #expect(value.title == "Find sessions")
        #expect(value.status == "1 session")
        #expect(value.sessions.map(\.title) == ["API design"])
        #expect(value.sessions.map(\.cwd) == ["/tmp/api"])
        #expect(value.sessions.map(\.availability) == ["available"])
        #expect(PeerSessionPresentation(tool("discover", #"{"action":"discover"}"#)).status == "No peer sessions available")
    }
    @Test func sendInspectAndWaitPresentRecordedRequest() {
        for action in ["send", "inspect", "wait"] {
            let value = PeerSessionPresentation(tool(action, """
            {"action":"\(action)","request":{"recipientSessionId":"\(id)","state":"completed","result":"Use the v2 endpoint."}}
            """))
            #expect(value.recipientID == id)
            #expect(value.status == "Completed")
            #expect(value.reply == "Use the v2 endpoint.")
            #expect(value.question == (action == "send" ? "What is the **API contract**?" : nil))
        }
        let waiting = PeerSessionPresentation(tool("wait", """
        {"action":"wait","request":{"recipientSessionId":"\(id)","state":"processing"},"timedOut":true}
        """))
        #expect(waiting.status == "Wait timed out · Processing")
        #expect(waiting.error == nil)
    }
    @Test func errorsPendingAndMalformedResultsRemainReadable() {
        #expect(PeerSessionPresentation(tool("send", #"{"error":"Recipient unavailable"}"#, failed: true)).error == "Recipient unavailable")
        #expect(PeerSessionPresentation(tool("send", "", status: "Running…")).status == "Ask session…")
        #expect(PeerSessionPresentation(tool("inspect", "{broken")).status == "Peer result unavailable")
        #expect(PeerSessionPresentation(tool("inspect", "{broken", failed: true)).error == "Peer request failed.")
        #expect(!PeerSessionPresentation.validSessionID("session_partial"))
        for (state, label) in [("queued", "Queued"), ("failed", "Failed"), ("aborted", "Aborted"), ("interrupted", "Interrupted"), ("recipient_archived", "Recipient archived"), ("recipient_unavailable", "Recipient unavailable")] {
            #expect(PeerSessionPresentation.stateLabel(state) == label)
        }
    }
    @MainActor @Test func rendersPeerDetailsInBothThemes() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let discover = ToolActivity(id: "discovery", name: "peer_session", summary: "", output: """
            {"action":"discover","sessions":[{"id":"\(id)","name":"API design","cwd":"/tmp/api","availability":"available"}]}
            """, arguments: #"{"action":"discover"}"#, failed: false)
            let response = tool("send", """
            {"action":"send","request":{"recipientSessionId":"\(id)","state":"completed","result":"Use the **v2 endpoint** with an ordered event cursor."}}
            """)
            let host = NSHostingView(rootView: VStack(alignment: .leading, spacing: 16) {
                ToolCallDetail(tool: discover, workspace: WorkspaceState())
                ToolCallDetail(tool: response, workspace: WorkspaceState())
            }.padding(20).frame(width: 700).environment(\.mica, theme)
                .environment(\.transcriptSessionLink, TranscriptSessionLink(serverID: "test", open: { _ in }))
                .environment(\.colorScheme, dark ? .dark : .light).foregroundStyle(theme.text).background(theme.surface))
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 700, height: 430), styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(200))
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            let actual = try #require(bitmap.colorAt(x: bitmap.pixelsWide / 2, y: 70)?.usingColorSpace(.deviceRGB))
            let expected = try #require(NSColor(theme.surface).usingColorSpace(.deviceRGB))
            #expect(abs(actual.redComponent - expected.redComponent) < 0.02)
            #expect(abs(actual.greenComponent - expected.greenComponent) < 0.02)
            #expect(abs(actual.blueComponent - expected.blueComponent) < 0.02)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-peer-\(dark ? "dark" : "light").png"))
        }
    }
}
