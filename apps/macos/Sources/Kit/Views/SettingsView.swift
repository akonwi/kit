import SwiftUI

struct SettingsView: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var storedConfiguration = ""
    @State private var selection = "appearance"

    var body: some View {
        let scheme = SystemAppearance.shared.resolve(appearance)
        let dark = scheme == .dark
        let theme = ThemeConfiguration.decode(storedConfiguration).theme(dark: dark)
        HStack(spacing: 0) {
            VStack(spacing: 6) {
                section("Appearance", icon: "circle.lefthalf.filled", id: "appearance", theme: theme)
                section("Models", icon: "square.3.layers.3d", id: "models", theme: theme)
                Spacer()
            }.padding(12).padding(.top, 16).frame(width: 170).background(theme.raised)
            Rectangle().fill(theme.border).frame(width: 1)
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    if selection == "appearance" {
                        VStack(alignment: .leading, spacing: 6) {
                            Text("Appearance").font(.kit(size: 23, weight: .semibold))
                            Text("These preferences apply to this app.").foregroundStyle(theme.muted)
                        }
                        ThemeSettingsView()
                        Divider()
                        TypographySettingsView()
                    } else {
                        ModelSettingsView()
                    }
                }.padding(28).frame(maxWidth: .infinity, alignment: .leading)
            }.background(theme.surface)
        }
        .font(.kit(size: 13)).foregroundStyle(theme.text).tint(theme.accent)
        .environment(\.mica, theme)
        .frame(width: 800, height: 690)
        .preferredColorScheme(scheme)
    }

    private func section(_ title: String, icon: String, id: String, theme: MicaTheme) -> some View {
        Button { selection = id } label: {
            Label(title, systemImage: icon).frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12).padding(.vertical, 10)
                .background(selection == id ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 6))
        }.buttonStyle(.plain).accessibilityAddTraits(selection == id ? .isSelected : [])
    }
}
