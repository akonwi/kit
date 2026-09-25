import SwiftUI

struct TranscriptSessionLink: Sendable {
    let serverID: String
    let open: @MainActor @Sendable (String) -> Void
}
private struct TranscriptSessionLinkKey: EnvironmentKey {
    static let defaultValue: TranscriptSessionLink? = nil
}
extension EnvironmentValues {
    var transcriptSessionLink: TranscriptSessionLink? {
        get { self[TranscriptSessionLinkKey.self] }
        set { self[TranscriptSessionLinkKey.self] = newValue }
    }
}

struct CreatedSessionDetail: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptSessionLink) private var sessionLink
    let value: CreatedSessionPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let prompt = value.prompt {
                Text("Initial prompt").foregroundStyle(theme.muted)
                MarkdownView(source: prompt)
            }
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(value.result).foregroundStyle(value.failed ? theme.danger : theme.muted)
                    .textSelection(.enabled)
                Spacer(minLength: 0)
                if let id = value.sessionID, let sessionLink {
                    Button("Open session", systemImage: "arrow.up.right") { sessionLink.open(id) }
                        .buttonStyle(MicaButtonStyle(compact: true))
                }
            }
        }
    }
}
