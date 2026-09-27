import Foundation
import Testing
@testable import Kit

struct DaemonCompatibilityTests {
    @Test func stableProtocol40ReleaseSkewAttachesBidirectionally() {
        for (app, daemon) in [("0.37.0", "0.38.0"), ("0.38.0", "0.37.0"), ("1.0.0", "0.37.0")] {
            #expect(DaemonCompatibility.accepts(clientVersion: app, daemonVersion: daemon,
                                               clientProtocol: 40, daemonProtocol: 40))
        }
    }

    @Test func prereleaseOrUnverifiedSkewDoesNotAttach() {
        for (app, daemon) in [("dev", "0.37.0"), ("0.37.0-rc.1", "0.37.0"),
                              ("0.37.0-rc.1", "0.37.0-rc.2"), ("0.37.0", "0.37.0-rc.1"),
                              ("0.36.9", "0.37.0"), ("00.37.0", "0.37.0"),
                              ("0.37.00", "0.38.0"), ("0.37.0+meta", "0.38.0"),
                              ("0.37.0", "0.38.0+meta")] {
            #expect(!DaemonCompatibility.accepts(clientVersion: app, daemonVersion: daemon,
                                                clientProtocol: 40, daemonProtocol: 40))
        }
        #expect(DaemonCompatibility.accepts(clientVersion: "dev", daemonVersion: "dev", clientProtocol: 40, daemonProtocol: 40))
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.37.0", daemonVersion: "0.37.0",
                                             clientProtocol: 40, daemonProtocol: 41) == .clientProtocolOlder)
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.37.0", daemonVersion: "0.37.0",
                                             clientProtocol: 40, daemonProtocol: 39) == .daemonProtocolOlder)
        #expect(DaemonCompatibility.mismatch(clientVersion: "0.37.0", daemonVersion: "dev",
                                             clientProtocol: 40, daemonProtocol: 40) == .releaseMismatch)
    }

    private func registry(version: String = "0.38.0", protocolNumber: Int = 40) throws -> LocalDaemonRegistry {
        try JSONDecoder().decode(LocalDaemonRegistry.self, from: Data("""
        {"registryVersion":1,"protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "url":"http://127.0.0.1:12345","instanceId":"test-instance","pid":123}
        """.utf8))
    }

    private func health(version: String = "0.38.0", protocolNumber: Int = 40,
                        instance: String = "test-instance", pid: Int = 123, ready: Bool = true) throws -> LocalDaemonHealth {
        try JSONDecoder().decode(LocalDaemonHealth.self, from: Data("""
        {"instanceId":"\(instance)","protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "pid":\(pid),"databaseReady":\(ready)}
        """.utf8))
    }

    @Test func discoveryAuthenticatesIdentityAndReadinessBeforeNegotiation() throws {
        try HTTPClient.validateDiscovery(registry: registry(), health: health(), appVersion: "0.37.0")
        for altered in [try health(version: "0.39.0"), try health(protocolNumber: 41),
                        try health(instance: "another"), try health(pid: 124)] {
            do {
                try HTTPClient.validateDiscovery(registry: registry(), health: altered, appVersion: "0.37.0")
                Issue.record("A mismatched server identity must not attach")
            } catch ClientError.invalidPayload { /* registry and health must agree */ }
        }
        do {
            try HTTPClient.validateDiscovery(registry: registry(), health: health(ready: false), appVersion: "0.37.0")
            Issue.record("An unready database must not attach")
        } catch ClientError.daemonNotReady { /* leave the server alone */ }
        do {
            try HTTPClient.validateDiscovery(registry: registry(version: "dev"), health: health(version: "dev"), appVersion: "0.37.0")
            Issue.record("A stable release must not attach to an unverified dev daemon")
        } catch ClientError.incompatibleDaemon(let app, let daemon, let appProtocol, let daemonProtocol, let reason) {
            #expect(app == "0.37.0")
            #expect(daemon == "dev")
            #expect(appProtocol == 40)
            #expect(daemonProtocol == 40)
            #expect(reason == .releaseMismatch)
        }
    }
}
