import Foundation

/// Native counterpart of internal/tui/transcript_model.go's presentBashCommand.
struct BashCommandPresentation {
    let text: String

    init(_ command: String) {
        let chars = Array(command)
        var parts: [(command: String, pipe: Bool)] = []
        var start = 0
        var index = 0
        var quote: Character?
        var escaped = false
        var nesting = 0
        var complex = false
        func push(_ end: Int, pipe: Bool = false) {
            let value = String(chars[start..<end]).trimmingCharacters(in: .whitespacesAndNewlines)
            if !value.isEmpty { parts.append((value, pipe)) }
        }
        while index < chars.count {
            let c = chars[index]
            let next: Character? = index + 1 < chars.count ? chars[index + 1] : nil
            let previous: Character? = index > 0 ? chars[index - 1] : nil
            defer { index += 1 }
            if escaped { escaped = false; continue }
            if c == "\\", quote != "'" { escaped = true; continue }
            if let current = quote {
                if c == current { quote = nil }
                continue
            }
            if c == "'" || c == "\"" || c == "`" { quote = c; continue }
            if c == "#", previous == nil || " \t\r\n;&|()".contains(previous!) {
                push(index)
                while index < chars.count && chars[index] != "\n" { index += 1 }
                start = min(index + 1, chars.count)
                continue
            }
            if c == "<", previous != "<", next == "<",
               index + 2 >= chars.count || chars[index + 2] != "<" { complex = true }
            if "({[".contains(c) { nesting += 1; continue }
            if ")}]".contains(c) { nesting = max(0, nesting - 1); continue }
            if nesting > 0 { continue }
            if c == "|", next != "|" {
                push(index, pipe: true)
                if next == "&" { index += 1 }
                start = index + 1
                continue
            }
            let separator = c == ";" || c == "\n" || (c == "&" && next == "&") ||
                (c == "&" && next != ">" && previous != ">" && previous != "<") || (c == "|" && next == "|")
            if separator {
                push(index)
                if next == c { index += 1 }
                start = index + 1
            }
        }
        push(chars.count)
        let lineCount = command.split(separator: "\n").filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.count
        complex = complex || parts.contains { ["for", "while", "until", "if", "case", "select", "function"].contains(Self.executable($0.command)) }
        if complex {
            text = lineCount > 1 ? "shell script · \(lineCount) lines" : "shell command · \(max(1, parts.count)) steps"
        } else if command.contains("\n") {
            text = parts.count > 1 ? "shell script · \(parts.count) commands" : "shell script · \(lineCount) lines"
        } else if parts.count <= 1 {
            let trimmed = command.trimmingCharacters(in: .whitespacesAndNewlines)
            text = trimmed.count <= 72 ? trimmed : Self.limited(trimmed.split(whereSeparator: \.isWhitespace).joined(separator: " "))
        } else {
            var summary = Self.executable(parts[0].command)
            for i in 1..<parts.count {
                summary += (parts[i - 1].pipe ? " → " : " · ") + Self.executable(parts[i].command)
            }
            text = Self.limited(summary)
        }
    }

    private static func limited(_ text: String) -> String {
        text.count <= 72 ? text : String(text.prefix(71)).trimmingCharacters(in: .whitespaces) + "…"
    }

    private static func executable(_ command: String) -> String {
        let pattern = #"([^\s"'\\]+|\\.|"[^"]*"|'[^']*')+"#
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return "shell" }
        let words = regex.matches(in: command, range: NSRange(command.startIndex..., in: command))
            .map { (command as NSString).substring(with: $0.range) }
        let word = words.first { $0.range(of: #"^[A-Za-z_][A-Za-z0-9_]*="# , options: .regularExpression) == nil }
        guard let word else { return "shell" }
        return (word.trimmingCharacters(in: CharacterSet(charactersIn: "'\"")) as NSString).lastPathComponent
    }
}

extension ToolActivity {
    var bashCommand: String? {
        guard ["bash", "shell", "exec", "exec_command"].contains(name.lowercased()) else { return nil }
        if let arguments, let data = arguments.data(using: .utf8),
           let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let command = object["command"] as? String ?? object["cmd"] as? String { return command }
        return summary
    }
}
