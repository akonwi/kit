import Foundation

// Mermaid diagrams drawn with box-drawing characters, ported from Kit's
// reference renderer in `apps/cli/tui/mermaid` (ADR 0046). A diagram that is
// unsupported, malformed, or too large isn't drawn, with the reason why.

/// Why a diagram isn't drawn, worded for the reader.
public struct NotDrawn: Error, Equatable, Sendable {
    public let reason: String
    public init(reason: String) { self.reason = reason }
    init(_ reason: String) { self.reason = reason }
}

public enum Mermaid {
    /// The largest source drawn, in UTF-8 bytes.
    static let maxSource = 16384

    /// Whether a fenced code block's language is Mermaid.
    public static func isMermaid(_ language: String) -> Bool {
        Go.toLower(Go.trimSpace(language)) == "mermaid"
    }

    /// Draws source, or explains why it can't be drawn.
    public static func render(_ source: String) -> Result<Canvas, NotDrawn> {
        if source.size > maxSource {
            return .failure(NotDrawn("the source is larger than \(maxSource / 1024) KiB"))
        }
        return renderLines(source)
    }

    private static func firstWord(_ line: String) -> String {
        let space = Go.indexAny(line, " \t")
        return space >= 0 ? line.slice(end: space) ?? line : line
    }

    private static func renderLines(_ source: String) -> Result<Canvas, NotDrawn> {
        let lines = Go.split(Go.replaceAll(source, "\r\n", "\n"), "\n")
        let line = { (index: Int) -> String in index >= 0 && index < lines.count ? lines[index] : "" }
        var index = 0
        var title = ""
        var failure = ""
        while index < lines.count && Go.trimSpace(line(index)) == "" {
            index += 1
        }
        // YAML frontmatter, of which only the title is used.
        if Go.trimSpace(line(index)) == "---" {
            index += 1
            var closed = false
            while !closed && index < lines.count {
                let trimmed = Go.trimSpace(line(index))
                if trimmed == "---" {
                    closed = true
                } else if Go.hasPrefix(line(index), "title:") {
                    title = Go.trim(Go.trimSpace(Go.trimPrefix(trimmed, "title:")), "\"'")
                }
                index += 1
            }
            if !closed { failure = "the frontmatter isn't closed" }
        }
        // The header is the first statement; comments and directives come first.
        var header = ""
        while header == "" && index < lines.count {
            let trimmed = Go.trimSpace(line(index))
            if trimmed != "" && !Go.hasPrefix(trimmed, "%%") { header = trimmed }
            index += 1
        }
        let body = index < lines.count ? Array(lines[index...]) : []
        let kind = firstWord(header)
        do {
            switch true {
            case failure != "": throw NotDrawn(failure)
            case header == "": throw NotDrawn("the diagram is empty")
            case kind == "sequenceDiagram" && header == kind:
                return .success(try Sequence.render(body, title: title))
            case kind == "sequenceDiagram": throw NotDrawn("unexpected text after `sequenceDiagram`")
            case kind == "flowchart" || kind == "graph":
                return .success(try Flowchart.render(header: header, lines: body, title: title))
            default: throw NotDrawn("`\(kind)` diagrams aren't drawn yet")
            }
        } catch let notDrawn as NotDrawn {
            return .failure(notDrawn)
        } catch {
            return .failure(NotDrawn(String(describing: error)))
        }
    }
}

/// value cut to max cells, with an ellipsis when cut.
func cut(_ value: String, to limit: Int) -> String {
    var text = value
    for brk in ["<br/>", "<br />", "<br>", "<BR>", "<BR/>"] {
        text = Go.replaceAll(text, brk, " ")
    }
    text = Go.trimSpace(Go.replaceAll(text, "\t", " "))
    if TextWidth.of(text) <= limit { return text }
    var kept = ""
    var width = 0
    for part in TextWidth.graphemes(text) where width + part.width <= limit - 1 {
        kept += part.text
        width += part.width
    }
    return kept + "…"
}

/// Reads an Ard list element, which falls back to a default out of range.
extension Array {
    func at(_ index: Int) -> Element? {
        index >= 0 && index < count ? self[index] : nil
    }

    /// Replaces the element at index when it exists, as Ard's `List.set`.
    mutating func put(_ index: Int, _ value: Element) {
        if index >= 0 && index < count { self[index] = value }
    }
}
