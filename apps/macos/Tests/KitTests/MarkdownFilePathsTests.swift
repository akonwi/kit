import KitClient
import Testing

struct MarkdownFilePathsTests {
    @Test func markdownFilesAreKnownByTheirExtension() {
        #expect(MarkdownFilePaths.isMarkdown("README.md"))
        #expect(MarkdownFilePaths.isMarkdown("docs/Guide.MARKDOWN"))
        #expect(MarkdownFilePaths.isMarkdown("~/notes.mdown"))
        #expect(!MarkdownFilePaths.isMarkdown("notes.txt"))
        #expect(!MarkdownFilePaths.isMarkdown(".md"))
        #expect(!MarkdownFilePaths.isMarkdown("md"))
    }
}
