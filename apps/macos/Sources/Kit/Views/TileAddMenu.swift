import SwiftUI

/// The + after a tile's tabs. It opens a menu of panes to add to the tile:
/// Terminal, Browser, Diff, Scratchpad, then Open file…. Panes already open
/// are dimmed; choosing one selects it. A terminal runs on this Mac, so on
/// another machine's server it stays in the menu, dimmed with the reason.
struct TileAddButton: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let workspace: WorkspaceState
    let tile: UUID
    @State private var popover = AnchoredPopover()
    @State private var anchor = ComposerPickerAnchor()
    @State private var open = false
    @State private var hovering = false

    var body: some View {
        Button(action: toggle) {
            IconView(.plus, size: 12)
                .foregroundStyle(open ? theme.text : theme.muted)
                .frame(width: 22, height: 22)
                .background(open || hovering ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .background(ComposerPickerAnchorView(anchor: anchor))
        .onHover { hovering = $0 }
        .padding(.horizontal, 6)
        .help("Add Tab")
        .accessibilityLabel("Add Tab")
        .onDisappear { popover.dismiss() }
    }

    private func toggle() {
        if popover.isOpen { popover.dismiss(); return }
        // The click that reached the button already closed the menu.
        if let closedAt = popover.closedAt, Date().timeIntervalSince(closedAt) < 0.3 { return }
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, workspace = workspace, state = state, tile = tile
        let menu = TileAddMenu(theme: theme, items: items) { choice in
            popover.dismiss()
            workspace.focus(tile: tile)
            switch choice {
            case .terminal:
                if let directory = state.terminalDirectory { workspace.openTerminal(in: tile, directory: directory) }
            case .browser: workspace.openBrowser(in: tile)
            case .diff: workspace.open(.review)
            case .scratchpad: workspace.open(.scratchpad)
            case .file: state.showFiles(intent: "open")
            }
        }
        open = true
        popover.show(anchor: frame, parent: window, edge: .below, alignment: .leading, content: menu) {
            open = false
        }
    }

    private var items: [TileAddMenu.Item] { Self.items(state: state, workspace: workspace) }

    /// The menu's items for a tab showing `state`'s session.
    static func items(state: SessionStore, workspace: WorkspaceState) -> [TileAddMenu.Item] {
        var items: [TileAddMenu.Item] = []
        if state.offersTerminal {
            items.append(.init(choice: .terminal, title: "Terminal", icon: .terminalWindow,
                               unavailableReason: state.terminalUnavailableReason))
        }
        items.append(.init(choice: .browser, title: "Browser", icon: .browser))
        if state.catalogClient is any DiffClient {
            items.append(.init(choice: .diff, title: "Diff", icon: .gitDiff, dimmed: workspace.panes.contains(.review)))
        }
        if state.canOpenScratchpad {
            items.append(.init(choice: .scratchpad, title: "Scratchpad", icon: .note,
                               dimmed: workspace.panes.contains(.scratchpad)))
        }
        if !state.unavailable {
            items.append(.init(choice: .file, title: "Open file…", icon: .file, shortcut: "⌘O", separated: true))
        }
        return items
    }
}

struct TileAddMenu: View {
    enum Choice { case terminal, browser, diff, scratchpad, file }
    struct Item: Identifiable, Equatable {
        var id: Choice { choice }
        let choice: Choice
        let title: String
        let icon: Icon
        var dimmed = false
        /// Why the item can't be chosen, shown after it; it is then disabled.
        var unavailableReason: String? = nil
        var shortcut: String? = nil
        /// Drawn after a rule.
        var separated = false
    }

    let theme: MicaTheme
    let items: [Item]
    let choose: (Choice) -> Void
    @State private var hovered: Choice?

    static let width: CGFloat = 220

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(items) { item in
                if item.separated { Rule().padding(.vertical, 4) }
                row(item)
            }
        }
        .padding(4)
        .frame(width: Self.width)
        .foregroundStyle(theme.text)
        .background(theme.raised)
        .popoverCard(theme)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Add tab")
    }

    @ViewBuilder private func row(_ item: Item) -> some View {
        if let reason = item.unavailableReason {
            // Not a button, so the system doesn't dim it further: the item
            // and its reason are drawn as unavailable on their own.
            label(item)
                .help(item.title + " · " + reason)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(item.title)
                .accessibilityValue("Unavailable, " + reason)
        } else {
            Button { choose(item.choice) } label: { label(item) }
                .buttonStyle(.plain)
                .onHover { hovered = $0 ? item.choice : (hovered == item.choice ? nil : hovered) }
                .accessibilityHint(item.dimmed ? "Already open" : "")
        }
    }

    private func label(_ item: Item) -> some View {
        let unavailable = item.unavailableReason != nil
        return HStack(spacing: 8) {
            IconView(item.icon).foregroundStyle(unavailable ? theme.strongBorder : theme.muted)
            Text(item.title).font(.kit(size: 12))
                .foregroundStyle(unavailable ? theme.strongBorder : theme.text)
            Spacer(minLength: 0)
            if let reason = item.unavailableReason {
                HStack(spacing: 4) {
                    IconView(.prohibit, size: 11)
                    Text(reason).font(.kit(size: 11))
                }
                .foregroundStyle(theme.muted)
            } else if let shortcut = item.shortcut {
                Text(shortcut).font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .opacity(item.dimmed ? 0.5 : 1)
        .padding(.horizontal, 8)
        .frame(height: 28)
        .background(hovered == item.choice && !unavailable ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
        .contentShape(Rectangle())
    }
}
