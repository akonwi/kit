import Foundation
import Observation

/// Local interaction presentation; server transport remains outside the demo.
@MainActor @Observable
final class InteractionFlow: Identifiable {
    enum Kind { case text, select, multiselect, boolean }
    struct Question {
        let prompt: String
        var detail = ""
        var kind: Kind = .text
        var required = true
        var options: [String] = []
        var optionLabels: [String: String] = [:]
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
    let questions: [Question]
    var answers: [Answer]
    var step = 0
    var question: Question { questions[step] }
    func optionDetail(_ id: String) -> String? {
        let options = request?.kind.rawValue == "guided" ? request?.questions?[step].options : request?.options
        return options?.first(where: { $0.id == id })?.detail
    }
    var canContinue: Bool {
        if request?.plugin != nil && request?.kind.rawValue == "input" { return true }
        if !question.required { return true }
        return question.kind == .text ? !answers[step].text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty : !answers[step].choices.isEmpty
    }
    init(title: String, confirmation: Bool = false, questions: [Question]) {
        precondition(!questions.isEmpty)
        self.title = title
        self.confirmation = confirmation
        self.questions = questions
        answers = questions.map { _ in Answer() }
    }
    func choose(_ option: String) {
        guard !submitting else { return }
        answers[step].skipped = false
        if question.kind == .multiselect {
            if !answers[step].choices.insert(option).inserted { answers[step].choices.remove(option) }
        } else { answers[step].choices = [option] }
    }
    /// Returns true only when the request is ready to submit.
    func advance(skip: Bool = false) -> Bool {
        guard !submitting else { return false }
        guard skip ? !question.required : canContinue else { return false }
        if skip { answers[step] = Answer(skipped: true) }
        else { answers[step].skipped = false }
        if step < questions.count - 1 { step += 1; return false }
        return true
    }
    /// Enter selects the highlighted single choice; Tab preserves unanswered booleans.
    func submitStep(option: String?, selectBoolean: Bool = true) -> Bool {
        if question.kind == .select || (question.kind == .boolean && selectBoolean) {
            if let option, question.options.contains(option) { choose(option) }
        }
        let empty = question.kind == .text
            ? answers[step].text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            : answers[step].choices.isEmpty
        return advance(skip: !question.required && empty)
    }

    var summary: String {
        zip(questions, answers).map { question, answer in
            let value = answer.skipped ? "Skipped" : question.kind == .text ? answer.text : question.options.filter(answer.choices.contains).map { question.optionLabels[$0] ?? $0 }.joined(separator: ", ")
            return "\(question.prompt)\n\(value)"
        }.joined(separator: "\n\n")
    }
    static func approval() -> InteractionFlow {
        .init(title: "Run workspace tests?", confirmation: true, questions: [
            .init(prompt: "Allow Kit to run the test suite?", detail: "go test ./internal/subagent/...", kind: .boolean, options: ["Allow once", "Deny"])
        ])
    }
    static func planning() -> InteractionFlow {
        .init(title: "Shape the next change", questions: [
            .init(prompt: "Where should the implementation start?", detail: "Choose one area to tackle first.", kind: .select, options: ["Session navigation", "File workspace", "Composer interactions"]),
            .init(prompt: "Which checks should be included?", detail: "Select all that apply.", kind: .multiselect, options: ["Unit tests", "Keyboard navigation", "Light and dark appearances"]),
            .init(prompt: "Anything else to keep in mind?", detail: "Add constraints or context for the implementation.", required: false),
            .init(prompt: "Keep the change isolated in this worktree?", kind: .boolean, options: ["Yes", "No"])
        ])
    }
}

extension InteractionFlow {
    convenience init(request: WireInteractionRequest) throws {
        guard request.id.hasPrefix("interaction_"), !(request.title.isEmpty),
              (request.questions?.count ?? 0) <= 32 else { throw ClientError.invalidPayload }
        func choices(_ options: [WireInteractionOption]) throws -> ([String], [String: String]) {
            guard options.count <= 64, Set(options.map(\.id)).count == options.count,
                  options.allSatisfy({ !$0.id.isEmpty && !$0.label.isEmpty }) else { throw ClientError.invalidPayload }
            return (options.map(\.id), Dictionary(uniqueKeysWithValues: options.map { ($0.id, $0.label) }))
        }
        var questions: [Question] = []
        switch request.kind.rawValue {
        case "confirm":
            questions = [.init(prompt: request.title, detail: request.detail ?? "", kind: .boolean,
                options: ["true", "false"], optionLabels: ["true": request.confirmLabel.flatMap { $0.isEmpty ? nil : $0 } ?? "Allow", "false": request.cancelLabel.flatMap { $0.isEmpty ? nil : $0 } ?? "Deny"])]
        case "input": questions = [.init(prompt: request.title, detail: request.detail ?? "")]
        case "select":
            let (ids, labels) = try choices(request.options ?? [])
            guard !ids.isEmpty else { throw ClientError.invalidPayload }
            questions = [.init(prompt: request.title, detail: request.detail ?? "", kind: .select, options: ids, optionLabels: labels)]
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
                let (ids, labels) = try choices(question.options ?? [])
                questions.append(.init(prompt: question.prompt, detail: question.detail ?? "", kind: kind,
                    required: question.required, options: kind == .boolean ? ["true", "false"] : ids,
                    optionLabels: kind == .boolean ? ["true": "Yes", "false": "No"] : labels))
            }
        default: throw ClientError.invalidPayload
        }
        guard !questions.isEmpty else { throw ClientError.invalidPayload }
        self.init(title: request.title, confirmation: request.kind.rawValue == "confirm", questions: questions)
        self.request = request
        if request.plugin != nil {
            if request.kind.rawValue == "input" { answers[0].text = request.initialValue ?? "" }
            if request.kind.rawValue == "confirm" { answers[0].choices = [request.defaultValue == true ? "true" : "false"] }
        }
    }

    func response(cancelled: Bool) -> WireInteractionResponse? {
        guard let request else { return nil }
        var confirmed: Bool?, value: String?, selected: String?, guided: [String: WireInteractionAnswer]?
        if !cancelled {
            switch request.kind.rawValue {
            case "confirm": confirmed = answers[0].choices.contains("true")
            case "input": value = answers[0].text
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
