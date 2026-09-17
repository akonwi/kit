import AppKit
import SwiftUI
import UniformTypeIdentifiers

struct ThemeSettingsView: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var storedConfiguration = ""
    @Environment(\.colorScheme) private var scheme
    @State private var importError: String?
    @State private var notice = ""

    private var configuration: ThemeConfiguration { .decode(storedConfiguration) }
    private var activeDark: Bool { appearance == "dark" || (appearance == "system" && scheme == .dark) }

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            HStack {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Appearance").font(.kit(size: 20, weight: .semibold))
                    Text("Choose a theme for each part of your day.").font(.kit(size: 12)).foregroundStyle(.secondary)
                }
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
            Divider()
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Your Kit themes").fontWeight(.medium)
                    Text("Import light and dark files together or separately.").font(.kit(size: 12)).foregroundStyle(.secondary)
                }
                Spacer()
                Button("Import themes…", systemImage: "square.and.arrow.down") { importThemes() }
            }
            if !notice.isEmpty {
                Text(notice).font(.kit(size: 12)).foregroundStyle(.secondary)
            }
        }.padding(24)
        .alert("Themes could not be imported", isPresented: Binding(get: { importError != nil }, set: { if !$0 { importError = nil } })) {
            Button("OK") { importError = nil }
        } message: { Text(importError ?? "") }
    }

    private func themeCard(dark: Bool) -> some View {
        let theme = configuration.theme(dark: dark)
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
                Text("Mica").tag("mica")
                Text("Slate").tag("slate")
                Text("Sand").tag("sand")
                ForEach(configuration.imported.filter { $0.definition.preferredColorScheme == nil || $0.definition.preferredColorScheme == (dark ? .dark : .light) }) { item in
                    Text(item.name).tag(item.id)
                }
            }.labelsHidden().frame(maxWidth: .infinity)
            preview(theme: theme)
        }.padding(14)
            .background(.quaternary.opacity(0.2), in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(.secondary.opacity(0.2)))
    }

    private func preview(theme: MicaTheme) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Circle().fill(theme.accent).frame(width: 6, height: 6)
                Text("Kit").font(.kit(size: 11, weight: .semibold))
                Spacer()
                Image(systemName: "sidebar.right").foregroundStyle(theme.muted)
            }
            Text("A little room to think.").font(.kit(size: 12))
                .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                .background(theme.raised, in: RoundedRectangle(cornerRadius: 6))
            (Text("let ").foregroundColor(theme.syntax("keyword", fallback: theme.accent)) +
             Text("idea = ").foregroundColor(theme.text) +
             Text("\"Hello\"").foregroundColor(theme.syntax("string", fallback: theme.success)))
                .font(.kit(size: 11, design: .monospaced))
            HStack {
                Text("Message…").foregroundStyle(theme.muted)
                Spacer()
                Image(systemName: "arrow.up").foregroundStyle(theme.surface)
                    .padding(5).background(theme.text, in: RoundedRectangle(cornerRadius: 4))
            }.font(.kit(size: 10)).padding(8)
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.focusBorder))
        }.padding(12).foregroundStyle(theme.text)
            .background(theme.surface, in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border))
    }

    private func importThemes() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json]
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Import themes"
        panel.begin { response in
            guard response == .OK else { return }
            do {
                var updated = configuration
                for url in panel.urls {
                    let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
                    guard size <= 262_144 else { throw NativeThemeDefinition.ThemeError.invalid("\(url.lastPathComponent) exceeds 256 KB.") }
                    let definition = try NativeThemeDefinition.parse(Data(contentsOf: url))
                    updated.add(name: url.deletingPathExtension().lastPathComponent,
                                definition: definition, fallbackDark: activeDark)
                }
                storedConfiguration = updated.json
                notice = "Imported \(panel.urls.count) theme\(panel.urls.count == 1 ? "" : "s") and assigned matching appearances."
            } catch { importError = error.localizedDescription }
        }
    }
}
