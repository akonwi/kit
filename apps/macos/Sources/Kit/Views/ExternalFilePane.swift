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
/// loads when the tab first shows and again on Refresh.
struct ExternalFilePane: View {
    @Environment(\.mica) private var theme
    let state: SessionStore
    let pane: WorkspacePane
    @State private var phase = ExternalFilePhase.loading
    @State private var attempt = 0
    /// The attempt last read to completion, so showing the tab again doesn't
    /// reload it.
    @State private var loadedAttempt: Int?

    private var workspace: WorkspaceState { state.ui.workspace }
    private var location: ViewedFileLocation { if case .localFile = pane { .thisMac } else { .server } }
    private var path: String {
        switch pane {
        case .hostFile(let path), .localFile(let path): path
        default: ""
        }
    }
    private var active: Bool { workspace.isVisible(pane) }
    private struct Request: Equatable { let server: String; let session: String; let attempt: Int; let active: Bool }

    var body: some View {
        Group {
            if case .loaded(let file, .some(let image)) = phase {
                ImageViewer(workspace: workspace, pane: pane, image: image, title: file.path) { LocationTag(location: location) }
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    header
                    Rule()
                    content
                }
            }
        }
        .task(id: Request(server: state.serverID, session: state.selectedID, attempt: attempt, active: active)) {
            // A tab loads when it's first shown; Refresh and Retry reload it.
            guard active, loadedAttempt != attempt else { return }
            await load()
            if !Task.isCancelled { loadedAttempt = attempt }
        }
    }

    private var header: some View {
        HStack(spacing: 12) {
            LocationTag(location: location)
            Text(phase.file?.path ?? path).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
            Spacer()
            if case .loading = phase { KitSpinner() }
            Button { attempt += 1 } label: { IconView(.arrowClockwise, size: 12) }
                .buttonStyle(.plain).accessibilityLabel("Refresh file").help("Refresh file")
                .disabled(phase == .loading)
        }
        .padding(.horizontal, 20).padding(.vertical, 10)
    }

    @ViewBuilder private var content: some View {
        switch phase {
        case .loading:
            Text("Loading file…").foregroundStyle(theme.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            VStack(spacing: 10) {
                Text(message).foregroundStyle(theme.muted).multilineTextAlignment(.center)
                Button("Retry") { attempt += 1 }.buttonStyle(FormButtonStyle())
            }
            .font(.kit(size: 12)).padding(20)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .loaded(let file, _):
            if case .text(let source, let truncated) = file.content {
                ReadOnlyFileEditor(path: file.path, source: source)
                    .workspaceFocusTarget(pane)
                Text(ExternalFilePhase.footer(size: file.size, truncated: truncated))
                    .font(.kit(size: 10)).foregroundStyle(theme.muted).padding(12)
            }
        }
    }

    private func load() async {
        phase = .loading
        let pane = pane, path = path
        do {
            let file: ViewedFile
            switch pane {
            case .localFile:
                file = try await Task.detached(priority: .userInitiated) { try ViewedFile.readLocal(path: path) }.value
            default:
                guard !state.isDemo, let client = state.catalogClient as? any HostFileClient else {
                    phase = .failed("Files outside the working directory are unavailable for this connection."); return
                }
                file = try await client.readHostFile(state.selectedID, path: path)
            }
            try Task.checkCancellation()
            phase = await ExternalFilePhase.loaded(file)
        } catch is CancellationError {
        } catch {
            phase = .failed(error.localizedDescription)
        }
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
