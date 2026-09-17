import Foundation

/// Projects whole-document captures into unified-diff rows, retaining the prefix column.
enum ReviewHighlighting {
    static func rows(patch: String, before: String, after: String,
                     beforeSpans: [SyntaxSpan], afterSpans: [SyntaxSpan]) -> [[SyntaxSpan]] {
        func ranges(_ source: String) -> [NSRange] {
            var offset = 0
            return source.components(separatedBy: "\n").map { line in
                defer { offset += line.utf16.count + 1 }
                return NSRange(location: offset, length: line.utf16.count)
            }
        }
        let oldRanges = ranges(before), newRanges = ranges(after)
        let header = try! NSRegularExpression(pattern: #"^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@"#)
        var oldLine = 0, newLine = 0, inHunk = false
        return patch.components(separatedBy: "\n").map { row in
            let ns = row as NSString
            if let match = header.firstMatch(in: row, range: NSRange(location: 0, length: ns.length)) {
                oldLine = Int(ns.substring(with: match.range(at: 1)))! - 1
                newLine = Int(ns.substring(with: match.range(at: 2)))! - 1
                inHunk = true
                return []
            }
            guard inHunk, let prefix = row.first, " +-".contains(prefix) else { return [] }
            let removed = prefix == "-"
            let line = removed ? oldLine : newLine
            let sourceRanges = removed ? oldRanges : newRanges
            let spans = removed ? beforeSpans : afterSpans
            defer {
                if prefix != "+" { oldLine += 1 }
                if prefix != "-" { newLine += 1 }
            }
            guard sourceRanges.indices.contains(line) else { return [] }
            let range = sourceRanges[line]
            // Mismatched snapshots must not color unrelated text.
            let source = (removed ? before : after) as NSString
            guard source.substring(with: range) == String(row.dropFirst()) else { return [] }
            return spans.compactMap { span in
                let intersection = NSIntersectionRange(range, span.range)
                guard intersection.length > 0 else { return nil }
                return SyntaxSpan(range: NSRange(location: intersection.location - range.location + 1,
                                                length: intersection.length), role: span.role)
            }
        }
    }
}
