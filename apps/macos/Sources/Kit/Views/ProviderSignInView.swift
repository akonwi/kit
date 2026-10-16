import AppKit
import SwiftUI

/// The window's first-run screen: Kit's wordmark over the provider list,
/// which each sign-in step replaces in the same column.
struct FirstRunView: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    /// Called after Continue on the connected step.
    let done: () -> Void

    static let width: CGFloat = 420

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 14) {
                Wordmark()
                if signIn.step == .list {
                    Text("Connect a provider").font(.kit(size: 18, weight: .semibold))
                        .accessibilityAddTraits(.isHeader)
                }
            }
            ProviderSignInFlow(signIn: signIn) {
                Task {
                    await signIn.finish()
                    if signIn.step == .list { done() }
                }
            }
        }
        .frame(width: Self.width, alignment: .leading)
        .padding(.bottom, 44)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private struct Wordmark: View {
        @Environment(\.mica) private var theme

        var body: some View {
            VStack(alignment: .leading, spacing: 6) {
                Text("kit").font(.kit(size: 16, weight: .semibold, design: .monospaced)).tracking(8)
                Rectangle().fill(theme.accent).frame(width: 40, height: 2)
            }
            .fixedSize()
            .accessibilityElement(children: .ignore).accessibilityLabel("Kit")
        }
    }
}

/// The provider list, or the step that replaced it.
struct ProviderSignInFlow: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    /// Continue on the connected step.
    let proceed: () -> Void

    var body: some View {
        switch signIn.step {
        case .list:
            if let error = signIn.loadError {
                VStack(alignment: .leading, spacing: 10) {
                    ProviderErrorLine(message: error)
                    Button("Retry") { Task { await signIn.load() } }.buttonStyle(FormButtonStyle())
                }
            } else if !signIn.loaded {
                KitSpinner("Loading providers…").foregroundStyle(theme.muted)
            } else {
                ProviderOptionList(options: signIn.options, select: signIn.choose)
            }
        case let .methods(provider):
            ProviderCard {
                BackLink(action: signIn.back)
                ProviderOptionList(options: signIn.options.filter { $0.provider == provider }, select: signIn.choose)
            }
        case let .signingIn(option):
            ProviderStepCard(signIn: signIn, option: option)
        case let .connected(option):
            ConnectedCard(signIn: signIn, option: option, proceed: proceed)
        }
    }
}

/// The 44 pt rows of sign-in methods, each naming its provider and method.
struct ProviderOptionList: View {
    @Environment(\.mica) private var theme
    let options: [ProviderOption]
    let select: (ProviderOption) -> Void
    @State private var hovered: String?

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                if index > 0 { Rule() }
                Button { select(option) } label: {
                    HStack(spacing: 12) {
                        Text(option.title).font(.kit(size: 13, weight: .medium))
                            .frame(maxWidth: .infinity, alignment: .leading)
                        HStack(spacing: 6) {
                            IconView(option.icon, size: 13)
                            Text(option.detail).font(.kit(size: 12))
                        }.foregroundStyle(theme.muted)
                        IconView(.caretRight, size: 10).foregroundStyle(theme.muted)
                    }
                    .padding(.horizontal, 14).frame(height: 44)
                    .background(hovered == option.id ? theme.hover : .clear)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .onHover { hovered = $0 ? option.id : (hovered == option.id ? nil : hovered) }
                .accessibilityLabel("\(option.title), \(option.detail)")
            }
        }
        .background(theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
    }
}

/// The card a sign-in step sits in.
struct ProviderCard<Content: View>: View {
    @Environment(\.mica) private var theme
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 16) { content }
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.surface, in: RoundedRectangle(cornerRadius: 4))
            .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
    }
}

private struct BackLink: View {
    @Environment(\.mica) private var theme
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                IconView(.caretLeft, size: 12)
                Text("All providers").font(.kit(size: 12))
            }
            .foregroundStyle(theme.muted).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .keyboardShortcut(.cancelAction)
    }
}

struct ProviderErrorLine: View {
    @Environment(\.mica) private var theme
    let message: String

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            IconView(.warningCircle, size: 14)
            Text(message).font(.kit(size: 12)).textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
        .foregroundStyle(theme.danger)
    }
}

/// A device, browser, or API key step.
private struct ProviderStepCard: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    let option: ProviderOption

    var body: some View {
        ProviderCard {
            BackLink(action: signIn.back)
            VStack(alignment: .leading, spacing: 4) {
                Text(option.title).font(.kit(size: 15, weight: .semibold)).accessibilityAddTraits(.isHeader)
                Text(option.detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            switch option.method {
            case .device: DeviceStep(signIn: signIn)
            case .browser: BrowserStep(signIn: signIn)
            case .apiKey: APIKeyStep(signIn: signIn, option: option)
            }
        }
    }
}

/// Starting, failed, or expired: what a device or browser step shows when
/// it has no instructions to follow.
private struct LoginOutcome: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn

    var body: some View {
        if let error = signIn.error {
            ProviderErrorLine(message: error)
            Button("Try again", action: signIn.retry).buttonStyle(FormButtonStyle())
        } else {
            KitSpinner("Starting sign-in…").font(.kit(size: 12)).foregroundStyle(theme.muted)
        }
    }
}

private struct DeviceStep: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    @State private var copied = false

    var body: some View {
        if let login = signIn.login, let device = login.device, login.state == .pending {
            Text("Enter this code on the sign-in page.").font(.kit(size: 13)).foregroundStyle(theme.muted)
            HStack {
                Text(device.userCode).font(.kit(size: 22, weight: .semibold, design: .monospaced)).tracking(4)
                    .textSelection(.enabled)
                Spacer(minLength: 8)
                Button(action: copy) {
                    HStack(spacing: 6) {
                        IconView(copied ? .check : .copy, size: 13)
                        Text(copied ? "Copied" : "Copy").font(.kit(size: 12))
                    }.foregroundStyle(theme.muted).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
            .padding(.horizontal, 14).padding(.vertical, 12)
            .fieldBox(theme)
            Button { signIn.open(device.verificationURL) } label: {
                HStack(spacing: 6) { IconView(.arrowSquareOut, size: 13); Text("Open") }
            }
            .buttonStyle(FormButtonStyle(fill: .primary))
            .keyboardShortcut(.defaultAction)
            Text(Self.address(device.verificationURL)).font(.kit(size: 11, design: .monospaced))
                .foregroundStyle(theme.muted).textSelection(.enabled)
            TimelineView(.periodic(from: .now, by: 1)) { context in
                KitSpinner(Self.waiting(until: device.expiresAt ?? login.expiresAt, now: context.date))
            }
            .font(.kit(size: 12)).foregroundStyle(theme.muted)
        } else {
            LoginOutcome(signIn: signIn)
        }
    }

    private func copy() {
        guard let code = signIn.login?.device?.userCode else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(code, forType: .string)
        copied = true
    }

    static func address(_ url: URL) -> String {
        (url.host ?? "") + url.path
    }

    static func waiting(until expiry: Date?, now: Date) -> String {
        guard let expiry else { return "Waiting for approval" }
        let seconds = max(0, Int(expiry.timeIntervalSince(now)))
        return "Waiting for approval · expires in \(seconds / 60):" + String(format: "%02d", seconds % 60)
    }
}

private struct BrowserStep: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    @State private var code = ""

    var body: some View {
        if let login = signIn.login, login.state == .pending {
            Text("Approve access in your browser.").font(.kit(size: 13)).foregroundStyle(theme.muted)
            Button(action: signIn.reopenBrowser) {
                HStack(spacing: 6) { IconView(.arrowSquareOut, size: 13); Text("Reopen") }
            }
            .buttonStyle(FormButtonStyle())
            KitSpinner("Waiting for browser approval…").font(.kit(size: 12)).foregroundStyle(theme.muted)
            Rule()
            VStack(alignment: .leading, spacing: 6) {
                Text("Or paste the code from the browser").font(.kit(size: 12)).foregroundStyle(theme.muted)
                HStack(spacing: 8) {
                    TextField("", text: $code, prompt: Text("Paste code…").foregroundStyle(theme.strongBorder))
                        .textFieldStyle(.plain).font(.kit(size: 12, design: .monospaced))
                        .onSubmit(submit)
                        .accessibilityLabel("Authorization code")
                    if signIn.pending { KitSpinner() }
                }
                .padding(.horizontal, 10).frame(height: 30)
                .fieldBox(theme)
                if let error = signIn.error { ProviderErrorLine(message: error) }
            }
        } else {
            LoginOutcome(signIn: signIn)
        }
    }

    private func submit() {
        let value = code
        Task { await signIn.submit(authorization: value) }
    }
}

private struct APIKeyStep: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    let signIn: ProviderSignIn
    let option: ProviderOption
    @State private var key = ""
    @State private var revealed = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("API key").font(.kit(size: 12)).foregroundStyle(theme.muted)
            HStack(spacing: 8) {
                Group {
                    if revealed { TextField("", text: $key) } else { SecureField("", text: $key) }
                }
                .textFieldStyle(.plain).font(.kit(size: 12, design: .monospaced))
                .focused($focused).onSubmit(save)
                .accessibilityLabel("\(option.providerName) API key")
                Button { revealed.toggle() } label: { IconView(revealed ? .eye : .eyeSlash, size: 13) }
                    .buttonStyle(.plain).foregroundStyle(theme.muted)
                    .help(revealed ? "Hide key" : "Show key")
                    .accessibilityLabel(revealed ? "Hide key" : "Show key")
            }
            .padding(.horizontal, 10).frame(height: 30)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay {
                RoundedRectangle(cornerRadius: 3)
                    .strokeBorder(signIn.error != nil ? theme.danger : focused ? theme.accent : theme.border)
            }
        }
        if let error = signIn.error { ProviderErrorLine(message: error).padding(.top, -8) }
        if let page = option.keyPage {
            HStack(spacing: 6) {
                Text("Create a key in the").foregroundStyle(theme.muted)
                Button { openURL(page.url) } label: {
                    HStack(spacing: 4) { Text(page.name); IconView(.arrowUpRight, size: 10) }
                        .foregroundStyle(theme.accent).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }.font(.kit(size: 12))
        }
        HStack(spacing: 8) {
            Spacer()
            Button("Cancel", action: signIn.back).buttonStyle(FormButtonStyle())
            Button(action: save) {
                HStack(spacing: 6) {
                    if signIn.pending { KitSpinner() }
                    Text(signIn.pending ? "Saving…" : "Save")
                }
            }
            .buttonStyle(FormButtonStyle(fill: .primary))
            .disabled(key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || signIn.pending)
        }
        .onAppear { focused = true }
    }

    private func save() {
        let value = key
        guard !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        Task { await signIn.save(apiKey: value) }
    }
}

/// The connected step: the default model for new sessions, then Continue or
/// another provider.
private struct ConnectedCard: View {
    @Environment(\.mica) private var theme
    let signIn: ProviderSignIn
    let option: ProviderOption
    let proceed: () -> Void
    @State private var popover = AnchoredPopover()
    @State private var open = false
    @State private var anchor = ComposerPickerAnchor()

    var body: some View {
        let selected = signIn.models.first { $0.id == signIn.defaultModel }
        ProviderCard {
            HStack(spacing: 10) {
                IconView(.checkCircle, size: 20).foregroundStyle(theme.success)
                Text("Connected to \(option.title)").font(.kit(size: 15, weight: .semibold))
            }
            .accessibilityElement(children: .combine)
            if !signIn.models.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Default model").font(.kit(size: 12)).foregroundStyle(theme.muted)
                    PickerField(open: open, anchor: anchor, action: toggle) {
                        Text(selected?.name ?? signIn.defaultModel).lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if let provider = selected?.provider {
                            Text(provider).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                    }
                    .accessibilityLabel("Default model: \(selected.map(ComposerView.modelLabel) ?? "none")")
                }
            }
            if let error = signIn.error { ProviderErrorLine(message: error) }
            HStack(spacing: 8) {
                Button("Add another", action: signIn.back).buttonStyle(FormButtonStyle())
                Spacer()
                Button("Continue", action: proceed)
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .keyboardShortcut(.defaultAction)
            }
        }
        .onDisappear { popover.dismiss() }
    }

    private func toggle() {
        if popover.isOpen { popover.dismiss(); return }
        if let closedAt = popover.closedAt, Date().timeIntervalSince(closedAt) < 0.3 { return }
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, theme = theme, signIn = signIn
        let content = ComposerModelPicker(models: signIn.models, selectedID: signIn.defaultModel,
            select: { id in popover.dismiss(); signIn.defaultModel = id },
            reload: {}, dismiss: { popover.dismiss() })
            .popoverCard(theme).accessibilityLabel("Default model")
        open = true
        popover.show(anchor: frame, parent: window, edge: .below, content: content) { open = false }
    }
}
