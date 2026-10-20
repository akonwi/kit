import Foundation
import Testing
@testable import Kit

struct DaemonCompatibilityTests {
    @Test func onlyTheProtocolDecidesCompatibility() {
        #expect(DaemonCompatibility.mismatch(clientProtocol: kitWireVersion, daemonProtocol: kitWireVersion) == nil)
        #expect(DaemonCompatibility.mismatch(clientProtocol: 44, daemonProtocol: kitWireVersion) == .clientProtocolOlder)
        #expect(DaemonCompatibility.mismatch(clientProtocol: kitWireVersion, daemonProtocol: 44) == .daemonProtocolOlder)
    }

    private func registry(version: String = "0.42.0", protocolNumber: Int = kitWireVersion) throws -> LocalDaemonRegistry {
        try JSONDecoder().decode(LocalDaemonRegistry.self, from: Data("""
        {"registryVersion":1,"protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "url":"http://127.0.0.1:12345","instanceId":"test-instance","pid":123}
        """.utf8))
    }

    private static let serverID = "server_" + String(repeating: "0123456789abcdef", count: 2)
    private static let machineID = String(repeating: "ab", count: 32)

    private func health(version: String = "0.42.0", protocolNumber: Int = kitWireVersion,
                        instance: String = "test-instance", pid: Int = 123, ready: Bool = true,
                        identity: String? = nil) throws -> LocalDaemonHealth {
        let identity = identity ?? #","serverId":"\#(Self.serverID)","machineId":"\#(Self.machineID)","home":"/Users/kit""#
        return try JSONDecoder().decode(LocalDaemonHealth.self, from: Data("""
        {"instanceId":"\(instance)","protocolVersion":\(protocolNumber),"kitVersion":"\(version)",
         "pid":\(pid),"databaseReady":\(ready)\(identity)}
        """.utf8))
    }

    @Test func discoveryReportsTheServersIdentity() throws {
        let identity = try HTTPClient.validateDiscovery(registry: registry(), health: health())
        #expect(identity == ServerIdentity(id: Self.serverID, instance: "test-instance", kitVersion: "0.42.0",
                                           protocolVersion: kitWireVersion, machine: Self.machineID, home: "/Users/kit"))
    }

    @Test func aServerOfAnotherProtocolIsIncompatibleWhateverItsHealthHolds() throws {
        // An older server may predate the identity fields; a newer one may add fields.
        let older = try health(protocolNumber: 40, identity: "")
        let newer = try health(protocolNumber: kitWireVersion + 1, identity: #","capabilities":{"terminals":true}"#)
        for (health, reason) in [(older, DaemonCompatibility.Mismatch.daemonProtocolOlder), (newer, .clientProtocolOlder)] {
            do {
                try HTTPClient.validateDiscovery(registry: registry(protocolNumber: health.protocolVersion), health: health)
                Issue.record("A server of another protocol must not attach")
            } catch ClientError.incompatibleDaemon(_, _, let daemonProtocol, let mismatch) {
                #expect(daemonProtocol == health.protocolVersion)
                #expect(mismatch == reason)
            }
        }
    }

    @Test func aCompatibleServerMustIdentifyItself() throws {
        for identity in ["", #","serverId":"server_x","machineId":"\#(Self.machineID)","home":"/Users/kit""#,
                         #","serverId":"\#(Self.serverID)","machineId":"AB","home":"/Users/kit""#,
                         #","serverId":"\#(Self.serverID)","machineId":"\#(Self.machineID)","home":"Users/kit""#] {
            do {
                try HTTPClient.validateDiscovery(registry: registry(), health: health(identity: identity))
                Issue.record("A server without a valid identity must not attach")
            } catch ClientError.invalidPayload {}
        }
    }

    @Test func discoveryAuthenticatesIdentityAndReadinessBeforeNegotiation() throws {
        for version in ["0.42.0", "0.43.0-rc.1", "dev"] {
            try HTTPClient.validateDiscovery(registry: registry(version: version), health: health(version: version))
        }
        for altered in [try health(version: "0.43.0"), try health(protocolNumber: 44),
                        try health(instance: "another"), try health(pid: 124)] {
            do {
                try HTTPClient.validateDiscovery(registry: registry(), health: altered)
                Issue.record("A mismatched server identity must not attach")
            } catch ClientError.invalidPayload { /* registry and health must agree */ }
        }
        do {
            try HTTPClient.validateDiscovery(registry: registry(), health: health(ready: false))
            Issue.record("An unready database must not attach")
        } catch ClientError.daemonNotReady { /* leave the server alone */ }
        do {
            try HTTPClient.validateDiscovery(registry: registry(protocolNumber: 44), health: health(protocolNumber: 44))
            Issue.record("An older server protocol must not attach")
        } catch ClientError.incompatibleDaemon(let daemon, let appProtocol, let daemonProtocol, let reason) {
            #expect(daemon == "0.42.0")
            #expect(appProtocol == kitWireVersion)
            #expect(daemonProtocol == 44)
            #expect(reason == .daemonProtocolOlder)
        }
    }
}
