import SwiftUI

struct SessionDetailsView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Session details").font(.kit(size: 16, weight: .medium))
                Spacer()
                Button { state.ui.palette = false } label: { Image(systemName: "xmark") }
                    .buttonStyle(.plain).keyboardShortcut(.cancelAction).accessibilityLabel("Close session details")
            }
            if let session = state.selected {
                Text(session.title).foregroundStyle(theme.muted).textSelection(.enabled)
                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        section("Configuration", rows: configuration(session))
                        if let usage = session.usage {
                            section("Cumulative usage", rows: usage.rows)
                        } else {
                            Text("Usage unavailable").foregroundStyle(theme.muted)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 4)
                }
            }
            HStack { Button("Back", action: back); Spacer() }
        }
        .font(.kit(size: 13)).padding(20).frame(width: 520, height: 510)
        .foregroundStyle(theme.text).background(theme.surface)
        .onExitCommand { state.ui.palette = false }
    }

    private func section(_ title: String, rows: [(String, String)]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(.kit(size: 13, weight: .medium))
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
