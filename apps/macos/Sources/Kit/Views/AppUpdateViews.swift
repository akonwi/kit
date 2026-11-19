import AppKit
import SwiftUI

/// The update chip: at the bottom of the sidebar with side tabs, or before
/// the tab-placement toggle with top tabs (ADR 0048). It shows nothing while
/// Kit is up to date.
struct AppUpdateChip: View {
    @Environment(\.mica) private var theme
    @Environment(\.controlActiveState) private var activeState
    @Environment(\.serverDirectory) private var servers
    var updater: AppUpdater = .shared
    @State private var sheet = false
    @State private var hovering = false

    var body: some View {
        Group {
            switch updater.state {
            case .idle:
                EmptyView()
            case .available:
                chip(label: "Update available", help: "See what’s new in Kit and update") { sheet = true }
            case .updating(let release, let progress):
                updating(release, progress: progress)
            case .ready:
                chip(label: "Relaunch to update", help: "Quit Kit, install the update, and open it again",
                     filled: true) { updater.relaunch() }
            case .failed:
                chip(label: "Update failed", dot: theme.warning, help: "See why the update failed") { sheet = true }
            }
        }
        .sheet(isPresented: $sheet) {
            AppUpdateSheet(updater: updater, servers: servers) { sheet = false }
                .environment(\.mica, theme)
        }
        // The menu asks; the key session window answers, once it is key.
        .onChange(of: updater.sheetPending, initial: true) { presentRequested() }
        .onChange(of: activeState) { presentRequested() }
    }

    private func presentRequested() {
        guard updater.sheetPending, activeState == .key, updater.state.release != nil else { return }
        updater.sheetShown()
        sheet = true
    }

    private func chip(label: String, dot: Color? = nil, help: String, filled: Bool = false,
                      action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 7) {
                Circle().fill(dot ?? theme.accent).frame(width: 6, height: 6)
                Text(label).font(.kit(size: 12)).foregroundStyle(theme.text).lineLimit(1)
            }
            .modifier(ChipFrame(filled: filled || hovering))
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(label)
    }

    private func updating(_ release: KitRelease, progress: Double?) -> some View {
        HStack(spacing: 7) {
            KitSpinner().foregroundStyle(theme.accent)
            Text("Updating…").font(.kit(size: 12)).foregroundStyle(theme.text).lineLimit(1)
            if let progress {
                Text("\(Int(progress * 100))%").font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
            }
        }
        .modifier(ChipFrame(filled: false))
        .help(release.appArchive.map { "Downloading Kit \(release.version), \(ByteCountFormatter.string(fromByteCount: $0.size, countStyle: .file))" }
              ?? "Downloading Kit \(release.version)")
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(progress.map { "Updating Kit, \(Int($0 * 100)) percent" } ?? "Updating Kit")
    }

    private struct ChipFrame: ViewModifier {
        @Environment(\.mica) private var theme
        let filled: Bool

        func body(content: Content) -> some View {
            content
                .padding(.leading, 9).padding(.trailing, 10)
                .frame(height: 26)
                .background(filled ? theme.frameHover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                .contentShape(Rectangle())
        }
    }
}

/// The sheet the chip opens: every release since this app's beside the
/// selected one's notes, with Later and Update, or why the update failed
/// with Try Again.
struct AppUpdateSheet: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    @Environment(\.openSettings) private var openSettings
    let updater: AppUpdater
    /// The servers this window reaches, for this Mac's server's version.
    var servers: ServerDirectory?
    let close: () -> Void
    @State private var selected: String?

    static let width: CGFloat = 720

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let release = updater.state.release, let current = updater.current {
                switch updater.state {
                case .failed(_, let failure):
                    failed(release, failure: failure, current: current)
                default:
                    available(release, current: current)
                }
            }
        }
        .padding(24)
        .frame(width: Self.width)
        .background(theme.surface)
        .foregroundStyle(theme.text)
        .font(.kit(size: 13))
    }

    /// The releases the sheet lists: those since this app's, or the
    /// offered one when no check has listed them.
    private func releases(_ release: KitRelease) -> [KitRelease] {
        updater.releasesSince.isEmpty ? [release] : updater.releasesSince
    }

    private func available(_ release: KitRelease, current: ReleaseVersion) -> some View {
        let releases = releases(release)
        let shown = releases.first { $0.tag == selected } ?? releases[0]
        let server = ServerRowPresentation.thisMacUpdate(servers?.local, newest: updater.newestCLI?.version)
        return VStack(alignment: .leading, spacing: 0) {
            AppUpdateHeader(title: "Kit \(release.version) is available",
                            subtitle: releases.count == 1 ? "You have \(current).\(Self.released(release))" : "You have \(current).")
            ReleaseNotesBrowser(releases: releases, selected: $selected)
            if let server {
                HStack(spacing: 8) {
                    Text("This Mac’s server has Kit \(server.running); \(server.newest) is out.")
                        .foregroundStyle(theme.muted)
                    Button("Settings › Servers") {
                        SettingsSection.servers.open(with: openSettings, server: ServerClient.localID)
                    }
                    .buttonStyle(.plain).foregroundStyle(theme.accent)
                    .pointerStyle(.link)
                }
                .font(.kit(size: 12))
                .padding(.top, 16)
            }
            Text("Kit downloads it in the background while you work, then asks to relaunch. Sessions keep running on their servers, and your tabs, layout and drafts come back.")
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, server == nil ? 20 : 12)
            AppUpdateFooter(link: "View on GitHub", url: shown.url) {
                Button("Later", action: close).buttonStyle(MicaButtonStyle(compact: true)).keyboardShortcut(.cancelAction)
                Button("Update") {
                    updater.update()
                    close()
                }.buttonStyle(MicaButtonStyle(primary: true, compact: true)).keyboardShortcut(.defaultAction)
            }
        }
    }

    private func failed(_ release: KitRelease, failure: AppUpdateInstaller.Failure, current: ReleaseVersion) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            AppUpdateHeader(title: "Kit couldn’t install \(release.version)", subtitle: "Nothing was changed. You’re still on \(current).")
            HStack(alignment: .top, spacing: 10) {
                IconView(.warning, size: 14).foregroundStyle(theme.warning)
                VStack(alignment: .leading, spacing: 4) {
                    Text(failure.title)
                    Text(failure.detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
            }
            .padding(16)
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            AppUpdateFooter(link: "Open Releases Page", url: KitReleases.releasesPage) {
                Button("Close", action: close).buttonStyle(MicaButtonStyle(compact: true)).keyboardShortcut(.cancelAction)
                Button("Try Again") {
                    updater.update()
                    close()
                }.buttonStyle(MicaButtonStyle(primary: true, compact: true)).keyboardShortcut(.defaultAction)
            }
        }
    }

    /// " Released November 16." for a release with a date.
    static func released(_ release: KitRelease) -> String {
        guard let date = release.publishedAt else { return "" }
        return " Released \(ReleaseNotesBrowser.date(date))."
    }
}

/// Kit › Release Notes: this app's release notes, with Done.
struct ReleaseNotesSheet: View {
    @Environment(\.mica) private var theme
    let release: KitRelease
    let close: () -> Void
    @State private var selected: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            AppUpdateHeader(title: "Kit \(release.version)",
                            subtitle: AppUpdateSheet.released(release).trimmingCharacters(in: .whitespaces))
            ReleaseNotesBrowser(releases: [release], selected: $selected)
                .padding(.bottom, 4)
            AppUpdateFooter(link: "View on GitHub", url: release.url) {
                Button("Done", action: close).buttonStyle(MicaButtonStyle(primary: true, compact: true))
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(24)
        .frame(width: AppUpdateSheet.width)
        .background(theme.surface)
        .foregroundStyle(theme.text)
        .font(.kit(size: 13))
        .onExitCommand(perform: close)
    }
}

/// Shows the release notes Kit › Release Notes found, in the key session
/// window.
struct ReleaseNotesPresenter: ViewModifier {
    @Environment(\.controlActiveState) private var activeState
    let theme: MicaTheme
    var updater: AppUpdater = .shared
    @State private var shown: KitRelease?

    func body(content: Content) -> some View {
        content
            .sheet(item: $shown) { release in
                ReleaseNotesSheet(release: release) { shown = nil }
                    .environment(\.mica, theme)
            }
            .onChange(of: updater.releaseNotesPending, initial: true) { present() }
            .onChange(of: activeState) { present() }
    }

    private func present() {
        guard let release = updater.releaseNotesPending, activeState == .key else { return }
        updater.releaseNotesShown()
        shown = release
    }
}

/// A sheet's title and the line under it.
private struct AppUpdateHeader: View {
    @Environment(\.mica) private var theme
    let title: String
    let subtitle: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.kit(size: 16, weight: .semibold))
            if !subtitle.isEmpty {
                Text(subtitle).font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .padding(.bottom, 20)
    }
}

/// A rule, then a link on the leading edge and the actions on the trailing.
private struct AppUpdateFooter<Actions: View>: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let link: String
    let url: URL
    @ViewBuilder let actions: Actions

    var body: some View {
        VStack(spacing: 0) {
            Rule().padding(.vertical, 16)
            HStack(spacing: 8) {
                Button { openURL(url) } label: {
                    HStack(spacing: 4) {
                        Text(link)
                        IconView(.arrowUpRight, size: 10)
                    }
                    .font(.kit(size: 12)).foregroundStyle(theme.accent)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                Spacer()
                actions
            }
        }
    }
}

/// Releases beside the selected one's notes: its lead and What's changed,
/// and for the newest, Compatibility folded at the end. One release shows
/// without the list.
struct ReleaseNotesBrowser: View {
    @Environment(\.mica) private var theme
    /// Newest first.
    let releases: [KitRelease]
    @Binding var selected: String?
    @State private var compatibilityOpen = false

    static let height: CGFloat = 400
    static let listWidth: CGFloat = 160

    private var shown: KitRelease { releases.first { $0.tag == selected } ?? releases[0] }

    var body: some View {
        Group {
            if releases.count > 1 {
                HStack(spacing: 0) {
                    list
                    Rectangle().fill(theme.border).frame(width: 1)
                    notes(shown)
                }
                .frame(height: Self.height)
            } else {
                notes(shown)
                    .frame(maxHeight: Self.height)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
    }

    private var list: some View {
        KitScrollView {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(releases) { release in
                    let isShown = release.tag == shown.tag
                    Button { selected = release.tag } label: {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(release.version.description).font(.kit(size: 13, weight: .medium)).foregroundStyle(theme.text)
                            if let date = release.publishedAt {
                                Text(Self.date(date)).font(.kit(size: 11)).foregroundStyle(theme.muted)
                            }
                        }
                        .padding(.horizontal, 8).padding(.vertical, 6)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(isShown ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Kit \(release.version)")
                    .accessibilityAddTraits(isShown ? .isSelected : [])
                }
            }
            .padding(6)
        }
        .scrollIndicators(.automatic)
        .frame(width: Self.listWidth)
        .background(theme.raised)
    }

    private func notes(_ release: KitRelease) -> some View {
        let notes = ReleaseNotes(markdown: release.notes)
        let newest = release.tag == releases[0].tag
        return KitScrollView {
            VStack(alignment: .leading, spacing: 10) {
                MarkdownView(source: notes.summary, size: 13, compactHeadings: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if newest, !notes.compatibility.isEmpty {
                    compatibility(notes.compatibility)
                }
            }
            .padding(.horizontal, 20).padding(.vertical, 16)
        }
        .id(release.tag)
    }

    private func compatibility(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Rule()
            Button { compatibilityOpen.toggle() } label: {
                HStack(spacing: 6) {
                    IconView(compatibilityOpen ? .caretDown : .caretRight, size: 10).foregroundStyle(theme.muted)
                    Text("Compatibility").font(.kit(size: 13, weight: .semibold))
                }
                .frame(height: 24)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(compatibilityOpen ? "Expanded" : "Collapsed")
            if compatibilityOpen {
                MarkdownView(source: text, size: 13, compactHeadings: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.leading, 16)
            }
        }
    }

    /// "November 16".
    static func date(_ date: Date) -> String {
        date.formatted(.dateTime.month(.wide).day())
    }
}
