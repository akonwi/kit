import SwiftUI

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
            if newestFirst.count >= 5 && !expanded {
                foldedStack
            } else if !newestFirst.isEmpty {
                if expanded {
                    HStack {
                        Text("\(newestFirst.count) in this session").foregroundStyle(theme.muted)
                        Spacer()
                        Button("Collapse", systemImage: "chevron.up") {
                            expanded = false; expandedReportID = nil
                        }
                            .labelStyle(.titleAndIcon).buttonStyle(.plain).foregroundStyle(theme.accent)
                            .focused($stackControlFocused)
                    }.font(.kit(size: 11)).padding(.horizontal, 4).padding(.bottom, 8)
                }
                if expanded {
                    ScrollView { toastRows }
                        .frame(maxHeight: 420)
                } else {
                    toastRows
                }
            }
        }
        .frame(width: expandedReportID == nil ? 360 : 568, alignment: .topTrailing)
        .foregroundStyle(theme.text)
        .onHover { hovered = $0 }
        .onExitCommand {
            if expanded && (stackControlFocused || !focusedIDs.isEmpty) {
                expanded = false
                expandedReportID = nil
                stackControlFocused = true // Move focus to the folded Expand control.
            }
        }
        .onChange(of: newestFirst.map(\.id)) { _, ids in
            if ids.count < 5 { expanded = false }
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
        LazyVStack(alignment: .trailing, spacing: 8) {
            ForEach(newestFirst) { item in
                SessionFeedbackCard(item: item, close: { feedback.dismiss(item.id) },
                                    stackExpanded: expanded,
                                    reportExpanded: expandedReportID == item.id,
                                    setReportExpanded: { expandedReportID = $0 ? item.id : nil },
                                    focusChanged: { setFocused(item.id, $0) })
                    .frame(width: expandedReportID == item.id ? 564 : 356)
            }
        }.padding(.trailing, 4).padding(.bottom, 3)
    }

    private var foldedStack: some View {
        Button { expanded = true } label: {
            ZStack(alignment: .top) {
                RoundedRectangle(cornerRadius: 9).fill(theme.raised)
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(theme.border))
                    .frame(height: 74).padding(.horizontal, 8).offset(y: 14)
                RoundedRectangle(cornerRadius: 9).fill(theme.raised)
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(theme.border))
                    .frame(height: 74).padding(.horizontal, 4).offset(y: 7)
                if let item = newestFirst.first {
                    SessionFeedbackCard(item: item, close: {}, interactive: false)
                }
            }
            .fixedSize(horizontal: false, vertical: true)
            .padding(.bottom, 15)
            .overlay(alignment: .bottomTrailing) {
                Text("+\(newestFirst.count - 1)")
                    .font(.kit(size: 11, weight: .medium))
                    .padding(.horizontal, 9).padding(.vertical, 4)
                    .background(theme.hover, in: Capsule())
                    .overlay(Capsule().stroke(theme.border))
            }
        }
        .buttonStyle(.plain).focused($stackControlFocused)
        .accessibilityLabel("Expand \(newestFirst.count) session toasts; newest: \(newestFirst.first?.title ?? "")")
    }

    private func setFocused(_ id: UUID, _ focused: Bool) {
        if focused { focusedIDs.insert(id) } else { focusedIDs.remove(id) }
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
    @FocusState private var focused: Bool
    private var color: Color {
        switch item.tone { case .info: theme.accent; case .warning: theme.warning; case .error: theme.danger }
    }
    private var symbol: String {
        switch item.tone { case .info: "checkmark.circle"; case .warning: "exclamationmark.triangle"; case .error: "exclamationmark.circle" }
    }
    private var isExpanded: Bool { item.reloadReport == nil ? expanded : reportExpanded }

    var body: some View {
        HStack(alignment: .top, spacing: 11) {
            Image(systemName: symbol).foregroundStyle(color).padding(.top, 2).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 5) {
                Text(item.title).font(.kit(size: 13, weight: .medium))
                if let report = item.reloadReport, isExpanded {
                    SessionReloadEvidenceView(report: report, outerScrollable: stackExpanded)
                } else if !item.detail.isEmpty {
                    Group {
                        if isExpanded {
                            ScrollView {
                                Text(item.detail).frame(maxWidth: .infinity, alignment: .leading)
                                    .textSelection(.enabled)
                            }.frame(maxHeight: 220)
                        } else {
                            Text(String(item.detail.prefix { $0 != "\n" }))
                                .lineLimit(2).textSelection(.enabled)
                        }
                    }.font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
                if interactive {
                    HStack(spacing: 14) {
                        if let title = item.actionTitle, let action = item.action {
                            Button(title, action: action).focused($focused)
                        }
                        if item.reloadReport != nil && isExpanded { Spacer(minLength: 0) }
                        if item.persistent && !item.detail.isEmpty {
                            Button(isExpanded ? "Less detail" : "View details") {
                                if item.reloadReport != nil { setReportExpanded(!reportExpanded) }
                                else { expanded.toggle() }
                            }.focused($focused)
                        }
                    }.font(.kit(size: 12)).foregroundStyle(theme.accent).buttonStyle(.plain)
                }
            }.frame(maxWidth: .infinity, alignment: .leading)
            if interactive && item.persistent {
                Button(action: close) { Image(systemName: "xmark").font(.kit(size: 11)).foregroundStyle(theme.muted) }
                    .buttonStyle(.plain).frame(width: 24, height: 24).focused($focused)
                    .accessibilityLabel("Acknowledge \(item.title)")
            }
        }
        .padding(14).background(theme.raised, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).stroke(theme.border, lineWidth: 1))
        .onChange(of: focused) { _, value in focusChanged(value) }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(item.title). \(item.reloadReport?.accessibilityDetail ?? item.detail)")
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
                    .scrollIndicators(.hidden)
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
                        Image(systemName: sourcesExpanded ? "chevron.down" : "chevron.right")
                            .font(.kit(size: 9))
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
