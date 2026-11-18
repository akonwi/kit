import AppKit
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

struct DeletedSessionsPresentationTests {
    private func tool(ids: [String], output: String = "", failed: Bool = false, status: String? = nil) -> ToolActivity {
        let list = ids.map { "\"\($0)\"" }.joined(separator: ",")
        return .init(id: "delete", name: "delete_session", summary: "delete_session", output: output,
                     arguments: #"{"sessionIds":["# + list + "]}", failed: failed, status: status)
    }

    @Test func runningCallCountsTheSessionsItNames() {
        let one = tool(ids: ["session_docs"], status: "Running…")
        #expect(ToolPresentation(one).title == "Delete session")
        #expect(ToolPresentation(one).chip == "1 session")
        let two = tool(ids: ["session_docs", "session_api"], status: "Running…")
        #expect(ToolPresentation(two).title == "Delete sessions")
        #expect(ToolPresentation(two).chip == "2 sessions")
    }

    @Test func finishedCallNamesTheDeletedAndKeptSessions() {
        let value = DeletedSessionsPresentation(tool(ids: ["session_docs", "session_api"], output: """
        {"deleted":[{"id":"session_fork"},{"id":"session_api","name":"API"}],"failed":[{"id":"session_docs","name":"Docs","error":"a session below it was not deleted"},{"id":"session_gone","error":"this session has no child with this ID"}],"resolution":"answered","message":"Deleted 2 sessions; 2 not deleted."}
        """))
        #expect(value.summary == "Untitled session, API")
        #expect(value.deleted == ["Untitled session", "API"])
        #expect(value.failures == [.init(name: "Docs", reason: "a session below it was not deleted"),
                                   .init(name: "session_gone", reason: "this session has no child with this ID")])
        #expect(value.result == "Deleted 2 sessions; 2 not deleted.")
        #expect(!value.failed)
        #expect(!value.declined)
    }

    @Test func declinedAndRefusedCallsSayWhy() {
        let declined = tool(ids: ["session_docs"], output: #"{"declined":true,"resolution":"negative_confirmation","message":"The user declined. Nothing was deleted."}"#)
        #expect(ToolPresentation(declined).chip == "declined")
        #expect(DeletedSessionsPresentation(declined).declined)
        let refused = DeletedSessionsPresentation(tool(ids: ["session_other"], output: #"{"error":"nothing was deleted: session_other is not a child of this session"}"#, failed: true))
        #expect(refused.failed)
        #expect(refused.result == "nothing was deleted: session_other is not a child of this session")
        #expect(refused.summary == "1 session")
    }

    @Test func discoveringChildSessionsSaysSo() {
        let discover = ToolActivity(id: "discover", name: "peer_session", summary: "peer_session", output: "",
                                    arguments: #"{"action":"discover","children":true}"#, failed: false)
        #expect(ToolPresentation(discover).chip == "child sessions")
    }

    @MainActor @Test func rendersDeletionDetailsInBothThemes() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let deleted = tool(ids: ["session_docs", "session_api"], output: """
            {"deleted":[{"id":"session_fork","name":"Docs fork"},{"id":"session_api","name":"API"}],"failed":[{"id":"session_docs","name":"Docs","error":"a session below it was not deleted"}],"message":"Deleted 2 sessions; 1 not deleted."}
            """)
            let refused = tool(ids: ["session_other"], output: #"{"error":"nothing was deleted: session_other is not a child of this session"}"#, failed: true)
            let content = VStack(alignment: .leading, spacing: 16) {
                ToolCallDetail(tool: deleted, workspace: WorkspaceState())
                ToolCallDetail(tool: refused, workspace: WorkspaceState())
            }.padding(20).frame(width: 700).environment(\.mica, theme)
                .environment(\.colorScheme, dark ? .dark : .light).foregroundStyle(theme.text).background(theme.surface)
            let host = NSHostingView(rootView: content)
            let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 700, height: 400))
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            try await Task.sleep(for: .milliseconds(200))
            host.layoutSubtreeIfNeeded()
            host.frame = NSRect(origin: .zero, size: host.fittingSize)
            host.layoutSubtreeIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: "/tmp/kit-delete-session-\(dark ? "dark" : "light").png"))
        }
    }
}
