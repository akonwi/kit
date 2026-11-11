import KitClient
import SwiftUI

/// The slim row under a session's header: the branch, its pull request,
/// which opens on GitHub, and the pull request's checks, which open the
/// checks sheet. It shows only in a repository.
struct RepositoryStrip: View {
    let status: RepositoryStatus
    let openChecks: () -> Void
    @Environment(\.openURL) private var openURL

    var body: some View {
        HStack(spacing: 6) {
            chip(Image("git-branch"), status.branch, color: Theme.muted)
                .truncationMode(.middle)
                .layoutPriority(-1)
                .accessibilityLabel(branchLabel)
                .accessibilityIdentifier("repository-branch")
            if let pull = status.pullRequest {
                Button { openURL(pull.url) } label: {
                    chip(Image("git-pull-request"), "#\(pull.number)", color: Theme.text)
                }
                .buttonStyle(.plain)
                .fixedSize()
                .accessibilityLabel("Pull request \(pull.number)")
                .accessibilityHint("Opens on GitHub")
                .accessibilityIdentifier("repository-pull-request")
                if let checks = pull.checks {
                    Button(action: openChecks) {
                        chip(Image(systemName: checks.symbol), checks.count, color: checks.tone.color)
                    }
                    .buttonStyle(.plain)
                    .fixedSize()
                    .accessibilityLabel(checks.spokenSummary)
                    .accessibilityHint("Shows the checks")
                    .accessibilityIdentifier("repository-checks")
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 8)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Repository")
        .accessibilityIdentifier("repository-strip")
    }

    private var branchLabel: String {
        let name = status.branch.hasSuffix("*") ? String(status.branch.dropLast()) : status.branch
        return "Branch \(name)" + (status.branch.hasSuffix("*") ? ", uncommitted changes" : "")
    }

    /// A chip's icon is 12 points, as in the design: Phosphor's branch and
    /// pull request glyphs, bundled as on the Mac, or a check's symbol.
    private func chip(_ icon: Image, _ label: String, color: Color) -> some View {
        HStack(spacing: 5) {
            icon
                .resizable()
                .scaledToFit()
                .frame(width: 12, height: 12)
            Text(label)
                .font(.system(size: 12, design: .monospaced))
                .lineLimit(1)
        }
        .foregroundStyle(color)
        .padding(.horizontal, 9)
        .frame(height: 26)
        .background(Theme.raised, in: Capsule())
        .overlay(Capsule().strokeBorder(Theme.border, lineWidth: 0.5))
        .contentShape(Capsule())
    }
}

extension ChecksPresentation.Tone {
    var color: Color {
        switch self {
        case .success: Theme.success
        case .danger: Theme.danger
        case .warning: Theme.warning
        case .muted: Theme.muted
        }
    }
}

/// A pull request's checks, most urgent first, kept current while open.
struct ChecksSheet: View {
    let repository: SessionRepository
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    private var checks: ChecksPresentation? { repository.status?.pullRequest?.checks }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            header
            if let checks {
                ScrollView {
                    ChecksList(checks: checks)
                }
                .scrollBounceBehavior(.basedOnSize)
                Button { openURL(checks.url) } label: {
                    Text("Open on GitHub")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(Theme.surface)
                        .frame(maxWidth: .infinity)
                        .frame(height: 50)
                        .background(Theme.text, in: Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("checks-open-github")
            } else {
                Text("This pull request has no checks.")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .padding(.leading, 4)
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 24)
        .padding(.bottom, 8)
        .background(Theme.raised)
        .presentationDetents([.medium, .large])
        .presentationBackground(Theme.raised)
        .accessibilityIdentifier("checks-sheet")
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text(checks.map { "Checks · #\($0.number)" } ?? "Checks")
                    .font(.system(size: 20, weight: .bold))
                    .foregroundStyle(Theme.text)
                if let checks {
                    Text(checks.summary)
                        .font(.system(size: 15))
                        .foregroundStyle(checks.tone.color)
                    if let requirement = checks.requirement {
                        Text(requirement)
                            .font(.system(size: 13))
                            .foregroundStyle(Theme.muted)
                    }
                }
            }
            .padding(.leading, 4)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            Button { dismiss() } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 32, height: 32)
                    .background(Theme.surface, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.border, lineWidth: 0.5))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Close")
        }
    }
}

/// The checks, one row each: a state glyph, the name and workflow, and
/// whether the check is required, then how many more GitHub lists.
struct ChecksList: View {
    let checks: ChecksPresentation

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(checks.rows.enumerated()), id: \.offset) { index, row in
                if index > 0 { divider }
                HStack(spacing: 12) {
                    Image(systemName: row.symbol)
                        .font(.system(size: 15))
                        .foregroundStyle(row.tone.color)
                        .frame(width: 20)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(row.name)
                            .font(.system(size: 15))
                            .foregroundStyle(Theme.text)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        if let workflow = row.workflow {
                            Text(workflow)
                                .font(.system(size: 12))
                                .foregroundStyle(Theme.muted)
                                .lineLimit(1)
                        }
                    }
                    Spacer(minLength: 8)
                    if row.required {
                        Text("Required")
                            .font(.system(size: 13))
                            .foregroundStyle(Theme.muted)
                    }
                }
                .padding(.horizontal, 16)
                .frame(minHeight: 58)
                .accessibilityElement(children: .combine)
            }
            if checks.omitted > 0 {
                if !checks.rows.isEmpty { divider }
                Text("\(checks.omitted) more on GitHub")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .padding(.horizontal, 16)
                    .frame(minHeight: 48)
            }
        }
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cardRadius, style: .continuous))
    }

    private var divider: some View {
        Rectangle().fill(Theme.border).frame(height: 0.5).padding(.leading, 48)
    }
}
