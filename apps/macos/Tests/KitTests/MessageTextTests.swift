import Testing
@testable import Kit

struct MessageTextTests {
    @Test func plainTextPreservesReadableStructure() {
        #expect(MessageText.plain("# Heading\n\n**Bold** and `code`\n\n```go\nhello()\n```") == "Heading\n\nBold and code\n\nhello()")
        #expect(MessageText.plain("- First\n- Second") == "• First\n• Second")
        #expect(MessageText.plain("| A | B |\n|---|---|\n| 1 | 2 |") == "A\tB\n1\t2")
    }
}
