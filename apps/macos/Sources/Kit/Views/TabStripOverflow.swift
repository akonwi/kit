import SwiftUI

/// Which tabs of a scrolled strip are not fully visible, and where to
/// scroll so one is. Frames are spans in the strip's content, so they don't
/// change as it scrolls.
struct TabStripOverflow<ID: Hashable> {
    let frames: [ID: ClosedRange<CGFloat>]
    let offset: CGFloat
    let viewport: CGFloat

    func hides(_ id: ID) -> Bool {
        guard viewport > 0, let frame = frames[id] else { return false }
        return frame.lowerBound - offset < -1 || frame.upperBound - offset > viewport + 1
    }
    var hiddenBefore: Bool { viewport > 0 && frames.values.contains { $0.lowerBound - offset < -1 } }
    var hiddenAfter: Bool { viewport > 0 && frames.values.contains { $0.upperBound - offset > viewport + 1 } }

    /// The offset that brings `span` into view clear of the edge fades,
    /// moving as little as possible; nil when it is already in view. At the
    /// content's ends nothing is hidden, so no fade needs clearing.
    static func revealOffset(_ span: ClosedRange<CGFloat>, offset: CGFloat, viewport: CGFloat,
                             content: CGFloat, leading: CGFloat, trailing: CGFloat) -> CGFloat? {
        guard viewport > 0 else { return nil }
        var target = offset
        if span.upperBound + trailing > target + viewport { target = span.upperBound + trailing - viewport }
        if span.lowerBound - leading < target { target = span.lowerBound - leading }
        target = min(max(target, 0), max(0, content - viewport))
        return abs(target - offset) > 0.5 ? target : nil
    }
}

/// The count of a strip's hidden tabs, which opens their menu.
struct TabOverflowCount: View {
    @Environment(\.mica) private var theme
    let count: Int

    var body: some View {
        Text("+\(count)")
            .font(.kit(size: 11, design: .monospaced))
            .foregroundStyle(theme.muted)
            .padding(.horizontal, 7)
            .frame(height: 22)
            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
            .contentShape(Rectangle())
    }
}

/// The fades at a scrolled strip's edges, opaque where nothing is hidden.
/// Apply it as the strip's mask.
struct TabStripEdgeMask: View {
    let hiddenBefore: Bool
    let hiddenAfter: Bool
    let leading: CGFloat
    let trailing: CGFloat

    var body: some View {
        HStack(spacing: 0) {
            LinearGradient(colors: [.black.opacity(hiddenBefore ? 0 : 1), .black],
                           startPoint: .leading, endPoint: .trailing).frame(width: leading)
            Rectangle()
            LinearGradient(colors: [.black, .black.opacity(hiddenAfter ? 0 : 1)],
                           startPoint: .leading, endPoint: .trailing).frame(width: trailing)
        }
    }
}
