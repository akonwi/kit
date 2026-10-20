import Foundation
import Network
import Testing
@testable import Kit

/// Loopback ports and requests for redirect tests.
enum LoopbackPort {
    /// A port nothing on 127.0.0.1 listens on.
    static func free() async throws -> UInt16 {
        let listener = try await listen(.any, reuse: false)
        let port = try #require(listener.port?.rawValue)
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            listener.stateUpdateHandler = { state in
                if case .cancelled = state { listener.stateUpdateHandler = nil; continuation.resume() }
            }
            listener.cancel()
        }
        return port
    }

    /// Binds 127.0.0.1:`port` as the terminal client's Go listener does,
    /// retrying briefly while a stopped listener lets go of it. Keeps the
    /// listener when asked; otherwise releases it.
    @discardableResult
    static func bind(_ port: UInt16, keep: Bool = false) async throws -> NWListener? {
        var failure: Error?
        for _ in 0..<50 {
            do {
                let listener = try await listen(.init(rawValue: port)!, reuse: true)
                if keep { return listener }
                listener.cancel()
                return nil
            } catch {
                failure = error
                try await Task.sleep(for: .milliseconds(20))
            }
        }
        throw failure!
    }

    /// GETs `url`, retrying while nothing listens there yet.
    static func get(_ url: String) async throws -> (Int, String) {
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        var failure: Error?
        for _ in 0..<100 {
            do {
                let (data, response) = try await session.data(from: URL(string: url)!)
                return ((response as? HTTPURLResponse)?.statusCode ?? 0, String(decoding: data, as: UTF8.self))
            } catch let error as URLError where error.code == .cannotConnectToHost {
                failure = error
                try await Task.sleep(for: .milliseconds(20))
            }
        }
        throw failure!
    }

    private static func listen(_ port: NWEndpoint.Port, reuse: Bool) async throws -> NWListener {
        let parameters = NWParameters.tcp
        parameters.allowLocalEndpointReuse = reuse
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: port)
        let listener = try NWListener(using: parameters)
        listener.newConnectionHandler = { $0.cancel() }
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready: listener.stateUpdateHandler = nil; continuation.resume()
                case let .failed(error), let .waiting(error):
                    listener.stateUpdateHandler = nil; listener.cancel(); continuation.resume(throwing: error)
                default: break
                }
            }
            listener.start(queue: .global())
        }
        return listener
    }
}

struct LoopbackRedirectTests {
    static let authorization = URL(string: "https://claude.ai/oauth/authorize?code=true&state=st4te&redirect_uri=x")!

    @Test func acceptsOnlyLoopbackHTTPAddressesWithAPort() {
        for valid in ["http://localhost:53692/callback", "http://LOCALHOST:1/", "http://127.0.0.1:8080/cb", "http://127.1.2.3:9/cb", "http://[::1]:53692/callback"] {
            #expect(LoopbackRedirect.address(valid)?.absoluteString == valid, "\(valid)")
        }
        for invalid in ["https://localhost:53692/callback", "http://localhost/callback", "http://example.com:53692/callback",
                        "http://localhost.example.com:53692/callback", "http://192.168.1.2:53692/callback",
                        "http://user@localhost:53692/callback", "http://localhost:0/callback", "localhost:53692", "file:///tmp/x"] {
            #expect(LoopbackRedirect.address(invalid) == nil, "\(invalid)")
        }
    }

    @Test func needsTheAuthorizationPagesState() throws {
        let address = URL(string: "http://localhost:53692/callback")!
        let redirect = try #require(LoopbackRedirect(redirect: address, authorization: Self.authorization))
        #expect(redirect.state == "st4te")
        #expect(redirect.port == 53692)
        #expect(redirect.listenHosts == ["127.0.0.1", "::1"])
        #expect(LoopbackRedirect(redirect: URL(string: "http://127.0.0.1:53692/callback")!, authorization: Self.authorization)?.listenHosts == ["127.0.0.1"])
        #expect(LoopbackRedirect(redirect: URL(string: "http://[::1]:53692/callback")!, authorization: Self.authorization)?.listenHosts == ["::1"])
        #expect(LoopbackRedirect(redirect: address, authorization: URL(string: "https://claude.ai/oauth/authorize?code=true")!) == nil)
        #expect(LoopbackRedirect(redirect: address, authorization: URL(string: "https://claude.ai/oauth/authorize?state=")!) == nil)
        #expect(LoopbackRedirect(redirect: URL(string: "https://claude.ai/callback")!, authorization: Self.authorization) == nil)
    }

    @Test func relaysOnlyARequestOnTheRedirectPathCarryingTheState() throws {
        let redirect = try #require(LoopbackRedirect(redirect: URL(string: "http://localhost:53692/callback")!, authorization: Self.authorization))
        #expect(redirect.relay("/callback?code=a%2Bb&state=st4te") == "http://localhost:53692/callback?code=a%2Bb&state=st4te")
        #expect(redirect.relay("http://localhost:53692/callback?state=st4te&code=c") == "http://localhost:53692/callback?state=st4te&code=c")
        for refused in ["/callback?code=abc&state=other", "/callback?code=abc", "/callback", "/other?code=abc&state=st4te",
                        "/callback/more?state=st4te", "/favicon.ico", "*", "/callback?state=ST4TE", "not a target"] {
            #expect(redirect.relay(refused) == nil, "\(refused)")
        }
    }
}

struct LoopbackRedirectReceiverTests {
    private func redirect(_ port: UInt16, host: String = "127.0.0.1") throws -> LoopbackRedirect {
        try #require(LoopbackRedirect(redirect: URL(string: "http://\(host):\(port)/callback")!, authorization: LoopbackRedirectTests.authorization))
    }

    @Test func answersTheBrowserAndHandsOverTheFirstMatchingRedirect() async throws {
        let port = try await LoopbackPort.free()
        let receiver = try await LoopbackRedirectReceiver.start(redirect(port, host: "localhost"))
        defer { receiver.stop() }

        let refused = try await LoopbackPort.get("http://127.0.0.1:\(port)/callback?code=abc&state=forged")
        #expect(refused.0 == 400)
        #expect(refused.1 == LoopbackRedirectReceiver.refusal)
        #expect(try await LoopbackPort.get("http://127.0.0.1:\(port)/favicon.ico").0 == 400)

        let accepted = try await LoopbackPort.get("http://localhost:\(port)/callback?code=abc&state=st4te")
        #expect(accepted.0 == 200)
        #expect(accepted.1 == LoopbackRedirectReceiver.page)
        #expect(await receiver.received() == "http://localhost:\(port)/callback?code=abc&state=st4te")
    }

    @Test func failsWhenAnotherClientListensOnThePort() async throws {
        let port = try await LoopbackPort.free()
        let taken = try #require(try await LoopbackPort.bind(port, keep: true))
        defer { taken.cancel() }
        await #expect(throws: (any Error).self) { _ = try await LoopbackRedirectReceiver.start(redirect(port)) }
    }

    @Test func stoppingEndsTheWaitAndReleasesThePort() async throws {
        let port = try await LoopbackPort.free()
        let receiver = try await LoopbackRedirectReceiver.start(redirect(port))
        let waiting = Task { await receiver.received() }
        receiver.stop()
        #expect(await waiting.value == nil)
        try await LoopbackPort.bind(port)
    }

    @Test func takesOverThePortAsAStoppedReceiverLetsGo() async throws {
        let port = try await LoopbackPort.free()
        let first = try await LoopbackRedirectReceiver.start(redirect(port))
        first.stop()
        let second = try await LoopbackRedirectReceiver.start(redirect(port))
        defer { second.stop() }
        #expect(try await LoopbackPort.get("http://127.0.0.1:\(port)/callback?code=abc&state=st4te").0 == 200)
        #expect(await second.received() == "http://127.0.0.1:\(port)/callback?code=abc&state=st4te")
    }

    @Test func cancellingTheWaitEndsIt() async throws {
        let port = try await LoopbackPort.free()
        let receiver = try await LoopbackRedirectReceiver.start(redirect(port))
        defer { receiver.stop() }
        let waiting = Task { await receiver.received() }
        waiting.cancel()
        #expect(await waiting.value == nil)
    }
}
