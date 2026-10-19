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

/// Settings › Workspace: how tiles behave in this app.
struct WorkspaceSettingsView: View {
    @Environment(\.mica) private var theme
    @AppStorage(WorkspacePreferences.focusFollowsPointerKey)
    private var focusFollowsPointer = WorkspacePreferences.focusFollowsPointerDefault

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Workspace").font(.kit(size: 23, weight: .semibold))
                Text("How tiles behave in this app.").foregroundStyle(theme.muted)
            }
            Toggle(isOn: $focusFollowsPointer) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Focus follows the pointer")
                    Text("Moving the pointer into a tile focuses it, as a click would.")
                        .font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
            }
            .toggleStyle(FormSwitchStyle(trailing: true))
            .padding(.horizontal, 14).padding(.vertical, 10)
            .frame(minHeight: 52)
            .clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
        }
    }
}
