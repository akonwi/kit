import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The outline a session tab shows while its session opens.
@MainActor
struct SessionSkeletonTests {
    private func host<Content: View>(_ view: Content) -> (NSWindow, NSView) {
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 900, height: 700))
        let host = NSHostingView(rootView: view.frame(width: 900, height: 700))
        window.contentView = host
        host.layoutSubtreeIfNeeded()
        return (window, host)
    }

    private func placeholders(in view: NSView) -> [NSView] {
        view.subviews.flatMap { subview -> [NSView] in
            let own = subview.layer?.animation(forKey: SkeletonPlaceholder.animationKey) != nil ? [subview] : []
            return own + placeholders(in: subview)
        }
    }

    @Test func everyPlaceholderFadesInStepWithTheOthers() throws {
        let (window, host) = host(SessionSkeleton(label: "Opening session").environment(\.mica, MicaTheme(dark: true)))
        defer { window.close() }
        let shapes = placeholders(in: host)
        // The tab strip's title, the user's message, and four lines of reply.
        #expect(shapes.count == 6)
        let fades = shapes.compactMap { $0.layer?.animation(forKey: SkeletonPlaceholder.animationKey) as? CABasicAnimation }
        #expect(fades.count == 6)
        for fade in fades {
            #expect(fade.keyPath == "opacity")
            #expect(fade.fromValue as? Float == 1)
            #expect(fade.toValue as? Float == 0.55)
            #expect(fade.duration == 1.1)
            #expect(fade.autoreverses)
            #expect(fade.repeatCount == .infinity)
        }
        #expect(Set(fades.map(\.beginTime)).count == 1)
        #expect(Set(shapes.compactMap { $0.layer?.backgroundColor }).count == 1)
        #expect(shapes.first?.layer?.backgroundColor == NSColor(MicaTheme(dark: true).hover).cgColor)
    }

    @Test func withReducedMotionAPlaceholderStaysStill() {
        let (window, host) = host(SkeletonPlaceholder(color: .gray, cornerRadius: 4, pulses: false))
        defer { window.close() }
        let layers = [host].flatMap { [$0] + $0.subviews.flatMap { [$0] + $0.subviews } }.compactMap(\.layer)
        let colored = layers.filter { $0.backgroundColor == NSColor.gray.cgColor }
        #expect(colored.count == 1)
        #expect(colored.first?.animation(forKey: SkeletonPlaceholder.animationKey) == nil)
        #expect(colored.first?.opacity == 1)
    }
}
