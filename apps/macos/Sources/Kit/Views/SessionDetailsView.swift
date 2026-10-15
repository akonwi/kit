import SwiftUI

struct SessionDetailsView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme

    var body: some View {
        PaletteSheet(title: "Session details", detail: state.selected?.title, height: PaletteMetrics.height) {
            if let session = state.selected {
                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        section("Configuration", rows: configuration(session))
                        if let usage = session.usage {
                            section("Cumulative usage", rows: usage.rows)
                        } else {
                            Text("Usage unavailable").foregroundStyle(theme.muted)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(16)
                }
            }
        } actions: {
            Button("Back", action: back).buttonStyle(MicaButtonStyle(compact: true))
            Spacer()
        }
        .onExitCommand { state.ui.palette = false }
    }

    private func section(_ title: String, rows: [(String, String)]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            MetaLabel(text: title.uppercased())
            Grid(alignment: .leading, horizontalSpacing: 20, verticalSpacing: 10) {
                ForEach(rows, id: \.0) { label, value in
                    GridRow(alignment: .top) {
                        Text(label).foregroundStyle(theme.muted).frame(width: 90, alignment: .leading)
                        Text(value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
    }

    private func configuration(_ session: SessionExcerpt) -> [(String, String)] {
        var context = "Unavailable"
        if let tokens = session.contextTokens, let capacity = session.contextWindow, capacity > 0 {
            let percentage = SessionContext(tokens: tokens, capacity: capacity)?.percentage ?? 0
            context = "\(SessionUsage.number(tokens)) / \(SessionUsage.number(capacity)) tokens (\(percentage)%)"
        }
        let parent = session.parentSessionID.map { id in
            session.parentSessionName.map { "\($0) (\(id))" } ?? id
        } ?? "None"
        return [("Model", session.model), ("Thinking", session.thinking.isEmpty ? "default" : session.thinking),
                ("Context", context), ("Parent", parent)]
    }
}
