import Foundation

/// Presentation of recorded tool data; never reads the current workspace file.
struct ToolPresentation {
    struct Edit: Equatable { let old: String; let new: String }
    struct Match: Equatable { let path: String; let line: Int; let text: String }
    let tool: ToolActivity
    private let args: [String: Any]

    init(_ tool: ToolActivity) {
        self.tool = tool
        args = tool.arguments.flatMap { $0.data(using: .utf8) }
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
    }
    func string(_ key: String) -> String? { args[key] as? String }
    var path: String { string("path") ?? tool.summary }
    var startLine: Int { max(1, args["offset"] as? Int ?? 1) }
    var limit: Int? { args["limit"] as? Int }
    var edits: [Edit] {
        let values = args["edits"] as? [[String: Any]] ?? [args]
        return values.compactMap { value in
            guard let old = value["oldText"] as? String, let new = value["newText"] as? String else { return nil }
            return Edit(old: old, new: new)
        }
    }
    var language: String {
        switch (path as NSString).pathExtension.lowercased() {
        case "go": return "go"
        case "swift": return "swift"
        case "js", "jsx", "mjs", "cjs": return "javascript"
        case "ts": return "typescript"
        case "tsx": return "tsx"
        case "json": return "json"
        case "py": return "python"
        case "sh", "bash", "zsh": return "bash"
        default: return "text"
        }
    }
    var title: String {
        switch tool.name {
        case "peer_session": return PeerSessionPresentation(tool).title
        case "create_session": return "Create session"
        case "read": return "Read file"
        case "write":
            if !tool.failed, let content = string("content") { return "Write \(Self.lines(content).count) lines" }
            return "Write file"
        case "edit": return edits.isEmpty ? "Edit file" : "Edit \(edits.count) \(edits.count == 1 ? "section" : "sections")"
        case "edit_scratchpad": return "Update scratchpad"
        case "grep": return "Search"
        case "find", "glob": return "Find files"
        case "ls": return "List directory"
        case "activate_skill": return "Load skill"
        case "change_cwd": return "Change directory"
        case "subagent":
            switch string("action") {
            case "run", "spawn": return "Start agent"
            case "send", "message": return "Send message"
            case "wait": return "Wait for agent"
            case "cancel": return "Cancel agent"
            default: return "Inspect agent"
            }
        default: return tool.bashCommand == nil ? tool.name.replacingOccurrences(of: "_", with: " ").capitalized : "Run command"
        }
    }
    var summary: String {
        if let command = tool.bashCommand { return BashCommandPresentation(command).text }
        switch tool.name {
        case "peer_session": return PeerSessionPresentation(tool).summary
        case "create_session": return CreatedSessionPresentation(tool).summary
        case "read":
            guard !tool.failed else { return path }
            let count = Self.lines(readContent).count
            let range = count > 0 ? ":\(startLine)–\(startLine + count - 1)" : " · empty"
            return path + range + (readTruncated ? " · truncated" : "")
        case "grep", "find", "glob": return "\(string("pattern") ?? "*") in \(path)"
        case "activate_skill": return string("name") ?? tool.summary
        case "subagent": return string("agent") ?? tool.summary
        case "edit_scratchpad": return "\(edits.count) \(edits.count == 1 ? "edit" : "edits")"
        default: return path
        }
    }
    var header: String {
        if let command = tool.bashCommand { return command }
        if tool.name == "subagent" { return string("message") ?? "" }
        if tool.name == "activate_skill" { return skill.description ?? "" }
        return ""
    }
    var readTruncated: Bool {
        tool.output.hasSuffix("\n[truncated]") || tool.output.hasSuffix("\n[truncated]\n")
    }
    var readContent: String {
        guard readTruncated, let marker = tool.output.range(of: "\n[truncated]", options: .backwards) else { return tool.output }
        return String(tool.output[..<marker.lowerBound])
    }
    var skill: SkillContent { SkillContent(tool.output) }

    var matches: [Match]? {
        guard tool.name == "grep", !tool.output.isEmpty else { return nil }
        let regex = try! NSRegularExpression(pattern: #"^(.+?):([0-9]+):(.*)$"#)
        var result: [Match] = []
        for line in Self.lines(tool.output) {
            let ns = line as NSString
            guard let match = regex.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)),
                  let number = Int(ns.substring(with: match.range(at: 2))) else { return nil }
            result.append(Match(path: ns.substring(with: match.range(at: 1)), line: number,
                                text: ns.substring(with: match.range(at: 3))))
        }
        return result
    }
    static func lines(_ text: String) -> [String] {
        guard !text.isEmpty else { return [] }
        var lines = text.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        return lines
    }
}

struct ToolDiffLine: Equatable {
    enum Kind { case context, removed, added }
    let kind: Kind
    let text: String

    /// Bound the LCS work; large replacements still retain every old/new line.
    static func build(_ edit: ToolPresentation.Edit) -> [Self] {
        let old = ToolPresentation.lines(edit.old), new = ToolPresentation.lines(edit.new)
        guard old.count <= 400, new.count <= 400 else {
            return old.map { Self(kind: .removed, text: $0) } + new.map { Self(kind: .added, text: $0) }
        }
        let width = new.count + 1
        var lengths = [Int](repeating: 0, count: (old.count + 1) * width)
        for i in old.indices.reversed() {
            for j in new.indices.reversed() {
                lengths[i * width + j] = old[i] == new[j] ? lengths[(i + 1) * width + j + 1] + 1 :
                    max(lengths[(i + 1) * width + j], lengths[i * width + j + 1])
            }
        }
        var i = 0, j = 0
        var result: [Self] = []
        while i < old.count || j < new.count {
            if i < old.count, j < new.count, old[i] == new[j] {
                result.append(Self(kind: .context, text: old[i])); i += 1; j += 1
            } else if i < old.count && (j == new.count || lengths[(i + 1) * width + j] >= lengths[i * width + j + 1]) {
                result.append(Self(kind: .removed, text: old[i])); i += 1
            } else {
                result.append(Self(kind: .added, text: new[j])); j += 1
            }
        }
        return result
    }
}


extension ToolDiffLine {
    struct Numbered: Equatable {
        let line: ToolDiffLine
        let oldNumber: Int?
        let newNumber: Int?
    }

    static func numbered(_ edit: ToolPresentation.Edit) -> [Numbered] {
        var old = 1, new = 1
        return build(edit).map { line in
            let row = Numbered(line: line, oldNumber: line.kind == .added ? nil : old,
                               newNumber: line.kind == .removed ? nil : new)
            if line.kind != .added { old += 1 }
            if line.kind != .removed { new += 1 }
            return row
        }
    }
}


struct SkillContent {
    let description: String?
    let body: String

    init(_ source: String) {
        let lines = source.components(separatedBy: "\n")
        guard lines.first?.trimmingCharacters(in: .whitespacesAndNewlines) == "---",
              let end = lines.dropFirst().firstIndex(where: { $0.trimmingCharacters(in: .whitespacesAndNewlines) == "---" }) else {
            description = nil; body = source; return
        }
        let metadata = Array(lines[1..<end])
        var result: String?
        if let index = metadata.firstIndex(where: { $0.hasPrefix("description:") }) {
            let value = metadata[index].dropFirst("description:".count).trimmingCharacters(in: .whitespacesAndNewlines)
            if ["|", ">", "|-", ">-"].contains(value) {
                result = metadata.dropFirst(index + 1).prefix(while: { $0.hasPrefix(" ") || $0.hasPrefix("\t") })
                    .map { $0.trimmingCharacters(in: .whitespaces) }.joined(separator: value.hasPrefix("|") ? "\n" : " ")
            } else if value.hasPrefix("\""), let data = value.data(using: .utf8),
                      let decoded = try? JSONDecoder().decode(String.self, from: data) {
                result = decoded
            } else {
                result = value.hasPrefix("'") && value.hasSuffix("'") ? String(value.dropFirst().dropLast()).replacingOccurrences(of: "''", with: "'") : value
            }
        }
        description = result?.isEmpty == false ? result : nil
        body = lines.dropFirst(end + 1).joined(separator: "\n").trimmingCharacters(in: .newlines)
    }
}

extension ToolPresentation.Edit {
    var newlineNotice: String? {
        let oldNewline = old.hasSuffix("\n"), newNewline = new.hasSuffix("\n")
        guard oldNewline != newNewline else { return nil }
        return newNewline ? "Final newline added" : "Final newline removed"
    }
}
