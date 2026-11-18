import SwiftUI

/// The app's own settings, kept on this iPhone.
struct SettingsView: View {
    @AppStorage(ComposerControlStyle.storageKey) private var controlStyle = ComposerControlStyle.hold

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Picker("Send Button", selection: $controlStyle) {
                        ForEach(ComposerControlStyle.allCases) { style in
                            Text(style.title).tag(style)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                    .accessibilityIdentifier("composer-control-style")
                } header: {
                    Text("Send Button")
                } footer: {
                    Text(controlStyle.summary)
                }
                .listRowBackground(Theme.raised)
            }
            .scrollContentBackground(.hidden)
            .background(Theme.surface)
            .navigationTitle("Settings")
        }
    }
}
