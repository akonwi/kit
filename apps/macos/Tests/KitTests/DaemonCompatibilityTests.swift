import Foundation
import Testing
@testable import Kit

struct DaemonCompatibilityTests {
    @Test func currentProtocolAllowsCanonicalStableReleaseSkew() {
        for (app, daemon) in [("0.42.0", "0.42.1"), ("0.42.1", "0.42.0"), ("1.0.0", "0.42.0")] {
            #expect(DaemonCompatibility.accepts(clientVersion: app, daemonVersion: daemon,
                                               clientProtocol: kitWireVersion, daemonProtocol: kitWireVersion))
        }
    }

    @Test func prereleaseOrUnverifiedSkewDoesNotAttach() {
        for (app, daemon) in [("dev", "0.42.0"), ("0.42.0-rc.1", "0.42.0"),
                              ("0.42.0-rc.1", "0.42.0-rc.2"), ("0.42.0", "0.42.0-rc.1"),
                              ("0.41.9", "0.42.0"), ("00.42.0", "0.42.0"),
                              ("0.42.00", "0.42.0"), ("0.42.0+meta", "0.42.0"),
                              ("0.42.0", "0.42.0+meta")] {
            #expect(!DaemonCompatibility.accepts(clientVersion: app, daemonVersion: daemon,
                                                clientProtocol: kitWireVersion, daemonProtocol: kitWireVersion))
        }
        #expect(DaemonCompatibility.accepts(clientVersion: "dev", daemonVersion: "dev", clientProtocol: kitWireVersion, daemonProtocol: kitWireVersion))
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.42.0", daemonVersion: "0.42.0",
                                             clientProtocol: 44, daemonProtocol: kitWireVersion) == .clientProtocolOlder)
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.42.0", daemonVersion: "0.42.0",
                                             clientProtocol: kitWireVersion, daemonProtocol: 44) == .daemonProtocolOlder)
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.42.0", daemonVersion: "dev",
                                             clientProtocol: kitWireVersion, daemonProtocol: kitWireVersion) == .releaseMismatch)
    }

    private func registry(version: String = "0.42.0", protocolNumber: Int = kitWireVersion) throws -> LocalDaemonRegistry {
        try JSONDecoder().decode(LocalDaemonRegistry.self, from: Data("""
        {"registryVersion":1,"protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "url":"http://127.0.0.1:12345","instanceId":"test-instance","pid":123}
        """.utf8))
    }

    private func health(version: String = "0.42.0", protocolNumber: Int = kitWireVersion,
                        instance: String = "test-instance", pid: Int = 123, ready: Bool = true) throws -> LocalDaemonHealth {
        try JSONDecoder().decode(LocalDaemonHealth.self, from: Data("""
        {"instanceId":"\(instance)","protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "pid":\(pid),"databaseReady":\(ready)}
        """.utf8))
    }

    @Test func discoveryAuthenticatesIdentityAndReadinessBeforeNegotiation() throws {
        try HTTPClient.validateDiscovery(registry: registry(), health: health(), clientRelease: "0.42.0")
        for altered in [try health(version: "0.43.0"), try health(protocolNumber: 44),
                        try health(instance: "another"), try health(pid: 124)] {
            do {
                try HTTPClient.validateDiscovery(registry: registry(), health: altered, clientRelease: "0.42.0")
                Issue.record("A mismatched server identity must not attach")
            } catch ClientError.invalidPayload { /* registry and health must agree */ }
        }
        do {
            try HTTPClient.validateDiscovery(registry: registry(), health: health(ready: false), clientRelease: "0.42.0")
            Issue.record("An unready database must not attach")
        } catch ClientError.daemonNotReady { /* leave the server alone */ }
        do {
            try HTTPClient.validateDiscovery(registry: registry(version: "dev"), health: health(version: "dev"), clientRelease: "0.42.0")
            Issue.record("A stable release must not attach to an unverified dev daemon")
        } catch ClientError.incompatibleDaemon(let app, let daemon, let appProtocol, let daemonProtocol, let reason) {
            #expect(app == "0.42.0")
            #expect(daemon == "dev")
            #expect(appProtocol == kitWireVersion)
            #expect(daemonProtocol == kitWireVersion)
            #expect(reason == .releaseMismatch)
        }
    }
}
