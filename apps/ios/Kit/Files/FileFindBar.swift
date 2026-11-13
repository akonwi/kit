import KitClient
import SwiftUI

/// A file's find bar, in the composer's place above the keyboard: the
/// field with its count and options, the arrows, and Done. Return or ⌘G
/// goes to the next match, Shift-Return or ⇧⌘G to the previous one, and
/// Done or Esc closes the bar.
struct FileFindBar: View {
    @Bindable var find: FileFind
    let close: () -> Void
    @FocusState private var focused: Bool

    private var stepping: Bool { !find.matches.isEmpty }

    var body: some View {
        HStack(spacing: 8) {
            field
            arrows
            Button("Done", action: close)
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.accent)
                .buttonStyle(.plain)
                .keyboardShortcut(.cancelAction)
                .padding(.horizontal, 4)
                .accessibilityIdentifier("find-done")
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 4)
        .onAppear { focused = true }
        .onChange(of: find.focusRequest) { focused = true }
    }

    private var field: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            TextField("Find in file", text: $find.query)
                .font(.system(size: 17))
                .foregroundStyle(Theme.text)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .focused($focused)
                .onSubmit {
                    find.next()
                    focused = true
                }
                .onKeyPress(.return, phases: .down) { press in
                    guard press.modifiers.contains(.shift) else { return .ignored }
                    find.previous()
                    return .handled
                }
                .accessibilityIdentifier("find-field")
            if !find.label.isEmpty {
                Text(find.label)
                    .font(.system(size: 13).monospacedDigit())
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .fixedSize()
                    .accessibilityIdentifier("find-count")
            }
            options
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .frame(height: 48)
        .glassEffect(.regular, in: Capsule())
    }

    /// Aa: Match Case and Whole Words, filled while either is on.
    private var options: some View {
        let on = find.matchCase || find.wholeWord
        return Menu {
            Toggle("Match Case", isOn: $find.matchCase)
            Toggle("Whole Words", isOn: $find.wholeWord)
        } label: {
            Text("Aa")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(on ? Theme.accent : Theme.text)
                .frame(width: 36, height: 36)
                .background(on ? Theme.userWash : .clear, in: Circle())
        }
        .accessibilityLabel("Find options")
        .accessibilityValue([find.matchCase ? "Match Case" : nil, find.wholeWord ? "Whole Words" : nil]
            .compactMap { $0 }.joined(separator: ", "))
        .accessibilityIdentifier("find-options")
    }

    private var arrows: some View {
        HStack(spacing: 0) {
            arrow("chevron.up", label: "Previous match", key: KeyboardShortcut("g", modifiers: [.command, .shift])) {
                find.previous()
            }
            arrow("chevron.down", label: "Next match", key: KeyboardShortcut("g", modifiers: .command)) {
                find.next()
            }
        }
        .frame(height: 48)
        .glassEffect(.regular, in: Capsule())
    }

    private func arrow(_ symbol: String, label: String, key: KeyboardShortcut, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(stepping ? Theme.text : Theme.strongBorder)
                .frame(width: 44, height: 48)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!stepping)
        .keyboardShortcut(key)
        .accessibilityLabel(label)
    }
}
