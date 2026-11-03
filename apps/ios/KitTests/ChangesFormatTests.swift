import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct ChangesFormatTests {
    @Test func countsReadAsTheMenuAndSummaryShowThem() {
        #expect(ChangesFormat.files(1, more: false) == "1 file")
        #expect(ChangesFormat.files(4, more: false) == "4 files")
        #expect(ChangesFormat.files(100, more: true) == "100+ files")
        #expect(ChangesFormat.lines(48, 12) == "+48 −12")
        #expect(ChangesFormat.lines(3, nil) == "+3")
        #expect(ChangesFormat.lines(nil, nil) == nil)
    }

    @Test func aPathSplitsIntoItsFolderAndName() {
        #expect(ChangesFormat.split("Client/HTTPClient.swift") == ("Client/", "HTTPClient.swift"))
        #expect(ChangesFormat.split("apps/ios/Kit/Theme.swift") == ("apps/ios/Kit/", "Theme.swift"))
        #expect(ChangesFormat.split("README.md") == ("", "README.md"))
    }

    @Test func targetsNameThemselvesInThePillAndTheSheet() throws {
        let working = try Self.target(kind: "working_tree", label: "Working tree")
        let branch = try Self.target(kind: "branch", label: "fix/sse-resume vs main")
        let commit = try Self.target(kind: "commit", label: "a41c9e2  fix(client): keep the event cursor",
                                     subject: "fix(client): keep the event cursor", oid: "a41c9e2")
        #expect(ChangesFormat.pill(working) == "Uncommitted")
        #expect(ChangesFormat.pill(branch) == "This branch")
        #expect(ChangesFormat.pill(commit) == "a41c9e2")
        #expect(ChangesFormat.title(working) == "Uncommitted changes")
        #expect(ChangesFormat.title(branch) == "This branch")
        #expect(ChangesFormat.detail(branch) == "fix/sse-resume vs main")
        #expect(ChangesFormat.title(commit) == "fix(client): keep the event cursor")
        #expect(CompareSheet.filter([working, branch, commit], query: "cursor A41C").map(\.kind) == ["commit"])
        #expect(CompareSheet.filter([working, branch, commit], query: "").count == 3)
    }

    static func target(kind: String, label: String, subject: String? = nil, oid: String? = nil) throws -> WireDiffTargetEntry {
        let endpoint = #"{"kind":"working_tree"}"#
        var metadata = "\"label\":\"\(label)\""
        if let subject { metadata += ",\"subject\":\"\(subject)\"" }
        if let oid { metadata += ",\"abbreviatedOid\":\"\(oid)\"" }
        let json = "{\"reference\":\"\(kind)\",\"targetId\":\"difftarget_\(kind)\",\"kind\":\"\(kind)\",\"base\":\(endpoint),\"head\":\(endpoint),\"metadata\":{\(metadata)}}"
        return try JSONDecoder().decode(WireDiffTargetEntry.self, from: Data(json.utf8))
    }
}
