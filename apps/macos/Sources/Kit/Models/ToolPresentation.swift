import Foundation

/// What a tool row's chip opens.
enum ToolLink: Equatable {
    /// A file the call names, as recorded; see `ToolPresentation.workspacePath`.
    case file(String)
    case agent(String)
    case session(String)
    case scratchpad
}

/// A short result shown on the right of a tool row, such as `exit 1` or `4 matches`.
struct ToolFact: Equatable {
    enum Tone: Equatable { case muted, text, success, danger }
    let text: String
    var tone: Tone = .muted
    /// Facts drawn in the monospaced face: counts, codes, and durations.
    var monospaced = true
}

/// Presentation of recorded tool data; never reads the current workspace file.
/// Titles and chips follow the terminal client's `toolpresentation` package.
struct ToolPresentation {
    struct Edit: Equatable { let old: String; let new: String }
    struct Match: Equatable { let path: String; let line: Int; let text: String }
    let tool: ToolActivity
    private let args: [String: Any]
    private let detailValues: [String: Any]
    /// The MCP server whose tool this call is, if any.
    let server: String?

    init(_ tool: ToolActivity, servers: [String] = []) {
        self.tool = tool
        args = Self.object(tool.arguments)
        detailValues = Self.object(tool.details)
        server = servers.first { Self.mcpToolName($0) == tool.name }
    }
    private static func object(_ text: String?) -> [String: Any] {
        text.flatMap { $0.data(using: .utf8) }
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
    }
    func string(_ key: String) -> String? { args[key] as? String }
    private func text(_ key: String) -> String? {
        guard let value = string(key)?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        return value
    }
    private func integer(_ key: String) -> Int? {
        (detailValues[key] as? NSNumber).map(\.intValue)
    }
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
    private var kind: String { tool.name.lowercased() }
    /// A plugin tool's plugin and local name, from `<plugin>__<tool>`.
    var pluginTool: (plugin: String, tool: String)? {
        guard server == nil, let match = tool.name.range(of: #"^[a-z][a-z0-9-]{0,31}__.+$"#, options: .regularExpression),
              match == tool.name.startIndex..<tool.name.endIndex, let split = tool.name.range(of: "__") else { return nil }
        return (String(tool.name[..<split.lowerBound]), String(tool.name[split.upperBound...]))
    }
    static let interactionTools: Set<String> = ["confirm_from_user", "input_from_user", "select_from_user", "guided_questions"]

    var title: String {
        if let server {
            switch string("action") {
            case "list": return "List \(server)"
            case "search": return "Search \(server)"
            case "describe": return "Describe \(server)"
            case "call": return "Call \(server)"
            case "logout": return "Log out of \(server)"
            default: return server
            }
        }
        if tool.bashCommand != nil { return "Run command" }
        switch kind {
        case "peer_session":
            switch string("action") {
            case "discover": return "Discover sessions"
            case "send": return "Ask session"
            case "inspect": return "Inspect peer query"
            case "wait": return "Wait for peer query"
            default: return "Peer session"
            }
        case "create_session": return "Create session"
        case "change_cwd": return "Change directory"
        case "read": return "Read file"
        case "write":
            if let content = string("content") { return "Write \(Self.lineLabel(Self.recordedLineCount(content)))" }
            return "Write file"
        case "edit":
            switch edits.count {
            case 0: return "Edit file"
            case 1: return "Edit 1 section"
            default: return "Edit \(edits.count) sections"
            }
        case "edit_scratchpad": return "Update scratchpad"
        case "read_scratchpad": return "Read scratchpad"
        case "grep": return "Search"
        case "find", "glob": return "Find files"
        case "ls": return "List directory"
        case "activate_skill": return "Load skill"
        case "subagent":
            switch string("action") {
            case "start", "run", "spawn": return "Start agent"
            case "message", "send": return "Message agent"
            case "wait": return "Wait for agent"
            case "cancel": return "Cancel agent"
            case "dismiss": return "Dismiss agent"
            case "inspect": return "Inspect agent"
            default: return "Inspect agents"
            }
        case "confirm_from_user": return "Confirm"
        case "input_from_user", "select_from_user": return "Ask"
        case "guided_questions": return "Ask questions"
        case "show_image": return "Show image"
        case "open_url": return "Open URL"
        case "inspect_image": return "Inspect image"
        case "subagent_inbox": return "Check inbox"
        case "subagent_send": return "Message sibling"
        case "subagent_reply": return "Reply"
        case "subagent_inspect": return "Inspect request"
        default: return Self.humanize(pluginTool?.tool ?? tool.name)
        }
    }

    /// The chip's text; nil when the call has nothing to show there.
    var chip: String? {
        if server != nil {
            switch string("action") {
            case "search": return text("query")
            case "describe", "call": return text("tool")
            case "list", "logout": return nil
            default: return fallbackArguments
            }
        }
        if let command = tool.bashCommand { return BashCommandPresentation(command).text }
        switch kind {
        case "read":
            guard !tool.failed else { return text("path") }
            let count = Self.lines(readContent).count
            let range = count > 0 ? ":\(startLine)–\(startLine + count - 1)" : " · empty"
            return (text("path") ?? "") + range + (readTruncated ? " · truncated" : "")
        case "write", "edit", "ls", "change_cwd": return text("path") ?? fallbackArguments
        case "grep", "find", "glob":
            let scope = text("path") ?? "."
            return text("pattern").map { "\($0) in \(scope)" } ?? scope
        case "activate_skill": return text("name")
        case "subagent", "subagent_send": return text("agent")
        case "subagent_reply", "subagent_inspect": return text("receipt")
        case "subagent_inbox", "read_scratchpad": return nil
        case "edit_scratchpad": return edits.count == 1 ? "1 edit" : "\(edits.count) edits"
        case "peer_session":
            if string("action") == "discover" { return "available sessions" }
            return text("message").map(Self.oneLine) ?? text("requestId") ?? text("sessionId")
        case "create_session": return CreatedSessionPresentation(tool).summary
        case "show_image": return text("path") ?? text("caption")
        case "open_url": return text("url")
        case "inspect_image": return text("path")
        default:
            if Self.interactionTools.contains(kind) { return text("title").map(Self.oneLine) }
            if let plugin = pluginTool { return plugin.plugin }
            return fallbackArguments
        }
    }
    /// The chip's text, or empty.
    var summary: String { chip ?? "" }
    /// Whether the chip leads with a path, whose end is kept when it is cut.
    var chipIsPath: Bool { ["read", "write", "edit", "ls", "change_cwd", "show_image", "inspect_image"].contains(kind) }

    private var fallbackArguments: String? {
        for key in ["command", "path", "agent", "url", "query", "name"] {
            if let value = text(key) { return Self.oneLine(value) }
        }
        let raw = (tool.arguments ?? "").split(whereSeparator: \.isWhitespace).joined(separator: " ")
        return raw.isEmpty || raw == "{}" ? nil : raw
    }

    /// What the chip opens, if anything.
    var link: ToolLink? {
        guard tool.notRun != true else { return nil }
        switch kind {
        case "read", "write", "edit":
            return text("path").map(ToolLink.file)
        case "subagent":
            return text("agent").map(ToolLink.agent)
        case "edit_scratchpad": return .scratchpad
        case "create_session": return CreatedSessionPresentation(tool).sessionID.map(ToolLink.session)
        case "peer_session": return PeerSessionPresentation(tool).recipientID.map(ToolLink.session)
        default: return nil
        }
    }

    /// Results shown on the right, without the duration.
    var facts: [ToolFact] {
        guard !tool.running, tool.notRun != true else { return [] }
        if tool.bashCommand != nil {
            if detailValues["timedOut"] as? Bool == true { return [ToolFact(text: "timed out", tone: .danger)] }
            if let code = integer("exitCode"), code != 0 { return [ToolFact(text: "exit \(code)", tone: .danger)] }
            return []
        }
        if tool.failed { return [] }
        switch kind {
        case "edit", "edit_scratchpad":
            let lines = edits.flatMap(ToolDiffLine.build)
            let added = lines.filter { $0.kind == .added }.count, removed = lines.filter { $0.kind == .removed }.count
            guard added + removed > 0 else { return [] }
            return [ToolFact(text: "+\(added)", tone: .success), ToolFact(text: "−\(removed)", tone: .danger)]
        case "grep":
            guard let count = integer("matchCount") ?? matches?.count else { return [] }
            return [ToolFact(text: count == 1 ? "1 match" : "\(count) matches")]
        case "find", "glob":
            guard let count = integer("count") else { return [] }
            return [ToolFact(text: count == 1 ? "1 file" : "\(count) files")]
        case "ls":
            guard let count = integer("count") else { return [] }
            return [ToolFact(text: count == 1 ? "1 entry" : "\(count) entries")]
        case "peer_session":
            let status = PeerSessionPresentation(tool).status
            return status == "Completed" ? [] : [ToolFact(text: status, monospaced: false)]
        default:
            if Self.interactionTools.contains(kind), let outcome = ask.outcome {
                return [ToolFact(text: outcome, tone: outcome == "Cancelled" ? .muted : .text, monospaced: false)]
            }
            return []
        }
    }

    /// Whether the row shows how long the call ran: slow, external work.
    var showsDuration: Bool {
        tool.bashCommand != nil || server != nil || pluginTool != nil || kind == "subagent"
            || (kind == "peer_session" && string("action") == "wait")
    }

    /// Whether the row opens a detail well.
    var expandable: Bool {
        if tool.notRun == true { return false }
        switch kind {
        case "change_cwd", "read_scratchpad", "activate_skill", "subagent_inbox": return false
        case "show_image": return tool.failed
        case "confirm_from_user", "input_from_user", "select_from_user":
            return tool.failed || ask.detail != nil || ask.answer != nil
        default: return true
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
    var ask: AskPresentation { AskPresentation(kind: kind, arguments: args, output: tool.output, details: detailValues) }

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
    static func recordedLineCount(_ value: String) -> Int {
        guard !value.isEmpty else { return 0 }
        return value.filter { $0 == "\n" }.count + (value.hasSuffix("\n") ? 0 : 1)
    }
    static func lineLabel(_ count: Int) -> String { count == 1 ? "1 line" : "\(count) lines" }
    static func oneLine(_ value: String) -> String {
        value.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }
    /// A tool name in sentence case: `fetch_url` reads "Fetch url".
    static func humanize(_ name: String) -> String {
        let words = name.replacingOccurrences(of: "_", with: " ").replacingOccurrences(of: "-", with: " ")
            .split(separator: " ").map { $0.lowercased() }
        guard let first = words.first else { return "Tool call" }
        return ([first.prefix(1).uppercased() + first.dropFirst()] + words.dropFirst()).joined(separator: " ")
    }
    /// The name a model calls an MCP server's tool by, as Kit's MCP manager
    /// derives it: lowercase ASCII letters, digits, `_` and `-`, with each run
    /// of other characters as one `_`, trimmed of `_` and `-`.
    static func mcpToolName(_ server: String) -> String {
        var name = "", underscore = false
        for character in server.lowercased() {
            if character.isASCII, character.isLetter || character.isNumber || character == "_" || character == "-" {
                name.append(character); underscore = false
            } else if !underscore {
                name.append("_"); underscore = true
            }
        }
        return name.trimmingCharacters(in: CharacterSet(charactersIn: "_-"))
    }
    /// A recorded path relative to the working directory `cwd`, as workspace
    /// files are named, or nil when it lies outside it.
    static func workspacePath(_ path: String, cwd: String?) -> String? {
        guard let cwd, cwd.hasPrefix("/"), !path.isEmpty, !path.contains("\0") else { return nil }
        let base = URL(fileURLWithPath: cwd).standardizedFileURL.path
        let absolute = (path.hasPrefix("/") ? URL(fileURLWithPath: path) : URL(fileURLWithPath: base).appendingPathComponent(path))
            .standardizedFileURL.path
        guard absolute.hasPrefix(base + "/") else { return nil }
        let relative = String(absolute.dropFirst(base.count + 1))
        return relative.isEmpty ? nil : relative
    }
}

/// The question a user-interaction tool asked and the answer it recorded.
struct AskPresentation {
    struct Answer: Equatable {
        let question: String
        /// Nil when the question was skipped.
        let answer: String?
    }
    let title: String?
    let detail: String?
    /// The answer as a short fact: Yes, No, the choice, the text, or Cancelled.
    let outcome: String?
    /// The full free-text answer of an input request.
    let answer: String?
    /// The guided questions in order, each with its answer.
    let answers: [Answer]

    init(kind: String, arguments: [String: Any], output: String, details: [String: Any]) {
        func nonEmpty(_ value: Any?) -> String? {
            guard let value = (value as? String)?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
            return value
        }
        title = nonEmpty(arguments["title"])
        detail = nonEmpty(arguments["detail"])
        let result = output.data(using: .utf8).flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
        let resolution = details["resolution"] as? String
        let cancelled = result?["cancelled"] as? Bool == true || resolution == "user_cancelled"
        var outcome: String?, answer: String?, answers: [Answer] = []
        if let result {
            switch kind {
            case "confirm_from_user":
                if result["confirmed"] as? Bool == true { outcome = "Yes" }
                else if resolution == "negative_confirmation" { outcome = "No" }
                else { outcome = "Cancelled" }
            case "input_from_user":
                answer = cancelled ? nil : nonEmpty(result["value"])
                outcome = cancelled ? "Cancelled" : answer.map(ToolPresentation.oneLine)
            case "select_from_user":
                outcome = cancelled ? "Cancelled" : nonEmpty(result["label"]) ?? nonEmpty(result["value"])
            case "guided_questions":
                if cancelled { outcome = "Cancelled" }
                let values = result["answers"] as? [String: Any] ?? [:]
                for question in arguments["questions"] as? [[String: Any]] ?? [] {
                    guard let id = question["id"] as? String, let prompt = nonEmpty(question["prompt"]) else { continue }
                    let options = question["options"] as? [[String: Any]] ?? []
                    func label(_ value: String) -> String {
                        options.first { $0["value"] as? String == value }.flatMap { $0["label"] as? String } ?? value
                    }
                    let text: String? = switch values[id] {
                    case let value as Bool where question["kind"] as? String == "boolean": value ? "Yes" : "No"
                    case let value as String: label(value)
                    case let value as [String]: value.isEmpty ? nil : value.map(label).joined(separator: ", ")
                    default: nil
                    }
                    answers.append(Answer(question: prompt, answer: text))
                }
            default: break
            }
        }
        self.outcome = outcome
        self.answer = answer
        self.answers = answers
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
