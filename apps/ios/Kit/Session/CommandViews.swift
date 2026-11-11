import KitClient
import SwiftUI

/// What the command search can open from the composer: the session's
/// changes count, and the session menu's actions. Only the session screen
/// passes one.
struct ComposerCommands {
    let changes: String
    let perform: (SessionCommand.BuiltIn) -> Void
}

/// The command search open over the composer: the commands, or the choices
/// of the model or thinking level picked from them.
struct CommandSearchState: Equatable {
    enum Page: Equatable { case commands, models, thinking }
    var page = Page.commands
    var query = ""
}

/// The button beside + that opens the command search. Filled while the
/// search is open or a command is picked.
struct CommandButton: View {
    let active: Bool
    let label: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                if active { Circle().fill(Theme.text) }
                CommandGlyph()
                    .stroke(active ? Theme.surface : Theme.text,
                            style: StrokeStyle(lineWidth: 1.6, lineCap: .round, lineJoin: .round))
                    .frame(width: 20, height: 20)
            }
            .frame(width: 40, height: 40)
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityIdentifier("command-button")
    }
}

/// A slash in a rounded square: the command button's mark.
struct CommandGlyph: Shape {
    func path(in rect: CGRect) -> Path {
        let scale = min(rect.width, rect.height) / 20
        var path = Path(roundedRect: CGRect(x: 2.5 * scale, y: 2.5 * scale, width: 15 * scale, height: 15 * scale),
                        cornerRadius: 4 * scale, style: .continuous)
        path.move(to: CGPoint(x: 11.8 * scale, y: 6.5 * scale))
        path.addLine(to: CGPoint(x: 8.2 * scale, y: 13.5 * scale))
        return path.offsetBy(dx: rect.minX, dy: rect.minY)
    }
}

/// The picked command before the draft. Tapping it removes it.
struct PickedCommandChip: View {
    let name: String
    let remove: () -> Void

    var body: some View {
        Button(action: remove) {
            Text(name)
                .font(.system(size: 16, weight: .medium))
                .foregroundStyle(Theme.accent)
                .lineLimit(1)
                .padding(.horizontal, 10)
                .frame(height: 30)
                .background(Theme.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
        .buttonStyle(.plain)
        .padding(.vertical, 5)
        .accessibilityLabel("\(name) command")
        .accessibilityHint("Removes the command")
        .accessibilityIdentifier("command-chip")
    }
}

/// What the picked command does, and what to type after it.
struct CommandHint: View {
    let command: SessionComposer.PickedCommand

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(description)
                .font(.system(size: 15))
                .foregroundStyle(Theme.text)
                .lineLimit(2)
            Text(usage)
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("command-hint")
    }

    private var description: String {
        switch command {
        case .prompt(let prompt): prompt.description
        case .plugin(let plugin): plugin.description
        }
    }

    private var usage: String {
        switch command {
        case .prompt(let prompt): "Prompt · " + (prompt.argumentHint.map { "type \($0)" } ?? "runs with what you type")
        case .plugin(let plugin): "\(plugin.pluginID) · type \(plugin.argName ?? "")"
        }
    }

    /// The field's placeholder while the command is picked.
    static func placeholder(_ command: SessionComposer.PickedCommand) -> String {
        switch command {
        case .prompt(let prompt): prompt.argumentHint ?? "Add details (optional)"
        case .plugin(let plugin): plugin.argName ?? ""
        }
    }
}

/// The list above the composer while the command search is open.
struct CommandPanel: View {
    @Binding var search: CommandSearchState
    let commands: [SessionCommand]
    let recents: [String]
    let actions: SessionActions
    let pick: (SessionCommand) -> Void
    let choose: (Choice) -> Void

    /// A model or thinking level picked from a choice page.
    enum Choice: Equatable { case model(String), thinking(String) }

    /// The tallest the list grows before it scrolls. With less room above
    /// the keyboard, it shrinks to fit.
    static let maxHeight: CGFloat = 308
    static let rowHeight: CGFloat = 44
    static let headerHeight: CGFloat = 28

    var body: some View {
        VStack(spacing: 0) {
            if search.page != .commands { choiceHeader }
            if let reason = lockedReason {
                Text(reason)
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
                    .padding(.horizontal, 20)
                    .padding(.top, 2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        switch search.page {
                        case .commands: commandRows
                        case .models: modelRows
                        case .thinking: thinkingRows
                        }
                    }
                    .padding(.vertical, 6)
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(minHeight: min(listHeight, Self.rowHeight * 2), idealHeight: listHeight, maxHeight: listHeight)
                // A choice page opens at the session's current choice.
                .onChange(of: search.page) { _, page in reveal(page, proxy) }
                .onChange(of: actions.configuration.models.isEmpty) { reveal(search.page, proxy) }
            }
        }
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 26, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("command-panel")
        .task { await actions.loadModels() }
    }

    // MARK: Commands

    private var sections: [CommandCatalog.Section] {
        CommandCatalog.sections(commands, query: search.query, recents: recents)
    }

    @ViewBuilder
    private var commandRows: some View {
        let sections = sections
        if sections.isEmpty {
            empty("No commands match")
        }
        ForEach(Array(sections.enumerated()), id: \.offset) { index, section in
            if let title = section.title { header(title, first: index == 0) }
            ForEach(section.rows) { command in
                CommandRow(command: command) { pick(command) }
            }
        }
    }

    // MARK: Choices

    private var choiceHeader: some View {
        ZStack {
            Text(search.page == .models ? "Model" : "Thinking")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
            HStack {
                Button {
                    search = CommandSearchState()
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left").font(.system(size: 15, weight: .semibold))
                        Text("Commands").font(.system(size: 16))
                    }
                    .foregroundStyle(Theme.accent)
                    .frame(height: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("command-back")
                Spacer()
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 4)
    }

    /// Why the page's choice can't change now, such as a turn starting while it's open.
    private var lockedReason: String? {
        switch search.page {
        case .commands: nil
        case .models: actions.modelLockedReason
        case .thinking: actions.thinkingLockedReason
        }
    }

    private var modelGroups: [(provider: String, models: [WireModelCapability])] {
        let query = search.query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return actions.modelGroups.compactMap { group in
            let models = CommandCatalog.rank(group.models, query: query, name: \.name, terms: { [$0.id, $0.provider] })
            return models.isEmpty ? nil : (group.provider, models)
        }
    }

    @ViewBuilder
    private var modelRows: some View {
        let groups = modelGroups
        if actions.configuration.models.isEmpty {
            if let error = actions.configuration.error {
                empty("Couldn’t load models. \(error)")
            } else {
                ProgressView().frame(maxWidth: .infinity, minHeight: Self.rowHeight)
            }
        } else if groups.isEmpty {
            empty("No models match")
        }
        ForEach(Array(groups.enumerated()), id: \.element.provider) { index, group in
            header(group.provider, first: index == 0)
            ForEach(group.models, id: \.id) { model in
                ChoiceRow(title: model.name, selected: model.id == actions.model,
                          enabled: actions.modelLockedReason == nil) { choose(.model(model.id)) }
            }
        }
    }

    private var levels: [String] {
        let query = search.query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return CommandCatalog.rank(actions.thinkingLevels, query: query, name: SessionContext.thinkingName, terms: { [$0] })
    }

    @ViewBuilder
    private var thinkingRows: some View {
        let levels = levels
        if levels.isEmpty { empty("No levels match") }
        ForEach(levels, id: \.self) { level in
            ChoiceRow(title: SessionContext.thinkingName(level), selected: level == actions.thinking,
                      enabled: actions.thinkingLockedReason == nil) {
                choose(.thinking(level))
            }
        }
    }

    private func reveal(_ page: CommandSearchState.Page, _ proxy: ScrollViewProxy) {
        switch page {
        case .commands: break
        case .models: proxy.scrollTo(actions.model, anchor: .center)
        case .thinking: proxy.scrollTo(actions.thinking, anchor: .center)
        }
    }

    // MARK: Parts

    private func header(_ title: String, first: Bool) -> some View {
        Text(title)
            .font(.system(size: 13))
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 20)
            .padding(.top, first ? 8 : 12)
            .padding(.bottom, 4)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }

    private func empty(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 15))
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 20)
            .frame(maxWidth: .infinity, minHeight: Self.rowHeight, alignment: .leading)
    }

    /// The list's height: its rows, up to the most it shows before scrolling.
    private var listHeight: CGFloat {
        let (rows, headers): (Int, Int) = switch search.page {
        case .commands:
            (max(1, sections.reduce(0) { $0 + $1.rows.count }), sections.filter { $0.title != nil }.count)
        case .models:
            (max(1, modelGroups.reduce(0) { $0 + $1.models.count }), modelGroups.count)
        case .thinking:
            (max(1, levels.count), 0)
        }
        let natural = CGFloat(rows) * Self.rowHeight + CGFloat(headers) * Self.headerHeight + 12
        return min(natural, Self.maxHeight - (search.page == .commands ? 0 : 48))
    }
}

/// One command: its symbol, name, and value or what it does. A command that
/// can't run now is dimmed and says why.
private struct CommandRow: View {
    let command: SessionCommand
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                icon
                    .frame(width: 22, height: 22)
                Text(command.title)
                    .font(.system(size: 17))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .layoutPriority(1)
                Spacer(minLength: 8)
                Text(command.unavailableReason ?? command.detail)
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
            }
            .padding(.horizontal, 20)
            .frame(height: CommandPanel.rowHeight)
            .contentShape(Rectangle())
            .opacity(command.unavailableReason == nil ? 1 : 0.45)
        }
        .buttonStyle(.plain)
        .disabled(command.unavailableReason != nil)
        .accessibilityIdentifier("command-" + command.id)
    }

    @ViewBuilder
    private var icon: some View {
        switch command.kind {
        case .builtIn(let builtIn):
            Image(systemName: builtIn.symbol)
                .font(.system(size: 16))
                .foregroundStyle(Theme.accent)
        case .prompt:
            Text("/")
                .font(.system(size: 17, design: .monospaced))
                .foregroundStyle(Theme.accent)
        case .plugin:
            Image(systemName: "puzzlepiece.extension")
                .font(.system(size: 15))
                .foregroundStyle(Theme.accent)
        }
    }
}

/// A model or thinking level, checked when it's the session's.
private struct ChoiceRow: View {
    let title: String
    let selected: Bool
    let enabled: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack {
                Text(title)
                    .font(.system(size: 17))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if selected {
                    Image(systemName: "checkmark")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.accent)
                }
            }
            .padding(.horizontal, 20)
            .frame(height: CommandPanel.rowHeight)
            .contentShape(Rectangle())
            .opacity(enabled ? 1 : 0.45)
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}
