import Foundation
import Testing
@testable import Kit

/// The facts, links, and disclosure of tool rows.
struct ToolRowPresentationTests {
    private func tool(_ name: String, _ args: String = "{}", output: String = "", details: String? = nil,
                      failed: Bool = false, status: String? = "Completed") -> ToolActivity {
        ToolActivity(id: "tool", name: name, summary: name, output: output, arguments: args, failed: failed,
                     status: status, details: details)
    }

    @Test func commandsReportOnlyAFailingExit() {
        let command = #"{"command":"go test ./..."}"#
        #expect(ToolPresentation(tool("bash", command, details: #"{"exitCode":0}"#)).facts == [])
        #expect(ToolPresentation(tool("bash", command, details: #"{"exitCode":1}"#, failed: true)).facts
            == [ToolFact(text: "exit 1", tone: .danger)])
        #expect(ToolPresentation(tool("bash", command, details: #"{"timedOut":true}"#, failed: true)).facts
            == [ToolFact(text: "timed out", tone: .danger)])
        #expect(ToolPresentation(tool("bash", command)).showsDuration)
        #expect(!ToolPresentation(tool("read", #"{"path":"a.go"}"#)).showsDuration)
    }

    @Test func editsCountChangedLines() {
        let args = #"{"path":"a.go","edits":[{"oldText":"a\nb","newText":"a\nc\nd"}]}"#
        #expect(ToolPresentation(tool("edit", args)).facts
            == [ToolFact(text: "+2", tone: .success), ToolFact(text: "−1", tone: .danger)])
    }

    @Test func searchesCountWhatTheyFound() {
        #expect(ToolPresentation(tool("grep", #"{"pattern":"x"}"#, output: "a.go:1:x\nb.go:4:x")).facts
            == [ToolFact(text: "2 matches")])
        #expect(ToolPresentation(tool("find", #"{"pattern":"*.go"}"#, details: #"{"count":1}"#)).facts
            == [ToolFact(text: "1 file")])
        #expect(ToolPresentation(tool("ls", #"{"path":"."}"#, details: #"{"count":12}"#)).facts
            == [ToolFact(text: "12 entries")])
    }

    @Test func runningAndUnrunCallsShowNoFacts() {
        let running = tool("grep", #"{"pattern":"x"}"#, output: "a.go:1:x", status: "Running…")
        #expect(ToolPresentation(running).facts == [])
        var unrun = tool("edit", #"{"path":"a.go","oldText":"a","newText":"b"}"#, status: nil)
        unrun.notRun = true
        #expect(ToolPresentation(unrun).facts == [])
        #expect(!ToolPresentation(unrun).expandable)
    }

    @Test func questionsShowTheAnswerAsTheFact() {
        let confirm = ToolPresentation(tool("confirm_from_user", #"{"title":"Delete?"}"#, output: #"{"confirmed":true}"#))
        #expect(confirm.title == "Confirm")
        #expect(confirm.facts == [ToolFact(text: "Yes", tone: .text, monospaced: false)])
        #expect(!confirm.expandable)
        let declined = ToolPresentation(tool("confirm_from_user", #"{"title":"Delete?"}"#, output: #"{"confirmed":false}"#,
                                             details: #"{"resolution":"negative_confirmation"}"#))
        #expect(declined.facts.map(\.text) == ["No"])
        let select = ToolPresentation(tool("select_from_user", #"{"title":"Pick"}"#, output: #"{"value":"b","label":"Beta"}"#))
        #expect(select.facts.map(\.text) == ["Beta"])
        let input = ToolPresentation(tool("input_from_user", #"{"title":"Name"}"#, output: #"{"cancelled":true}"#))
        #expect(input.facts == [ToolFact(text: "Cancelled", tone: .muted, monospaced: false)])
        let questions = #"{"title":"Setup","questions":[{"id":"a","prompt":"Mode?","kind":"select","options":[{"label":"Fast","value":"f"}]},{"id":"b","prompt":"Tests?","kind":"boolean"},{"id":"c","prompt":"Notes?","kind":"text"}]}"#
        let guided = ToolPresentation(tool("guided_questions", questions, output: #"{"answers":{"a":"f","b":false}}"#))
        #expect(guided.ask.answers == [
            AskPresentation.Answer(question: "Mode?", answer: "Fast"),
            AskPresentation.Answer(question: "Tests?", answer: "No"),
            AskPresentation.Answer(question: "Notes?", answer: nil),
        ])
    }

    @Test func chipsLinkWhatTheyName() {
        #expect(ToolPresentation(tool("read", #"{"path":"Sources/a.swift"}"#)).link == .file("Sources/a.swift"))
        #expect(ToolPresentation(tool("subagent", #"{"agent":"reviewer","message":"Look"}"#)).link == .agent("reviewer"))
        #expect(ToolPresentation(tool("edit_scratchpad", #"{"edits":[]}"#)).link == .scratchpad)
        #expect(ToolPresentation(tool("grep", #"{"pattern":"x"}"#)).link == nil)
    }

    @Test func quickLookupsHaveNoWell() {
        for name in ["read_scratchpad", "activate_skill", "change_cwd"] {
            #expect(!ToolPresentation(tool(name, #"{"name":"design"}"#, output: "text")).expandable)
        }
        #expect(ToolPresentation(tool("read", #"{"path":"a.go"}"#, output: "text")).expandable)
    }

    @Test func workspacePathsAreRelativeToTheWorkingDirectory() {
        #expect(ToolPresentation.workspacePath("Sources/a.swift", cwd: "/repo") == "Sources/a.swift")
        #expect(ToolPresentation.workspacePath("/repo/./Sources/../a.swift", cwd: "/repo/") == "a.swift")
        #expect(ToolPresentation.workspacePath("/elsewhere/a.swift", cwd: "/repo") == nil)
        #expect(ToolPresentation.workspacePath("../a.swift", cwd: "/repo") == nil)
        #expect(ToolPresentation.workspacePath("a.swift", cwd: nil) == nil)
    }

    @Test func mcpToolsAreRecognizedByTheirServer() {
        #expect(ToolPresentation.mcpToolName("Paper Design") == "paper_design")
        #expect(ToolPresentation.mcpToolName("--cloudflare.api--") == "cloudflare_api")
        let call = ToolPresentation(tool("paper_design", #"{"action":"call"}"#), servers: ["Paper Design"])
        #expect(call.server == "Paper Design")
        #expect(call.showsDuration)
    }

    @Test func durationsReadInTheLargestUsefulUnits() {
        #expect(ToolTiming.format(0.4) == "<1s")
        #expect(ToolTiming.format(8.9) == "8s")
        #expect(ToolTiming.format(72) == "1m 12s")
        #expect(ToolTiming.format(7500) == "2h 5m")
    }

    @Test func timesSurviveSnapshotsThatCarryNone() {
        let start = Date(timeIntervalSince1970: 10), end = Date(timeIntervalSince1970: 18)
        var live = tool("bash", status: "Running…")
        live.startedAt = start
        var log: [String: ToolTiming] = [:]
        _ = ToolTiming.merge([TranscriptMessage(id: "t", role: "tools", text: "", tools: [live])], log: &log)
        var finished = live
        finished.status = "Completed"
        finished.finishedAt = end
        _ = ToolTiming.merge([TranscriptMessage(id: "t", role: "tools", text: "", tools: [finished])], log: &log)
        let snapshot = TranscriptMessage(id: "t", role: "tools", text: "", tools: [tool("bash")])
        let merged = ToolTiming.merge([snapshot], log: &log)
        #expect(merged[0].tools[0].startedAt == start)
        #expect(merged[0].tools[0].finishedAt == end)
    }
}
