import SwiftUI

/// App-local preferences for the tiled workspace.
enum WorkspacePreferences {
    /// Moving the pointer into a tile focuses it. On by default.
    static let focusFollowsPointerKey = "focusFollowsPointer"
    static let focusFollowsPointerDefault = true

    static var focusFollowsPointer: Bool {
        UserDefaults.standard.object(forKey: focusFollowsPointerKey) as? Bool ?? focusFollowsPointerDefault
    }
}

/// Settings › Workspace: how tiles and diffs behave in this app.
struct WorkspaceSettingsView: View {
    @Environment(\.mica) private var theme
    @AppStorage(WorkspacePreferences.focusFollowsPointerKey)
    private var focusFollowsPointer = WorkspacePreferences.focusFollowsPointerDefault
    @Bindable private var diff = DiffPreferences.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Workspace").font(.kit(size: 23, weight: .semibold))
                Text("How tiles and diffs behave in this app.").foregroundStyle(theme.muted)
            }
            Toggle(isOn: $focusFollowsPointer) {
                label("Focus follows the pointer", "Moving the pointer into a tile focuses it, as a click would.")
            }
            .toggleStyle(FormSwitchStyle(trailing: true))
            .padding(.horizontal, 14).padding(.vertical, 10)
            .frame(minHeight: 52)
            .clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
            VStack(spacing: 0) {
                HStack(spacing: 20) {
                    label("Diff layout", "Side by side shows old and new lines in two columns where the pane is wide enough.")
                    Spacer(minLength: 0)
                    Picker("Diff layout", selection: $diff.layout) {
                        ForEach(DiffLayout.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }.labelsHidden().frame(width: 150)
                }
                .padding(.horizontal, 14).padding(.vertical, 10).frame(minHeight: 52)
                Rule()
                Toggle(isOn: $diff.wrapLines) {
                    label("Wrap diff lines", "Long lines wrap instead of scrolling sideways.")
                }
                .toggleStyle(FormSwitchStyle(trailing: true))
                .padding(.horizontal, 14).padding(.vertical, 10).frame(minHeight: 52)
            }
            .clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
        }
    }

    private func label(_ title: String, _ detail: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
            Text(detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
        }
    }
}
