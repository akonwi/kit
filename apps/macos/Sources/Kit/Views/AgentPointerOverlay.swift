import SwiftUI

/// The agent pointer over a browser tab's page (ADR 0051). It is drawn by
/// Kit above the web view, so the page can neither hide nor imitate it. It
/// glides to each element the agent acts on, plays a press ring for clicks,
/// and fades once the agent has finished.
struct AgentPointerOverlay: View {
    @Environment(\.mica) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let pointer: AgentPointer?

    /// How long the pointer takes to reach an element.
    static let glide = 0.4

    var body: some View {
        ZStack(alignment: .topLeading) {
            if let pointer {
                PressRing(color: theme.accent, presses: pointer.presses)
                    .position(pointer.location)
                AgentPointerShape()
                    .fill(theme.accent)
                    // The system pointer's light outline and shadow keep it
                    // legible on any page.
                    .overlay(AgentPointerShape().stroke(.white, style: StrokeStyle(lineWidth: 1.5, lineJoin: .round)))
                    .frame(width: 18, height: 24)
                    .shadow(color: .black.opacity(0.35), radius: 2, y: 1)
                    // The tip sits on the location.
                    .position(x: pointer.location.x + 9, y: pointer.location.y + 12)
                    .opacity(pointer.visible ? 1 : 0)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .animation(reduceMotion ? nil : .easeInOut(duration: Self.glide), value: pointer?.location)
        .animation(.easeOut(duration: 0.3), value: pointer?.visible)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// An arrow pointer whose tip is the top-left corner of its frame.
struct AgentPointerShape: Shape {
    func path(in rect: CGRect) -> Path {
        let w = rect.width, h = rect.height
        var path = Path()
        path.move(to: CGPoint(x: rect.minX, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.minX, y: rect.minY + h * 0.78))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.27, y: rect.minY + h * 0.6))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.45, y: rect.minY + h))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.62, y: rect.minY + h * 0.93))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.45, y: rect.minY + h * 0.55))
        path.addLine(to: CGPoint(x: rect.minX + w * 0.78, y: rect.minY + h * 0.55))
        path.closeSubpath()
        return path
    }
}

/// A ring that grows and fades from the pointer's tip each time `presses`
/// changes.
struct PressRing: View {
    let color: Color
    let presses: Int

    struct Frame: Equatable {
        var scale = 0.4
        var opacity = 0.0
    }

    /// The ring's animation. Each track starts with a jump, never a
    /// zero-length segment before a curve, which makes SwiftUI produce
    /// non-finite values.
    @KeyframesBuilder<Frame> static var tracks: some Keyframes<Frame> {
        KeyframeTrack(\.scale) {
            MoveKeyframe(0.4)
            LinearKeyframe(1.6, duration: 0.45, timingCurve: .easeOut)
        }
        KeyframeTrack(\.opacity) {
            MoveKeyframe(0.9)
            LinearKeyframe(0, duration: 0.45, timingCurve: .easeOut)
        }
    }

    var body: some View {
        Circle()
            .strokeBorder(color, lineWidth: 2)
            .frame(width: 28, height: 28)
            .keyframeAnimator(initialValue: Frame(), trigger: presses) { ring, frame in
                ring.scaleEffect(frame.scale).opacity(frame.opacity)
            } keyframes: { _ in Self.tracks }
    }
}
