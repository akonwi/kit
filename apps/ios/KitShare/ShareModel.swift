import Foundation
import KitClient
import Observation

/// What the share sheet knows: what was shared, the sessions it can go to,
/// and the servers that didn't list theirs.
@MainActor @Observable
final class ShareModel {
    /// A shared web link.
    struct Link: Equatable {
        let url: URL
        let title: String?
    }

    enum Shared: Equatable {
        case reading
        case link(Link)
        /// Nothing the sheet can add.
        case nothing
    }

    private(set) var shared = Shared.reading
    private(set) var destinations: [ShareDestination] = []
    /// Servers that didn't list their sessions, with why.
    private(set) var problems: [(server: String, reason: String)] = []
    /// Whether some server hasn't answered yet.
    private(set) var loading = true
    /// Whether any server is paired.
    private(set) var paired = true
    /// Why the share couldn't be added, if it couldn't.
    var failure: String?

    private let finish: (Bool) -> Void
    private let inbox: ShareInbox

    init(inbox: ShareInbox = .live, finish: @escaping (Bool) -> Void) {
        self.inbox = inbox
        self.finish = finish
    }

    func received(link url: URL?, title: String?) {
        guard let url else { shared = .nothing; return }
        let title = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        shared = .link(Link(url: url, title: title?.isEmpty == false && title != url.absoluteString ? title : nil))
    }

    /// Lists every paired server's sessions, each as it answers.
    func load() {
        let servers = (try? PairedServers.saved()) ?? []
        paired = !servers.isEmpty
        guard paired else { loading = false; return }
        Task {
            await withTaskGroup(of: (String, Result<AttentionListing, any Error>).self) { group in
                for server in servers {
                    group.addTask {
                        let client = PairedServers.client(for: server)
                        do { return (server.name, .success(try await client.sessionsByAttention())) }
                        catch { return (server.name, .failure(error)) }
                    }
                }
                for await (server, result) in group {
                    switch result {
                    case .success(let listing):
                        destinations = ShareDestination.ordered(destinations + ShareDestination.listed(listing, on: server))
                    case .failure(let error):
                        problems.append((server, Self.reason(error)))
                    }
                }
            }
            loading = false
        }
    }

    /// The sessions a search finds.
    func destinations(matching search: String) -> [ShareDestination] {
        destinations.filter { $0.matches(search) }
    }

    /// Adds what was shared, after the note, to the session's draft in Kit,
    /// and closes the sheet.
    func addToDraft(_ destination: ShareDestination, note: String) {
        guard case .link(let link) = shared else { return }
        let item = SharedItem(server: destination.server, session: destination.session,
                              text: SharedItem.text(note: note, shared: [link.url.absoluteString]))
        do {
            try inbox.put(item)
            finish(true)
        } catch {
            failure = error.localizedDescription
        }
    }

    func cancel() {
        finish(false)
    }

    private static func reason(_ error: any Error) -> String {
        if let error = error as? ClientError {
            switch error {
            case .needsPairing, .http(401): return "Pair it again in Kit."
            case .incompatibleDaemon: return "Update Kit or the server."
            default: return error.localizedDescription
            }
        }
        if error is URLError { return "It didn’t answer." }
        return error.localizedDescription
    }
}
