import SwiftUI

/// The only screen until a server is paired: how to get a pairing code,
/// and the two ways to bring one in.
struct FirstRunView: View {
    @Environment(PairingCoordinator.self) private var pairing

    var body: some View {
        ZStack(alignment: .bottom) {
            VStack(spacing: 28) {
                VStack(spacing: 10) {
                    Text("kit")
                        .font(.system(size: 34, weight: .medium, design: .monospaced))
                        .tracking(34 * 0.35)
                        // Tracking trails the last letter; this centers the word.
                        .padding(.leading, 34 * 0.35)
                        .foregroundStyle(Theme.text)
                    RoundedRectangle(cornerRadius: 2)
                        .fill(Theme.accent)
                        .frame(width: 96, height: 3)
                }
                VStack(spacing: 8) {
                    Text("Pair with a Kit server")
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(Theme.text)
                    Text("On the server, run")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.muted)
                    CommandChip(command: "kit server pair")
                    Text("and scan the code it shows.")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.muted)
                }
                .multilineTextAlignment(.center)
            }
            .padding(.horizontal, 40)
            .padding(.bottom, 80)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            VStack(spacing: 12) {
                Button { pairing.scan() } label: {
                    Label("Scan pairing code", systemImage: "qrcode.viewfinder")
                }
                .buttonStyle(FilledCapsuleButtonStyle(height: 52))
                Button("Paste pairing link") { pairing.paste() }
                    .font(.system(size: 17, weight: .medium))
                    .foregroundStyle(Theme.accent)
                    .frame(maxWidth: .infinity, minHeight: 52)
                    .contentShape(Capsule())
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 14)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.surface)
    }
}

/// A shell command set apart from prose.
struct CommandChip: View {
    let command: String

    var body: some View {
        Text(command)
            .font(.system(size: 14, design: .monospaced))
            .foregroundStyle(Theme.text)
            .padding(.vertical, 6)
            .padding(.horizontal, 10)
            .background(Theme.raised, in: RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(Theme.border, lineWidth: 0.5))
    }
}

/// The primary action of a screen: a capsule in the text color.
struct FilledCapsuleButtonStyle: ButtonStyle {
    var height: CGFloat = 48
    var fullWidth = true

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .semibold))
            .foregroundStyle(Theme.surface)
            .padding(.horizontal, 22)
            .frame(maxWidth: fullWidth ? .infinity : nil, minHeight: height)
            .background(Theme.text, in: Capsule())
            .opacity(configuration.isPressed ? 0.8 : 1)
            .contentShape(Capsule())
    }
}

#Preview {
    FirstRunView()
        .environment(PairingCoordinator())
}
