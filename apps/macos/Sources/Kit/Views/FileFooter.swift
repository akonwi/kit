import SwiftUI

/// A file pane's footer: what the pane shows, and while the file downloads,
/// its progress and a way to stop it.
struct FileFooter: View {
    @Environment(\.mica) private var theme
    let text: String
    let download: WholeFileDownload?

    var body: some View {
        HStack(spacing: 8) {
            Text(text).font(.kit(size: 10)).foregroundStyle(theme.muted)
            if case .downloading = download?.state {
                Button("Cancel") { download?.cancel() }
                    .buttonStyle(.plain).font(.kit(size: 10)).foregroundStyle(theme.accent)
                    .help("Stop loading the file")
            }
            Spacer(minLength: 0)
        }
        .padding(12)
    }

    /// How a pane's text relates to its file: " · " and a note, or nothing
    /// when it shows the whole file as it is.
    static func truncation(previewTruncated: Bool, whole: WholeFileDownload.Text?, download: WholeFileDownload?) -> String {
        if let download, let note = WholeFileDownload.note(download.state, text: whole) { return " · " + note }
        return previewTruncated ? " · Preview truncated" : ""
    }
}
