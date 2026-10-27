import AppKit
import SwiftUI
import UniformTypeIdentifiers
import ImageIO

struct ComposerView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    /// Square top corners where the queued tray sits on the composer.
    var joinedTop = false
    @State private var mentions = ComposerMentionState()
    @State private var history = ComposerHistoryState()
    @State private var pickerAnchor = ComposerPickerAnchor()
    @State private var focused = false
    @State private var attachmentDropTargeted = false
    @State private var popover = AnchoredPopover()
    @State private var openMenu: ComposerChipMenu?
    @State private var lastClosedMenu: ComposerChipMenu?
    @State private var modelAnchor = ComposerPickerAnchor()
    @State private var thinkingAnchor = ComposerPickerAnchor()

    static func modelLabel(_ model: WireModelCapability) -> String {
        "\(model.name) · \(model.provider)"
    }

    static func activeModelLabel(id: String, models: [WireModelCapability]) -> String {
        models.first(where: { $0.id == id })?.name
            ?? String(id.split(separator: "/", maxSplits: 1).last ?? "")
    }

    /// The thinking levels the model offers, or none when it has none or is unknown.
    static func thinkingLevels(model: String, models: [WireModelCapability]) -> [String] {
        models.first(where: { $0.id == model })?.thinkingLevels?.map(\.rawValue) ?? []
    }

    var body: some View {
        @Bindable var ui = state.ui
        VStack(alignment: .leading, spacing: 0) {
            if let error = state.configuration.error {
                HStack {
                    Text(error).foregroundStyle(theme.muted)
                    Button("Dismiss") { state.configuration.error = nil }
                }.font(.kit(size: 12)).padding(16)
            }
            if !state.isDemo && showOperationDetails {
                ComposerOperationsView(state: state)
            }
            AnnotationStrip(state: state)
            RestoredAttachmentStrip(state: state)
            if !state.ui.attachments.isEmpty || !state.ui.workspace.notes.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        if !state.ui.workspace.notes.isEmpty {
                            attachmentChip("Review · \(state.ui.workspace.notes.count) notes", icon: .chatCenteredText) {
                                state.ui.workspace.notes = [:]
                            } content: {
                                Button("Review · \(state.ui.workspace.notes.count) notes") { state.ui.workspace.open(.review) }
                                    .buttonStyle(.plain)
                            }
                        }
                        ForEach(state.ui.attachments, id: \.self) { name in
                            if let preview = state.ui.attachmentPreviews[name] {
                                imageAttachment(name, preview: preview)
                            } else {
                                attachmentChip(name, icon: attachmentIcon(name)) {
                                    removeAttachment(name)
                                } content: {
                                    Text(name).lineLimit(1).truncationMode(.middle).frame(maxWidth: 220)
                                }
                            }
                        }
                    }.padding(.horizontal, 16).padding(.vertical, 10)
                }
                .scrollIndicators(.never)
                .background(HorizontalWheelScrolling())
                .background(theme.raised)
                Rule()
            }
            if !state.isDemo { ComposerAttachmentStrip(state: state) }
            GrowingComposerEditor(text: $ui.draft, focused: $focused,
                                  focusRequest: state.ui.composerFocus,
                                  focusAtEndRequest: state.ui.composerFocusAtEndRequest,
                                  foreground: theme.text, placeholderColor: theme.muted,
                                  attachmentDrop: acceptAttachments, attachmentDropTargeted: { attachmentDropTargeted = $0 },
                                  submit: { state.send() },
                                  // A hidden Agent pane takes a focus request once it's shown.
                                  focusEnabled: state.ui.workspace.isVisible(.conversation), exitShell: exitShell,
                                  openPalette: { state.ui.palette = true }, mentions: mentions,
                                  history: history, historySession: state.selectedID, historyClient: state.composerHistoryClient,
                                  recallQueued: {
                                      guard (state.operations.queue?.count ?? 0) > 0 else { return false }
                                      state.changeQueue(.edit); return true
                                  }, mentionRevision: mentions.revision,
                                  mentionIdentity: state.serverID + "/" + state.selectedID,
                                  mentionLoader: mentionLoader, sessionMentionLoader: sessionMentionLoader,
                                  pickerAnchor: pickerAnchor, theme: theme,
                                  selection: $ui.composerSelection)
                .fixedSize(horizontal: false, vertical: true)
                .workspaceFocusTarget(.conversation)
                .padding(.horizontal, 12).padding(.top, 10).padding(.bottom, 8)
            HStack(spacing: 12) {
                if let shell = state.shellMode {
                    HStack(spacing: 8) {
                        IconView(.terminalWindow, size: 12)
                        Text("Shell")
                        Button(shell.excluded ? "Excluded from context" : "Include in context") {
                            ui.draft = (shell.excluded ? "!" : "!!") + String(ui.draft.dropFirst(shell.excluded ? 2 : 1))
                        }.buttonStyle(.plain).foregroundStyle(theme.accent)
                    }.font(.kit(size: 12)).foregroundStyle(theme.muted)
                } else {
                    let levels = Self.thinkingLevels(model: state.model, models: state.configuration.models)
                    ComposerConfigurationChip(
                        model: Self.activeModelLabel(id: state.model, models: state.configuration.models),
                        modelHelp: state.configuration.models.first(where: { $0.id == state.model }).map(Self.modelLabel) ?? state.model,
                        thinking: levels.isEmpty || state.thinking.isEmpty ? nil : state.thinking,
                        open: openMenu, modelAnchor: modelAnchor, thinkingAnchor: thinkingAnchor,
                        toggle: toggle)
                        .disabled(state.unavailable)
                }
                Spacer(minLength: 8)
                Button {
                    if state.activeBashID != nil { state.stopBash() }
                    else if state.running { state.abortRun() }
                    else if state.replaying { state.stop() }
                    else { state.send() }
                } label: {
                    IconView(state.activeBashID != nil || state.running || state.replaying ? .stop : .arrowUp, size: 12)
                        .frame(width: 26, height: 26)
                        .foregroundStyle(theme.surface)
                        .background(theme.text, in: RoundedRectangle(cornerRadius: 3))
                }.buttonStyle(.plain)
                    .accessibilityLabel(state.activeBashID != nil ? "Stop shell command" : state.running ? "Stop run" : state.replaying ? "Stop" : "Send")
                    .help(state.activeBashID != nil ? "Stop shell command" : state.running || state.replaying ? "Stop response" : "Send message")
                    .disabled(primaryActionDisabled)
            }.font(.kit(size: 11)).padding(.horizontal, 12).padding(.bottom, 10)
        }
        .background(ComposerPickerAnchorView(anchor: pickerAnchor))
        .background(theme.raised)
        .clipShape(shape)
        .overlay(shape.strokeBorder((focused || attachmentDropTargeted) ? theme.focusBorder : theme.border, lineWidth: 1))
        .onDrop(of: [UTType.fileURL.identifier, UTType.image.identifier], isTargeted: $attachmentDropTargeted,
                perform: acceptAttachments)
        .task(id: state.selectedID) { await state.loadComposer() }
        .onChange(of: state.selected?.cwd) { mentions.reset() }
        .onChange(of: state.selectedID) { popover.dismiss() }
        .onChange(of: state.unavailable) { if state.unavailable { popover.dismiss() } }
        .onDisappear { popover.dismiss() }
        .onChange(of: state.ui.draft) {
            if !state.ui.draft.isEmpty { state.operations.clearAcknowledgement() }
        }
    }

    private func exitShell() {
        guard let shell = state.shellMode else { return }
        state.ui.draft = String(state.ui.draft.dropFirst(shell.excluded ? 2 : 1))
        state.ui.composerFocus += 1
    }

    private var mentionLoader: @Sendable (Bool) async throws -> FileIndex {
        let client = state.composerClient, id = state.selectedID
        let fixtures = state.ui.workspace.fixture.files.map(\.path)
        let cache = state.ui.workspace.fileIndex
        let key = FileIndexCache.Key(server: state.serverID, session: id, cwd: state.selected?.cwd ?? "")
        return { refresh in
            guard let client else { return FileIndex(paths: fixtures) }
            return try await cache.load(key: key, refresh: refresh) { try await client.fileIndex(id, refresh: $0) }
        }
    }

    /// Lists the other sessions on this session's server afresh, with what
    /// each is doing when the server reports it.
    private var sessionMentionLoader: @Sendable () async throws -> [SessionMention] {
        let client = state.catalogClient, id = state.selectedID
        return {
            let reporter = client as? any SessionActivityClient
            async let activity: [String: SessionTabStatus]? = try? await reporter?.sessionActivity()
            let sessions = try await client.sessions()
            return SessionMentions.candidates(sessions, activity: await activity ?? [:], excluding: id)
        }
    }

    private var primaryActionDisabled: Bool {
        if state.activeBashID != nil { return state.bashOperation.stopping || state.connectionState != .connected }
        if state.bashOperation.pending { return true }
        if state.shellMode?.command.isEmpty == true { return true }
        if state.running { return state.operations.abortingRun != nil || state.connectionState != .connected }
        if state.replaying { return false }
        if !state.ui.uploads.ready || state.hasRejectedImages || state.configuration.changing || state.directoryChange.pending || (state.reloadOperation.pending || state.compactionOperation.pending) { return true }
        if !state.isDemo && (state.connectionState != .connected || state.operations.sending || state.operations.uncertain || state.operations.queuePending) { return true }
        return state.ui.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            state.annotationState.records.isEmpty && state.ui.workspace.notes.isEmpty && state.ui.attachments.isEmpty && state.ui.serverAttachmentIDs.isEmpty && state.ui.uploads.items.isEmpty
    }

    private var shape: UnevenRoundedRectangle {
        let top: CGFloat = joinedTop ? 0 : 4
        return UnevenRoundedRectangle(topLeadingRadius: top, bottomLeadingRadius: 4,
                                      bottomTrailingRadius: 4, topTrailingRadius: top)
    }

    /// Input the composer can't send, or a queue error with no tray to show it.
    private var showOperationDetails: Bool {
        if case .rejected = state.operations.submission { return true }
        return state.operations.queueError != nil && (state.operations.queue?.count ?? 0) == 0
    }

    private func acceptAttachments(_ providers: [NSItemProvider]) -> Bool {
        guard !state.unavailable else { return false }
        if !state.isDemo {
            guard let client = state.composerClient else { return false }
            let uploads = state.ui.uploads, session = state.selectedID
            let rejecting = imageRejectingModel(for: session)
            let accepted = providers.filter { $0.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) || $0.hasItemConformingToTypeIdentifier(UTType.image.identifier) }
            for provider in accepted {
                let fileURL = provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier)
                let name = provider.suggestedName ?? (fileURL ? "File" : "Pasted image.png")
                guard let id = uploads.reserve(filename: name) else { continue }
                if fileURL {
                    provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, error in
                        let url = (item as? URL) ?? (item as? Data).flatMap { URL(dataRepresentation: $0, relativeTo: nil) }
                        Task { @MainActor in
                            if let url, url.isFileURL { uploads.add(url: url, client: client, session: session, reserved: id, imageRejectingModel: rejecting) }
                            else { uploads.failReserved(id, error: error ?? MutationNotSent(reason: "Couldn’t read the file.")) }
                        }
                    }
                } else {
                    provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, error in
                        Task { @MainActor in
                            if let data { uploads.add(filename: name, data: data, client: client, session: session, reserved: id, imageRejectingModel: rejecting) }
                            else { uploads.failReserved(id, error: error ?? MutationNotSent(reason: "Couldn’t read the image.")) }
                        }
                    }
                }
            }
            return !accepted.isEmpty
        }
        let accepted = providers.filter {
            $0.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier)
                || $0.hasItemConformingToTypeIdentifier(UTType.image.identifier)
        }
        for provider in accepted {
            if provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
                provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, _ in
                    let url = (item as? URL) ?? (item as? Data).flatMap { URL(dataRepresentation: $0, relativeTo: nil) }
                    guard let url, url.isFileURL else { return }
                    let access = url.startAccessingSecurityScopedResource()
                    let source = CGImageSourceCreateWithURL(url as CFURL, nil)
                    let preview = source.flatMap(Self.thumbnail)
                    if access { url.stopAccessingSecurityScopedResource() }
                    Task { @MainActor in addAttachment(url.lastPathComponent, preview: preview) }
                }
            } else {
                provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, _ in
                    guard let data, let source = CGImageSourceCreateWithData(data as CFData, nil),
                          let preview = Self.thumbnail(source) else { return }
                    let name = provider.suggestedName ?? "Dropped image"
                    Task { @MainActor in addAttachment(name, preview: preview) }
                }
            }
        }
        return !accepted.isEmpty
    }

    nonisolated private static func thumbnail(_ source: CGImageSource) -> CGImage? {
        CGImageSourceCreateThumbnailAtIndex(source, 0, [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 320
        ] as CFDictionary)
    }

    private func addAttachment(_ name: String, preview: CGImage?) {
        if let preview {
            state.ui.attachmentPreviews[name] = NSImage(cgImage: preview, size: .zero)
        }
        if !state.ui.attachments.contains(name) { state.ui.attachments.append(name) }
    }

    private func removeAttachment(_ name: String) {
        state.ui.attachments.removeAll { $0 == name }
        state.ui.attachmentPreviews[name] = nil
    }

    private func imageAttachment(_ name: String, preview: NSImage) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Image(nsImage: preview)
                .resizable().scaledToFit()
                .frame(width: 112, height: 76)
                .background(theme.raised)
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .accessibilityLabel("Preview of \(name)")
            Text(name).font(.kit(size: 11)).lineLimit(1).truncationMode(.middle)
                .frame(width: 112, alignment: .leading)
        }
        .padding(6)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.border, lineWidth: 1))
        .overlay(alignment: .topTrailing) {
            Button { removeAttachment(name) } label: {
                IconView(.x, size: 10)
                    .frame(width: 20, height: 20)
                    .background(theme.surface, in: Circle())
            }.buttonStyle(.plain).padding(3).accessibilityLabel("Remove \(name)")
        }
        .help(name)
    }

    private func attachmentChip<Content: View>(_ name: String, icon: Icon,
        remove: @escaping () -> Void, @ViewBuilder content: () -> Content) -> some View {
        HStack(spacing: 6) {
            IconView(icon, size: 12).foregroundStyle(theme.muted)
            content()
            Button(action: remove) {
                IconView(.x, size: 10)
                    .frame(width: 18, height: 18).contentShape(Rectangle())
            }.buttonStyle(.plain).foregroundStyle(theme.muted)
                .accessibilityLabel("Remove \(name)")
        }
        .font(.kit(size: 12))
        .padding(.leading, 9).padding(.trailing, 4).padding(.vertical, 4)
        .background(theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.border, lineWidth: 1))
        .help(name)
    }

    private func attachmentIcon(_ name: String) -> Icon {
        switch (name as NSString).pathExtension.lowercased() {
        case "png", "jpg", "jpeg", "gif", "webp", "heic": .image
        default: .file
        }
    }

    /// Reads the rejecting model when preparation finishes, while `session` is still selected.
    private func imageRejectingModel(for session: String) -> @MainActor () -> String? {
        { [state] in state.selectedID == session ? state.imageRejectingModel : nil }
    }

    private func toggle(_ menu: ComposerChipMenu) {
        if popover.isOpen {
            let same = openMenu == menu
            popover.dismiss()
            if same { return }
        } else if lastClosedMenu == menu, let closedAt = popover.closedAt,
                  Date().timeIntervalSince(closedAt) < 0.3 {
            // The click that reached this half already closed its popover.
            return
        }
        let anchor = menu == .model ? modelAnchor : thinkingAnchor
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, theme = theme
        let content: AnyView = switch menu {
        case .model:
            AnyView(ComposerModelMenu(state: state, theme: theme) { popover.dismiss() })
        case .thinking:
            AnyView(ComposerThinkingMenu(theme: theme,
                levels: Self.thinkingLevels(model: state.model, models: state.configuration.models),
                selected: state.thinking,
                select: { [state] level in
                    guard !state.configuration.changing else { return }
                    Task { await state.configure(thinking: level) }
                }, dismiss: { popover.dismiss() }))
        }
        openMenu = menu
        popover.show(anchor: frame, parent: window, content: content) {
            lastClosedMenu = menu
            if openMenu == menu { openMenu = nil }
        }
    }
}

/// Which half of the configuration chip opened its popover.
enum ComposerChipMenu: Equatable { case model, thinking }

/// One bordered chip split by a hairline: the model, then the thinking level.
/// Each half opens its own popover and lights while hovered or open. A model
/// without thinking levels shows only the model half.
struct ComposerConfigurationChip: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let model: String
    let modelHelp: String
    let thinking: String?
    let open: ComposerChipMenu?
    let modelAnchor: ComposerPickerAnchor
    let thinkingAnchor: ComposerPickerAnchor
    let toggle: (ComposerChipMenu) -> Void
    /// Names the configured target when it is not the attached session.
    var subject: String? = nil
    @State private var hovered: ComposerChipMenu?

    private var target: String { subject.map { " for \($0)" } ?? "" }

    var body: some View {
        let lit = enabled && (open != nil || hovered != nil)
        HStack(spacing: 0) {
            half(model, menu: .model, anchor: modelAnchor)
                .help(modelHelp)
                .accessibilityLabel("Model\(target): \(model)")
            if let thinking {
                Rectangle().fill(theme.border).frame(width: 1)
                half(thinking, menu: .thinking, anchor: thinkingAnchor)
                    .help("Thinking level")
                    .accessibilityLabel("Thinking\(target): \(thinking)")
            }
        }
        .frame(height: 24)
        .clipShape(RoundedRectangle(cornerRadius: 3))
        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(lit ? theme.strongBorder : theme.border))
        .opacity(enabled ? 1 : 0.5)
        .fixedSize()
    }

    private func half(_ label: String, menu: ComposerChipMenu, anchor: ComposerPickerAnchor) -> some View {
        let lit = enabled && (open == menu || hovered == menu)
        return Button { toggle(menu) } label: {
            Text(label).font(.kit(size: 12)).lineLimit(1)
                .foregroundStyle(lit ? theme.text : theme.muted)
                .padding(.horizontal, 8).frame(maxHeight: .infinity)
                .background(lit ? theme.frameHover : .clear)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .background(ComposerPickerAnchorView(anchor: anchor))
        .onHover { hovered = $0 ? menu : (hovered == menu ? nil : hovered) }
    }
}
