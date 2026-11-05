import SwiftUI

/// How wide a transcript reads in its pane. Rows keep a gutter on each side
/// and otherwise sit in a centred reading column; with the Full-width
/// transcript preference on they fill the pane instead. A pane narrower
/// than the column plus its gutters looks the same either way. The composer
/// region keeps its own centred column at either setting.
enum TranscriptLayout {
    /// The app preference: whether transcripts fill wide panes. Off by default.
    static let fullWidthKey = "fullWidthTranscript"
    static let fullWidthDefault = false
    /// The reading column's widest.
    static let columnWidth: CGFloat = 780
    /// The space between each pane edge and the transcript's rows.
    static let gutter: CGFloat = 32
    /// The composer region's widest, whatever the preference.
    static let composerWidth: CGFloat = 844

    /// The width of each transcript row in a pane `pane` wide.
    static func rowWidth(pane: CGFloat, fullWidth: Bool) -> CGFloat {
        let available = max(1, pane - 2 * gutter)
        return fullWidth ? available : min(columnWidth, available)
    }

    /// The widest a column of `width` may grow: uncapped in full width.
    static func maxWidth(_ width: CGFloat, fullWidth: Bool) -> CGFloat {
        fullWidth ? .infinity : width
    }
}

extension EnvironmentValues {
    /// Whether transcripts and the views that share their column fill wide
    /// panes. Set from the app preference by `transcriptWidthPreference()`.
    @Entry var transcriptFullWidth = TranscriptLayout.fullWidthDefault
}

/// Publishes the Full-width transcript preference to the views below, so
/// every window and tile follows a change as soon as it's made.
private struct TranscriptWidthPreference: ViewModifier {
    @AppStorage(TranscriptLayout.fullWidthKey) private var fullWidth = TranscriptLayout.fullWidthDefault
    func body(content: Content) -> some View {
        content.environment(\.transcriptFullWidth, fullWidth)
    }
}

extension View {
    /// Sets `transcriptFullWidth` from the app preference.
    func transcriptWidthPreference() -> some View { modifier(TranscriptWidthPreference()) }
}
