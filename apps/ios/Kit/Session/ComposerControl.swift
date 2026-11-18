import KitDictation
import SwiftUI

/// How the message box reaches what isn't its main action. Chosen in
/// Settings.
enum ComposerControlStyle: String, CaseIterable, Identifiable {
    /// One button: a tap does the main action, and pressing and holding it
    /// opens the rest.
    case hold
    /// The main action with a chevron beside it that opens the rest.
    case split

    static let storageKey = "composerControlStyle"

    var id: Self { self }

    var title: String {
        switch self {
        case .hold: "Press and Hold"
        case .split: "Split Button"
        }
    }

    /// How the rest is reached, for Settings.
    var summary: String {
        switch self {
        case .hold: "Press and hold the send button for attachments and commands."
        case .split: "Tap the arrow beside the send button for attachments and commands."
        }
    }
}

/// What the button at the end of the message box does with one tap.
enum ComposerMain: Equatable {
    /// Dictates: a recording is going, or there's nothing to send or stop.
    case mic
    /// Sends, stops, or waits on the server.
    case action(SessionComposer.Action)

    static func resolve(dictating: Bool, micInSendSlot: Bool, action: SessionComposer.Action) -> ComposerMain {
        dictating || micInSendSlot ? .mic : .action(action)
    }
}

/// The message box's other actions, in its menu.
struct ComposerMore {
    /// Starts dictating, when the mic isn't the main action but can start.
    var dictate: (() -> Void)?
    /// Whether the draft can take another file.
    var canAttach: Bool
    var hasCamera: Bool
    /// Opens a picker for the draft's attachments.
    var attach: (AttachmentSource) -> Void
    /// Opens the command search, when the session has commands.
    var commands: (() -> Void)?
}

/// The button at the end of the message box, in the chosen style.
struct ComposerControl: View {
    let style: ComposerControlStyle
    let main: ComposerMain
    let more: ComposerMore
    let dictation: Dictation
    @Binding var press: DictationPress
    let send: () -> Void
    let stop: () -> Void
    /// Sends dictated words.
    let sendDictation: (String) -> Void

    var body: some View {
        Group {
            switch style {
            case .hold: hold
            case .split: split
            }
        }
        .onChange(of: dictation.phase == .idle) { _, idle in
            if idle { press.reset() }
        }
    }

    // MARK: Press and hold

    /// A tap does the main action; a press opens the menu, with the
    /// system's haptic. While dictating, the button only sends the words.
    private var hold: some View {
        Button(action: tapMain) { face }
            .buttonStyle(.plain)
            // Disables only the tap; a press still opens the menu.
            .disabled(!mainEnabled)
            .contentShape(.contextMenuPreview, Circle())
            .contextMenu {
                if !dictation.isActive { ComposerMoreItems(more: more) }
            }
            .accessibilityLabel(label)
            .accessibilityHint(dictation.isActive ? "" : "Press and hold for attachments and commands")
            .accessibilityIdentifier(identifier)
    }

    @ViewBuilder
    private var face: some View {
        switch main {
        case .mic:
            DictationButtonFace(state: DictationButtonState(phase: dictation.phase, mode: press.mode,
                                                            cancelArmed: press.cancelArmed),
                                look: .filled)
        case .action(let action):
            ComposerActionFace(action: action)
        }
    }

    private func tapMain() {
        switch main {
        case .mic:
            DictationTap(dictation: dictation, press: $press, send: sendDictation).tap()
        case .action(.send(enabled: true)):
            send()
        case .action(.stop(enabled: true)):
            stop()
        case .action:
            break
        }
    }

    private var label: String {
        switch main {
        case .mic: DictationButtonState(phase: dictation.phase, mode: press.mode, cancelArmed: press.cancelArmed).label
        case .action(let action): ComposerActionFace.label(action)
        }
    }

    private var identifier: String {
        main == .mic ? "dictate" : "composer-action"
    }

    // MARK: Split

    /// The main action as it is alone, joined to a chevron that opens the
    /// menu. A recording hides the chevron.
    private var split: some View {
        HStack(spacing: 0) {
            switch main {
            case .mic:
                DictationButton(dictation: dictation, look: .filled, press: $press, send: sendDictation)
            case .action(let action):
                ComposerButton(action: action, send: send, stop: stop)
            }
            if !dictation.isActive {
                Rectangle()
                    .fill(Theme.surface.opacity(0.25))
                    .frame(width: 1, height: 20)
                Menu {
                    ComposerMoreItems(more: more)
                } label: {
                    Image(systemName: "chevron.up")
                        .font(.system(size: 13, weight: .bold))
                        .foregroundStyle(mainEnabled ? Theme.surface : Theme.text)
                        .frame(width: 32, height: 40)
                        .contentShape(Rectangle())
                }
                .menuOrder(.fixed)
                .buttonStyle(.plain)
                .accessibilityLabel("More")
                .accessibilityIdentifier("composer-more")
            }
        }
        .background {
            if !dictation.isActive { Capsule().fill(splitFill) }
        }
    }

    /// The pill takes the main button's color, so the two read as one.
    private var splitFill: Color {
        mainEnabled ? Theme.text : Theme.border
    }

    private var mainEnabled: Bool {
        switch main {
        case .mic: true
        case .action(let action): ComposerActionFace.enabled(action)
        }
    }
}

/// The menu's items: dictation when it isn't the main action, the
/// attachment pickers, and the command search.
struct ComposerMoreItems: View {
    let more: ComposerMore

    var body: some View {
        if let dictate = more.dictate {
            Section {
                Button("Dictate", systemImage: "mic", action: dictate)
            }
        }
        Section {
            Button("Photo Library", systemImage: "photo.on.rectangle") { more.attach(.photos) }
            if more.hasCamera {
                Button("Take Photo", systemImage: "camera") { more.attach(.camera) }
            }
            Button("Choose File", systemImage: "folder") { more.attach(.files) }
        }
        .disabled(!more.canAttach)
        if let commands = more.commands {
            Section {
                Button(action: commands) {
                    Label { Text("Commands") } icon: { Image(uiImage: CommandGlyph.menuImage) }
                }
            }
        }
    }
}
