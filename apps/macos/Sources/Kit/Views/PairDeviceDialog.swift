import AppKit
import CoreImage.CIFilterBuiltins
import SwiftUI

/// What Pair a Device offers for a pairing code: the addresses another
/// device might reach the server at, or none when only this Mac can.
struct PairingAddresses: Equatable {
    struct Address: Equatable, Identifiable {
        let url: String
        /// The code and token cross the network unencrypted.
        var insecure: Bool { url.hasPrefix("http://") }
        var id: String { url }
        /// The address without its scheme, as the picker shows it.
        var label: String { url.replacingOccurrences(of: "https://", with: "").replacingOccurrences(of: "http://", with: "") }
    }

    let addresses: [Address]

    /// The served server's own URL comes first, since this Mac reaches it
    /// there; then the URLs the server reports. Loopback URLs reach only the
    /// server's machine, so they're left out.
    init(connector: ServerConnector, reported: [String]) {
        var urls: [String] = []
        if case .url(let origin, _) = connector { urls.append(origin) }
        for url in reported where !Self.loopback(url) && !urls.contains(url) { urls.append(url) }
        addresses = urls.map(Address.init)
    }

    static func loopback(_ url: String) -> Bool {
        guard let host = URLComponents(string: url)?.host?.lowercased() else { return true }
        return host == "localhost" || host == "::1" || host == "[::1]" || host.hasPrefix("127.")
    }
}

/// Pair a Device (S11, S12): a single-use link, and its QR code, that pairs
/// another device with a server. The code lives only in the server's memory.
struct PairDeviceDialog: View {
    @Environment(\.mica) private var theme
    let connection: ServerConnection
    let close: () -> Void
    @State private var code: PairingCode?
    @State private var error: String?
    @State private var selected: String?

    var body: some View {
        let name = connection.name
        VStack(alignment: .leading, spacing: 18) {
            if !connection.status.isConnected {
                DialogHeading(title: "Pair a Device with \(name)",
                              message: "\(name) isn’t connected, so it can’t make a link now. Try again when it is, or run kit server pair on \(name).")
                footer(note: nil)
            } else if let code {
                let addresses = PairingAddresses(connector: connection.connector, reported: code.urls)
                if addresses.addresses.isEmpty {
                    onlyThisMac(name: name)
                } else {
                    link(name: name, code: code, addresses: addresses.addresses)
                }
            } else if let error {
                DialogHeading(title: "Pair a Device with \(name)", message: "Scan the code with the other device, or send it the link.")
                ProviderErrorLine(message: error)
                footer(note: nil)
            } else {
                DialogHeading(title: "Pair a Device with \(name)", message: "Scan the code with the other device, or send it the link.")
                KitSpinner("Making a link…").foregroundStyle(theme.muted)
                footer(note: nil)
            }
        }
        // A restarted server may answer on new addresses, as after
        // kit server expose; its earlier codes are gone with it.
        .task(id: connection.status.identity?.instance) { await create() }
    }

    private func create() async {
        guard connection.status.isConnected else { return }
        do {
            let code = try await connection.client.createPairing()
            self.code = code
            error = nil
            let urls = PairingAddresses(connector: connection.connector, reported: code.urls).addresses.map(\.url)
            if selected.map({ !urls.contains($0) }) ?? true { selected = urls.first }
        } catch {
            self.error = error.localizedDescription
        }
    }

    @ViewBuilder private func link(name: String, code: PairingCode, addresses: [PairingAddresses.Address]) -> some View {
        let address = addresses.first { $0.url == selected } ?? addresses[0]
        let link = PairingLink.link(server: address.url, code: code.code)
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let expired = context.date >= code.expiresAt
            VStack(alignment: .leading, spacing: 18) {
                DialogHeading(title: "Pair a Device with \(name)", message: "Scan the code with the other device, or send it the link.")
                HStack(alignment: .center, spacing: 18) {
                    QRCodeView(text: link, dimmed: expired)
                    VStack(alignment: .leading, spacing: 8) {
                        if expired {
                            Text("This link expired").font(.kit(size: 13, weight: .medium))
                            Text("Make a new one when the other device is ready.").font(.kit(size: 12)).foregroundStyle(theme.muted)
                                .fixedSize(horizontal: false, vertical: true)
                        } else if addresses.count > 1 {
                            Text("Address the device can reach").font(.kit(size: 12)).foregroundStyle(theme.muted)
                            ForEach(addresses) { candidate in
                                AddressOption(address: candidate, selected: candidate.url == address.url) { selected = candidate.url }
                            }
                        } else {
                            Text("On the other device").font(.kit(size: 13, weight: .medium))
                            Text("Paste the link in Add Server, or run kit attach with it.").font(.kit(size: 12)).foregroundStyle(theme.muted)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        ExpiryLine(expires: code.expiresAt, now: context.date)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                HStack(spacing: 8) {
                    Text(link).font(.kit(size: 12, design: .monospaced)).lineLimit(1).truncationMode(.tail)
                        .foregroundStyle(expired ? theme.muted : theme.text)
                        .textSelection(.enabled)
                        .padding(.horizontal, 10).frame(maxWidth: .infinity, minHeight: 30, alignment: .leading)
                        .fieldBox(theme)
                        .accessibilityLabel("Pairing link")
                    Button("Copy Link") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(link, forType: .string)
                    }
                    .buttonStyle(FormButtonStyle())
                    .disabled(expired)
                }
                if address.insecure && !expired {
                    Text("This address is plain HTTP: the code and the device’s token cross the network unencrypted unless it’s private, like a tailnet.")
                        .font(.kit(size: 12)).foregroundStyle(theme.warning).fixedSize(horizontal: false, vertical: true)
                }
                if expired {
                    Rule()
                    HStack(spacing: 8) {
                        Spacer()
                        Button("Done", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction)
                        Button("New Link") { Task { await create() } }
                            .buttonStyle(FormButtonStyle(fill: .primary)).keyboardShortcut(.defaultAction)
                    }
                } else {
                    footer(note: "Anyone with this link can pair with \(name). Share it only with your own devices.")
                }
            }
        }
    }

    @ViewBuilder private func onlyThisMac(name: String) -> some View {
        let command = Self.exposeCommand(connector: connection.connector)
        DialogHeading(title: "Pair a Device with \(name)",
                      message: "Only this Mac can reach \(name), so a link wouldn’t work anywhere else. Serve it on your tailnet first:")
        HStack(spacing: 8) {
            Text(command).font(.kit(size: 12, design: .monospaced)).lineLimit(1).truncationMode(.middle)
                .textSelection(.enabled)
                .padding(.horizontal, 10).frame(maxWidth: .infinity, minHeight: 30, alignment: .leading)
                .fieldBox(theme)
                .help(command)
            Button("Copy") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(command, forType: .string)
            }.buttonStyle(FormButtonStyle())
        }
        Text("This sheet shows a link as soon as \(name) answers on another address.")
            .font(.kit(size: 12)).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
        footer(note: nil)
    }

    private func footer(note: String?) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            Rule()
            HStack(alignment: .center, spacing: 12) {
                if let note {
                    Text(note).font(.kit(size: 12)).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
                Button("Done", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.defaultAction)
            }
        }
    }

    /// The command that serves a home's server on the tailnet. A home other
    /// than the default is named, so the command serves that server.
    static func exposeCommand(connector: ServerConnector) -> String {
        let restart = ServerStatePresentation.restartCommand(connector: connector)
        return restart.replacingOccurrences(of: "kit server restart", with: "kit server expose --tailscale")
    }
}

private struct AddressOption: View {
    @Environment(\.mica) private var theme
    let address: PairingAddresses.Address
    let selected: Bool
    let choose: () -> Void

    var body: some View {
        Button(action: choose) {
            HStack(spacing: 8) {
                ZStack {
                    Circle().strokeBorder(selected ? theme.accent : theme.strongBorder).frame(width: 12, height: 12)
                    if selected { Circle().fill(theme.accent).frame(width: 6, height: 6) }
                }
                Text(address.label).font(.kit(size: 12, design: .monospaced)).lineLimit(1).truncationMode(.middle)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if address.insecure { Text("HTTP").font(.kit(size: 11)).foregroundStyle(theme.warning) }
            }
            .padding(.horizontal, 10).frame(height: 30)
            .fieldBox(theme, focused: selected)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(address.label)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// "Works once · expires in 4:52", or when the code expired.
private struct ExpiryLine: View {
    @Environment(\.mica) private var theme
    let expires: Date
    let now: Date

    var body: some View {
        HStack(spacing: 6) {
            Circle().fill(now < expires ? theme.success : theme.muted).frame(width: 6, height: 6)
            Text(Self.label(expires: expires, now: now))
        }
        .font(.kit(size: 12)).foregroundStyle(theme.muted)
    }

    static func label(expires: Date, now: Date) -> String {
        guard now < expires else { return "Expired at " + expires.formatted(date: .omitted, time: .shortened) }
        let seconds = Int(expires.timeIntervalSince(now).rounded(.up))
        return String(format: "Works once · expires in %d:%02d", seconds / 60, seconds % 60)
    }
}

/// A QR code of `text` on white, which cameras read in either appearance.
struct QRCodeView: View {
    let text: String
    var dimmed = false

    var body: some View {
        Group {
            if let image = Self.image(text) {
                Image(nsImage: image).interpolation(.none).resizable().scaledToFit()
            } else {
                Color.clear
            }
        }
        .padding(10)
        .frame(width: 144, height: 144)
        .background(Color.white, in: RoundedRectangle(cornerRadius: 3))
        .opacity(dimmed ? 0.25 : 1)
        .accessibilityLabel("QR code of the pairing link")
    }

    static func image(_ text: String) -> NSImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let output = filter.outputImage,
              let cgImage = CIContext().createCGImage(output, from: output.extent) else { return nil }
        return NSImage(cgImage: cgImage, size: NSSize(width: output.extent.width, height: output.extent.height))
    }
}
