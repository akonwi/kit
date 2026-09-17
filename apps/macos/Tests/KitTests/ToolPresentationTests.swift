import Testing
@testable import Kit

struct ToolPresentationTests {
    private func tool(_ name: String, _ args: String, output: String = "", failed: Bool = false) -> ToolActivity {
        ToolActivity(id: "tool", name: name, summary: name, output: output, arguments: args, failed: failed)
    }
    @Test func readRangeAndLanguageUseRecordedArguments() {
        let value = ToolPresentation(tool("read", #"{"path":"internal/model.go","offset":120,"limit":60}"#, output: "old content"))
        #expect(value.summary == "internal/model.go:120–120")
        #expect(value.startLine == 120)
        #expect(value.language == "go")
    }
    @Test func readTruncationIsSeparateFromFileLines() {
        let value = ToolPresentation(tool("read", #"{"path":"a.go","offset":10,"limit":50}"#, output: "a\nb\n[truncated]"))
        #expect(value.summary == "a.go:10–11 · truncated")
        #expect(value.readContent == "a\nb")
        #expect(value.readTruncated)
    }
    @Test func skillMetadataBecomesDescriptionAndBody() {
        let value = SkillContent("---\nname: test\ndescription: >\n  First line\n  second line\n---\n\n# Instructions\nDo this.")
        #expect(value.description == "First line second line")
        #expect(value.body == "# Instructions\nDo this.")
        let plain = SkillContent("---\nUnclosed section")
        #expect(plain.body == "---\nUnclosed section")
    }
    @Test func newlineChangesRemainVisible() {
        #expect(ToolPresentation.Edit(old: "a", new: "a\n").newlineNotice == "Final newline added")
        #expect(ToolPresentation.Edit(old: "a\n", new: "a").newlineNotice == "Final newline removed")
        #expect(ToolPresentation.Edit(old: "a\n", new: "b\n").newlineNotice == nil)
    }
    @Test func detailHeadersOnlyAddNewInformation() {
        for name in ["read", "write", "edit", "grep", "find", "ls", "change_cwd"] {
            #expect(ToolPresentation(tool(name, #"{"path":"a.go"}"#)).header == "")
        }
    }

    @Test func writeCountAndFailureLabels() {
        let args = #"{"path":"main.swift","content":"let x = 1\nlet y = 2\n"}"#
        #expect(ToolPresentation(tool("write", args)).title == "Write 2 lines")
        #expect(ToolPresentation(tool("write", args, failed: true)).title == "Write file")
    }
    @Test func editDiffRetainsContextAndReplacement() {
        let value = ToolPresentation(tool("edit", #"{"path":"main.go","edits":[{"oldText":"first\nold\nlast","newText":"first\nnew\nlast"}]}"#))
        #expect(value.title == "Edit 1 section")
        #expect(ToolDiffLine.build(value.edits[0]) == [
            .init(kind: .context, text: "first"), .init(kind: .removed, text: "old"),
            .init(kind: .added, text: "new"), .init(kind: .context, text: "last")
        ])
    }
    @Test func diffLineNumbersFollowEachSide() {
        let rows = ToolDiffLine.numbered(.init(old: "first\nold\nlast", new: "first\nnew\nextra\nlast"))
        #expect(rows.map(\.oldNumber) == [1, 2, nil, nil, 3])
        #expect(rows.map(\.newNumber) == [1, nil, 2, 3, 4])
    }

    @Test func legacyAndMalformedEdits() {
        #expect(ToolPresentation(tool("edit", #"{"oldText":"a","newText":"b"}"#)).edits == [.init(old: "a", new: "b")])
        #expect(ToolPresentation(tool("edit", #"{"edits":[{"oldText":1}]}"#)).edits == [])
        let edit = ToolPresentation.Edit(old: "", new: "hello")
        #expect(ToolDiffLine.build(edit) == [.init(kind: .added, text: "hello")])
    }
    @Test func searchPreservesUnstructuredResults() {
        let args = #"{"pattern":"todo","path":"src"}"#
        let value = ToolPresentation(tool("grep", args, output: "src/a.go:12:todo: fix\nsrc/a.go:20:todo again\n"))
        #expect(value.summary == "todo in src")
        #expect(value.matches == [.init(path: "src/a.go", line: 12, text: "todo: fix"), .init(path: "src/a.go", line: 20, text: "todo again")])
        #expect(ToolPresentation(tool("grep", args, output: "No matches")).matches == nil)
    }
    @Test func skillAndAgentNamesAreMeaningful() {
        #expect(ToolPresentation(tool("activate_skill", #"{"name":"vaxis-ui"}"#)).summary == "vaxis-ui")
        let value = ToolPresentation(tool("subagent", #"{"action":"run","agent":"reviewer","message":"Review the diff"}"#))
        #expect(value.title == "Start agent")
        #expect(value.header == "Review the diff")
    }
}
