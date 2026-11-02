import KitClient
import SwiftUI

@main
struct KitApp: App {
    @State private var store = ServerStore.live()
    @State private var sessions = SessionsStore.live()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(store)
                .environment(sessions)
                .tint(Theme.accent)
        }
    }
}

/// First run until a server is paired, then the tabs. Pairing is presented
/// from here so it outlives the switch between them.
struct RootView: View {
    @Environment(ServerStore.self) private var store
    @Environment(\.scenePhase) private var scenePhase
    @State private var pairing = PairingCoordinator()

    var body: some View {
        @Bindable var pairing = pairing
        Group {
            if store.entries.isEmpty {
                FirstRunView()
            } else {
                MainTabs()
            }
        }
        .environment(pairing)
        .fullScreenCover(isPresented: $pairing.scanning, onDismiss: pairing.scannerDismissed) {
            ScannerView()
                .environment(pairing)
        }
        .sheet(item: $pairing.sheet, onDismiss: pairing.sheetDismissed) { request in
            PairSheet(request: request)
                .environment(store)
                .environment(pairing)
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            guard phase == .active else { return }
            Task { await store.checkAll() }
        }
        #if DEBUG
        .task { openLaunchPairingLink() }
        #endif
    }

    #if DEBUG
    /// Opens the pair sheet with `KIT_PAIRING_LINK` from the launch
    /// environment, so a simulator can pair without a camera or clipboard:
    /// `SIMCTL_CHILD_KIT_PAIRING_LINK=… xcrun simctl launch …`.
    private func openLaunchPairingLink() {
        guard let value = ProcessInfo.processInfo.environment["KIT_PAIRING_LINK"], let link = PairingLink(value) else { return }
        pairing.sheet = PairingCoordinator.Request(link: link, origin: .paste, target: nil)
    }
    #endif
}

/// The app's sections once a server is paired.
struct MainTabs: View {
    enum Section: Hashable { case sessions, servers, settings }

    @State private var selection = Section.sessions

    var body: some View {
        TabView(selection: $selection) {
            Tab("Sessions", systemImage: "apple.terminal", value: .sessions) {
                SessionsView()
            }
            Tab("Servers", systemImage: "server.rack", value: .servers) {
                ServersView()
            }
            Tab("Settings", systemImage: "gearshape", value: .settings) {
                Placeholder(title: "Settings", symbol: "gearshape")
            }
        }
    }
}

/// A section that isn't built yet.
private struct Placeholder: View {
    let title: String
    let symbol: String

    var body: some View {
        NavigationStack {
            ContentUnavailableView(title, systemImage: symbol, description: Text("Not built yet."))
                .navigationTitle(title)
        }
    }
}
