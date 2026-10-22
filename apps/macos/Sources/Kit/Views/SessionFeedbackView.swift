import SwiftUI

/// Toast metrics from Paper's "Explore · toasts" board.
enum ToastMetrics {
    static let width: CGFloat = 360
    /// A toast showing a reload report widens while it is open.
    static let reportWidth: CGFloat = 560
    static let spacing: CGFloat = 8
    /// The stack folds from this many toasts.
    static let foldCount = 5
    static let listedHeight: CGFloat = 420
    static let detailHeight: CGFloat = 220
}

/// Session-owned feedback floats over the workspace rather than resizing the composer.
struct SessionFeedbackView: View {
    @Environment(\.mica) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @Bindable var feedback: SessionFeedback
    @State private var expanded = false
    @State private var expandedReportID: UUID?
    @State private var hovered = false
    @State private var focusedIDs = Set<UUID>()
    @FocusState private var stackControlFocused: Bool

    private var newestFirst: [SessionFeedback.Item] { Array(feedback.items.reversed()) }
    private var paused: Bool { hovered || stackControlFocused || !focusedIDs.isEmpty || scenePhase != .active }

    var body: some View {
        VStack(alignment: .trailing, spacing: 0) {
            if newestFirst.count >= ToastMetrics.foldCount && !expanded {
                foldedStack
            } else if !newestFirst.isEmpty {
                if expanded {
                    HStack {
                        Text("\(newestFirst.count) in this session").foregroundStyle(theme.muted)
                        Spacer()
                        Button { expanded = false; expandedReportID = nil } label: {
                            HStack(spacing: 4) {
                                Text("Collapse")
                                IconView(.caretUp, size: 10)
                            }
                        }
                        .buttonStyle(.plain).foregroundStyle(theme.muted)
                        .focused($stackControlFocused)
                    }.font(.kit(size: 12)).padding(.horizontal, 2).padding(.bottom, ToastMetrics.spacing)
                    ScrollView { toastRows }
                        .scrollIndicators(.never)
                        .frame(maxHeight: ToastMetrics.listedHeight)
                } else {
                    toastRows
                }
            }
        }
        .frame(width: expandedReportID == nil ? ToastMetrics.width : ToastMetrics.reportWidth, alignment: .topTrailing)
        .foregroundStyle(theme.text)
        .onHover { hovered = $0 }
        .onExitCommand {
            if expanded && (stackControlFocused || !focusedIDs.isEmpty) {
                expanded = false
                expandedReportID = nil
                stackControlFocused = true // Move focus to the folded count.
            }
        }
        .onChange(of: newestFirst.map(\.id)) { previous, ids in
            if let newest = feedback.items.last, !previous.contains(newest.id) { announce(newest) }
            if ids.count < ToastMetrics.foldCount { expanded = false }
            focusedIDs.formIntersection(ids)
            if let expandedReportID, !ids.contains(expandedReportID) { self.expandedReportID = nil }
        }
        .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: newestFirst.map(\.id))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session toasts, \(newestFirst.count) items")
        .overlay(alignment: .topLeading) {
            // Timers belong to the session stack, not a visible card: folding
            // the stack must not restart or suspend an ephemeral toast's life.
            ForEach(feedback.items.filter { !$0.persistent }) { item in
                SessionToastExpiry(paused: paused, initialRemaining: feedback.remainingLifetime(for: item.id),
                                   save: { feedback.saveRemainingLifetime($0, for: item.id) },
                                   expire: { feedback.expire(item.id) })
                    .frame(width: 0, height: 0).accessibilityHidden(true)
            }
        }
    }

    private var toastRows: some View {
        LazyVStack(alignment: .trailing, spacing: ToastMetrics.spacing) {
            ForEach(newestFirst) { item in
                card(item)
            }
        }
    }

    private func card(_ item: SessionFeedback.Item) -> some View {
        SessionFeedbackCard(item: item, close: { feedback.dismiss(item.id) },
                            stackExpanded: expanded,
                            reportExpanded: expandedReportID == item.id,
                            setReportExpanded: { expandedReportID = $0 ? item.id : nil },
                            focusChanged: { setFocused(item.id, $0) })
            .frame(width: expandedReportID == item.id ? ToastMetrics.reportWidth : ToastMetrics.width)
    }

    /// The newest toast with two card edges below it, then a count that
    /// lists the whole stack.
    private var foldedStack: some View {
        VStack(alignment: .trailing, spacing: ToastMetrics.spacing) {
            if let item = newestFirst.first {
                VStack(spacing: 0) {
                    card(item)
                    ToastEdge(inset: 6).opacity(0.85).zIndex(-1)
                    ToastEdge(inset: 12).opacity(0.6).zIndex(-2)
                }
            }
            Button { expanded = true } label: {
                HStack(spacing: 6) {
                    Text("\(newestFirst.count - 1) more").foregroundStyle(theme.text)
                    IconView(.caretDown, size: 10)
                        .foregroundStyle(theme.muted)
                }
                .font(.kit(size: 12)).padding(.horizontal, 8).frame(height: 22)
                .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).focused($stackControlFocused)
            .accessibilityLabel("List \(newestFirst.count) session toasts; newest: \(newestFirst.first?.title ?? "")")
        }
    }

    /// Toasts don't take focus, so VoiceOver hears them as announcements.
    private func announce(_ item: SessionFeedback.Item) {
        AccessibilityAnnouncement.post(SessionFeedback.announcement(for: item), urgent: item.tone != .info)
    }

    private func setFocused(_ id: UUID, _ focused: Bool) {
        if focused { focusedIDs.insert(id) } else { focusedIDs.remove(id) }
    }
}

/// The lower edge of a card folded under the newest toast.
private struct ToastEdge: View {
    @Environment(\.mica) private var theme
    let inset: CGFloat

    var body: some View {
        UnevenRoundedRectangle(bottomLeadingRadius: 4, bottomTrailingRadius: 4)
            .fill(theme.surface)
            .overlay(UnevenRoundedRectangle(bottomLeadingRadius: 4, bottomTrailingRadius: 4)
                .stroke(theme.border, lineWidth: 1))
            .frame(height: 5)
            .padding(.horizontal, inset)
            .accessibilityHidden(true)
    }
}

/// Counts unpaused time even when its toast is behind the folded stack.
private struct SessionToastExpiry: View {
    let paused: Bool
    let save: (TimeInterval) -> Void
    let expire: () -> Void
    @State private var remaining: TimeInterval
    @State private var runningSince: Date?

    init(paused: Bool, initialRemaining: TimeInterval, save: @escaping (TimeInterval) -> Void,
         expire: @escaping () -> Void) {
        self.paused = paused
        self.save = save
        self.expire = expire
        _remaining = State(initialValue: initialRemaining)
    }

    var body: some View {
        Color.clear
            .task(id: paused) {
                guard !paused else { return }
                runningSince = Date()
                do { try await Task.sleep(for: .seconds(remaining)) } catch { return }
                guard !Task.isCancelled else { return }
                expire()
            }
            .onChange(of: paused) { _, isPaused in
                if isPaused { pauseClock() }
            }
            .onDisappear { pauseClock() }
    }

    private func pauseClock() {
        if let runningSince {
            remaining = max(0, remaining - Date().timeIntervalSince(runningSince))
            self.runningSince = nil
            save(remaining)
        }
    }
}

/// One toast: a surface card with a border, icon, and title in its tone's
/// colour, a two-line detail, and small actions.
struct SessionFeedbackCard: View {
    @Environment(\.mica) private var theme
    let item: SessionFeedback.Item
    let close: () -> Void
    var interactive = true
    var stackExpanded = false
    var reportExpanded = false
    var setReportExpanded: (Bool) -> Void = { _ in }
    var focusChanged: (Bool) -> Void = { _ in }
    @State private var expanded = false
    @State private var clampedHeight: CGFloat = 0
    @State private var fullHeight: CGFloat = 0
    @FocusState private var focused: Bool

    private var color: Color {
        switch item.tone { case .info: theme.accent; case .warning: theme.warning; case .error: theme.danger }
    }
    private var icon: Icon {
        if let icon = item.icon { return icon }
        return switch item.tone { case .info: .info; case .warning: .warning; case .error: .warningCircle }
    }
    private var isExpanded: Bool { item.reloadReport == nil ? expanded : reportExpanded }
    /// Details appear only when there is more to read than the two lines.
    private var hasMore: Bool { item.reloadReport != nil || fullHeight > clampedHeight + 1 }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            IconView(icon).foregroundStyle(color)
                .frame(height: 16)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(item.title).font(.kit(size: 13, weight: .medium)).foregroundStyle(color)
                        .fixedSize(horizontal: false, vertical: true)
                    if item.count > 1 {
                        Text("×\(item.count)").font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                            .fixedSize()
                    }
                    Spacer(minLength: 0)
                    if let source = item.source {
                        Text(source).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                            .lineLimit(1)
                    }
                }
                if let report = item.reloadReport, isExpanded {
                    SessionReloadEvidenceView(report: report, outerScrollable: stackExpanded)
                } else if !item.detail.isEmpty {
                    detail
                }
                if interactive && (item.action != nil || (hasMore && !item.detail.isEmpty)) {
                    actions.padding(.top, 4)
                }
            }.frame(maxWidth: .infinity, alignment: .leading)
            if interactive {
                Button(action: close) {
                    IconView(.x, size: 10).foregroundStyle(theme.muted)
                        .frame(width: 18, height: 16).contentShape(Rectangle())
                }
                .buttonStyle(.plain).focused($focused)
                .accessibilityLabel(item.persistent ? "Acknowledge \(item.title)" : "Dismiss \(item.title)")
            }
        }
        .padding(.top, 11).padding(.leading, 12).padding(.trailing, 8).padding(.bottom, 12)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(color, lineWidth: 1))
        .shadow(color: .black.opacity(theme.dark ? 0.45 : 0.15), radius: 12, y: 8)
        .onChange(of: focused) { _, value in focusChanged(value) }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(item.title)\(item.count > 1 ? ", repeated \(item.count) times" : "")\(item.source.map { ", from " + $0 } ?? ""). \(item.reloadReport?.accessibilityDetail ?? item.detail)")
    }

    /// Two lines, or the whole text once opened, scrolling past 220 pt.
    private var detail: some View {
        let text = Text(item.detail).font(.kit(size: 12)).lineSpacing(2).foregroundStyle(theme.muted)
            .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
        return Group {
            if isExpanded && fullHeight > ToastMetrics.detailHeight {
                ScrollView { text.fixedSize(horizontal: false, vertical: true) }
                    .frame(height: ToastMetrics.detailHeight)
            } else {
                text.lineLimit(isExpanded ? nil : 2)
                    .fixedSize(horizontal: false, vertical: true)
                    .onGeometryChange(for: CGFloat.self, of: \.size.height) { if !isExpanded { clampedHeight = $0 } }
            }
        }
        .background(alignment: .topLeading) {
            // The unclamped text, measured at the same width, tells whether
            // the two visible lines hide any more.
            Text(item.detail).font(.kit(size: 12)).lineSpacing(2)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .hidden()
                .onGeometryChange(for: CGFloat.self, of: \.size.height) { fullHeight = $0 }
        }
    }

    private var actions: some View {
        HStack(spacing: 8) {
            if let title = item.actionTitle, let action = item.action {
                // Acting on a toast resolves it.
                Button(title) { action(); close() }
                    .buttonStyle(SmallButtonStyle(bordered: true)).focused($focused)
            }
            if hasMore && !item.detail.isEmpty {
                Button {
                    if item.reloadReport != nil { setReportExpanded(!reportExpanded) } else { expanded.toggle() }
                } label: {
                    HStack(spacing: 4) {
                        Text(isExpanded ? "Less" : "Details")
                        IconView(isExpanded ? .caretUp : .caretDown, size: 10)
                    }
                    .font(.kit(size: 12)).foregroundStyle(theme.muted).frame(height: 22).contentShape(Rectangle())
                }
                .buttonStyle(.plain).focused($focused)
            }
        }
    }
}

/// A reload report remains a toast: compact evidence first, optional sources folded away.
struct SessionReloadEvidenceView: View {
    @Environment(\.mica) private var theme
    let report: SessionReloadReport
    let outerScrollable: Bool
    @State private var sourcesExpanded = false

    init(report: SessionReloadReport, outerScrollable: Bool, sourcesInitiallyExpanded: Bool = false) {
        self.report = report
        self.outerScrollable = outerScrollable
        _sourcesExpanded = State(initialValue: sourcesInitiallyExpanded)
    }

    var body: some View {
        Group {
            // Ordinary reports lay out naturally. Exceptionally long diagnostics
            // are bounded and scrollable without adding a second visible scrollbar.
            if !outerScrollable && (report.sources.count > 4 || report.diagnostics.count > 4
                || report.diagnostics.contains(where: { $0.message.count > 280 })) {
                ScrollView { evidence }
                    .scrollIndicators(.never)
                    .frame(maxHeight: 360)
            } else {
                evidence
            }
        }
        .padding(.top, 5)
    }

    private var evidence: some View {
        VStack(alignment: .leading, spacing: 11) {
            ForEach(report.diagnostics.indices, id: \.self) { index in
                let diagnostic = report.diagnostics[index]
                VStack(alignment: .leading, spacing: 4) {
                    Text(diagnostic.message)
                        .font(.kit(size: 12))
                        .foregroundStyle(theme.text)
                        .textSelection(.enabled)
                    if let path = diagnostic.sourcePath ?? diagnostic.sourceID {
                        Text(path)
                            .font(.system(size: 11, design: .monospaced))
                            .foregroundStyle(theme.muted)
                            .textSelection(.enabled)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !report.sources.isEmpty {
                Divider().overlay(theme.border)
                Button { sourcesExpanded.toggle() } label: {
                    HStack(spacing: 6) {
                        IconView(sourcesExpanded ? .caretDown : .caretRight, size: 10)
                        Text("Loaded sources (\(report.sources.count))")
                        Spacer()
                        Text(sourcesExpanded ? "Hide" : "Show")
                    }
                    .font(.kit(size: 11))
                    .foregroundStyle(theme.muted)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(sourcesExpanded ? "Hide" : "Show") loaded sources, \(report.sources.count)")
                if sourcesExpanded {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(report.sources.indices, id: \.self) { index in
                            let source = report.sources[index]
                            VStack(alignment: .leading, spacing: 2) {
                                Text(source.id).font(.kit(size: 11))
                                if let path = source.path {
                                    Text(path).font(.system(size: 11, design: .monospaced))
                                }
                            }
                            .foregroundStyle(theme.muted)
                            .textSelection(.enabled)
                        }
                    }
                    .padding(.top, 4)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
