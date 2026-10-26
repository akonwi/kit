import AppKit
import SwiftUI

/// A Markdown file tab’s header control: Preview and Source, the current
/// one filled. A tab opens rendered; the choice is the workspace's, kept per
/// tab and through window restoration.
struct MarkdownModeToggle: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let pane: WorkspacePane

    var body: some View {
        let source = workspace.showsMarkdownSource(pane)
        HStack(spacing: 2) {
            segment("Preview", selected: !source) { workspace.showMarkdownSource(pane, false) }
            segment("Source", selected: source) { workspace.showMarkdownSource(pane, true) }
        }
        .padding(2)
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
        .help(source ? "Show the file’s preview (⇧⌘M)" : "Show the file’s source (⇧⌘M)")
    }

    private func segment(_ title: String, selected: Bool, action: @escaping () -> Void) -> some View {
        MarkdownModeSegment(title: title, selected: selected, action: action)
    }
}

private struct MarkdownModeSegment: View {
    @Environment(\.mica) private var theme
    let title: String
    let selected: Bool
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Text(title).font(.kit(size: 10, weight: .medium))
                .foregroundStyle(selected || hovering ? theme.text : theme.muted)
                .padding(.horizontal, 6).frame(height: 16)
                .background(selected ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A Markdown file's text rendered, following its links: a file link opens
/// the file it names on the same machine, resolved against this file's
/// directory, as a tab in this one's tile; a web link opens as transcript
/// links do.
struct MarkdownFileText: View {
    let workspace: WorkspaceState
    let pane: WorkspacePane
    let source: String
    /// The session's working directory, which decides whether a linked
    /// server path opens as a workspace file or a host file.
    let cwd: String?
    /// The comments a workspace file's preview shows and takes.
    var annotations: MarkdownPreviewAnnotations? = nil

    var body: some View {
        MarkdownDocumentView(source: source, annotations: annotations) { [workspace, pane] in workspace.showMarkdownSource(pane) }
            .environment(\.openURL, .markdownFile(workspace, file: pane, cwd: cwd))
    }
}

extension OpenURLAction {
    /// Opens links in the Markdown file `file`. See `MarkdownFileText`.
    @MainActor static func markdownFile(_ workspace: WorkspaceState, file: WorkspacePane, cwd: String?) -> OpenURLAction {
        let web = OpenURLAction.workspace(workspace, beside: file)
        return OpenURLAction { [weak workspace] url in
            guard let path = FilePaths.path(fromLink: url) else { web(url); return .handled }
            guard let workspace, let target = MarkdownFiles.pane(forLink: path, in: file, cwd: cwd) else { return .handled }
            workspace.openLinked(target, from: file)
            return .handled
        }
    }
}

extension WorkspaceState {
    /// Opens a pane a link in `source` names, as a tab in `source`'s tile.
    func openLinked(_ pane: WorkspacePane, from source: WorkspacePane) {
        if let tile = tile(containing: source), !panes.contains(pane) { focus(tile: tile) }
        open(pane)
    }
}
