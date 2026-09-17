import SwiftUI

struct PeerSessionDetail: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptSessionLink) private var sessionLink
    let value: PeerSessionPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if value.action == "discover" {
                ForEach(Array(value.sessions.enumerated()), id: \.offset) { index, session in
                    if index > 0 { Rule() }
                    HStack(alignment: .top, spacing: 12) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(session.title)
                            Text(session.cwd).foregroundStyle(theme.muted).textSelection(.enabled)
                            Text(session.availability.replacingOccurrences(of: "_", with: " ").capitalized)
                                .foregroundStyle(theme.muted)
                        }
                        Spacer(minLength: 0)
                        openButton(session.id)
                    }
                }
            }
            if let question = value.question {
                Text("Question").foregroundStyle(theme.muted)
                MarkdownView(source: question)
            }
            if let reply = value.reply {
                Text("Reply").foregroundStyle(theme.muted)
                MarkdownView(source: reply)
            }
            if let error = value.error {
                Text(error).foregroundStyle(theme.danger).textSelection(.enabled)
            }
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(value.status).foregroundStyle(theme.muted)
                Spacer(minLength: 0)
                if let id = value.recipientID { openButton(id) }
            }
        }
    }

    @ViewBuilder private func openButton(_ id: String) -> some View {
        if PeerSessionPresentation.validSessionID(id), let sessionLink {
            Button("Open session", systemImage: "arrow.up.right") { sessionLink.open(id) }
                .buttonStyle(MicaButtonStyle(compact: true))
        }
    }
}
