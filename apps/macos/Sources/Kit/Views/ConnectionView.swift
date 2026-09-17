import SwiftUI

struct ConnectionView: View {
    @Bindable var app: AppModel
    @State private var retryRequest = 0
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            if let error = app.error {
                Text("Couldn’t connect to Kit").font(.title2.weight(.semibold))
                Text("Make sure your local Kit server is running, then retry.")
                    .foregroundStyle(.secondary)
                Text(error).foregroundStyle(.secondary).textSelection(.enabled)
                Button("Retry connection") { retryRequest += 1 }
                    .keyboardShortcut(.defaultAction)
            } else {
                HStack(spacing: 12) {
                    KitSpinner()
                    Text("Connecting to Kit…").font(.title2.weight(.semibold))
                }
                Text("Opening your local sessions.").foregroundStyle(.secondary)
            }
        }.padding(32).frame(maxWidth: 520).frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: retryRequest) {
            if retryRequest > 0 { await app.connectLocal() }
        }
    }
}
