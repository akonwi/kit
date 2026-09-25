import SwiftUI

struct SessionReloadView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme
    @State private var started = false

    var body: some View {
        let operation = state.reloadOperation
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Reload session context").font(.kit(size: 16, weight: .medium))
                Spacer()
                Button { state.ui.palette = false } label: { Image(systemName: "xmark") }
                    .buttonStyle(.plain).keyboardShortcut(.cancelAction).accessibilityLabel("Close reload results")
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if operation.pending {
                        HStack { KitSpinner(); Text("Reloading session context…") }
                    } else if let error = operation.error {
                        Text("Session reload failed").fontWeight(.medium)
                        Text(error).foregroundStyle(theme.danger).textSelection(.enabled)
                    } else if operation.result != nil {
                        Text("Session context reloaded").fontWeight(.medium)
                    }
                    if let error = operation.refreshError {
                        Text("Session refresh failed: " + error).foregroundStyle(theme.warning).textSelection(.enabled)
                    }
                    if let result = operation.result {
                        ForEach(Array((result.warnings ?? []).enumerated()), id: \.offset) { _, warning in
                            Text(warning).foregroundStyle(theme.warning).textSelection(.enabled)
                        }
                        ForEach(Array((result.diagnostics ?? []).enumerated()), id: \.offset) { _, diagnostic in
                            VStack(alignment: .leading, spacing: 6) {
                                Text(diagnostic.message).foregroundStyle(diagnostic.severity == "error" ? theme.danger : diagnostic.severity == "warning" ? theme.warning : theme.text)
                                Text(diagnostic.source.path ?? diagnostic.source.id)
                                    .font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.muted)
                            }.textSelection(.enabled)
                        }
                        if let sources = result.sources, !sources.isEmpty {
                            Text("Loaded sources").fontWeight(.medium)
                            ForEach(Array(sources.enumerated()), id: \.offset) { _, source in
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(source.id)
                                    if let path = source.path {
                                        Text(path).font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.muted)
                                    }
                                }.textSelection(.enabled)
                            }
                        }
                    }
                }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 4)
            }
            HStack {
                Button("Back", action: back)
                Spacer()
                if operation.refreshError != nil {
                    Button("Refresh session") { Task { await state.reloadSession(refreshOnly: true) } }
                        .disabled(operation.pending)
                }
                if operation.error != nil {
                    Button("Reload again") { Task { await state.reloadSession() } }
                        .disabled(state.reloadUnavailableReason != nil)
                }
            }
        }
        .font(.kit(size: 13)).padding(20).frame(width: 560, height: 460)
        .foregroundStyle(theme.text).background(theme.surface)
        .onAppear {
            guard !started else { return }
            started = true
            // The store owns completion; dismissing the dialog does not cancel a server mutation.
            Task { await state.reloadSession() }
        }
        .onExitCommand { state.ui.palette = false }
    }
}
