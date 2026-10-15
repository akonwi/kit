import SwiftUI

/// Problems shown at the top of the composer: input that can't be sent, and
/// a queue error once the queued tray has nothing left to show it in.
struct ComposerOperationsView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // A turn that failed to start or stop is reported in the activity card.
            if case .rejected(let message) = state.operations.submission {
                Text(message).foregroundStyle(theme.danger)
            }
            if (state.operations.queue?.count ?? 0) == 0, let error = state.operations.queueError {
                Text(error).foregroundStyle(theme.danger)
            }
        }.font(.kit(size: 12)).padding(.horizontal, 16).padding(.top, 12)
    }
}
