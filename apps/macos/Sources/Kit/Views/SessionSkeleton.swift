import AppKit
import SwiftUI

/// A session tab's outline while its server connects or its session opens:
/// the conversation tile's tab strip, the reading column's latest turn, and
/// the composer, in their places and without content. It fills the
/// window's single tile, which draws the tile's surface and border.
struct SessionSkeleton: View {
    @Environment(\.mica) private var theme
    @Environment(\.transcriptFullWidth) private var fullWidth
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// What the tab waits for, read to VoiceOver.
    let label: String

    /// The assistant's lines under the user's message, as fractions of the
    /// column, so the outline reads as prose.
    static let lines: [CGFloat] = [1, 0.94, 0.97, 0.62]

    var body: some View {
        VStack(spacing: 0) {
            bar(width: 64, height: 9)
                .padding(.horizontal, 12)
                .frame(maxWidth: .infinity, maxHeight: tabStripHeight, alignment: .leading)
                .overlay(alignment: .bottom) { Rule() }
            GeometryReader { proxy in
                let width = TranscriptLayout.rowWidth(pane: proxy.size.width, fullWidth: fullWidth)
                // A session opens at its latest turn, just above the composer.
                VStack(alignment: .leading, spacing: 12) {
                    placeholder(cornerRadius: 4).frame(height: 54)
                        .padding(.bottom, 12)
                    ForEach(Self.lines.indices, id: \.self) { index in
                        bar(width: width * Self.lines[index], height: 9)
                    }
                }
                .frame(width: width)
                .padding(.bottom, 24)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
            }
            .clipped()
            RoundedRectangle(cornerRadius: 4).fill(theme.raised)
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border, lineWidth: 1))
                .frame(height: Self.composerHeight)
                .frame(maxWidth: TranscriptLayout.composerWidth)
                .padding([.horizontal, .bottom], 14)
                .frame(maxWidth: .infinity)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
    }

    /// An empty composer's height.
    static let composerHeight: CGFloat = 70

    private func placeholder(cornerRadius: CGFloat) -> some View {
        SkeletonPlaceholder(color: NSColor(theme.hover), cornerRadius: cornerRadius, pulses: !reduceMotion)
    }

    private func bar(width: CGFloat, height: CGFloat) -> some View {
        placeholder(cornerRadius: height / 2).frame(width: width, height: height)
    }
}

/// One of the outline's shapes, fading to `SkeletonPlaceholder.faded` and
/// back. Core Animation runs the fade in the render server, so a window
/// waiting for its session spends no main-thread time on it; every
/// placeholder fades in step with the others.
struct SkeletonPlaceholder: NSViewRepresentable {
    let color: NSColor
    let cornerRadius: CGFloat
    let pulses: Bool

    /// The opacity the placeholders fade to.
    static let faded: Float = 0.55
    /// How long one fade takes; the placeholders fade out and back in.
    static let period: CFTimeInterval = 1.1
    /// The fade's key on each placeholder's layer.
    static let animationKey = "pulse"
    /// When every placeholder's fade began, so they stay in step.
    @MainActor private static let start = CACurrentMediaTime()

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        view.wantsLayer = true
        return view
    }

    func updateNSView(_ view: NSView, context: Context) {
        guard let layer = view.layer else { return }
        layer.backgroundColor = color.cgColor
        layer.cornerRadius = cornerRadius
        if !pulses {
            layer.removeAnimation(forKey: Self.animationKey)
        } else if layer.animation(forKey: Self.animationKey) == nil {
            layer.add(Self.pulse(beginTime: layer.convertTime(Self.start, from: nil)), forKey: Self.animationKey)
        }
    }

    /// The fade, begun at `beginTime` in its layer's time.
    static func pulse(beginTime: CFTimeInterval) -> CABasicAnimation {
        let animation = CABasicAnimation(keyPath: "opacity")
        animation.fromValue = 1
        animation.toValue = faded
        animation.duration = period
        animation.autoreverses = true
        animation.repeatCount = .infinity
        animation.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
        animation.beginTime = beginTime
        animation.isRemovedOnCompletion = false
        return animation
    }
}
