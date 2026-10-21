import SwiftUI

/// Which Settings section is showing, kept so other windows can open one.
enum SettingsSection: String {
    case appearance, workspace, servers, models, providers
    static let storageKey = "settingsSection"

    /// Opens Settings at this section.
    @MainActor func open(with openSettings: OpenSettingsAction) {
        UserDefaults.standard.set(rawValue, forKey: Self.storageKey)
        openSettings()
    }
}

/// Settings › Providers: each provider's sign-in, with Sign in and Sign out.
/// Sign in replaces the list with the same steps as first run.
struct ProviderSettingsView: View {
    @Environment(\.mica) private var theme
    @Environment(\.scenePhase) private var phase
    @State private var signIn = ProviderSignIn(client: ServerClient.local())

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Providers").font(.kit(size: 23, weight: .semibold))
                Text("The Kit server on this Mac keeps these sign-ins for every session.").foregroundStyle(theme.muted)
            }
            if signIn.step == .list && signIn.loaded && signIn.loadError == nil {
                list
                if let error = signIn.error { ProviderErrorLine(message: error) }
            } else {
                ProviderSignInFlow(signIn: signIn) { Task { await signIn.finish() } }
                    .frame(maxWidth: FirstRunView.width, alignment: .leading)
            }
        }
        .task { await signIn.load() }
        .onChange(of: phase) { if phase == .active, signIn.step == .list { Task { await signIn.load() } } }
    }

    private var list: some View {
        VStack(spacing: 0) {
            ForEach(Array(signIn.providers.enumerated()), id: \.element.id) { index, provider in
                if index > 0 { Rule() }
                row(provider)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
    }

    private func row(_ provider: ProviderInfo) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(provider.name)
                Text(Self.detail(provider)).font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if provider.configured {
                HStack(spacing: 6) {
                    Circle().fill(theme.success).frame(width: 6, height: 6)
                    Text("Connected")
                }.font(.kit(size: 12)).foregroundStyle(theme.muted)
            } else {
                Text("Not connected").font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            Group {
                if !provider.configured {
                    Button("Sign in") { signIn.signIn(provider) }
                        .buttonStyle(FormButtonStyle(fill: .primary))
                } else if !provider.fromEnvironment {
                    Button("Sign out") { Task { await signIn.signOut(provider) } }
                        .buttonStyle(FormButtonStyle())
                } else {
                    Color.clear.frame(height: 28)
                }
            }
            .frame(width: 72)
            .disabled(signIn.pending)
        }
        .padding(.horizontal, 14).frame(height: 52)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(provider.name)
        .accessibilityValue(provider.configured ? "Connected" : "Not connected")
    }

    /// How the provider signs in, or where its credential comes from.
    static func detail(_ provider: ProviderInfo) -> String {
        if provider.configured && provider.fromEnvironment { return "API key · from the server’s environment" }
        return provider.methods.map { method in
            let option = ProviderOption(provider: provider.id, providerName: provider.name, method: method)
            return option.title == provider.name ? option.detail : "\(option.title) \(option.detail)"
        }.joined(separator: ", or ")
    }
}
