import AppKit
import SwiftUI

/// The update chip: at the bottom of the sidebar with side tabs, or before
/// the tab-placement toggle with top tabs (ADR 0048). It shows nothing while
/// Kit is up to date.
struct AppUpdateChip: View {
    @Environment(\.mica) private var theme
    @Environment(\.controlActiveState) private var activeState
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
            AppUpdateSheet(updater: updater) { sheet = false }
                .environment(\.mica, theme)
        }
        .onChange(of: updater.sheetRequest) {
            // The menu asks; the key window answers.
            if activeState == .key, updater.state.release != nil { sheet = true }
        }
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

/// The sheet the chip opens: the release's notes with Later and Update, or
/// why the update failed with Try Again.
struct AppUpdateSheet: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let updater: AppUpdater
    let close: () -> Void

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
        .frame(width: 520)
        .background(theme.surface)
        .foregroundStyle(theme.text)
        .font(.kit(size: 13))
    }

    private func available(_ release: KitRelease, current: ReleaseVersion) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            header("Kit \(release.version) is available", subtitle: "You have \(current).\(released(release))")
            ScrollView {
                MarkdownView(source: Self.notes(release.notes), size: 13, compactHeadings: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(16)
            }
            .frame(maxHeight: 320)
            .fixedSize(horizontal: false, vertical: true)
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            Text("Kit downloads it in the background while you work, then asks to relaunch. Sessions keep running on their servers, and your tabs, layout and drafts come back.")
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 20)
            footer(link: "View on GitHub", url: release.url) {
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
            header("Kit couldn’t install \(release.version)", subtitle: "Nothing was changed. You’re still on \(current).")
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
            footer(link: "Open Releases Page", url: KitReleases.releasesPage) {
                Button("Close", action: close).buttonStyle(MicaButtonStyle(compact: true)).keyboardShortcut(.cancelAction)
                Button("Try Again") {
                    updater.update()
                    close()
                }.buttonStyle(MicaButtonStyle(primary: true, compact: true)).keyboardShortcut(.defaultAction)
            }
        }
    }

    private func header(_ title: String, subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.kit(size: 16, weight: .semibold))
            Text(subtitle).font(.kit(size: 12)).foregroundStyle(theme.muted)
        }
        .padding(.bottom, 20)
    }

    private func footer<Actions: View>(link: String, url: URL, @ViewBuilder actions: () -> Actions) -> some View {
        VStack(spacing: 0) {
            Rule().padding(.vertical, 16)
            HStack(spacing: 8) {
                Button { openURL(url) } label: {
                    HStack(spacing: 4) {
                        Text(link)
                        IconView(.arrowUpRight, size: 10)
                    }
                    .font(.kit(size: 12)).foregroundStyle(theme.accent)
                }
                .buttonStyle(.plain)
                Spacer()
                actions()
            }
        }
    }

    private func released(_ release: KitRelease) -> String {
        guard let date = release.publishedAt else { return "" }
        return " Released \(date.formatted(.dateTime.month(.wide).day()))."
    }

    /// The notes without their title, which the sheet's header repeats.
    static func notes(_ markdown: String) -> String {
        var lines = markdown.components(separatedBy: "\n")
        while let first = lines.first, first.trimmingCharacters(in: .whitespaces).isEmpty { lines.removeFirst() }
        if let first = lines.first, first.hasPrefix("# ") { lines.removeFirst() }
        return lines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
