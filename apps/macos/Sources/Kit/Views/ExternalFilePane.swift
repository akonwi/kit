import AppKit
import SwiftUI

/// Chooses files on this Mac with the system open panel, the only way a
/// local file opens (ADR 0050), and opens each in a view-only tab.
@MainActor enum LocalFilePanel {
    static func open(into workspace: WorkspaceState) {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = true
        panel.message = "Choose files on this Mac to view in Kit."
        panel.prompt = "View"
        panel.begin { [weak workspace] response in
            guard response == .OK, let workspace else { return }
            for url in panel.urls where url.isFileURL { workspace.open(.localFile(url.path)) }
        }
    }
}

/// Where a file outside the workspace is read from.
enum ViewedFileLocation: Equatable {
    /// The session's server, through the session API.
    case server
    /// This Mac, by the app.
    case thisMac

    var label: String {
        switch self {
        case .server: "Server"
        case .thisMac: "This Mac"
        }
    }
}

/// A view-only tab for a file outside the session's workspace (ADR 0050): a
/// host file on the session's server, or a file on this Mac. Text shows in
/// the read-only editor; a supported image shows in the image viewer. The
/// header names the file's machine so the two never read alike. The file
/// loads when the tab first shows, again on Refresh, and again after the
/// session's activity changes, as a workspace file's preview does; a reload
/// keeps the file on screen until the new read arrives. A Markdown file
/// shows rendered until its header's toggle switches it to source.
struct ExternalFilePane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let pane: WorkspacePane
    @State private var loader = ExternalFileLoader()
    @State private var attempt = 0

    private var workspace: WorkspaceState { state.ui.workspace }
    private var location: ViewedFileLocation { if case .localFile = pane { .thisMac } else { .server } }
    private var path: String {
        switch pane {
        case .hostFile(let path), .localFile(let path): path
        default: ""
        }
    }
    private var active: Bool { workspace.isVisible(pane) }
    /// The version of the file the tab should show: a Refresh, or activity
    /// that may have changed the file.
    private var version: ExternalFileLoader.Version {
        .init(attempt: attempt, activity: workspace.filePreviews.revision)
    }
    private struct Request: Equatable {
        let server: String; let session: String; let version: ExternalFileLoader.Version; let active: Bool
    }

    var body: some View {
        Group {
            if case .loaded(let file, .some(let image)) = loader.phase {
                ImageViewer(workspace: workspace, pane: pane, image: image, title: file.path) { LocationTag(location: location) }
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    header
                    Rule()
                    if let find = workspace.find(for: pane), find.isOpen {
                        FileFindBar(find: find)
                        Rule()
                    }
                    if let note = loader.note {
                        Text("Showing an older preview. " + note)
                            .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                        Rule()
                    }
                    content
                }
            }
        }
        .task(id: Request(server: state.serverID, session: state.selectedID, version: version, active: active)) {
            let version = version
            guard loader.needsRead(version, visible: active) else { return }
            if loader.waitsForActivityToSettle(version) {
                do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
            }
            await loader.load(version, read: reader())
        }
    }

    /// Reads the tab's file from its machine.
    private func reader() -> @MainActor () async throws -> ViewedFile {
        let path = path
        if case .localFile = pane {
            return { try await Task.detached(priority: .userInitiated) { try ViewedFile.readLocal(path: path) }.value }
        }
        guard !state.isDemo, let client = state.catalogClient as? any HostFileClient else {
            return { throw ExternalFileUnavailable() }
        }
        let session = state.selectedID
        return { try await client.readHostFile(session, path: path) }
    }

    private var header: some View {
        HStack(spacing: 12) {
            LocationTag(location: location)
            Text(loader.phase.file?.path ?? path).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
            Spacer()
            if loader.reading { KitSpinner() }
            if pane.isMarkdown { MarkdownModeToggle(workspace: workspace, pane: pane) }
            Button { attempt += 1 } label: { IconView(.arrowClockwise, size: 12) }
                .buttonStyle(.plain).accessibilityLabel("Refresh file").help("Refresh file")
                .disabled(loader.reading)
        }
        .padding(.horizontal, 20).padding(.vertical, 10)
    }

    @ViewBuilder private var content: some View {
        switch loader.phase {
        case .loading:
            Text("Loading file…").foregroundStyle(theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            VStack(spacing: 10) {
                Text(message).foregroundStyle(theme.muted).multilineTextAlignment(.center)
                Button("Retry") { attempt += 1 }.buttonStyle(FormButtonStyle()).disabled(loader.reading)
            }
            .font(.kit(size: 12)).padding(20)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .loaded(let file, _):
            if case .text(let source, let truncated) = file.content {
                Group {
                    if pane.isMarkdown && !workspace.showsMarkdownSource(pane) {
                        MarkdownFileText(workspace: workspace, pane: pane, source: source, cwd: state.selected?.cwd,
                                         find: workspace.find(for: pane))
                    } else {
                        ReadOnlyFileEditor(path: file.path, source: source, lineReveal: workspace.lineReveal(for: pane),
                                           lineRevealed: { [workspace, pane] in workspace.finishLineReveal($0, in: pane) },
                                           find: workspace.find(for: pane))
                    }
                }
                .workspaceFocusTarget(pane)
                .onChange(of: FileFind.searchedLines(source, truncated: truncated), initial: true) {
                    workspace.find(for: pane)?.searchedLines = $1
                }
                Text(ExternalFilePhase.footer(size: file.size, truncated: truncated))
                    .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
            }
        }
    }
}

/// Why a host file can't be read over this connection.
private struct ExternalFileUnavailable: LocalizedError {
    var errorDescription: String? { "Files outside the working directory are unavailable for this connection." }
}

/// What an external file tab shows, and when it reads its file again.
///
/// The first read shows "Loading file…"; later reads, after a Refresh or
/// session activity, keep the file (or its failure) on screen with a
/// spinner until the new read arrives, so a reload neither flickers nor
/// loses the reader's place. A reload that fails while a file shows keeps
/// the file and says why in `note`.
@MainActor @Observable
final class ExternalFileLoader {
    /// A version of the file the tab should show: the Refresh count and the
    /// session activity the read follows.
    struct Version: Equatable {
        var attempt: Int
        var activity: Int
    }

    private(set) var phase = ExternalFilePhase.loading
    /// Why the latest reload failed while an earlier read still shows.
    private(set) var note: String?
    /// Whether a read is under way.
    private(set) var reading = false
    /// The version last read to completion, so showing the tab again
    /// doesn't reread it.
    private var completed: Version?
    private var generation = 0

    /// Whether the tab should read `version`: it shows, and hasn't read it.
    func needsRead(_ version: Version, visible: Bool) -> Bool { visible && completed != version }

    /// Whether a read of `version` follows only session activity on a file
    /// already read, so it waits out a burst of activity first.
    func waitsForActivityToSettle(_ version: Version) -> Bool {
        guard let completed else { return false }
        return completed.attempt == version.attempt
    }

    /// Reads `version` with `read`, unless a later read starts or the task
    /// is cancelled first.
    func load(_ version: Version, read: @MainActor () async throws -> ViewedFile) async {
        generation += 1
        let generation = generation
        reading = true
        defer { if self.generation == generation { reading = false } }
        let next: ExternalFilePhase
        do {
            let file = try await read()
            next = await ExternalFilePhase.loaded(file)
        } catch is CancellationError {
            return
        } catch {
            next = .failed(error.localizedDescription)
        }
        guard !Task.isCancelled, self.generation == generation else { return }
        if case .failed(let message) = next, phase.file != nil {
            note = message
        } else {
            phase = next
            note = nil
        }
        completed = version
    }
}

/// What an external file tab shows.
enum ExternalFilePhase: Equatable {
    case loading
    case failed(String)
    /// The file and, for an image, its decoded form.
    case loaded(ViewedFile, LoadedImage?)

    var file: ViewedFile? { if case .loaded(let file, _) = self { file } else { nil } }

    /// The phase a read file shows, decoding an image off the main actor.
    static func loaded(_ file: ViewedFile) async -> ExternalFilePhase {
        guard case .image(let data, _, _, _) = file.content else { return .loaded(file, nil) }
        guard let image = await Task.detached(priority: .userInitiated, operation: { LoadedImage(data: data) }).value else {
            return .failed(ViewedFileError.unsupported.localizedDescription)
        }
        return .loaded(file, image)
    }

    /// The text footer: read-only, the file's size, and whether the preview
    /// stops before the file ends.
    static func footer(size: Int64, truncated: Bool) -> String {
        "Read-only · " + ByteCountFormatter.string(fromByteCount: size, countStyle: .file) + (truncated ? " · Preview truncated" : "")
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        switch (lhs, rhs) {
        case (.loading, .loading): true
        case (.failed(let a), .failed(let b)): a == b
        case (.loaded(let a, let x), .loaded(let b, let y)): a == b && (x == nil) == (y == nil)
        default: false
        }
    }
}

/// The machine an external file is on, ahead of its path.
private struct LocationTag: View {
    @Environment(\.mica) private var theme
    let location: ViewedFileLocation

    var body: some View {
        Text(location.label).font(.kit(size: 10, weight: .medium)).foregroundStyle(theme.muted)
            .padding(.horizontal, 6).frame(height: 18)
            .background(theme.hover, in: RoundedRectangle(cornerRadius: 3))
            .accessibilityLabel("On " + location.label)
    }
}
