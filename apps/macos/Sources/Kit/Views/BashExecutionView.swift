import SwiftUI

struct BashExecutionView: View {
    @Environment(\.mica) private var theme
    let execution: BashExecution
    @State private var hovered = false
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top, spacing: 10) {
                if execution.running { KitSpinner() }
                else { Image(systemName: "terminal").foregroundStyle(theme.muted) }
                HighlightedCodeText(language: "bash", source: execution.command, fontSize: 12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if execution.status != "completed" || execution.timedOut || (execution.exitCode ?? 0) != 0 {
                    Text(execution.statusLabel).font(.kit(size: 11)).foregroundStyle(theme.muted)
                }
                Button { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(execution.command, forType: .string) } label: {
                    Image(systemName: "doc.on.doc")
                }.buttonStyle(.plain).accessibilityLabel("Copy shell command")
                    .opacity(hovered ? 1 : 0)
                    .allowsHitTesting(hovered)
            }.padding(12)
            if !execution.output.isEmpty {
                Rule()
                ScrollView([.horizontal, .vertical]) {
                    Text(execution.output)
                        .font(.kit(size: 12, design: .monospaced))
                        .foregroundStyle(theme.text)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(12)
                }.frame(height: min(240, CGFloat(execution.output.split(separator: "\n", omittingEmptySubsequences: false).count) * 18 + 24))
                    .defaultScrollAnchor(.topLeading, for: .alignment)
            }
            if let error = execution.error { Text(error).foregroundStyle(theme.danger).padding(12) }
            if execution.truncated {
                Text("Output truncated").font(.kit(size: 11)).foregroundStyle(theme.muted).padding(12)
            }
        }.background(theme.raised, in: RoundedRectangle(cornerRadius: 8))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay {
                if !execution.excluded {
                    RoundedRectangle(cornerRadius: 8)
                        .strokeBorder(theme.accent.opacity(theme.dark ? 0.45 : 0.3), lineWidth: 1)
                        .allowsHitTesting(false)
                }
            }
            .accessibilityValue(execution.excluded ? "Excluded from model context" : "Included in model context")
            .onHover { hovered = $0 }
    }

}
