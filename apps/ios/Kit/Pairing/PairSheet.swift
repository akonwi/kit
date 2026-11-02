import KitClient
import SwiftUI
import UIKit

/// Pairs with a server from a link: asks for one when pasting, then names
/// the server and this phone, redeems the code, and says what happened.
struct PairSheet: View {
    @Environment(ServerStore.self) private var store
    @Environment(PairingCoordinator.self) private var pairing
    @Environment(\.dismiss) private var dismiss

    let request: PairingCoordinator.Request

    @State private var link: PairingLink?
    @State private var linkText = ""
    @State private var name = ""
    @State private var deviceName = DeviceName.clean(UIDevice.current.name)
    @State private var phase = Phase.editing
    @State private var problem: String?

    enum Phase: Equatable {
        case editing, pairing
        case refused, unreachable, rateLimited
        case serverNeedsUpdate(kitVersion: String, serverProtocol: Int)
        case appNeedsUpdate(kitVersion: String, serverProtocol: Int)
    }

    init(request: PairingCoordinator.Request) {
        self.request = request
        _link = State(initialValue: request.link)
    }

    var body: some View {
        NavigationStack {
            Group {
                if let link {
                    switch phase {
                    case .editing, .pairing: confirmation(link)
                    default: outcome(link)
                    }
                } else {
                    linkEntry
                }
            }
            .navigationTitle("Pair server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("Close")
                }
                if link != nil, phase == .editing || phase == .pairing {
                    ToolbarItem(placement: .confirmationAction) {
                        if phase == .pairing {
                            ProgressView()
                        } else {
                            Button("Pair") { Task { await pair() } }
                                .buttonStyle(.borderedProminent)
                                .disabled(!canPair)
                        }
                    }
                }
            }
        }
        .interactiveDismissDisabled(phase == .pairing)
        .onAppear { if let link, name.isEmpty { name = request.target ?? store.suggestedName(for: link) } }
    }

    // MARK: Link entry

    private var linkEntry: some View {
        Form {
            Section {
                TextField("Pairing link", text: $linkText,
                          prompt: Text(verbatim: "https://host/pair#code").foregroundStyle(Theme.muted), axis: .vertical)
                    .font(.system(size: 15, design: .monospaced))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    .submitLabel(.done)
                    .accessibilityIdentifier("pairing-link")
                    .listRowBackground(Theme.raised)
            } footer: {
                Text(linkEntryNote)
                    .foregroundStyle(problem == nil ? Theme.muted : Theme.danger)
            }
            Section {
                PasteButton(payloadType: String.self) { values in
                    if let value = values.first { linkText = value }
                }
                .frame(maxWidth: .infinity)
                .listRowBackground(Color.clear)
            }
        }
        .scrollContentBackground(.hidden)
        .background(Theme.surface)
        .onChange(of: linkText) { _, text in accept(text) }
    }

    private var linkEntryNote: String {
        problem ?? "Paste the link kit server pair prints. It looks like https://host/pair#code."
    }

    private func accept(_ text: String) {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { problem = nil; return }
        guard let parsed = PairingLink(text) else {
            problem = PairingLink.looksLikeLink(text) ? PairingError.invalidLink.localizedDescription : nil
            return
        }
        if let target = request.target, let saved = store.entry(target), saved.url != parsed.server {
            problem = PairingError.otherServer(expected: saved.url, link: parsed.server).localizedDescription
            return
        }
        problem = nil
        name = request.target ?? store.suggestedName(for: parsed)
        link = parsed
    }

    // MARK: Confirmation

    private func confirmation(_ link: PairingLink) -> some View {
        Form {
            Section {
                ServerBadge(address: link.server, tint: Theme.text)
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
            }
            if ServerAddress.isPlainHTTP(link.server) {
                Section {
                    PlainHTTPWarning()
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                }
            }
            Section {
                LabeledContent("Server name") {
                    if let target = request.target {
                        Text(target)
                            .foregroundStyle(Theme.muted)
                    } else {
                        TextField("name", text: $name)
                            .multilineTextAlignment(.trailing)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .foregroundStyle(Theme.muted)
                    }
                }
                LabeledContent("This iPhone as") {
                    TextField("iPhone", text: $deviceName)
                        .multilineTextAlignment(.trailing)
                        .foregroundStyle(Theme.muted)
                }
            } footer: {
                Text(problem ?? "The server lists this phone under that name in kit server devices, where you can revoke it. Its token stays in this iPhone’s Keychain.")
                    .foregroundStyle(problem == nil ? Theme.muted : Theme.danger)
            }
            .listRowBackground(Theme.raised)
            .foregroundStyle(Theme.text)
        }
        .scrollContentBackground(.hidden)
        .background(Theme.surface)
        .disabled(phase == .pairing)
    }

    private var canPair: Bool {
        let device = DeviceName.clean(deviceName)
        guard !device.isEmpty else { return false }
        if request.target != nil { return true }
        return SavedServer.validName(name) && store.entry(name) == nil
    }

    private func pair() async {
        guard let link else { return }
        phase = .pairing
        problem = nil
        let device = DeviceName.clean(deviceName)
        let saved = request.target ?? name
        do {
            if let target = request.target {
                try await store.pairAgain(target, link: link, deviceName: device)
            } else {
                try await store.pair(link, as: name, deviceName: device)
            }
        } catch PairingError.codeRefused {
            phase = .refused
            return
        } catch PairingError.rateLimited {
            phase = .rateLimited
            return
        } catch {
            if case .unreachable = ServerState.from(error, at: .now) {
                phase = .unreachable
            } else {
                problem = error.localizedDescription
                phase = .editing
            }
            return
        }
        switch store.entry(saved)?.state {
        case .serverNeedsUpdate(let version, let serverProtocol):
            phase = .serverNeedsUpdate(kitVersion: version, serverProtocol: serverProtocol)
        case .appNeedsUpdate(let version, let serverProtocol):
            phase = .appNeedsUpdate(kitVersion: version, serverProtocol: serverProtocol)
        default:
            dismiss()
        }
    }

    // MARK: Outcomes

    private func outcome(_ link: PairingLink) -> some View {
        let saved = request.target ?? name
        let host = ServerAddress.display(link.server)
        return ScrollView {
            VStack(spacing: 24) {
                ServerBadge(address: link.server, tint: outcomeTint)
                VStack(spacing: 8) {
                    switch phase {
                    case .refused:
                        OutcomeText(title: "This link was already used or has expired",
                                    detail: "Pairing codes work once and expire after 5 minutes. Run kit server pair again for a new one.")
                        retryNewLink
                    case .rateLimited:
                        OutcomeText(title: "Too many pairing attempts",
                                    detail: "The server is refusing pairing for a moment. Wait a minute, then try again.")
                        Button("Try again") { Task { await pair() } }
                            .buttonStyle(FilledCapsuleButtonStyle(fullWidth: false))
                            .padding(.top, 12)
                    case .unreachable:
                        OutcomeText(title: "Can’t reach \(host)",
                                    detail: "\(ServerAddress.reachabilityHint(link.server)) The code lasts 5 minutes from when kit server pair showed it.")
                        Button("Try again") { Task { await pair() } }
                            .buttonStyle(FilledCapsuleButtonStyle(fullWidth: false))
                            .padding(.top, 12)
                    case .serverNeedsUpdate(let version, let serverProtocol):
                        OutcomeText(title: "Paired. \(saved) needs an update.",
                                    detail: "\(saved) runs Kit \(version) (protocol \(serverProtocol)). This app speaks protocol \(kitWireVersion). Upgrade Kit on the server; \(saved) connects as soon as it matches.")
                        done
                    case .appNeedsUpdate(let version, let serverProtocol):
                        OutcomeText(title: "Paired. This app needs an update.",
                                    detail: "\(saved) runs Kit \(version) (protocol \(serverProtocol)). This app speaks protocol \(kitWireVersion). Update Kit on this iPhone; \(saved) connects as soon as it matches.")
                        done
                    case .editing, .pairing:
                        EmptyView()
                    }
                }
                .padding(.horizontal, 24)
                .padding(.top, 12)
            }
            .padding(.top, 28)
            .padding(.horizontal, 16)
        }
        .background(Theme.surface)
    }

    private var outcomeTint: Color {
        switch phase {
        case .refused: Theme.danger
        case .serverNeedsUpdate, .appNeedsUpdate: Theme.warning
        default: Theme.text
        }
    }

    @ViewBuilder
    private var retryNewLink: some View {
        Group {
            if request.origin == .scan {
                Button("Scan again") { pairing.scanAgain() }
            } else {
                Button("Paste a new link") {
                    link = nil
                    linkText = ""
                    phase = .editing
                }
            }
        }
        .buttonStyle(FilledCapsuleButtonStyle(fullWidth: false))
        .padding(.top, 12)
    }

    private var done: some View {
        Button("Done") { dismiss() }
            .buttonStyle(FilledCapsuleButtonStyle(fullWidth: false))
            .padding(.top, 12)
    }
}

/// A server's icon above its address, with a lock that says whether the
/// connection is encrypted.
struct ServerBadge: View {
    let address: String
    let tint: Color

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "server.rack")
                .font(.system(size: 24))
                .foregroundStyle(tint)
                .frame(width: 56, height: 56)
                .background(Theme.raised, in: RoundedRectangle(cornerRadius: 14))
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.border, lineWidth: 0.5))
            HStack(spacing: 6) {
                let plain = ServerAddress.isPlainHTTP(address)
                Image(systemName: plain ? "lock.open.fill" : "lock.fill")
                    .font(.system(size: 12))
                    .foregroundStyle(plain ? Theme.warning : Theme.success)
                    .accessibilityLabel(plain ? "Not encrypted" : "Encrypted")
                Text(ServerAddress.display(address))
                    .font(.system(size: 14, design: .monospaced))
                    .foregroundStyle(Theme.text)
            }
        }
        .padding(.horizontal, 8)
    }
}

private struct PlainHTTPWarning: View {
    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 16))
                .foregroundStyle(Theme.warning)
            VStack(alignment: .leading, spacing: 2) {
                Text("Not encrypted")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.warning)
                Text("This link uses plain HTTP, so the pairing code and this iPhone’s token travel unencrypted. Use it only on a private network such as a tailnet.")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.text)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(Theme.warning.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.warning.opacity(0.3), lineWidth: 0.5))
    }
}

private struct OutcomeText: View {
    let title: String
    let detail: String

    var body: some View {
        Text(title)
            .font(.system(size: 20, weight: .semibold))
            .foregroundStyle(Theme.text)
            .multilineTextAlignment(.center)
        Text(detail)
            .font(.system(size: 15))
            .foregroundStyle(Theme.muted)
            .multilineTextAlignment(.center)
            .frame(maxWidth: 300)
    }
}
