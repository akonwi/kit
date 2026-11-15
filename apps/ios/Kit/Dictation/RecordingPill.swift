import KitDictation
import SwiftUI

/// A recording whose composer isn't on screen, at the bottom of whatever is:
/// ✕ throws it away, ✓ puts the words in the draft, ↑ sends them, and a tap
/// anywhere else goes back to the composer.
struct RecordingPill: View {
    let dictation: Dictation
    /// The recording session's title, if its server lists it.
    let title: String?
    let stop: () -> Void
    let send: () -> Void
    let cancel: () -> Void
    let open: () -> Void

    var body: some View {
        let recording = dictation.phase == .recording
        HStack(spacing: 6) {
            Button(action: cancel) {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(recording ? Theme.text : Theme.border)
                    .frame(width: 36, height: 40)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!recording)
            .accessibilityLabel("Cancel dictation")
            .accessibilityIdentifier("recording-pill-cancel")

            Button(action: open) {
                HStack(spacing: 8) {
                    if recording {
                        Circle().fill(Theme.danger).frame(width: 8, height: 8)
                        if let startedAt = dictation.startedAt {
                            TimelineView(.periodic(from: startedAt, by: 1)) { context in
                                Text(Duration.seconds(max(0, context.date.timeIntervalSince(startedAt))),
                                     format: .time(pattern: .minuteSecond))
                                    .font(.system(size: 15))
                                    .monospacedDigit()
                                    .foregroundStyle(Theme.text)
                            }
                        }
                        DictationLevel(level: dictation.level)
                    } else {
                        ProgressView().controlSize(.small)
                        Text("Transcribing…")
                            .font(.system(size: 15))
                            .foregroundStyle(Theme.muted)
                    }
                    if let title {
                        Text(title)
                            .font(.system(size: 13))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 0)
                }
                .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(recording ? "Recording\(title.map { " for \($0)" } ?? ""). Show composer" : "Transcribing")
            .accessibilityIdentifier("recording-pill")

            Button(action: stop) {
                Image(systemName: "checkmark")
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(recording ? Theme.text : Theme.border)
                    .frame(width: 40, height: 40)
                    .background(Theme.hover, in: Circle())
            }
            .buttonStyle(.plain)
            .disabled(!recording)
            .accessibilityLabel("Add to draft")
            .accessibilityIdentifier("recording-pill-draft")

            Button(action: send) {
                Image(systemName: "arrow.up")
                    .font(.system(size: 16, weight: .bold))
                    .foregroundStyle(Theme.surface)
                    .frame(width: 40, height: 40)
                    .background(recording ? Theme.text : Theme.border, in: Circle())
            }
            .buttonStyle(.plain)
            .disabled(!recording)
            .accessibilityLabel("Send dictation")
            .accessibilityIdentifier("recording-pill-send")
        }
        .padding(.leading, 4)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .glassEffect(.regular, in: Capsule())
    }
}
