import SwiftUI

/// What the checks chip beside the pull request chip shows, derived from the
/// session's pull request status. Nil when the presented pull request has no
/// checks to report.
struct PullRequestChecksPresentation: Equatable {
    enum Tone: Equatable { case success, danger, warning, muted }

    struct Row: Equatable {
        let name: String
        let workflow: String?
        let icon: Icon
        let tone: Tone
        let required: Bool
    }

    let icon: Icon
    let tone: Tone
    /// Passed checks of those that ran or will run, such as "10/12".
    let count: String
    /// Counts by state, most urgent first, such as "2 failing, 1 running, 9 passed".
    let summary: String
    /// The required checks' state, when GitHub reported it.
    let requirement: String?
    let rows: [Row]
    /// Checks the server counted but didn't list.
    let omitted: Int
    /// Where the omitted checks can be seen.
    let checksURL: URL

    static func make(_ session: SessionExcerpt?) -> PullRequestChecksPresentation? {
        guard let session, let number = WorkspaceLocation.pullRequestNumber(session),
              let status = session.pullRequestStatus, status.number == number else { return nil }
        let checks = status.checks
        let icon: Icon, tone: Tone
        switch checks.state {
        case .none: return nil
        case .passing: (icon, tone) = (.checkCircle, .success)
        case .failing: (icon, tone) = (.xCircle, .danger)
        case .pending: (icon, tone) = (.clock, .warning)
        }
        let counts = checks.counts
        let parts: [(Int, String)] = [
            (counts.failed, "failing"), (counts.canceled, "canceled"), (counts.pending, "running"),
            (counts.passed, "passed"), (counts.skipped, "skipped"), (counts.unknown, "unknown"),
        ]
        let summary = parts.filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }.joined(separator: ", ")
        let requirement: String? = switch checks.required {
        case .unknown: nil
        case .pending: "Required checks are running"
        case .satisfied: "Required checks passed"
        case .unsatisfied: "Required checks aren’t met"
        }
        let rows = checks.items.map { check in
            let (icon, tone) = Self.glyph(check.state)
            return Row(name: check.name, workflow: check.workflow, icon: icon, tone: tone, required: check.required)
        }
        return PullRequestChecksPresentation(
            icon: icon, tone: tone, count: "\(counts.passed)/\(counts.total - counts.skipped)",
            summary: summary, requirement: requirement, rows: rows, omitted: checks.omitted,
            checksURL: status.url.appendingPathComponent("checks"))
    }

    static func glyph(_ state: PullRequestChecks.CheckState) -> (Icon, Tone) {
        switch state {
        case .passed: (.checkCircle, .success)
        case .failed: (.xCircle, .danger)
        case .pending: (.clock, .warning)
        case .canceled: (.prohibit, .muted)
        case .skipped: (.minusCircle, .muted)
        case .unknown: (.question, .muted)
        }
    }

    /// One line for the chip's tooltip and VoiceOver.
    var spokenSummary: String {
        ["Checks: " + summary, requirement].compactMap { $0 }.joined(separator: ". ")
    }
}

extension MicaTheme {
    func color(_ tone: PullRequestChecksPresentation.Tone) -> Color {
        switch tone {
        case .success: success
        case .danger: danger
        case .warning: warning
        case .muted: muted
        }
    }
}

/// The checks chip beside the pull request chip. Clicking it lists the
/// checks in the app's anchored popover card, below the chip; the pull
/// request chip still opens the pull request.
struct PullRequestChecksChip: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let presentation: PullRequestChecksPresentation
    @State private var popover = AnchoredPopover()
    @State private var anchor = ComposerPickerAnchor()
    @State private var listed = PullRequestChecksListed()
    @State private var open = false

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 5) {
                IconView(presentation.icon, size: 12)
                Text(presentation.count)
            }
            .foregroundStyle(theme.color(presentation.tone))
            .padding(.horizontal, 6)
            .frame(height: 20)
            .background(open ? theme.hover : theme.frameHover, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .fixedSize()
        .background(ComposerPickerAnchorView(anchor: anchor))
        .help(presentation.spokenSummary)
        .accessibilityLabel(presentation.spokenSummary)
        .accessibilityHint("Shows the pull request's checks")
        .onAppear { listed.presentation = presentation }
        // An open list follows the stream.
        .onChange(of: presentation) { listed.presentation = presentation }
        .onDisappear { popover.dismiss() }
    }

    private func toggle() {
        if popover.isOpen { popover.dismiss(); return }
        // The click that reached the chip already closed the list.
        if let closedAt = popover.closedAt, Date().timeIntervalSince(closedAt) < 0.3 { return }
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        listed.presentation = presentation
        let content = PullRequestChecksList(listed: listed, theme: theme)
            .environment(\.openURL, openURL)
        open = true
        popover.show(anchor: frame, parent: window, edge: .below, alignment: .leading, content: content) {
            open = false
        }
    }
}

/// The checks an open list shows, updated while it is open.
@MainActor @Observable final class PullRequestChecksListed {
    var presentation: PullRequestChecksPresentation?
}

/// The popover card listing a pull request's checks, most urgent first: a
/// header with the counts and the required checks' state, then the checks.
struct PullRequestChecksList: View {
    let listed: PullRequestChecksListed
    let theme: MicaTheme

    static let width: CGFloat = 360
    static let rowHeight: CGFloat = 26
    static let maxListHeight: CGFloat = 280

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let presentation = listed.presentation {
                HStack(spacing: 8) {
                    Text(presentation.summary).font(.kit(size: 12)).foregroundStyle(theme.text)
                        .lineLimit(1).truncationMode(.tail)
                    Spacer(minLength: 8)
                    if let requirement = presentation.requirement {
                        Text(requirement).font(.kit(size: 11)).foregroundStyle(theme.muted).fixedSize()
                    }
                }
                .padding(.horizontal, 10).frame(height: 32)
                Rule()
                ScrollView {
                    PullRequestChecksRows(rows: presentation.rows).padding(4)
                }
                .frame(height: min(Self.maxListHeight, CGFloat(presentation.rows.count) * Self.rowHeight + 8))
                if presentation.omitted > 0 {
                    Rule()
                    Link("\(presentation.omitted) more on GitHub", destination: presentation.checksURL)
                        .font(.kit(size: 11)).foregroundStyle(theme.accent)
                        .padding(.horizontal, 12).frame(height: 28)
                }
            }
        }
        .frame(width: Self.width, alignment: .leading)
        .foregroundStyle(theme.text)
        .background(theme.raised)
        .popoverCard(theme)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Pull request checks")
    }
}

/// The listed checks: a state glyph, the name, the workflow, and whether the
/// check is required.
struct PullRequestChecksRows: View {
    @Environment(\.mica) private var theme
    let rows: [PullRequestChecksPresentation.Row]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                HStack(spacing: 8) {
                    IconView(row.icon, size: 12).foregroundStyle(theme.color(row.tone)).frame(width: 12)
                    Text(row.name).font(.kit(size: 12)).foregroundStyle(theme.text)
                        .lineLimit(1).truncationMode(.middle).layoutPriority(1)
                    if let workflow = row.workflow {
                        Text(workflow).font(.kit(size: 11)).foregroundStyle(theme.muted)
                            .lineLimit(1).truncationMode(.tail)
                    }
                    Spacer(minLength: 8)
                    if row.required {
                        Text("Required").font(.kit(size: 11)).foregroundStyle(theme.muted).fixedSize()
                    }
                }
                .padding(.horizontal, 8)
                .frame(height: PullRequestChecksList.rowHeight)
                .accessibilityElement(children: .combine)
            }
        }
    }
}
