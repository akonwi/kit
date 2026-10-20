import Foundation
import Observation

/// One pending request's answers and highlight, as the interaction dock
/// presents it. Keys and clicks follow the TUI's dock: a single choice answers
/// at once, a multiple choice toggles, y and n answer a confirmation, and a
/// guided request is sent only after its last question.
@MainActor @Observable
final class InteractionFlow: Identifiable {
    enum Kind { case text, select, multiselect, boolean }
    /// How the current question is answered.
    enum Mode: Equatable { case confirm, text, choice, multiple, unsupported }
    struct Question {
        let prompt: String
        var detail = ""
        var kind: Kind = .text
        var required = true
        var options: [String] = []
        var optionLabels: [String: String] = [:]
        var optionDetails: [String: String] = [:]
    }
    struct Answer {
        var text = ""
        var choices: Set<String> = []
        var skipped = false
    }
    var request: WireInteractionRequest?
    var submitting = false
    var error: String?
    let id = UUID()
    let title: String
    let confirmation: Bool
    /// Questions answered one at a time and sent together.
    let guided: Bool
    /// Why this client can't answer the request; it can only be cancelled.
    let unsupported: String?
    let questions: [Question]
    var answers: [Answer]
    private(set) var step = 0
    /// The highlighted option of the current question.
    var cursor = 0
    /// The highlighted answer of a confirmation that offers a choice.
    var confirming = false
    var question: Question { questions[step] }

    var mode: Mode {
        if unsupported != nil { return .unsupported }
        switch question.kind {
        case .text: return .text
        case .select: return .choice
        case .multiselect: return .multiple
        case .boolean: return .confirm
        }
    }
    var plugin: String? { request?.plugin?.pluginId }
    /// The https page an authorize request asks the user to open. The user
    /// approves access there and pastes the address the browser ends up at.
    var authorizationURL: URL? {
        guard request?.kind.rawValue == "authorize", let raw = request?.url,
              let url = URL(string: raw), url.scheme?.lowercased() == "https", url.host() != nil else { return nil }
        return url
    }
    /// Where the authorization page sends the browser once access is approved.
    var authorizationRedirect: AuthorizationRedirect? {
        authorizationURL.flatMap { AuthorizationRedirect(authorization: $0, redirectURI: request?.redirectUri) }
    }
    /// Answers an authorization with the redirect the browser received.
    func receiveRedirect(_ url: URL) -> Bool {
        guard !submitting, authorizationRedirect?.matches(url) == true else { return false }
        answers[step].text = url.absoluteString
        return submitText()
    }
    /// The request's own detail, shown above a guided question.
    var detail: String { request?.detail ?? (guided ? "" : question.detail) }
    /// Plugin confirmations, and any with a default, highlight one answer
    /// that Return chooses and Tab switches.
    var offersChoice: Bool { !guided && (request?.plugin != nil || request?.defaultValue != nil) }
    var isLastStep: Bool { step == questions.count - 1 }
    /// Whether a question has a recorded answer, for the step trail.
    func answered(_ index: Int) -> Bool {
        let answer = answers[index]
        return !answer.skipped && (!answer.text.isEmpty || !answer.choices.isEmpty)
    }

    init(title: String, confirmation: Bool = false, guided: Bool? = nil, unsupported: String? = nil, questions: [Question]) {
        precondition(!questions.isEmpty)
        self.title = title
        self.confirmation = confirmation
        self.guided = guided ?? (questions.count > 1)
        self.unsupported = unsupported
        self.questions = questions
        answers = questions.map { _ in Answer() }
    }

    // MARK: Answering. Each returns true once the request is ready to send.

    /// Clicks or Return on an option: a single choice answers with it; a
    /// multiple choice toggles it.
    func choose(_ index: Int) -> Bool {
        guard !submitting, question.options.indices.contains(index) else { return false }
        cursor = index
        let option = question.options[index]
        if mode == .multiple {
            if !answers[step].choices.insert(option).inserted { answers[step].choices.remove(option) }
            answers[step].skipped = false
            return false
        }
        answers[step] = Answer(choices: [option])
        return advance()
    }

    /// Answers a multiple choice with what is checked; nothing checked skips
    /// an optional question.
    func continueMultiple() -> Bool {
        guard !submitting else { return false }
        if answers[step].choices.isEmpty { return skip() }
        answers[step].skipped = false
        return advance()
    }

    func confirm(_ value: Bool) -> Bool {
        guard !submitting, mode == .confirm else { return false }
        answers[step] = Answer(choices: [value ? "true" : "false"])
        return advance()
    }

    /// Sends a text answer. Plugins may accept an empty answer; otherwise an
    /// empty answer skips an optional question and is ignored.
    func submitText() -> Bool {
        guard !submitting else { return false }
        let trimmed = answers[step].text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty && authorizationURL != nil { return false }
        if trimmed.isEmpty && request?.plugin == nil { return skip() }
        if authorizationURL != nil { answers[step].text = trimmed }
        if guided { answers[step].text = trimmed }
        answers[step].skipped = false
        return advance()
    }

    func skip() -> Bool {
        guard !submitting, guided, !question.required else { return false }
        answers[step] = Answer(skipped: true)
        return advance()
    }

    /// Tab and Next: answers the current question from its current state. A
    /// yes/no question has no state until it has been answered.
    func next() -> Bool {
        switch mode {
        case .text: return submitText()
        case .choice: return choose(cursor)
        case .multiple: return continueMultiple()
        case .confirm: return answers[step].choices.isEmpty ? false : advance()
        case .unsupported: return false
        }
    }

    func back() {
        guard !submitting, step > 0 else { return }
        show(step - 1)
    }

    func move(_ delta: Int) {
        guard !question.options.isEmpty else { return }
        cursor = min(max(0, cursor + delta), question.options.count - 1)
    }

    private func advance() -> Bool {
        if isLastStep { return true }
        show(step + 1)
        return false
    }

    /// Shows a question with its earlier answer, if any, highlighted.
    func show(_ index: Int) {
        guard questions.indices.contains(index) else { return }
        step = index
        cursor = question.options.firstIndex { answers[index].choices.contains($0) } ?? 0
    }

    var summary: String {
        zip(questions, answers).map { question, answer in
            let value = answer.skipped ? "Skipped" : question.kind == .text ? answer.text : question.options.filter(answer.choices.contains).map { question.optionLabels[$0] ?? $0 }.joined(separator: ", ")
            return "\(question.prompt)\n\(value)"
        }.joined(separator: "\n\n")
    }
    static func approval() -> InteractionFlow {
        .init(title: "Run workspace tests?", confirmation: true, questions: [
            .init(prompt: "Run workspace tests?", detail: "```\ngo test ./internal/subagent/...\n```", kind: .boolean,
                  options: ["true", "false"], optionLabels: ["true": "Allow once", "false": "Deny"])
        ])
    }
    static func planning() -> InteractionFlow {
        .init(title: "Shape the next change", questions: [
            .init(prompt: "Where should the implementation start?", detail: "Choose one area to tackle first.", kind: .select, options: ["Session navigation", "File workspace", "Composer interactions"]),
            .init(prompt: "Which checks should be included?", detail: "Select all that apply.", kind: .multiselect, options: ["Unit tests", "Keyboard navigation", "Light and dark appearances"]),
            .init(prompt: "Anything else to keep in mind?", detail: "Add constraints or context for the implementation.", required: false),
            .init(prompt: "Keep the change isolated in this worktree?", kind: .boolean, options: ["true", "false"], optionLabels: ["true": "Yes", "false": "No"])
        ])
    }
}

extension InteractionFlow {
    /// Builds the flow for a request. A request this client can't present
    /// still gets a flow, so it can be cancelled rather than left waiting.
    convenience init(request: WireInteractionRequest) throws {
        guard request.id.hasPrefix("interaction_") else { throw ClientError.invalidPayload }
        do {
            try self.init(supported: request)
        } catch {
            self.init(title: request.title.isEmpty ? "Request" : request.title, guided: false,
                      unsupported: "This version of Kit can’t answer this request. Update Kit, or cancel it.",
                      questions: [.init(prompt: request.title)])
            self.request = request
        }
    }

    private convenience init(supported request: WireInteractionRequest) throws {
        guard !request.title.isEmpty, (request.questions?.count ?? 0) <= 32 else { throw ClientError.invalidPayload }
        typealias Choices = (ids: [String], labels: [String: String], details: [String: String])
        func choices(_ options: [WireInteractionOption]) throws -> Choices {
            guard options.count <= 64, Set(options.map(\.id)).count == options.count,
                  options.allSatisfy({ !$0.id.isEmpty && !$0.label.isEmpty }) else { throw ClientError.invalidPayload }
            return (options.map(\.id), Dictionary(uniqueKeysWithValues: options.map { ($0.id, $0.label) }),
                    Dictionary(uniqueKeysWithValues: options.compactMap { option in option.detail.map { (option.id, $0) } }))
        }
        var questions: [Question] = []
        switch request.kind.rawValue {
        case "confirm":
            let plugin = request.plugin != nil
            let yes = request.confirmLabel.flatMap { $0.isEmpty ? nil : $0 } ?? (plugin ? "Confirm" : "Yes")
            let no = request.cancelLabel.flatMap { $0.isEmpty ? nil : $0 } ?? (plugin ? "Cancel" : "No")
            questions = [.init(prompt: request.title, detail: request.detail ?? "", kind: .boolean,
                options: ["true", "false"], optionLabels: ["true": yes, "false": no])]
        case "input": questions = [.init(prompt: request.title, detail: request.detail ?? "")]
        case "authorize":
            guard let raw = request.url, let url = URL(string: raw), url.scheme?.lowercased() == "https", url.host() != nil else {
                throw ClientError.invalidPayload
            }
            questions = [.init(prompt: request.title, detail: request.detail ?? "")]
        case "select":
            let choices = try choices(request.options ?? [])
            guard !choices.ids.isEmpty else { throw ClientError.invalidPayload }
            questions = [.init(prompt: request.title, detail: request.detail ?? "", kind: .select,
                options: choices.ids, optionLabels: choices.labels, optionDetails: choices.details)]
        case "guided":
            for question in request.questions ?? [] {
                let kind: Kind
                switch question.kind.rawValue {
                case "text": kind = .text
                case "select": kind = .select
                case "multiselect": kind = .multiselect
                case "boolean": kind = .boolean
                default: throw ClientError.invalidPayload
                }
                let choices = try choices(question.options ?? [])
                if kind == .select || kind == .multiselect { guard !choices.ids.isEmpty else { throw ClientError.invalidPayload } }
                questions.append(.init(prompt: question.prompt, detail: question.detail ?? "", kind: kind,
                    required: question.required, options: kind == .boolean ? ["true", "false"] : choices.ids,
                    optionLabels: kind == .boolean ? ["true": "Yes", "false": "No"] : choices.labels,
                    optionDetails: kind == .boolean ? [:] : choices.details))
            }
        default: throw ClientError.invalidPayload
        }
        guard !questions.isEmpty else { throw ClientError.invalidPayload }
        self.init(title: request.title, confirmation: request.kind.rawValue == "confirm",
                  guided: request.kind.rawValue == "guided", questions: questions)
        self.request = request
        if request.kind.rawValue == "input" { answers[0].text = request.initialValue ?? "" }
        confirming = request.defaultValue == true
    }

    func response(cancelled: Bool) -> WireInteractionResponse? {
        guard let request, cancelled || unsupported == nil else { return nil }
        var confirmed: Bool?, value: String?, selected: String?, guided: [String: WireInteractionAnswer]?
        if !cancelled {
            switch request.kind.rawValue {
            case "confirm": confirmed = answers[0].choices.contains("true")
            case "input", "authorize": value = answers[0].text
            case "select": selected = questions[0].options.first(where: answers[0].choices.contains)
            case "guided":
                guided = [:]
                for (index, question) in (request.questions ?? []).enumerated() {
                    let answer = answers[index]
                    guided?[question.id] = WireInteractionAnswer(
                        text: !answer.skipped && question.kind.rawValue == "text" ? answer.text : nil,
                        boolean: !answer.skipped && question.kind.rawValue == "boolean" ? answer.choices.contains("true") : nil,
                        optionIds: !answer.skipped && ["select", "multiselect"].contains(question.kind.rawValue)
                            ? questions[index].options.filter(answer.choices.contains) : nil,
                        skipped: answer.skipped ? true : nil)
                }
            default: return nil
            }
        }
        return .init(requestId: request.id, cancelled: cancelled ? true : nil, confirmed: confirmed,
            value: value, selectedOptionId: selected, answers: guided)
    }
}
