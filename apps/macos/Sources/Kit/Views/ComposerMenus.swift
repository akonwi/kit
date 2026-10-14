import SwiftUI

/// The model half's popover: the session's models, dimmed while a turn runs.
/// Picking a model applies it and closes the popover.
struct ComposerModelMenu: View {
    @Bindable var state: SessionStore
    let theme: MicaTheme
    let dismiss: () -> Void

    static let lockedNote = "Model can change when the turn ends"

    var body: some View {
        ComposerModelPicker(models: state.configuration.models, selectedID: state.model,
            select: { id in
                dismiss()
                guard id != state.model, !state.configuration.changing else { return }
                Task { await state.configure(model: id) }
            }, reload: {
                Task { await state.loadComposer() }
            }, dismiss: dismiss,
            lockedNote: state.running ? Self.lockedNote : nil)
            .popoverCard(theme)
            .accessibilityLabel("Model")
    }
}

/// The thinking half's popover: the model's levels under a caption, the
/// current one checked. Picking a level applies it and closes the popover.
struct ComposerThinkingMenu: View {
    let theme: MicaTheme
    let levels: [String]
    let selected: String
    let select: (String) -> Void
    let dismiss: () -> Void
    @State private var selection = 0
    @FocusState private var focused: Bool

    static let minimumWidth: CGFloat = 128
    static let rowHeight: CGFloat = 26

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("THINKING").font(.kit(size: 10, weight: .medium)).tracking(0.8)
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 8).frame(height: 24)
                .accessibilityAddTraits(.isHeader)
            ForEach(Array(levels.enumerated()), id: \.element) { index, level in
                Button { pick(level) } label: {
                    HStack(spacing: 8) {
                        IconView(.check, size: 10)
                            .opacity(level == selected ? 1 : 0).frame(width: 12)
                        Text(level).font(.kit(size: 12)).lineLimit(1)
                        Spacer(minLength: 8)
                    }
                    .foregroundStyle(theme.text)
                    .padding(.horizontal, 8)
                    .frame(height: Self.rowHeight)
                    .background(selection == index ? theme.accent.opacity(0.18) : .clear,
                                in: RoundedRectangle(cornerRadius: 3))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .onHover { if $0 { selection = index } }
                .accessibilityAddTraits(level == selected ? .isSelected : [])
            }
        }
        .padding(4)
        .frame(minWidth: Self.minimumWidth, alignment: .leading)
        .fixedSize()
        .background(theme.surface)
        .focusable().focused($focused)
        .focusEffectDisabled()
        .onAppear {
            selection = levels.firstIndex(of: selected) ?? 0
            focused = true
        }
        .onKeyPress(.downArrow) { selection = min(selection + 1, levels.count - 1); return .handled }
        .onKeyPress(.upArrow) { selection = max(selection - 1, 0); return .handled }
        .onKeyPress(.return) {
            guard levels.indices.contains(selection) else { return .ignored }
            pick(levels[selection]); return .handled
        }
        .onExitCommand(perform: dismiss)
        .popoverCard(theme)
        .accessibilityLabel("Thinking")
    }

    private func pick(_ level: String) {
        dismiss()
        if level != selected { select(level) }
    }
}
