import AppKit
import SwiftUI

struct TypographySettingsView: View {
    @Bindable private var typography = Typography.shared
    @Environment(\.mica) private var theme
    private let families = NSFontManager.shared.availableFontFamilies.sorted()
    private var monospaceFamilies: [String] {
        families.filter { family in
            family == "JetBrains Mono" || NSFontManager.shared.availableMembers(ofFontFamily: family)?.contains { member in
                guard let name = member.first as? String, let font = NSFont(name: name, size: 12) else { return false }
                return font.isFixedPitch
            } == true
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text("Typography").fontWeight(.semibold)
                Spacer()
                Button("Restore defaults") { typography.reset() }.buttonStyle(.link)
            }
            HStack {
                Text("Interface")
                Spacer()
                Picker("Interface font", selection: $typography.interfaceFamily) {
                    Text("System").tag("System")
                    ForEach(families, id: \.self) { Text($0).tag($0) }
                }.labelsHidden().frame(width: 180)
                size($typography.interfaceSize, range: 10...20, label: "Interface font size")
            }
            Divider()
            HStack {
                Text("Code & technical text")
                Spacer()
                Picker("Code font", selection: $typography.monoFamily) {
                    Text("System monospace").tag("System")
                    ForEach(monospaceFamilies, id: \.self) { Text($0).tag($0) }
                }.labelsHidden().frame(width: 180)
                size($typography.monoSize, range: 9...22, label: "Code font size")
            }
            Text("JetBrains Mono is included with Kit.").font(.kit(size: 12)).foregroundStyle(theme.muted)
            Divider()
            VStack(alignment: .leading, spacing: 14) {
                Text("Keep the workspace simple and focused.")
                    .padding(12).frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 7))
                Text("The session is ready. Let’s build something.")
                (Text("func ").foregroundColor(theme.accent) + Text("restoreWorkspace() {\n    tabs.restore()\n}"))
                    .font(.kit(size: 12, design: .monospaced))
                    .padding(.leading, 14).overlay(alignment: .leading) { Rectangle().fill(theme.border).frame(width: 1) }
            }.accessibilityElement(children: .contain).accessibilityLabel("Typography preview")
        }
    }

    private func size(_ value: Binding<Double>, range: ClosedRange<Double>, label: String) -> some View {
        HStack(spacing: 4) {
            Text("\(Int(value.wrappedValue)) pt").monospacedDigit().frame(width: 38)
            Stepper(label, value: value, in: range).labelsHidden().fixedSize()
        }.accessibilityElement(children: .contain)
    }
}
