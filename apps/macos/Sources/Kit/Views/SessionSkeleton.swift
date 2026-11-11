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
    @State private var faded = false

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
                    RoundedRectangle(cornerRadius: 4).fill(placeholder).frame(height: 54)
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
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true)) { faded = true }
        }
    }

    /// An empty composer's height.
    static let composerHeight: CGFloat = 70

    private var placeholder: Color { theme.hover.opacity(faded ? 0.55 : 1) }

    private func bar(width: CGFloat, height: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: height / 2).fill(placeholder).frame(width: width, height: height)
    }
}
