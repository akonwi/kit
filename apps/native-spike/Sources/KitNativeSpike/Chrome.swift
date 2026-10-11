import AppKit
import SwiftUI

enum AppWindow {
    static weak var window: NSWindow?
}

struct RootView: View {
    @Environment(AppModel.self) private var model
    let theme: Theme

    var body: some View {
        let session = model.current
        ZStack(alignment: .topLeading) {
            theme.frame.ignoresSafeArea()
            if model.sideTabs {
                HStack(spacing: 0) {
                    Sidebar()
                    VStack(spacing: 0) {
                        SessionHeader(session: session, showTitle: true)
                        WorkspaceView(session: session)
                            .id(session.id)
                            .padding(.trailing, 6).padding(.bottom, 6)
                    }
                }
            } else {
                VStack(spacing: 0) {
                    TopTabs()
                    SessionHeader(session: session, showTitle: false)
                    WorkspaceView(session: session)
                        .id(session.id)
                        .padding([.horizontal, .bottom], 6)
                }
            }
            ToastStack(session: session)
                .padding(.top, model.sideTabs ? 44 + 6 : 44 + 32 + 6)
                .padding(.trailing, 18)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing)
            if model.paletteOpen {
                CommandPalette()
            }
        }
        .ignoresSafeArea()
        .environment(\.theme, theme)
        .preferredColorScheme(theme.colorScheme)
        .tint(theme.accent)
        .onChange(of: model.focusGeneration) { FocusBridge.sync(model) }
        .onChange(of: model.paletteOpen) { _, open in if !open { FocusBridge.restore(model) } }
    }
}

/// Moves AppKit's first responder to the focused tile's native view. SwiftUI
/// panes take focus themselves through FocusState.
@MainActor enum FocusBridge {
    private static weak var saved: NSResponder?

    static func sync(_ model: AppModel) {
        DispatchQueue.main.async {
            guard !model.paletteOpen, let window = AppWindow.window else { return }
            let session = model.current
            guard let tile = session.tiles[session.focusedTile], let pane = model.pane(tile.selected) else { return }
            defer {
                DispatchQueue.main.async { spikeLog("first responder: \(window.firstResponder.map { String(describing: type(of: $0)) } ?? "nil")") }
            }
            if pane.kind.isNative {
                if let view = NativeViews.shared.existing(pane.id), view.window === window,
                   window.firstResponder !== view {
                    window.makeFirstResponder(view)
                }
            } else if isNative(window.firstResponder), [.diff, .reviewer].contains(where: { $0 == pane.kind })
                        || { if case .file = pane.kind { return true } else { return false } }() {
                window.makeFirstResponder(nil)
            }
        }
    }

    static func save() { saved = AppWindow.window?.firstResponder }

    static func restore(_ model: AppModel) {
        DispatchQueue.main.async {
            if let saved = saved as? NSView, let window = saved.window {
                window.makeFirstResponder(saved)
                spikeLog("palette closed, restored \(type(of: saved))")
            } else {
                model.focusGeneration += 1
            }
            saved = nil
        }
    }

    private static func isNative(_ responder: NSResponder?) -> Bool {
        var view = responder as? NSView
        while let v = view {
            if v is TerminalSurfaceView || v is FocusReportingWebView { return true }
            view = v.superview
        }
        return false
    }
}

// MARK: Side tabs

private struct Sidebar: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Spacer()
                IconButton(glyph: .plus, size: 26, help: "New session (⌘N)") { model.newSession() }
            }
            .padding(.horizontal, 8)
            .frame(height: 44)
            .contentShape(Rectangle())
            .gesture(WindowDragGesture())

            VStack(alignment: .leading, spacing: 18) {
                ForEach(groups, id: \.0) { project, directory, sessions in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack {
                            Text(project).font(Fonts.ui(10, weight: .medium)).tracking(1.2).foregroundStyle(theme.muted)
                            Spacer()
                            Text(directory).font(Fonts.mono(10)).foregroundStyle(theme.strongBorder)
                        }
                        .padding(.horizontal, 8).frame(height: 24)
                        ForEach(sessions) { session in SessionRow(session: session) }
                    }
                }
            }
            .padding(.top, 8)
            Spacer(minLength: 0)
                .frame(maxWidth: .infinity)
                .contentShape(Rectangle())
                .gesture(WindowDragGesture())
            HStack {
                Spacer()
                TabsToggle()
            }
            .padding(.leading, 8).padding(.trailing, 4).frame(height: 32)
        }
        .padding(.horizontal, 8).padding(.bottom, 10)
        .frame(width: 248)
    }

    private var groups: [(String, String, [Session])] {
        var order: [String] = []
        var map: [String: [Session]] = [:]
        for s in model.sessions {
            if map[s.project] == nil { order.append(s.project) }
            map[s.project, default: []].append(s)
        }
        return order.map { ($0, map[$0]!.first!.directory, map[$0]!) }
    }
}

private struct SessionRow: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    @State private var hover = false

    var body: some View {
        let selected = model.currentID == session.id
        let index = model.sessions.firstIndex { $0.id == session.id }! + 1
        let strong = selected || session.status != .idle
        HStack(spacing: 10) {
            StatusMarker(status: session.status).frame(width: 14)
            Text(session.title)
                .font(Fonts.ui(13, weight: selected ? .medium : .regular))
                .foregroundStyle(strong ? theme.text : theme.muted)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            if index <= 9 {
                Text("⌘\(index)").font(Fonts.mono(11)).foregroundStyle(theme.muted)
            }
        }
        .padding(.horizontal, 8)
        .frame(height: 32)
        .background(RoundedRectangle(cornerRadius: 3).fill(selected ? theme.frameHover : (hover ? theme.frameHover.opacity(0.5) : .clear)))
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture { model.switchSession(index - 1) }
    }
}

private struct TabsToggle: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: 2) {
            segment(.sidebar, side: true)
            segment(.layout, side: false)
        }
        .padding(2)
        .background(RoundedRectangle(cornerRadius: 3).fill(theme.surface))
        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
    }

    private func segment(_ glyph: Ph, side: Bool) -> some View {
        let on = model.sideTabs == side
        return Button { withAnimation(.easeOut(duration: 0.15)) { model.sideTabs = side } } label: {
            Icon(glyph: side ? .sidebar : .rows, size: 14, color: on ? theme.text : theme.muted)
                .frame(width: 26, height: 22)
                .background(RoundedRectangle(cornerRadius: 2).fill(on ? theme.frameHover : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(side ? "Side tabs" : "Top tabs")
    }
}

// MARK: Top tabs

private struct TopTabs: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var frames: [UUID: CGRect] = [:]
    @State private var viewport: CGFloat = 0
    @State private var offset: CGFloat = 0

    var body: some View {
        let hiddenRight = model.sessions.filter { (frames[$0.id]?.maxX ?? 0) - offset > viewport + 1 }.count
        HStack(spacing: 6) {
            Color.clear.frame(width: 78) // traffic lights
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: 2) {
                        ForEach(model.sessions) { session in
                            TopTab(session: session).id(session.id)
                                .onGeometryChange(for: CGRect.self) { $0.frame(in: .named("toptabs")) } action: {
                                    frames[session.id] = $0
                                }
                        }
                    }
                    .coordinateSpace(.named("toptabs"))
                }
                .scrollIndicators(.never)
                .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.x } action: { _, x in offset = x }
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
                .mask(
                    HStack(spacing: 0) {
                        LinearGradient(colors: [.black.opacity(offset > 1 ? 0 : 1), .black], startPoint: .leading, endPoint: .trailing).frame(width: 24)
                        Rectangle()
                        LinearGradient(colors: [.black, .black.opacity(hiddenRight > 0 ? 0 : 1)], startPoint: .leading, endPoint: .trailing).frame(width: 32)
                    }
                )
                .onChange(of: model.currentID) { _, id in withAnimation { proxy.scrollTo(id) } }
            }
            if hiddenRight > 0 {
                Text("+\(hiddenRight)").font(Fonts.mono(11)).foregroundStyle(theme.muted)
                    .padding(.horizontal, 6).frame(height: 22)
                    .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
            }
            IconButton(glyph: .plus, size: 26, help: "New session (⌘N)") { model.newSession() }
            TabsToggle().padding(.trailing, 8)
        }
        .frame(height: 44)
        .contentShape(Rectangle())
        .gesture(WindowDragGesture())
    }
}

private struct TopTab: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    @State private var hover = false

    var body: some View {
        let selected = model.currentID == session.id
        let index = model.sessions.firstIndex { $0.id == session.id }!
        let otherProject = session.project != model.current.project
        HStack(spacing: 8) {
            StatusMarker(status: session.status).frame(width: 14)
            Text(session.title)
                .font(Fonts.ui(12, weight: selected ? .medium : .regular))
                .foregroundStyle(selected || session.status != .idle ? theme.text : theme.muted)
                .lineLimit(1)
            if otherProject {
                Text(session.project.lowercased()).font(Fonts.mono(10)).foregroundStyle(theme.strongBorder)
            }
            Icon(glyph: .x, size: 10, color: theme.muted).opacity(selected || hover ? 1 : 0)
        }
        .padding(.horizontal, 10)
        .frame(width: 200, height: 30)
        .background(RoundedRectangle(cornerRadius: 3).fill(selected ? theme.frameHover : (hover ? theme.frameHover.opacity(0.5) : .clear)))
        .contentShape(Rectangle())
        .onHover { hover = $0 }
        .onTapGesture { model.switchSession(index) }
    }
}

// MARK: Session bar

private struct SessionHeader: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let session: Session
    let showTitle: Bool

    var body: some View {
        HStack(spacing: 14) {
            if showTitle {
                Text(session.title).font(Fonts.ui(13, weight: .semibold)).foregroundStyle(theme.text).fixedSize()
            }
            HStack(spacing: 12) {
                label(.folder, session.directory)
                label(.gitBranch, session.branch)
                if let pr = session.pr {
                    HStack(spacing: 5) {
                        Icon(glyph: .gitPullRequest, size: 12, color: theme.text)
                        Text("PR #\(pr)").font(Fonts.mono(11)).foregroundStyle(theme.text)
                    }
                    .padding(.horizontal, 6).frame(height: 20)
                    .background(RoundedRectangle(cornerRadius: 3).fill(theme.chip))
                }
            }
            Spacer(minLength: 24)
            HStack(spacing: 8) {
                Text("CI pass 12/12").foregroundStyle(theme.success)
                Text("·").foregroundStyle(theme.muted)
                (Text("🔬 autoresearch").fontWeight(.bold).foregroundColor(theme.success)
                 + Text(" 14").foregroundColor(theme.text) + Text(" runs ").foregroundColor(theme.muted)
                 + Text("3").foregroundColor(theme.success) + Text(" kept │ best ").foregroundColor(theme.muted)
                 + Text("total_ms: 812ms").fontWeight(.bold).foregroundColor(theme.success))
            }
            .font(Fonts.mono(11))
            .lineLimit(1)
        }
        .padding(.leading, showTitle ? 8 : 16).padding(.trailing, 14)
        .frame(height: showTitle ? 44 : 32)
        .contentShape(Rectangle())
        .gesture(WindowDragGesture())
    }

    private func label(_ glyph: Ph, _ text: String) -> some View {
        HStack(spacing: 5) {
            Icon(glyph: glyph, size: 12, color: theme.muted)
            Text(text).font(Fonts.mono(11)).foregroundStyle(theme.muted).lineLimit(1)
        }
    }
}
