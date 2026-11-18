import Foundation
import KitClient
import Testing
@testable import Kit

/// A shell command's row in the transcript.
struct BashRowTests {
    private func execution(status: WireBashExecutionStatus, exitCode: Int? = nil, timedOut: Bool = false,
                           error: String? = nil) throws -> BashExecution {
        let running = status == .value0
        return try BashExecution(.init(id: "bash_1", sessionId: "s", sequence: 1, command: "go vet ./...", status: status,
                                       output: "", exitCode: exitCode, excludeFromContext: false, truncated: false,
                                       timedOut: timedOut, errorMessage: error, startedAt: "2026-11-18T00:00:00Z",
                                       completedAt: running ? nil : "2026-11-18T00:00:01Z"), session: "s")
    }

    @Test func aStatusShowsOnlyWhenTheCommandDidntSimplySucceed() throws {
        #expect(BashRow.status(of: try execution(status: .value0)) == nil)
        #expect(BashRow.status(of: try execution(status: .value1, exitCode: 0)) == nil)
        #expect(BashRow.status(of: try execution(status: .value1, exitCode: 1)) == "Exit 1")
        #expect(BashRow.status(of: try execution(status: .value1, exitCode: 0, timedOut: true)) == "Timed out")
    }
}
