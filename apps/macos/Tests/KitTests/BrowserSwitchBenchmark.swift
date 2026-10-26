import AppKit
import Foundation
import Network
import QuartzCore
import SwiftUI
import Testing
@testable import Kit

/// Serves one HTML page on loopback so real WebKit pages load offline.
private final class HTMLServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "HTMLServer")
    private let response: Data

    init(body: String) throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
        response = Data("HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)".utf8)
    }

    func start() async throws -> URL {
        listener.newConnectionHandler = { [weak self] connection in self?.serve(connection) }
        let port: UInt16 = try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { [listener] state in
                switch state {
                case .ready: continuation.resume(returning: listener.port!.rawValue); listener.stateUpdateHandler = nil
                case .failed(let error): continuation.resume(throwing: error); listener.stateUpdateHandler = nil
                default: break
                }
            }
            listener.start(queue: queue)
        }
        return URL(string: "http://127.0.0.1:\(port)")!
    }

    func stop() { listener.cancel() }

    private func serve(_ connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [response] _, _, _, _ in
            connection.send(content: response, completion: .contentProcessed { _ in connection.cancel() })
        }
    }
}

/// Measures switching back to a session whose workspace holds many real
/// WebKit browser tabs (#279). Opt in with
/// `TEST_RUNNER_KIT_BROWSER_SWITCH_BENCH=1`, optionally choosing tab counts
/// with `TEST_RUNNER_KIT_BROWSER_SWITCH_TABS=0,10,30` and switches with
/// `TEST_RUNNER_KIT_BROWSER_SWITCH_CYCLES=6`:
///
///     TEST_RUNNER_KIT_BROWSER_SWITCH_BENCH=1 xcodebuild -scheme Kit -destination 'platform=macOS' \
///       -derivedDataPath .build/xcode -skipPackagePluginValidation -parallel-testing-enabled NO \
///       -only-testing:KitTests/BrowserSwitchBenchmark test | grep BENCH
///
/// It prints the median time until the main thread next runs after the
/// switch, and asserts nothing about speed.
@MainActor struct BrowserSwitchBenchmark {
    nonisolated static let enabled = ProcessInfo.processInfo.environment["KIT_BROWSER_SWITCH_BENCH"] == "1"

    nonisolated static let counts = ProcessInfo.processInfo.environment["KIT_BROWSER_SWITCH_TABS"]
        .map { $0.split(separator: ",").compactMap { Int($0) } } ?? [0, 10, 30]
    nonisolated static let cycles = ProcessInfo.processInfo.environment["KIT_BROWSER_SWITCH_CYCLES"].flatMap(Int.init).map { max(2, $0) } ?? 6

    @Test(.enabled(if: enabled), arguments: counts)
    func switchingBackToASessionWithManyBrowserTabs(count: Int) async throws {
        let rows = (0..<400).map { "<li>Row \($0) <b>bold</b> <i>text</i></li>" }.joined()
        let page = """
            <!doctype html><html><head><title>Page</title><style>li{padding:2px}</style></head>
            <body><ul>\(rows)</ul><div id=t></div>
            <script>let n=0;function f(){document.getElementById('t').textContent=n++;requestAnimationFrame(f)}f()</script>
            </body></html>
            """
        let server = try HTMLServer(body: page)
        let base = try await server.start()
        defer { server.stop() }

        let store = SessionStore(fixture: try Fixture.load())
        let workspace = store.ui.workspace
        // Two tiles: the Agent on the left, browser tabs on the right.
        var tabs: [BrowserTab] = []
        for index in 0..<count {
            let tab = workspace.openBrowser(URL(string: "\(base.absoluteString)/\(index)")!)
            if index == 0 { workspace.split(.browser(tab.id), .right) }
            tabs.append(tab)
        }
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let session = { AnyView(WorkspaceLayout(state: store).environment(\.mica, theme).environment(\.colorScheme, .dark)) }
        let host = NSHostingView(rootView: session())
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1400, height: 900), styleMask: [.titled],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }

        // Visit every tab so each page has loaded, as a person would have.
        for tab in tabs {
            workspace.select(.browser(tab.id))
            try await pump(.milliseconds(60))
        }
        try await eventually { tabs.allSatisfy { !$0.loading && $0.page.url != nil } }
        try await pump(.milliseconds(500))

        var syncs: [Double] = [], firsts: [Double] = [], settles: [Double] = [], blocked: [Double] = []
        for _ in 0..<Self.cycles {
            // Another session tab shows: this session's views go away.
            host.rootView = AnyView(Color.clear)
            host.layoutSubtreeIfNeeded()
            try await pump(.milliseconds(700))

            // Switch back, then watch the main thread until it's responsive.
            let start = CACurrentMediaTime()
            host.rootView = session()
            host.layoutSubtreeIfNeeded()
            window.displayIfNeeded()
            CATransaction.flush()
            let sync = CACurrentMediaTime()
            var first: Double?
            var last = sync, stall = 0.0, settled: Double?
            while CACurrentMediaTime() - start < 1.5 {
                try await Task.sleep(for: .milliseconds(2))
                let now = CACurrentMediaTime()
                if first == nil { first = now }
                let gap = now - last
                if gap > 0.016 { stall += gap; settled = nil } else if settled == nil { settled = now }
                last = now
            }
            syncs.append((sync - start) * 1000)
            firsts.append(((first ?? sync) - start) * 1000)
            settles.append(((settled ?? last) - start) * 1000)
            blocked.append(stall * 1000)
        }
        // The first cycle warms caches; report the rest.
        func median(_ values: [Double]) -> Double { let v = values.dropFirst().sorted(); return v[v.count / 2] }
        print(String(format: "BENCH tabs=%d sync=%.1fms firstTurn=%.1fms settled=%.1fms stalls=%.1fms (median of %d)",
                     count, median(syncs), median(firsts), median(settles), median(blocked), Self.cycles - 1))
    }

    private func pump(_ duration: Duration) async throws {
        let end = ContinuousClock.now + duration
        while ContinuousClock.now < end { try await Task.sleep(for: .milliseconds(10)) }
    }
}
