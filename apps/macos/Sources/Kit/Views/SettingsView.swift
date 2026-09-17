import SwiftUI

struct SettingsView: View {
    @AppStorage("appearance") private var appearance = "system"
    var body: some View {
        TabView {
            TypographySettingsView().tabItem { Label("Typography", systemImage: "textformat") }
            ThemeSettingsView().tabItem { Label("Appearance", systemImage: "paintpalette") }
        }
        .frame(width: 620, height: 580)
        .preferredColorScheme(appearance == "system" ? nil : appearance == "dark" ? .dark : .light)
    }
}
