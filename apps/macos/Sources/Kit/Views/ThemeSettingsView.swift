import SwiftUI

struct ThemeSettingsView: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var storedConfiguration = ""
    let library: NativeThemeLibrary

    private var configuration: ThemeConfiguration { .decode(storedConfiguration) }
    private var activeDark: Bool { SystemAppearance.shared.resolve(appearance) == .dark }

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            HStack {
                Text("Appearance mode")
                Spacer()
                Picker("Appearance mode", selection: $appearance) {
                    Text("System").tag("system")
                    Text("Light").tag("light")
                    Text("Dark").tag("dark")
                }.labelsHidden().pickerStyle(.segmented).frame(width: 220)
            }
            HStack(alignment: .top, spacing: 16) {
                themeCard(dark: false)
                themeCard(dark: true)
            }
            Text(appearance == "system"
                 ? "Follows macOS, switching between your light and dark themes automatically."
                 : "Always uses your \(appearance) theme. Choose System to follow macOS.")
                .font(.kit(size: 12)).foregroundStyle(.secondary)
        }
        .onAppear { reloadThemes() }
    }

    private func themeCard(dark: Bool) -> some View {
        let theme = configuration.theme(dark: dark, installed: library.themes)
        return VStack(alignment: .leading, spacing: 14) {
            HStack {
                Label(dark ? "Dark theme" : "Light theme", systemImage: dark ? "moon" : "sun.max")
                    .font(.kit(size: 13, weight: .semibold))
                Spacer()
                if activeDark == dark { Text("Active").font(.kit(size: 10, weight: .medium)).foregroundStyle(.secondary) }
            }
            Picker(dark ? "Dark theme" : "Light theme", selection: Binding(
                get: { dark ? configuration.dark : configuration.light },
                set: { id in
                    var updated = configuration
                    if dark { updated.dark = id } else { updated.light = id }
                    storedConfiguration = updated.json
                })) {
                Text("Kit").tag("mica")
                ForEach(library.themes.filter { $0.definition.preferredColorScheme == nil || $0.definition.preferredColorScheme == (dark ? .dark : .light) }) { item in
                    Text(item.name).tag(item.id)
                }
            }.labelsHidden().frame(maxWidth: .infinity)
            preview(theme: theme)
        }.padding(14)
            .background(.quaternary.opacity(0.2), in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(.secondary.opacity(0.2)))
    }

    private func preview(theme: MicaTheme) -> some View {
        HStack(spacing: 6) {
            ForEach(Array([theme.surface, theme.raised, theme.text, theme.accent].enumerated()), id: \.offset) { _, color in
                Circle().fill(color).overlay(Circle().strokeBorder(theme.border)).frame(width: 20, height: 20)
            }
            Spacer()
        }
    }

    private func reloadThemes() {
        library.reload()
        var updated = configuration
        if updated.normalize(installed: library.themes) { storedConfiguration = updated.json }
    }

}
