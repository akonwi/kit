import SwiftUI

/// The TUI's ten Braille frames, drawn independently of the user's font.
struct KitSpinner: View {
    static let frames: [UInt8] = [0x0b, 0x19, 0x39, 0x38, 0x3c, 0x34, 0x26, 0x27, 0x07, 0x0f]
    static let interval = 0.08
    let label: String?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(_ label: String? = nil) { self.label = label }

    var body: some View {
        HStack(spacing: 8) {
            TimelineView(.animation(minimumInterval: Self.interval, paused: reduceMotion)) { timeline in
                let frame = reduceMotion ? Self.frames[0] : Self.frame(at: timeline.date.timeIntervalSinceReferenceDate)
                Canvas { context, size in
                    for rect in Self.dots(for: frame) {
                        context.fill(Path(ellipseIn: rect), with: .foreground)
                    }
                }.frame(width: 12, height: 14)
            }.accessibilityHidden(true)
            if let label { Text(label) }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label ?? "Loading")
    }

    static func dots(for frame: UInt8) -> [CGRect] {
        let positions = [(0, 0), (0, 1), (0, 2), (1, 0), (1, 1), (1, 2), (0, 3), (1, 3)]
        return positions.enumerated().compactMap { bit, point in
            guard frame & (1 << bit) != 0 else { return nil }
            return CGRect(x: CGFloat(point.0) * 5 + 2, y: CGFloat(point.1) * 3.5 + 1, width: 2.5, height: 2.5)
        }
    }

    static func frame(at seconds: TimeInterval) -> UInt8 {
        frames[Int((max(0, seconds) / interval).rounded(.down)).quotientAndRemainder(dividingBy: frames.count).remainder]
    }
}
