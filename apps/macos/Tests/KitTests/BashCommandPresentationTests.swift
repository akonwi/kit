import Testing
@testable import Kit

struct BashCommandPresentationTests {
    @Test(arguments: [
        ("cd /Users/akonwi/Developer/agent/kit-v2 && git branch --show-current && git worktree list && ls ..", "cd · git · git · ls"),
        ("grep x app | head -10; grep y app | head", "grep → head · grep → head"),
        ("printf '%s' 'a|b;c'", "printf '%s' 'a|b;c'"),
        ("echo $(printf 'a|b') | sed 's/a/b/'", "echo → sed"),
        ("pwd\nbun test", "shell script · 2 commands"),
        ("sleep 1 & echo done", "sleep · echo"),
        ("echo ok # note | sed x", "echo ok # note | sed x"),
        ("for x in a b; do echo $x; done", "shell command · 3 steps"),
        ("cat <<EOF\na | b\nEOF", "shell script · 3 lines"),
        ("FOO=\"a b\" grep x | head", "grep → head"),
        ("cat <&0 | wc", "cat → wc"),
        ("echo foo \\\n bar", "shell script · 2 lines"),
        ("my\\ command | head", "my\\ command → head")
    ])
    func matchesTUIPresentation(sample: (String, String)) {
        #expect(BashCommandPresentation(sample.0).text == sample.1)
    }

    @Test func fullCommandComesFromArguments() {
        let tool = ToolActivity(id: "1", name: "bash", summary: "truncated…", output: "", arguments: #"{"command":"pwd && ls"}"#, failed: false)
        #expect(tool.bashCommand == "pwd && ls")
    }
}
