import SwiftUI

struct SessionChooserView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore

    @State private var refreshRequest = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Image(systemName: "text.bubble").font(.kit(size: 32, weight: .light)).foregroundStyle(theme.accent)
            Text("Open a session").font(.kit(size: 24, weight: .semibold))
            Text(state.isDemo ? "Choose a demo session for this window." : "Choose a session from the connected server.").foregroundStyle(theme.muted)
            if !state.isDemo {
                if let error = state.catalogError {
                    Text(error).foregroundStyle(theme.muted)
                }
                Button("Refresh sessions") { refreshRequest += 1 }
                    .disabled(state.refreshingSessions)
            }
            VStack(spacing: 8) {
                ForEach(state.sessions) { session in
                    Button { state.select(session.id) } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(session.title).fontWeight(.medium)
                                Text(session.workspace).font(.kit(size: 11)).foregroundStyle(theme.muted)
                            }
                            Spacer()
                            Image(systemName: "arrow.up.forward")
                        }.padding(14).contentShape(Rectangle())
                            .background(theme.raised, in: RoundedRectangle(cornerRadius: 8))
                    }.buttonStyle(.plain)
                }
            }
        }.task(id: refreshRequest) { await state.refreshSessions() }
        .frame(maxWidth: 380).padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
