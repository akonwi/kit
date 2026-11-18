import SwiftUI

/// The chip before a shell command in the composer. Tapping it makes the
/// draft a message again.
struct ShellChip: View {
    let remove: () -> Void

    var body: some View {
        Button(action: remove) {
            HStack(spacing: 5) {
                Image(systemName: "apple.terminal")
                    .font(.system(size: 13, weight: .medium))
                Text("Shell")
                    .font(.system(size: 16, weight: .medium))
            }
            .foregroundStyle(Theme.accent)
            .lineLimit(1)
            .padding(.horizontal, 10)
            .frame(height: 30)
            .background(Theme.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
        .buttonStyle(.plain)
        .padding(.leading, 6)
        .padding(.vertical, 5)
        .accessibilityLabel("Shell command")
        .accessibilityHint("Makes this a message again")
        .accessibilityIdentifier("shell-chip")
    }
}

/// Above a shell command: where it runs, whether the agent sees its
/// output, and the switch between the two.
struct ShellHint: View {
    let directory: String?
    let output: SessionComposer.ShellOutput
    let change: (SessionComposer.ShellOutput) -> Void

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(directory.map { "Runs in " + SessionScreen.shortDirectory($0) } ?? "Runs in the session’s folder")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(output == .seen ? "The agent sees the output" : "The agent doesn’t see the output")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            Button(output == .seen ? "Hide" : "Show") {
                change(output == .seen ? .hidden : .seen)
            }
            .font(.system(size: 15, weight: .medium))
            .foregroundStyle(Theme.accent)
            .padding(.horizontal, 12)
            .frame(height: 30)
            .background(Theme.accent.opacity(0.12), in: Capsule())
            .buttonStyle(.plain)
            .accessibilityLabel(output == .seen ? "Hide the output from the agent" : "Show the output to the agent")
            .accessibilityIdentifier("shell-output")
        }
        .padding(.leading, 16)
        .padding(.trailing, 10)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("shell-hint")
    }
}
