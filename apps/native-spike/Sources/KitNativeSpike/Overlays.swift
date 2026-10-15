import SwiftUI

// MARK: Command palette

struct CommandPalette: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var query = ""
    @State private var highlighted = 0
    @FocusState private var fieldFocused: Bool

    private var results: [Fixtures.Command] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return Fixtures.commands }
        return Fixtures.commands.filter { $0.name.contains(q) || $0.detail.lowercased().contains(q) }
    }

    var body: some View {
        ZStack(alignment: .top) {
            Color.black.opacity(theme.dark ? 0.45 : 0.18)
                .contentShape(Rectangle())
                .onTapGesture { close() }
            VStack(spacing: 0) {
                HStack(spacing: 10) {
                    Icon(glyph: .magnifyingGlass, size: 14, color: theme.muted)
                    TextField("Search commands…", text: $query)
                        .textFieldStyle(.plain)
                        .font(Fonts.ui(14))
                        .foregroundStyle(theme.text)
                        .focused($fieldFocused)
                        .onKeyPress(.upArrow) { move(-1); return .handled }
                        .onKeyPress(.downArrow) { move(1); return .handled }
                        .onKeyPress(.escape) { close(); return .handled }
                        .onKeyPress(.tab) {
                            if results.indices.contains(highlighted) { query = results[highlighted].name + " " }
                            return .handled
                        }
                        .onSubmit(run)
                        .onChange(of: query) { _, q in highlighted = 0; spikeLog("palette query '\(q)' -> \(results.count) results") }
                }
                .padding(.horizontal, 16).frame(height: 48)
                Rectangle().fill(theme.border).frame(height: 1)
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(Array(results.enumerated()), id: \.element.id) { index, command in
                                row(command, on: index == highlighted)
                                    .id(index)
                                    .onTapGesture { highlighted = index; run() }
                            }
                            if results.isEmpty {
                                Text("No commands match \u{201C}\(query)\u{201D}")
                                    .font(Fonts.ui(13)).foregroundStyle(theme.muted)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.horizontal, 20).frame(height: 40)
                            }
                        }
                        .padding(.vertical, 6)
                    }
                    .frame(height: 380)
                    .onChange(of: highlighted) { _, i in proxy.scrollTo(i) }
                }
                Rectangle().fill(theme.border).frame(height: 1)
                HStack {
                    Text("tab complete").font(Fonts.mono(10)).foregroundStyle(theme.muted)
                    Spacer()
                }
                .padding(.horizontal, 16).frame(height: 34)
            }
            .frame(width: 640)
            .background(RoundedRectangle(cornerRadius: 4).fill(theme.raised))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .shadow(color: .black.opacity(0.35), radius: 24, y: 12)
            .padding(.top, 180)
        }
        .onAppear { fieldFocused = true }
    }

    private func row(_ command: Fixtures.Command, on: Bool) -> some View {
        HStack(spacing: 0) {
            Text(command.name).font(Fonts.mono(12)).foregroundStyle(command.idleOnly ? theme.muted : theme.text)
                .frame(width: 116, alignment: .leading)
            Text(command.args ?? "").font(Fonts.mono(12)).foregroundStyle(theme.muted)
                .frame(width: 84, alignment: .leading)
            if command.idleOnly {
                Icon(glyph: .prohibit, size: 12, color: theme.muted).padding(.trailing, 6)
                Text("idle only · \(command.detail)").font(Fonts.ui(13)).foregroundStyle(theme.muted)
            } else {
                Text(command.detail).font(Fonts.ui(13)).foregroundStyle(theme.text)
            }
            Spacer(minLength: 0)
        }
        .lineLimit(1)
        .padding(.horizontal, 14)
        .frame(height: 28)
        .background(RoundedRectangle(cornerRadius: 2).fill(on ? theme.frameHover : .clear))
        .overlay(alignment: .leading) { if on { Rectangle().fill(theme.accent).frame(width: 2) } }
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
    }

    private func move(_ delta: Int) {
        guard !results.isEmpty else { return }
        highlighted = (highlighted + delta + results.count) % results.count
    }

    private func run() {
        guard results.indices.contains(highlighted) else { return }
        let command = results[highlighted]
        close()
        if let action = command.run {
            action(model)
        } else {
            model.toast(Toast(variant: .info, title: "/\(command.name)", detail: "Not wired up in the spike."))
        }
    }

    private func close() { model.paletteOpen = false }
}

// MARK: Toasts

struct ToastStack: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    @State private var hovering = false
    private let timer = Timer.publish(every: 0.25, on: .main, in: .common).autoconnect()

    var body: some View {
        let toasts = session.toasts
        VStack(alignment: .trailing, spacing: 8) {
            if toasts.count > 4 && session.toastsExpanded {
                HStack {
                    Text("\(toasts.count) in this session").font(Fonts.ui(11)).foregroundStyle(theme.muted)
                    Spacer()
                    Button { session.toastsExpanded = false } label: {
                        HStack(spacing: 4) {
                            Text("Collapse").font(Fonts.ui(11))
                            Icon(glyph: .caretUp, size: 9)
                        }.foregroundStyle(theme.text)
                    }.buttonStyle(.plain)
                }
                .frame(width: 360)
                ScrollView {
                    VStack(spacing: 8) { ForEach(toasts) { ToastCard(session: session, toast: $0) } }
                }
                .frame(width: 360).frame(maxHeight: 420)
            } else if toasts.count > 4 {
                ToastCard(session: session, toast: toasts[0])
                    .background(alignment: .bottom) {
                        // Two card edges peek out below the newest toast.
                        ZStack(alignment: .bottom) {
                            edge.padding(.horizontal, 12).offset(y: 8)
                            edge.padding(.horizontal, 6).offset(y: 4)
                        }
                    }
                Button { session.toastsExpanded = true } label: {
                    HStack(spacing: 4) {
                        Text("\(toasts.count - 1) more").font(Fonts.ui(11))
                        Icon(glyph: .caretDown, size: 9)
                    }
                    .foregroundStyle(theme.text)
                    .padding(.horizontal, 8).frame(height: 22)
                    .background(RoundedRectangle(cornerRadius: 3).fill(theme.surface))
                    .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                }
                .buttonStyle(.plain)
                .padding(.top, 10)
            } else {
                ForEach(toasts) { ToastCard(session: session, toast: $0) }
            }
        }
        .animation(.easeOut(duration: 0.18), value: toasts.map(\.id))
        .onHover { hovering = $0 }
        .onReceive(timer) { _ in tick() }
    }

    private var edge: some View {
        RoundedRectangle(cornerRadius: 4).fill(theme.surface)
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .frame(height: 20)
    }

    /// Expiry pauses while the pointer is over the stack or the window is inactive.
    private func tick() {
        guard !hovering, NSApp.isActive else { return }
        var changed = false
        var toasts = session.toasts
        for i in toasts.indices.reversed() where !toasts[i].persistent {
            toasts[i].remaining -= 0.25
            if toasts[i].remaining <= 0 { toasts.remove(at: i) }
            changed = true
        }
        if changed { session.toasts = toasts }
    }
}

private struct ToastCard: View {
    @Environment(\.theme) private var theme
    let session: Session
    let toast: Toast
    @State private var detailsOpen = false

    var body: some View {
        let color: Color = switch toast.variant {
        case .info: theme.accent
        case .warning: theme.warning
        case .error: theme.danger
        }
        let glyph: Ph = switch toast.variant {
        case .info: .info
        case .warning: .warning
        case .error: .warningCircle
        }
        HStack(alignment: .top, spacing: 8) {
            Icon(glyph: glyph, size: 14, color: color).padding(.top, 1)
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(toast.title).font(Fonts.ui(12, weight: .medium)).foregroundStyle(color)
                    Spacer()
                    if let plugin = toast.plugin {
                        Text(plugin).font(Fonts.mono(10)).foregroundStyle(theme.muted)
                    }
                }
                if let detail = toast.detail {
                    Text(detail).font(Fonts.ui(12)).foregroundStyle(theme.muted)
                        .lineLimit(detailsOpen ? nil : 2)
                        .textSelection(.enabled)
                }
                if let action = toast.action {
                    SmallButton(title: action) { dismiss() }.padding(.top, 4)
                }
            }
            Button(action: dismiss) { Icon(glyph: .x, size: 10, color: theme.muted).frame(width: 14, height: 14) }
                .buttonStyle(.plain)
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .frame(width: 360, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 4).fill(theme.surface))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(color))
        .shadow(color: .black.opacity(theme.dark ? 0.45 : 0.12), radius: 12, y: 6)
        .transition(.move(edge: .trailing).combined(with: .opacity))
    }

    private func dismiss() { session.toasts.removeAll { $0.id == toast.id } }
}
