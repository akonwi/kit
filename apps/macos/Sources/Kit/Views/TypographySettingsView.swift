import AppKit
import SwiftUI

struct TypographySettingsView: View {
    @Bindable private var typography = Typography.shared
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
        Form {
            Section("Interface") {
                Picker("Font", selection: $typography.interfaceFamily) {
                    Text("System (default)").tag("System")
                    ForEach(families, id: \.self) { Text($0).tag($0) }
                }
                Stepper("Size: \(Int(typography.interfaceSize)) pt", value: $typography.interfaceSize, in: 10...20)
            }
            Section("Monospace") {
                Picker("Font", selection: $typography.monoFamily) {
                    Text("System monospace").tag("System")
                    ForEach(monospaceFamilies, id: \.self) { Text($0).tag($0) }
                }
                Stepper("Size: \(Int(typography.monoSize)) pt", value: $typography.monoSize, in: 9...22)
                Text("Used for code, editors, diffs, tool output, and paths. JetBrains Mono is included with Kit.")
                    .foregroundStyle(.secondary)
            }
            Section("Preview") {
                VStack(alignment: .leading, spacing: 12) {
                    Text("A workspace for you and your agent.").font(.kit(size: 13))
                    Text("func greet(name: String) {\n    print(\"Hello, \\(name)!\")\n}")
                        .font(.kit(size: 12, design: .monospaced))
                    Text("0O 1Il  {} []  ->  !=").font(.kit(size: 12, design: .monospaced))
                }.padding(.vertical, 8)
            }
            Button("Restore defaults") { typography.reset() }
        }
        .formStyle(.grouped)
    }
}
