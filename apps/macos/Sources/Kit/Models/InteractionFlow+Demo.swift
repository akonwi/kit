import Foundation

/// Sample requests for the demo session and tests.
extension InteractionFlow {
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
