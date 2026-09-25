import SwiftUI

struct SessionContext: Equatable {
    enum Level { case normal, warning, critical }
    let tokens: Int
    let capacity: Int
    let percentage: Int

    init?(tokens: Int?, capacity: Int?) {
        guard let tokens, let capacity, tokens > 0, capacity > 0 else { return nil }
        self.tokens = tokens
        self.capacity = capacity
        percentage = Int(min(100, max(1, (Double(tokens) * 100 / Double(capacity)).rounded())))
    }

    var level: Level { percentage > 90 ? .critical : percentage >= 80 ? .warning : .normal }

    func color(in theme: MicaTheme) -> Color {
        switch level {
        case .normal: theme.token("progressNormal", fallback: theme.muted)
        case .warning: theme.token("progressWarning", fallback: theme.warning)
        case .critical: theme.token("progressCritical", fallback: theme.danger)
        }
    }
}

struct ContextUsageView: View {
    let context: SessionContext
    @Environment(\.mica) private var theme
    var body: some View {
        Text("\(context.percentage)%")
            .monospacedDigit()
            .foregroundStyle(context.color(in: theme))
            .padding(.horizontal, 6).padding(.vertical, 3)
            .help("Context used: \(context.tokens.formatted()) of \(context.capacity.formatted()) tokens")
            .accessibilityLabel("Context used, \(context.percentage) percent")
    }
}
