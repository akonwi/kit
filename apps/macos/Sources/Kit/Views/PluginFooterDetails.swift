import SwiftUI

/// Full content is selectable and wraps; the footer's one-line limit does not
/// propagate into this scrollable overflow surface.
struct PluginFooterDetails: View {
    @Environment(\.mica) private var theme
    let session: SessionExcerpt?

    static func location(_ session: SessionExcerpt?) -> String? {
        guard session?.pluginFooter?.locationHidden != true else { return nil }
        let path = session?.cwd ?? session?.workspace ?? ""
        let head = WorkspaceLocation.repositoryLabel(session)
        let value = [path, head].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
        return value.isEmpty ? nil : value
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text("Footer content").font(.kit(size: 13, weight: .medium))
                if let location = Self.location(session) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Location").font(.kit(size: 11)).foregroundStyle(theme.muted)
                        Text(location).font(.kit(size: 12, design: .monospaced))
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                ForEach(session?.pluginFooter?.items ?? [], id: \.id) { item in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(item.id).font(.kit(size: 11)).foregroundStyle(theme.muted)
                        Text(PluginFooterView.styledContent(items: [item], theme: theme))
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(16).lineLimit(nil).textSelection(.enabled)
        }
        .frame(width: 440, height: 300)
        .foregroundStyle(theme.text).background(theme.raised)
    }
}
