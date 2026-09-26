import AppKit
import SwiftUI
import UniformTypeIdentifiers
import ImageIO

struct ComposerView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var mentions = ComposerMentionState()
    @State private var history = ComposerHistoryState()
    @State private var pickerAnchor = ComposerPickerAnchor()
    @State private var focused = false
    @State private var attachmentDropTargeted = false

    private var activeModelName: String {
        state.configuration.models.first(where: { $0.id == state.model })?.name
            ?? String(state.model.split(separator: "/", maxSplits: 1).last ?? "")
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
                            attachmentChip("Review · \(state.ui.workspace.notes.count) notes", icon: "text.bubble") {
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
                .scrollIndicators(.hidden)
                .background(theme.raised)
                Rule()
            }
            if !state.isDemo { ComposerAttachmentStrip(state: state) }
            GrowingComposerEditor(text: $ui.draft, focused: $focused,
                                  focusRequest: state.ui.composerFocus,
                                  focusAtEndRequest: state.ui.composerFocusAtEndRequest,
                                  foreground: theme.text, placeholderColor: theme.muted,
                                  attachmentDrop: acceptAttachments, attachmentDropTargeted: { attachmentDropTargeted = $0 },
                                  submit: { state.send() }, exitShell: exitShell,
                                  openPalette: { state.ui.palette = true }, mentions: mentions,
                                  history: history, historySession: state.selectedID, historyClient: state.composerHistoryClient,
                                  recallQueued: {
                                      guard (state.operations.queue?.count ?? 0) > 0 else { return false }
                                      state.changeQueue(.edit); return true
                                  }, mentionRevision: mentions.revision,
                                  mentionIdentity: state.serverID + "/" + state.selectedID,
                                  mentionLoader: mentionLoader, pickerAnchor: pickerAnchor, theme: theme)
                .fixedSize(horizontal: false, vertical: true)
                .padding(16)
            HStack(spacing: 12) {
                if let shell = state.shellMode {
                    HStack(spacing: 8) {
                        Image(systemName: "terminal")
                        Text("Shell")
                        Button(shell.excluded ? "Excluded from context" : "Include in context") {
                            ui.draft = (shell.excluded ? "!" : "!!") + String(ui.draft.dropFirst(shell.excluded ? 2 : 1))
                        }.buttonStyle(.plain).foregroundStyle(theme.accent)
                    }.font(.kit(size: 12)).foregroundStyle(theme.muted)
                } else {
                    Button { chooseAttachment() } label: { Image(systemName: "plus") }
                        .buttonStyle(.plain).accessibilityLabel("Add attachment").disabled(state.unavailable)
                    Menu {
                        ForEach(state.configuration.models, id: \.id) { model in
                            Button(model.name) { Task { await state.configure(model: model.id) } }
                        }
                        if state.configuration.models.isEmpty {
                            Button("Reload models") { Task { await state.loadComposer() } }
                        }
                    } label: { Text(activeModelName) }
                        .menuStyle(.borderlessButton).tint(theme.text).fixedSize()
                        .disabled(state.unavailable || state.configuration.changing || state.running)
                    Rectangle().fill(theme.border).frame(width: 1, height: 14)
                    Menu {
                        ForEach(state.thinkingLevels, id: \.self) { value in
                            Button(value.capitalized) { Task { await state.configure(thinking: value) } }
                        }
                    } label: { Text(state.thinking.capitalized) }
                        .menuStyle(.borderlessButton).tint(theme.text).fixedSize()
                        .disabled(state.unavailable || state.configuration.changing)
                    if let context = SessionContext(tokens: state.selected?.contextTokens, capacity: state.selected?.contextWindow) {
                        ContextUsageView(context: context)
                    }
                }
                Spacer(minLength: 8)
                Button {
                    if state.activeBashID != nil { state.stopBash() }
                    else if state.running { state.abortRun() }
                    else if state.replaying { state.stop() }
                    else { state.send() }
                } label: {
                    Image(systemName: state.activeBashID != nil || state.running || state.replaying ? "stop.fill" : "arrow.up")
                        .frame(width: 28, height: 28)
                        .foregroundStyle(theme.surface)
                        .background(theme.text, in: RoundedRectangle(cornerRadius: 6))
                }.buttonStyle(.plain)
                    .accessibilityLabel(state.activeBashID != nil ? "Stop shell command" : state.running ? "Stop run" : state.replaying ? "Stop" : "Send")
                    .help(state.activeBashID != nil ? "Stop shell command" : state.running || state.replaying ? "Stop response" : "Send message")
                    .disabled(primaryActionDisabled)
            }.font(.kit(size: 11)).padding(.horizontal, 16).padding(.bottom, 12)
        }
        .background(ComposerPickerAnchorView(anchor: pickerAnchor))
        .background(theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder((focused || attachmentDropTargeted) ? theme.focusBorder : theme.strongBorder, lineWidth: 1))
        .onDrop(of: [UTType.fileURL.identifier, UTType.image.identifier], isTargeted: $attachmentDropTargeted,
                perform: acceptAttachments)
        .padding(.horizontal, 28)
        .task(id: state.selectedID) { await state.loadComposer() }
        .onChange(of: state.selected?.cwd) { mentions.reset() }
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

    private var primaryActionDisabled: Bool {
        if state.activeBashID != nil { return state.bashOperation.stopping || state.connectionState != .connected }
        if state.bashOperation.pending { return true }
        if state.shellMode?.command.isEmpty == true { return true }
        if state.running { return state.operations.abortingRun != nil || state.connectionState != .connected }
        if state.replaying { return false }
        if !state.ui.uploads.ready || state.configuration.changing || state.directoryChange.pending || (state.reloadOperation.pending || state.compactionOperation.pending) { return true }
        if !state.isDemo && (state.connectionState != .connected || state.operations.sending || state.operations.uncertain || state.operations.queuePending) { return true }
        return state.ui.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
            state.annotationState.records.isEmpty && state.ui.workspace.notes.isEmpty && state.ui.attachments.isEmpty && state.ui.serverAttachmentIDs.isEmpty && state.ui.uploads.items.isEmpty
    }

    private var showOperationDetails: Bool {
        switch state.operations.submission {
        case .failed: return true
        default:
            return (state.operations.queue?.count ?? 0) > 0 || state.operations.queueError != nil || state.operations.abortError != nil
        }
    }

    private func acceptAttachments(_ providers: [NSItemProvider]) -> Bool {
        guard !state.unavailable else { return false }
        if !state.isDemo {
            guard let client = state.composerClient else { return false }
            let uploads = state.ui.uploads, session = state.selectedID
            let accepted = providers.filter { $0.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) || $0.hasItemConformingToTypeIdentifier(UTType.image.identifier) }
            for provider in accepted {
                let fileURL = provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier)
                let name = provider.suggestedName ?? (fileURL ? "File" : "Pasted image.png")
                guard let id = uploads.reserve(filename: name) else { continue }
                if fileURL {
                    provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, error in
                        let url = (item as? URL) ?? (item as? Data).flatMap { URL(dataRepresentation: $0, relativeTo: nil) }
                        Task { @MainActor in
                            if let url, url.isFileURL { uploads.add(url: url, client: client, session: session, reserved: id) }
                            else { uploads.failReserved(id, error: error ?? MutationNotSent(reason: "Couldn’t read the file.")) }
                        }
                    }
                } else {
                    provider.loadDataRepresentation(forTypeIdentifier: UTType.image.identifier) { data, error in
                        Task { @MainActor in
                            if let data { uploads.add(filename: name, data: data, client: client, session: session, reserved: id) }
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
                Image(systemName: "xmark").font(.kit(size: 9, weight: .medium))
                    .frame(width: 20, height: 20)
                    .background(theme.surface, in: Circle())
            }.buttonStyle(.plain).padding(3).accessibilityLabel("Remove \(name)")
        }
        .help(name)
    }

    private func attachmentChip<Content: View>(_ name: String, icon: String,
        remove: @escaping () -> Void, @ViewBuilder content: () -> Content) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon).foregroundStyle(theme.muted)
            content()
            Button(action: remove) {
                Image(systemName: "xmark").font(.kit(size: 9, weight: .medium))
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

    private func attachmentIcon(_ name: String) -> String {
        switch (name as NSString).pathExtension.lowercased() {
        case "png", "jpg", "jpeg", "gif", "webp", "heic": "photo"
        case "pdf": "doc.richtext"
        case "swift", "go", "js", "ts", "tsx", "py", "json", "sh": "doc.text"
        default: "doc"
        }
    }

    private func chooseAttachment() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Attach"
        let uploads = state.ui.uploads, session = state.selectedID, client = state.composerClient
        panel.begin { response in
            guard response == .OK else { return }
            if let client {
                for url in panel.urls { uploads.add(url: url, client: client, session: session) }
                return
            }
            for url in panel.urls where !state.ui.attachments.contains(url.lastPathComponent) {
                let access = url.startAccessingSecurityScopedResource()
                let source = CGImageSourceCreateWithURL(url as CFURL, nil)
                addAttachment(url.lastPathComponent, preview: source.flatMap(Self.thumbnail))
                if access { url.stopAccessingSecurityScopedResource() }
            }
        }
    }
}
