import AppKit
import SwiftUI

/// A browser tab's pane: a toolbar with navigation, the address field, and
/// Open in default browser over the page. The page draws with its own
/// colours; a page that can't load shows Kit's message instead.
///
/// The pane draws only while its tab is on screen. A hidden tab's page stays
/// loaded in its engine but out of the window, so showing a session with
/// many browser tabs builds and attaches only the ones in view.
struct BrowserPane: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let tab: BrowserTab
    /// What the address field holds while it's edited. A blank tab keeps
    /// it while hidden.
    @State private var draft = ""
    private var pane: WorkspacePane { .browser(tab.id) }

    var body: some View {
        if workspace.isVisible(pane) {
            content
        } else {
            Color.clear
        }
    }

    private var content: some View {
        VStack(spacing: 0) {
            BrowserToolbar(workspace: workspace, tab: tab, draft: $draft)
            if let site = tab.blockedDownload {
                BrowserBlockedDownloadBar(site: site) { tab.dismissBlockedDownload() }
            }
            ZStack {
                theme.surface
                if tab.url != nil {
                    LiveViewHost(view: tab.view)
                        .opacity(tab.failure == nil ? 1 : 0)
                        .allowsHitTesting(tab.failure == nil)
                        .accessibilityHidden(tab.failure != nil)
                        .workspaceFocusTarget(pane, when: tab.failure == nil)
                }
                if let failure = tab.failure {
                    BrowserFailureView(failure: failure) { tab.reload() }
                }
            }
        }
    }
}

private struct BrowserToolbar: View {
    @Environment(\.mica) private var theme
    let workspace: WorkspaceState
    let tab: BrowserTab
    @Binding var draft: String
    @FocusState private var addressFocused: Bool

    var body: some View {
        let page = tab.page
        HStack(spacing: 2) {
            BrowserToolbarButton(icon: .caretLeft, help: "Back") { tab.goBack() }
                .disabled(!page.canGoBack)
            BrowserToolbarButton(icon: .caretRight, help: "Forward") { tab.goForward() }
                .disabled(!page.canGoForward)
            if tab.loading {
                BrowserToolbarButton(icon: .x, size: 13, help: "Stop") { tab.stop() }
            } else {
                BrowserToolbarButton(icon: .arrowClockwise, size: 13, help: "Reload") { tab.reload() }
                    .disabled(tab.url == nil)
            }
            address.padding(.leading, 6)
            BrowserToolbarButton(icon: .arrowSquareOut, help: "Open in Default Browser") {
                if let url = tab.url { NSWorkspace.shared.open(url) }
            }
            .disabled(tab.url.map(BrowserAddress.isWeb) != true)
            .padding(.leading, 2)
        }
        .padding(.horizontal, 8)
        .frame(height: 38)
        .overlay(alignment: .bottom) { Rule() }
        .overlay(alignment: .bottomLeading) {
            if tab.loading {
                GeometryReader { geometry in
                    Rectangle().fill(theme.accent)
                        .frame(width: geometry.size.width * max(0.05, min(1, page.progress)), height: 2)
                        .animation(.easeOut(duration: 0.2), value: page.progress)
                }
                .frame(height: 2)
                .offset(y: 1)
                .accessibilityLabel("Loading")
                .accessibilityValue("\(Int(page.progress * 100)) percent")
            }
        }
        .task(id: tab.addressFocusRequest) {
            // A toolbar shown again doesn't repeat a request it answered.
            guard tab.takeAddressFocusRequest() else { return }
            // Let the new tab's field mount before it takes the keyboard.
            try? await Task.sleep(for: .milliseconds(50))
            addressFocused = true
        }
    }

    /// At rest the field shows the host, then the rest of the address in
    /// muted text. Editing shows the whole address, selected.
    private var address: some View {
        ZStack(alignment: .leading) {
            TextField("", text: $draft,
                      prompt: tab.url == nil ? Text("Enter address").foregroundStyle(theme.strongBorder) : nil)
                .labelsHidden()
                .textFieldStyle(.plain)
                .font(.kit(size: 12))
                .foregroundStyle(theme.text)
                .focused($addressFocused)
                .onSubmit(submit)
                .onExitCommand(perform: cancel)
                .workspaceFocusTarget(.browser(tab.id), when: tab.url == nil || tab.failure != nil)
                .accessibilityLabel("Address")
                .accessibilityValue(tab.url?.absoluteString ?? "")
            if !addressFocused, let url = tab.url {
                let parts = BrowserAddress.display(url)
                (Text(parts.host).foregroundColor(theme.text) + Text(parts.rest).foregroundColor(theme.muted))
                    .font(.kit(size: 12))
                    .lineLimit(1).truncationMode(.tail)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 10)
        .frame(maxWidth: .infinity, minHeight: 26, maxHeight: 26)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(addressFocused ? theme.accent : theme.border))
        .contentShape(Rectangle())
        .onTapGesture { addressFocused = true }
        .onChange(of: addressFocused) { _, focused in
            if focused {
                if let url = tab.url { draft = url.absoluteString }
                // Select the address so typing replaces it.
                DispatchQueue.main.async { NSApp.sendAction(#selector(NSResponder.selectAll(_:)), to: nil, from: nil) }
            } else if tab.url != nil {
                draft = ""
            }
        }
        // Hiding the tab ends an edit, as leaving the field does.
        .onDisappear { if tab.url != nil { draft = "" } }
    }

    private func submit() {
        tab.navigate(draft)
        // A page that loads takes the keyboard; text that isn't an address stays to be fixed.
        guard tab.failure?.retry != nil || (tab.failure == nil && tab.url != nil) else { return }
        draft = ""
        focusPage()
    }

    private func cancel() {
        guard tab.url != nil else { draft = ""; return }
        draft = ""
        focusPage()
    }

    private func focusPage() {
        addressFocused = false
        let view = tab.view
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
    }
}

/// A page's attempt to download without a click, owned by its tab: a slim
/// warning strip under the toolbar until dismissed or the next page loads.
struct BrowserBlockedDownloadBar: View {
    @Environment(\.mica) private var theme
    /// The site's host label, or empty when unknown.
    let site: String
    let dismiss: () -> Void

    static let height: CGFloat = 30
    var message: String { (site.isEmpty ? "This page" : site) + " tried to download a file without a click." }

    var body: some View {
        HStack(spacing: 8) {
            IconView(.warning, size: 12).foregroundStyle(theme.warning)
            Text("Blocked a download").foregroundStyle(theme.text).fixedSize()
            Text(message).foregroundStyle(theme.muted).lineLimit(1).truncationMode(.middle).help(message)
            Spacer(minLength: 0)
            Button(action: dismiss) {
                IconView(.x, size: 10).foregroundStyle(theme.muted)
                    .frame(width: 18, height: 18).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Dismiss")
            .accessibilityLabel("Dismiss blocked download notice")
        }
        .font(.kit(size: 12))
        .padding(.leading, 14).padding(.trailing, 8)
        .frame(height: Self.height)
        .background(theme.raised)
        .overlay(alignment: .bottom) { Rule() }
        .accessibilityElement(children: .contain)
        .onAppear { AccessibilityAnnouncement.post("Blocked a download. " + message) }
    }
}

/// A 26 pt toolbar button. Disabled buttons dim.
private struct BrowserToolbarButton: View {
    @Environment(\.mica) private var theme
    @Environment(\.isEnabled) private var enabled
    let icon: Icon
    var size: CGFloat = 14
    let help: String
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            IconView(icon, size: size)
                .foregroundStyle(enabled ? theme.text : theme.muted)
                .frame(width: 26, height: 26)
                .background(hovering && enabled ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.4)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
    }
}

/// Kit's message for a page that can't load or an address that isn't one.
struct BrowserFailureView: View {
    @Environment(\.mica) private var theme
    let failure: BrowserFailure
    let retry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                IconView(.warningCircle, size: 16).foregroundStyle(theme.danger)
                Text(failure.title)
                    .font(.kit(size: 15, weight: .semibold))
                    .foregroundStyle(theme.text)
            }
            Text(failure.detail)
                .font(.kit(size: 13))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            if failure.retry != nil {
                Button(action: retry) {
                    Text("Try again")
                        .font(.kit(size: 12, weight: .medium))
                        .foregroundStyle(theme.text)
                        .padding(.horizontal, 12)
                        .frame(height: 28)
                        .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
                        .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.top, 6)
            }
        }
        .textSelection(.enabled)
        .padding(.horizontal, 64)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .background(theme.surface)
        .accessibilityElement(children: .contain)
    }
}
