import Foundation
import Testing
@testable import Kit

struct OpenURLRequestTests {
    private func tool(_ id: String, name: String = "open_url", url: String = "https://example.com/page",
                      presentation: String = "open-url", live: Bool? = true, failed: Bool = false) -> ToolActivity {
        let details = try! String(decoding: JSONSerialization.data(withJSONObject: ["presentation": presentation, "url": url]), as: UTF8.self)
        return ToolActivity(id: id, name: name, summary: name, output: "true", failed: failed, status: "Completed",
                            details: details, completedLive: live)
    }
    private func rows(_ tools: [ToolActivity]) -> [TranscriptMessage] {
        [TranscriptMessage(id: "tools", role: "tools", text: "", tools: tools)]
    }

    @Test func opensEachLiveCallOnceInOrder() {
        var handled: Set<String> = []
        let messages = rows([tool("a", url: "https://example.com/a"), tool("b", url: "http://localhost:3000/")])
        #expect(OpenURLRequest.pending(in: messages, handled: &handled)
            == [URL(string: "https://example.com/a")!, URL(string: "http://localhost:3000/")!])
        #expect(OpenURLRequest.pending(in: messages, handled: &handled) == [])
        #expect(handled == ["a", "b"])
    }

    @Test func opensOnlySuccessfulLiveOpenURLCallsWithWebAddresses() {
        var handled: Set<String> = []
        let messages = rows([
            tool("replayed", live: nil),
            tool("failed", failed: true),
            tool("other", name: "show_image"),
            tool("file", url: "file:///tmp/page.html"),
            tool("script", url: "javascript:alert(1)"),
            tool("credentials", url: "https://user:secret@example.com/"),
            tool("space", url: "https://example.com/a b"),
            tool("presentation", presentation: "transcript-image"),
            tool("opened", url: "https://example.com/ok"),
        ])
        #expect(OpenURLRequest.pending(in: messages, handled: &handled) == [URL(string: "https://example.com/ok")!])
    }

    @Test func opensEachLivePageRequestOnceAfterTheToolCallsInOrder() {
        var handled: Set<String> = []
        let messages = rows([tool("call", url: "https://example.com/tool")])
        let requests = [RequestedPage(id: "stream:4", url: URL(string: "https://example.com/plugin")!),
                        RequestedPage(id: "stream:5", url: URL(string: "http://localhost:3000/")!)]
        #expect(OpenURLRequest.pending(in: messages, requests: requests, handled: &handled)
            == [URL(string: "https://example.com/tool")!, URL(string: "https://example.com/plugin")!, URL(string: "http://localhost:3000/")!])
        let later = requests + [RequestedPage(id: "stream:9", url: URL(string: "https://example.com/later")!)]
        #expect(OpenURLRequest.pending(in: messages, requests: later, handled: &handled) == [URL(string: "https://example.com/later")!])
        #expect(handled == ["call", "stream:4", "stream:5", "stream:9"])
    }

    @Test func acceptsOnlyWebAddressesWithAHostAndNoCredentials() {
        #expect(OpenURLRequest.url("https://example.com/docs?q=1#top") == URL(string: "https://example.com/docs?q=1#top"))
        #expect(OpenURLRequest.url("http://localhost:3000/") == URL(string: "http://localhost:3000/"))
        for raw in ["", "file:///etc/passwd", "javascript:alert(1)", "https://user:secret@example.com/", "https://example.com/a b",
                    "/relative", "https://example.com/" + String(repeating: "a", count: OpenURLRequest.maxURLBytes)] {
            #expect(OpenURLRequest.url(raw) == nil, "\(raw)")
        }
    }

    @Test func forgetsOnlyCallsNoLongerShownWhenTheBoundIsExceeded() {
        var handled = Set((0..<OpenURLRequest.handledLimit).map { "old\($0)" })
        let messages = rows([tool("shown")])
        #expect(OpenURLRequest.pending(in: messages, handled: &handled) == [URL(string: "https://example.com/page")!])
        #expect(handled == ["shown"])
        #expect(OpenURLRequest.pending(in: messages, handled: &handled) == [])
    }
}
