import Foundation
import KitClient
import Observation

/// What starting a session asks of one server.
protocol NewSessionServer: Sendable {
    func models() async throws -> [WireModelCapability]
    func serverSettings() async throws -> ServerSettings
    /// The server user's home folder, where a typed `~` points.
    func home() async throws -> String
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt
    func snapshot(_ id: String) async throws -> SessionExcerpt
}

extension ServerClient: NewSessionServer {
    func home() async throws -> String { try await verify().identity.home }
}

/// The new session form: which server, which folder on it, the model and
/// thinking level, and an optional name. The folder is one field: one of the
/// server's recent folders, or any path typed relative to its home.
@MainActor @Observable
final class NewSessionForm {
    /// A paired server and the folders its sessions ran in, newest first.
    struct Place: Equatable {
        let server: String
        let recent: [String]
    }

    enum Folder: Equatable {
        /// An absolute folder from the server's sessions.
        case recent(String)
        /// A path as typed: absolute, `~`, or relative to the home folder.
        case typed(String)
    }

    let places: [Place]
    private(set) var server: String
    var folder: Folder? {
        didSet { if folder != oldValue { folderError = nil } }
    }
    var name = ""
    private(set) var models: [WireModelCapability] = []
    var model = "" {
        didSet { if !levels.contains(thinking) { thinking = SessionCreation.defaultThinking(levels) } }
    }
    var thinking = ""
    /// The server user's home folder, once the server says.
    private(set) var home: String?
    private(set) var loading = false
    private(set) var pending = false
    /// The server might have created the session even though the request
    /// failed, so creating another could duplicate it.
    private(set) var uncertain = false
    private(set) var error: String?
    /// The server has no folder at the chosen path.
    private(set) var folderError: String?

    /// Chosen before the request, so a create whose answer was lost can be found.
    private let creationID = SessionCreation.newID()
    private let connect: @MainActor (String) -> any NewSessionServer
    private var loads = 0

    /// The most recent folders a server offers.
    static let recentLimit = 5

    /// `places` in the order servers are listed; the form starts on
    /// `server`, or the first place.
    init(places: [Place], server: String?, connect: @escaping @MainActor (String) -> any NewSessionServer) {
        self.places = places
        self.server = server.flatMap { name in places.first { $0.server == name }?.server } ?? places.first?.server ?? ""
        self.connect = connect
        folder = places.first { $0.server == self.server }?.recent.first.map(Folder.recent)
    }

    /// Each paired server with its recent folders, starting on the server of
    /// the latest session.
    static func places(servers: [String], rows: [SessionsStore.Row]) -> [Place] {
        servers.map { server in
            var seen = Set<String>()
            let folders = rows.filter { $0.server == server }
                .compactMap { $0.session.cwd ?? ($0.session.workspace.isEmpty ? nil : $0.session.workspace) }
                .filter { $0.hasPrefix("/") && seen.insert($0).inserted }
            return Place(server: server, recent: Array(folders.prefix(recentLimit)))
        }
    }

    var recent: [String] { places.first { $0.server == server }?.recent ?? [] }
    var levels: [String] { ComposerConfiguration.thinkingLevels(model: model, models: models) }
    var selectedModel: WireModelCapability? { models.first { $0.id == model } }

    /// The models grouped by provider, in the order the server lists them.
    var modelGroups: [(provider: String, models: [WireModelCapability])] {
        var groups: [(provider: String, models: [WireModelCapability])] = []
        for model in models where model.available {
            if let index = groups.firstIndex(where: { $0.provider == model.provider }) {
                groups[index].models.append(model)
            } else {
                groups.append((model.provider, [model]))
            }
        }
        return groups
    }

    /// The absolute folder the session starts in, when it's known.
    var path: String? {
        switch folder {
        case .recent(let path): return path
        case .typed(let typed):
            let trimmed = typed.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.hasPrefix("/") { return SessionCreation.folder(trimmed, home: "") }
            return home.flatMap { SessionCreation.folder(trimmed, home: $0) }
        case nil: return nil
        }
    }

    var canCreate: Bool {
        path != nil && !model.isEmpty && (levels.isEmpty || !thinking.isEmpty) && !loading && !pending && !uncertain
    }

    /// The folder field's text: a recent folder as the form shows it, or the
    /// path as typed. Text that names a recent folder chooses it.
    var folderText: String {
        get {
            switch folder {
            case .recent(let path): return display(path)
            case .typed(let typed): return typed
            case nil: return ""
            }
        }
        set {
            guard newValue != folderText else { return }
            if let path = recent.first(where: { display($0) == newValue || $0 == newValue }) {
                folder = .recent(path)
            } else {
                folder = newValue.isEmpty ? nil : .typed(newValue)
            }
        }
    }

    /// The recent folders the field offers: all of them while it shows one of
    /// them, else those containing what was typed.
    var suggestions: [String] {
        guard case .typed(let typed) = folder else { return recent }
        let query = typed.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else { return recent }
        return recent.filter { display($0).localizedCaseInsensitiveContains(query) || $0.localizedCaseInsensitiveContains(query) }
    }

    /// A suggestion split into the part already typed and the rest, when the
    /// typed text starts it.
    func completion(_ path: String) -> (typed: String, rest: String)? {
        guard case .typed(let typed) = folder, !typed.isEmpty else { return nil }
        let shown = display(path)
        guard shown.count > typed.count, shown.lowercased().hasPrefix(typed.lowercased()) else { return nil }
        return (String(shown.prefix(typed.count)), String(shown.dropFirst(typed.count)))
    }

    /// A folder as the form shows it, with the server's home written `~`.
    func display(_ path: String) -> String {
        guard let home, !home.isEmpty, home != "/" else { return SessionFormat.directory(path) }
        let base = home.hasSuffix("/") ? String(home.dropLast()) : home
        if path == base { return "~" }
        if path.hasPrefix(base + "/") { return "~" + path.dropFirst(base.count) }
        return path
    }

    /// Moves the form to another server, on its latest folder.
    func select(server name: String) async {
        guard name != server, !pending, !uncertain, places.contains(where: { $0.server == name }) else { return }
        server = name
        folder = recent.first.map(Folder.recent)
        await load()
    }

    /// Reads the server's models, its default model, and its home folder.
    func load() async {
        loads += 1
        let load = loads, server = connect(self.server)
        loading = true; error = nil; home = nil; models = []; model = ""
        defer { if load == loads { loading = false } }
        async let home = server.home()
        do {
            let models = try await server.models()
            guard load == loads else { return }
            self.models = models
            let available = models.filter(\.available).map(\.id)
            let settings: ServerSettings
            do { settings = try await server.serverSettings() }
            catch {
                guard load == loads else { return }
                settings = ServerSettings()
                self.error = "Couldn’t load the default model, so choose one. " + error.localizedDescription
            }
            guard load == loads else { return }
            model = settings.preferredModel(available: available, suggested: nil)
            thinking = SessionCreation.defaultThinking(levels)
        } catch {
            guard load == loads else { return }
            self.error = error.localizedDescription
        }
        let found = try? await home
        if load == loads { self.home = found }
    }

    /// Asks the server to create the session, and returns it once it exists.
    func create() async -> SessionIdentity? {
        guard canCreate, let path else { return nil }
        pending = true; error = nil; folderError = nil
        defer { pending = false }
        let server = connect(self.server)
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let session = try await server.createSession(WireCreateSessionInput(
                id: creationID, cwd: path, name: trimmed, model: model,
                thinkingLevel: levels.isEmpty ? nil : thinking, temporary: false))
            return SessionIdentity(server: self.server, session: session.id)
        } catch {
            if SessionCreation.definitelyRejected(error) {
                // The server answers any invalid create with a bare 400. The
                // model, thinking level and ID come from its catalog and this
                // form, so the folder is what it refused.
                if case ClientError.http(400) = error {
                    folderError = "No folder at \(folderText.trimmingCharacters(in: .whitespacesAndNewlines)) on \(self.server)."
                } else {
                    self.error = error.localizedDescription
                }
                return nil
            }
            // The answer may have been lost after the server created it.
            if let session = try? await server.snapshot(creationID) {
                return SessionIdentity(server: self.server, session: session.id)
            }
            uncertain = true
            self.error = "\(self.server) may have created the session. Close this and pull to refresh Sessions before trying again. \(error.localizedDescription)"
            return nil
        }
    }
}

extension NewSessionForm: Identifiable {
    nonisolated var id: ObjectIdentifier { ObjectIdentifier(self) }
}

/// A server that was removed while the form was open.
struct UnpairedServer: NewSessionServer {
    let name: String
    private var gone: ClientError { .needsPairing(.missing) }
    func models() async throws -> [WireModelCapability] { throw gone }
    func serverSettings() async throws -> ServerSettings { throw gone }
    func home() async throws -> String { throw gone }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        throw MutationNotSent(reason: "\(name) is no longer paired with this iPhone.")
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw gone }
}
