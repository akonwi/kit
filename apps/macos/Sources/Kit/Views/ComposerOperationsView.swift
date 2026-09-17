import SwiftUI

struct ComposerOperationsView: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let queue = state.operations.queue, queue.count > 0 {
                HStack {
                    Text("\(queue.count) queued \(queue.count == 1 ? "message" : "messages")").fontWeight(.medium)
                    Spacer()
                    Button("Edit all") { state.changeQueue(.edit) }
                    Button("Remove all") { state.changeQueue(.remove) }
                    Button("Send now") { state.changeQueue(.promote) }.disabled(!state.running)
                }.disabled(state.unavailable || state.operations.queuePending || state.operations.sending || state.operations.uncertain)
                ScrollView {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(Array(queue.previews.enumerated()), id: \.offset) { _, preview in
                            Text(preview).lineLimit(2).foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                }.frame(height: min(132, CGFloat(queue.previews.count) * 32))
                Rule()
            }
            switch state.operations.submission {
            case .idle, .pending, .acknowledged, .uncertain: EmptyView()
            case .failed(let message):
                HStack {
                    Text(message).foregroundStyle(theme.danger)
                    Spacer()
                    Button("Retry", action: state.send)
                }
            }
            if let error = state.operations.queueError { Text(error).foregroundStyle(theme.danger) }
            if let error = state.operations.abortError {
                HStack { Text(error).foregroundStyle(theme.danger); Button("Retry stop", action: state.abortRun) }
            }
        }.font(.kit(size: 12)).buttonStyle(.plain).padding(.horizontal, 16).padding(.top, 12)
    }
}
