import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct PluginFooterTests {
    private func footer(_ text: String = "Ready") throws -> WirePluginFooter {
        let json = """
        {"locationHidden":true,"items":[{"id":"demo.status","pluginId":"demo","instance":"host:1","content":[{"text":"\(text)","style":{"fg":"toolText","bold":true,"underline":true}}]}]}
        """
        return try JSONDecoder().decode(WirePluginFooter.self, from: Data(json.utf8))
    }
    @Test func projectsOwnedFooterAndRendersSemanticStyles() throws {
        let source = try footer()
        let projected = try PluginFooterProjection.validate(source)
        let value = try #require(projected)
        #expect(value.locationHidden)
        #expect(value.items?.map(\.id) == ["demo.status"])
        let theme = MicaTheme(dark: true)
        let content = PluginFooterView.styledContent(items: value.items ?? [], theme: theme)
        #expect(String(content.characters) == "Ready")
        #expect(content.runs.first?.foregroundColor == theme.success)
        #expect(content.runs.first?.underlineStyle == .single)
        #expect(content.runs.first?.font == Font.kit(size: 12, weight: .bold))
    }
    @Test func footerUsesTheEntireOfferedSlotInsteadOfCappingAt340Points() async throws {
        _ = NSApplication.shared
        let content = "A longer plugin contribution now uses the available footer width"
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "off", workspace: "/repo", date: "", messages: [])
        session.pluginFooter = try footer(content)
        let measurement = FooterWidthMeasurement()
        let host = NSHostingView(rootView: HStack(spacing: 0) {
            PluginFooterView(session: session)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { measurement.width = $0 }
        }.frame(width: 620, height: 38))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 620, height: 38), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        #expect(measurement.width == 620)
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        let png = try #require(bitmap.representation(using: .png, properties: [:]))
        try png.write(to: URL(fileURLWithPath: "/tmp/kit-footer-wide.png"))
    }

    @Test func overflowDetailsExposeFullLocationAndUnabridgedStyledText() throws {
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "off", workspace: "/repo", date: "", messages: [])
        session.cwd = "/a/long/workspace/path/that/must/be/readable"
        session.gitHead = "feature/footer"; session.gitDirty = true
        #expect(PluginFooterDetails.location(session) == "/a/long/workspace/path/that/must/be/readable\nfeature/footer*")
        let text = String(repeating: "Complete contribution text. ", count: 30)
        session.pluginFooter = try footer(text)
        #expect(PluginFooterDetails.location(session) == nil)
        let displayed = PluginFooterView.styledContent(items: session.pluginFooter?.items ?? [], theme: MicaTheme(dark: false))
        #expect(String(displayed.characters) == text)
        let host = NSHostingView(rootView: PluginFooterDetails(session: session))
        host.frame = NSRect(origin: .zero, size: host.fittingSize)
        host.layoutSubtreeIfNeeded()
        #expect(host.bounds.width == 440)
        #expect(host.bounds.height == 300)
    }

    @Test func overflowReportsOnlyItemsOutsideTheVisiblePrefix() {
        #expect(PluginFooterView.overflowLabel(total: 3, visible: 1) == "… 2 more")
        #expect(PluginFooterView.overflowLabel(total: 3, visible: 0) == "… 3 more")
        #expect(PluginFooterView.overflowLabel(total: 3, visible: 3) == nil)
    }

    @Test func tokenValidationMatchesThePublicSchema() throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .appendingPathComponent("../../../../app/docs/plugin-protocol/protocol.schema.json")
        let data = try Data(contentsOf: url)
        let decoded = try JSONSerialization.jsonObject(with: data)
        let root = try #require(decoded as? [String: Any])
        let definitions = try #require(root["$defs"] as? [String: Any])
        let token = try #require(definitions["ThemeToken"] as? [String: Any])
        let names = try #require(token["enum"] as? [String])
        #expect(PluginFooterProjection.tokens == Set(names))
    }

    @Test func rejectsOversizedAndUnknownStyleMetadata() throws {
        #expect(throws: ClientError.self) { try PluginFooterProjection.validate(footer(String(repeating: "x", count: 4097))) }
        let json = #"{"locationHidden":false,"items":[{"id":"demo.status","pluginId":"demo","instance":"host:1","content":[{"text":"Ready","style":{"fg":"arbitrary"}}]}]}"#
        let invalid = try JSONDecoder().decode(WirePluginFooter.self, from: Data(json.utf8))
        #expect(throws: ClientError.self) { try PluginFooterProjection.validate(invalid) }
    }
}

@MainActor private final class FooterWidthMeasurement { var width: CGFloat = 0 }
