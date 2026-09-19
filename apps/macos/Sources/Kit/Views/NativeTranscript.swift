import AppKit
import SwiftUI

/// Native row reuse owns geometry; SwiftUI continues to own message presentation.
struct NativeTranscript: NSViewRepresentable {
    @Environment(\.transcriptSessionLink) var sessionLink
    let messages: [TranscriptMessage]
    let hasHistory: Bool
    let historyLoading: Bool
    let historyError: String?
    let active: Bool
    let presentation: TranscriptPresentationState
    let workspace: WorkspaceState
    let resumeRequest: Int
    @Binding var latestOutOfView: Bool
    let loadHistory: () -> Void
    let theme: MicaTheme
    var attachmentClient: (any AttachmentClient)? = nil
    var attachmentSession = ""
    @AppStorage("interfaceFont") private var interfaceFont = ""
    @AppStorage("monoFont") private var monoFont = ""
    @AppStorage("interfaceFontSize") private var interfaceSize = 0.0
    @AppStorage("monoFontSize") private var monoSize = 0.0
    var typography: String { "\(interfaceFont)|\(monoFont)|\(interfaceSize)|\(monoSize)" }

    func makeCoordinator() -> NativeTranscriptCoordinator { NativeTranscriptCoordinator(self) }
    func makeNSView(context: Context) -> NSScrollView { context.coordinator.scroll }
    func updateNSView(_ view: NSScrollView, context: Context) { context.coordinator.receive(self) }
    static func dismantleNSView(_ view: NSScrollView, coordinator: NativeTranscriptCoordinator) { coordinator.stop() }
}

@MainActor final class NativeTranscriptCoordinator: NSObject, NSTableViewDataSource, NSTableViewDelegate {
    let scroll = NativeTranscriptScrollView()
    let table = NSTableView()
    private var input: NativeTranscript
    private var messages: [TranscriptMessage] = []
    private var liveGroups: Set<String> = []
    private var attachments: TranscriptAttachmentStore?
    private var heights: [String: CGFloat] = [:]
    private var heightOrder: [String] = []
    private var stopped = false
    private var widthGeneration = 0
    private var follow = TranscriptScrollFollow()
    private var width: CGFloat = 0
    private var viewport: CGFloat = 0
    private var updating = false
    private var lastOutOfView = false
    private var typography = ""
    private var scrollObserver: NSObjectProtocol?
    private var insertionAnchor: Anchor?
    private var pendingMeasurements: [String: PendingMeasurement] = [:]
    private var measurementFlushQueued = false
    private var indices: [String: Int] = [:]

    init(_ input: NativeTranscript) {
        self.input = input
        attachments = input.attachmentClient.map { TranscriptAttachmentStore(client: $0, session: input.attachmentSession) }
        super.init()
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = true
        scroll.backgroundColor = NSColor(input.theme.surface)
        table.headerView = nil
        // Transcript rows own their margins. Automatic/inset table styling adds
        // 16 points per side outside our viewport-sized column and shifts its center.
        table.style = .plain
        table.backgroundColor = NSColor(input.theme.surface)
        table.selectionHighlightStyle = .none
        table.intercellSpacing = NSSize(width: 0, height: 0)
        table.rowHeight = 90
        table.usesAutomaticRowHeights = false
        table.columnAutoresizingStyle = .uniformColumnAutoresizingStyle
        table.autoresizingMask = [.width]
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("transcript"))
        column.resizingMask = .autoresizingMask
        table.addTableColumn(column)
        table.delegate = self
        table.dataSource = self
        scroll.documentView = table
        scroll.startWheelRouting()
        scroll.changed = { [weak self] in self?.viewportChanged() }
        scroll.userScrolled = { [weak self] in self?.userScrolled() }
        scroll.willScroll = { [weak self] event in
            guard let self else { return }
            self.insertionAnchor = nil
            // Unpin before AppKit lays out newly exposed rows during the gesture.
            if event.scrollingDeltaY > 0 {
                self.follow.userScrolled(distanceFromBottom: max(9, self.distanceFromBottom))
            }
        }
        scrollObserver = NotificationCenter.default.addObserver(forName: NSScrollView.didLiveScrollNotification,
            object: scroll, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.userScrolled() }
            }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { messages.count }
    func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
        heights[messages[row].id] ?? 90
    }
    func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { false }
    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let identifier = NSUserInterfaceItemIdentifier("message")
        let cell: NativeTranscriptCell
        if let existing = tableView.makeView(withIdentifier: identifier, owner: nil) as? NativeTranscriptCell {
            cell = existing
        } else {
            cell = NativeTranscriptCell()
            cell.identifier = identifier
        }
        configure(cell, row: row)
        return cell
    }

    private func configure(_ cell: NativeTranscriptCell, row: Int) {

        let message = messages[row]
        let generation = widthGeneration
        let token = UUID()
        cell.configuration = token
        let content: AnyView
        if message.id == Self.headerID {
            content = AnyView(header)
        } else {
            content = AnyView(NativeMessageRow(message: message, presentation: input.presentation,
                                               workspace: input.workspace, inProgress: liveGroups.contains(message.id)) { [weak self] in
                guard let self else { return }
                let current = self.liveGroups.contains(message.id)
                self.insertionAnchor = nil
                _ = self.follow.expandedDrawer(isCurrent: current)
                // Follow after the expanded row has its new height, not once at
                // the collapsed height and again after measurement.
            })
        }
        cell.host.rootView = AnyView(content
            .transaction { $0.animation = nil; $0.disablesAnimations = true }
            .id(message.id).environment(\.mica, input.theme)
            .environment(\.transcriptAttachments, attachments)
            .environment(\.transcriptSessionLink, input.sessionLink)
            .foregroundStyle(input.theme.text)
            .environment(\.colorScheme, input.theme.dark ? .dark : .light)
            .frame(width: min(780, max(1, scroll.contentSize.width - 64)), alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .center)
            .padding(.horizontal, 32).padding(.top, message.id == Self.headerID ? 32 : 12).padding(.bottom, 12)
            .onGeometryChange(for: Measurement.self) {
                Measurement(height: $0.size.height, configuration: token)
            } action: { [weak self, weak cell] measurement in
                guard let self, let cell else { return }
                self.enqueue(measurement.height, id: message.id, cell: cell,
                             configuration: token, generation: generation)
            })
    }

    private struct Measurement: Equatable {
        let height: CGFloat
        let configuration: UUID
    }

    private struct PendingMeasurement {
        weak var cell: NativeTranscriptCell?
        let height: CGFloat
        let configuration: UUID
        let generation: Int
    }

    private func enqueue(_ height: CGFloat, id: String, cell: NativeTranscriptCell,
                         configuration: UUID, generation: Int) {
        guard !stopped, height.isFinite, height > 0 else { return }
        pendingMeasurements[id] = PendingMeasurement(cell: cell, height: ceil(height),
            configuration: configuration, generation: generation)
        guard !measurementFlushQueued else { return }
        measurementFlushQueued = true
        DispatchQueue.main.async { [weak self] in self?.flushMeasurements() }
    }

    private func flushMeasurements() {
        measurementFlushQueued = false
        let pending = pendingMeasurements
        pendingMeasurements.removeAll()
        guard !stopped else { return }
        let saved = insertionAnchor ?? anchor()
        var changed = IndexSet()
        for (id, value) in pending {
            guard value.generation == widthGeneration,
                  value.cell?.configuration == value.configuration,
                  let row = indices[id], abs((heights[id] ?? 90) - value.height) > 0.5 else { continue }
            heights[id] = value.height
            heightOrder.removeAll { $0 == id }
            heightOrder.append(id)
            changed.insert(row)
        }
        while heightOrder.count > 256 { heights.removeValue(forKey: heightOrder.removeFirst()) }
        guard !changed.isEmpty else { return }
        // Resize all changed rows and correct the viewport in one nonanimated
        // layout transaction. Expanded content stays clipped until this commits.
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0
            context.allowsImplicitAnimation = false
            table.noteHeightOfRows(withIndexesChanged: changed)
            table.layoutSubtreeIfNeeded()
            if follow.followsBottom { positionAtBottom() }
            else if let saved { restore(saved) }
        }
        CATransaction.commit()
        publishVisibility()
    }

    struct Anchor {
        let id: String
        let inset: CGFloat
    }
    func anchor() -> Anchor? {
        let visible = table.row(at: NSPoint(x: 1, y: scroll.contentView.bounds.minY + 1))
        // The loading header is not reading content. Keep the first message's
        // negative inset when it sits below that header at the absolute top.
        let index = messages.first?.id == Self.headerID && messages.count > 1 ? max(1, visible) : visible
        guard messages.indices.contains(index) else { return nil }
        return Anchor(id: messages[index].id, inset: scroll.contentView.bounds.minY - table.rect(ofRow: index).minY)
    }
    func restore(_ anchor: Anchor) {
        guard let index = indices[anchor.id] else { return }
        table.layoutSubtreeIfNeeded()
        scrollToOffset(table.rect(ofRow: index).minY + anchor.inset)
    }
    func invalidateHeights() {
        let saved = insertionAnchor ?? anchor()
        scroll.layoutSubtreeIfNeeded()
        widthGeneration += 1
        heights.removeAll()
        heightOrder.removeAll()
        table.frame.size.width = scroll.contentSize.width
        table.tableColumns[0].width = scroll.contentSize.width
        let visible = table.rows(in: scroll.contentView.bounds)
        if visible.location != NSNotFound {
            for row in visible.location..<min(messages.count, NSMaxRange(visible)) {
                if let cell = table.view(atColumn: 0, row: row, makeIfNecessary: false) as? NativeTranscriptCell {
                    configure(cell, row: row)
                }
            }
        }
        if follow.followsBottom { positionAtBottom() }
        else if let saved { restore(saved) }
        publishVisibility()
    }
    var distanceFromBottom: CGFloat {
        max(0, table.bounds.height - scroll.contentView.bounds.maxY)
    }

    private func positionAtBottom() {
        guard !messages.isEmpty else { return }
        table.layoutSubtreeIfNeeded()
        scrollToOffset(max(0, table.bounds.height - scroll.contentSize.height))
    }
    private func scrollToOffset(_ offset: CGFloat) {
        let target = min(max(0, offset), max(0, table.bounds.height - scroll.contentSize.height))
        guard abs(scroll.contentView.bounds.minY - target) > 0.5 else { return }
        scroll.contentView.scroll(to: NSPoint(x: 0, y: target))
        scroll.reflectScrolledClipView(scroll.contentView)
    }

    private static let headerID = "native-transcript-header"
    private var header: some View {
        VStack(spacing: 24) {
            if input.hasHistory || input.historyError != nil {
                HStack {
                    if input.historyLoading {
                        KitSpinner()
                        Text("Loading earlier messages…")
                    } else if let error = input.historyError {
                        Text(error).textSelection(.enabled)
                        Button("Retry", action: input.loadHistory).buttonStyle(MicaButtonStyle(compact: true))
                    } else {
                        Button("Load earlier messages", action: input.loadHistory).buttonStyle(MicaButtonStyle(compact: true))
                    }
                }.font(.kit(size: 12)).foregroundStyle(input.theme.muted)
            }
        }
    }

    func receive(_ next: NativeTranscript) {
        guard !stopped else { return }
        updating = true
        defer { updating = false }
        let saved = insertionAnchor ?? anchor()
        let old = messages
        let styleChanged = input.theme != next.theme || typography != next.typography
            || input.sessionLink?.serverID != next.sessionLink?.serverID
            || input.presentation !== next.presentation || input.workspace !== next.workspace
            || input.attachmentSession != next.attachmentSession || input.attachmentClient?.serverID != next.attachmentClient?.serverID
        let resume = next.resumeRequest != input.resumeRequest
        if input.attachmentSession != next.attachmentSession || input.attachmentClient?.serverID != next.attachmentClient?.serverID {
            attachments = next.attachmentClient.map { TranscriptAttachmentStore(client: $0, session: next.attachmentSession) }
        }
        let oldLiveGroups = liveGroups
        liveGroups = Set(ToolGroupActivity.liveGroups(in: next.messages, active: next.active).map { "message:" + $0 })
        let activityChanged = oldLiveGroups.symmetricDifference(liveGroups)
        input = next
        typography = next.typography
        scroll.backgroundColor = NSColor(next.theme.surface)
        table.backgroundColor = NSColor(next.theme.surface)
        let header = TranscriptMessage(id: Self.headerID, role: "header",
            text: "\(next.hasHistory)|\(next.historyLoading)|\(next.historyError ?? "")", tools: [])
        // Namespace message identities separately from container-owned rows.
        let headers = next.hasHistory || next.historyError != nil ? [header] : []
        messages = headers + next.messages.map {
            TranscriptMessage(id: "message:" + $0.id, role: $0.role, text: $0.text, tools: $0.tools, attachments: $0.attachments, bash: $0.bash)
        }
        indices = Dictionary(uniqueKeysWithValues: messages.enumerated().map { ($0.element.id, $0.offset) })
        let diff = messages.map(\.id).difference(from: old.map(\.id))
        var removals = IndexSet(), insertions = IndexSet()
        for change in diff {
            switch change {
            case .remove(let offset, _, _): removals.insert(offset)
            case .insert(let offset, _, _): insertions.insert(offset)
            }
        }
        if old.isEmpty { table.reloadData() }
        else if !diff.isEmpty {
            // Keep this content anchor through asynchronous measurements of the
            // inserted page, including the row just above the old first message.
            if !follow.followsBottom { insertionAnchor = saved }
            table.beginUpdates()
            table.removeRows(at: removals, withAnimation: [])
            table.insertRows(at: insertions, withAnimation: [])
            table.endUpdates()
        }
        let previous = Dictionary(uniqueKeysWithValues: old.map { ($0.id, $0) })
        for (row, message) in messages.enumerated() where previous[message.id] != message || styleChanged || activityChanged.contains(message.id) {
            heights.removeValue(forKey: message.id)
            heightOrder.removeAll { $0 == message.id }
            if !insertions.contains(row), let cell = table.view(atColumn: 0, row: row, makeIfNecessary: false) as? NativeTranscriptCell {
                configure(cell, row: row)
            }
        }
        if resume { follow.resume() }
        if styleChanged { invalidateHeights() }
        if follow.followsBottom { positionAtBottom() }
        else if !diff.isEmpty, let saved { restore(saved) }
        publishVisibility()
    }

    private func viewportChanged() {
        guard !stopped, !updating else { return }
        let newWidth = scroll.contentSize.width
        let newHeight = scroll.contentSize.height
        if abs(newWidth - width) > 0.5 {
            width = newWidth
            invalidateHeights()
        }
        if newHeight != viewport {
            viewport = newHeight
            if follow.followsBottom { positionAtBottom() }
        }
        publishVisibility()
    }

    private func userScrolled() {
        guard !stopped else { return }
        insertionAnchor = nil
        follow.userScrolled(distanceFromBottom: distanceFromBottom)
        publishVisibility()
        if scroll.contentView.bounds.minY < 240, !follow.followsBottom,
           input.hasHistory, !input.historyLoading, input.historyError == nil {
            input.loadHistory()
        }
    }

    private func publishVisibility() {
        guard !stopped else { return }
        let outOfView: Bool
        if messages.count <= 1 { outOfView = false }
        else {
            let frame = table.rect(ofRow: messages.count - 1)
            let viewport = scroll.contentView.bounds
            outOfView = frame.maxY <= viewport.minY || frame.minY >= viewport.maxY
        }
        guard outOfView != lastOutOfView else { return }
        lastOutOfView = outOfView
        DispatchQueue.main.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.input.latestOutOfView = self.lastOutOfView
        }
    }

    func stop() {
        stopped = true
        pendingMeasurements.removeAll()
        if let scrollObserver { NotificationCenter.default.removeObserver(scrollObserver) }
        scrollObserver = nil
        scroll.stopWheelRouting()
        scroll.changed = nil
        scroll.userScrolled = nil
        scroll.willScroll = nil
        table.delegate = nil
        table.dataSource = nil
        scroll.documentView = nil
    }
}

@MainActor final class NativeTranscriptCell: NSTableCellView {
    var configuration = UUID()
    let host = NSHostingView(rootView: AnyView(EmptyView()))
    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.masksToBounds = true
        host.translatesAutoresizingMaskIntoConstraints = false
        host.sizingOptions = [.intrinsicContentSize]
        addSubview(host)
        NSLayoutConstraint.activate([
            host.leadingAnchor.constraint(equalTo: leadingAnchor),
            host.trailingAnchor.constraint(equalTo: trailingAnchor),
            host.topAnchor.constraint(equalTo: topAnchor)
        ])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }
}


/// Only input-driven scrolling changes the follow policy; layout scrolls do not.
@MainActor final class NativeTranscriptScrollView: NSScrollView {
    var changed: (() -> Void)?
    var userScrolled: (() -> Void)?
    var willScroll: ((NSEvent) -> Void)?
    private var queued = false
    private var wheelMonitor: Any?

    func startWheelRouting() {
        wheelMonitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
            guard let self, let window = self.window, event.window === window,
                  abs(event.scrollingDeltaY) > abs(event.scrollingDeltaX),
                  self.contentView.bounds.contains(self.contentView.convert(event.locationInWindow, from: nil)),
                  let hit = self.contentView.hitTest(self.convert(event.locationInWindow, from: nil)) else { return event }
            let target = self.verticalRecipient(from: hit, delta: event.scrollingDeltaY)
            // Native child scrollers consume wheel events even at their boundary.
            // Chain to the nearest ancestor that can scroll in this direction.
            let nearest = (hit as? NSScrollView) ?? hit.enclosingScrollView
            if target !== nearest {
                target.scrollWheel(with: event)
                return nil
            }
            return event
        }
    }

    func verticalRecipient(from hit: NSView, delta: CGFloat) -> NSScrollView {
        var view: NSView? = hit
        while let current = view, current !== self {
            if let candidate = current as? NSScrollView, candidate !== self,
               let document = candidate.documentView {
                let maximum = max(0, document.bounds.height - candidate.contentSize.height)
                let origin = candidate.contentView.bounds.minY - document.bounds.minY
                let fromTop = document.isFlipped ? origin : maximum - origin
                if maximum > 1, (delta > 0 && fromTop > 0.5) || (delta < 0 && fromTop < maximum - 0.5) {
                    return candidate
                }
            }
            view = current.superview
        }
        return self
    }

    func stopWheelRouting() {
        if let wheelMonitor { NSEvent.removeMonitor(wheelMonitor) }
        wheelMonitor = nil
    }

    override func layout() {
        super.layout()
        guard !queued else { return }
        queued = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.queued = false
            self.changed?()
        }
    }
    override func scrollWheel(with event: NSEvent) {
        willScroll?(event)
        super.scrollWheel(with: event)
        userScrolled?()
    }
}
private struct NativeMessageRow: View {
    let message: TranscriptMessage
    let presentation: TranscriptPresentationState
    let workspace: WorkspaceState
    let inProgress: Bool
    let onExpand: () -> Void
    @Environment(\.mica) private var theme
    var body: some View {
        if let bash = message.bash {
            BashExecutionView(execution: bash)
        } else if message.role == "tools" {
            VStack(alignment: .leading, spacing: 12) {
                ToolActivityView(tools: message.tools, workspace: workspace,
                                 state: presentation.drawer(for: String(message.id.dropFirst("message:".count))), inProgress: inProgress, onExpand: onExpand)
                ForEach(message.tools.filter { $0.name == "show_image" }) { tool in
                    TranscriptAttachments(attachments: (tool.attachments ?? []).filter(\.isImage))
                }
            }
        } else {
                                VStack(alignment: .leading, spacing: 12) {
                                    if message.role != "assistant" && message.role != "user" {
                                        Text(message.role == "preview" ? "Preview" : "Compacted context")
                                            .font(.kit(size: 12, weight: .semibold))
                                    }
                                    if !message.text.isEmpty { MarkdownView(source: message.text) }
                                    TranscriptAnnotations(annotations: message.annotations ?? [])
                                    TranscriptAttachments(attachments: message.attachments ?? [])
                                }
                                .padding(.horizontal, message.role == "user" ? 20 : 0)
                                .padding(.vertical, message.role == "user" ? 18 : 0)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background {
                                    if message.role == "user" {
                                        RoundedRectangle(cornerRadius: 10).fill(theme.raised)
                                        RoundedRectangle(cornerRadius: 10).fill(theme.accent.opacity(theme.dark ? 0.18 : 0.12))
                                    }
                                }
                                .padding(.vertical, message.role == "user" ? 6 : 0)
                                .background(MessageCopyRegion(markdown: message.text))
        }
    }
}
