import Foundation
import KitClient
import Synchronization
import Testing
@testable import Kit

/// A server that answers the new session form from fixed values.
private final class FakeNewSessionServer: NewSessionServer {
    struct State {
        var models: [WireModelCapability] = []
        var settings = ServerSettings()
        var home = "/home/me"
        var createError: (any Error)?
        var created: [WireCreateSessionInput] = []
        /// Sessions the server has, by ID, as a lost answer would leave them.
        var existing: Set<String> = []
    }

    let state: Mutex<State>

    init(_ configure: (inout State) -> Void = { _ in }) {
        var initial = State()
        configure(&initial)
        state = Mutex(initial)
    }

    var created: [WireCreateSessionInput] { state.withLock { $0.created } }

    func models() async throws -> [WireModelCapability] { state.withLock { $0.models } }
    func serverSettings() async throws -> ServerSettings { state.withLock { $0.settings } }
    func home() async throws -> String { state.withLock { $0.home } }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        try state.withLock { state in
            state.created.append(input)
            if let error = state.createError {
                // A lost answer: the server made it anyway.
                if !SessionCreation.definitelyRejected(error), let id = input.id { state.existing.insert(id) }
                throw error
            }
            return NewSessionFormTests.session(input.id ?? "session_x", cwd: input.cwd)
        }
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard state.withLock({ $0.existing.contains(id) }) else { throw ClientError.missingSession }
        return NewSessionFormTests.session(id, cwd: "/")
    }
}

@MainActor
struct NewSessionFormTests {
    nonisolated static func session(_ id: String, cwd: String?) -> SessionExcerpt {
        SessionExcerpt(id: id, title: "Session \(id)", sourceTitle: "Kit server", model: "m", thinking: "off",
                       workspace: "", cwd: cwd, date: "2026-10-31T12:00:00Z", messages: [])
    }

    static func model(_ id: String, _ levels: [String]?, available: Bool = true) -> WireModelCapability {
        WireModelCapability(id: id, name: id.uppercased(), provider: "p", api: "test", contextWindow: 1000, maxInputTokens: nil,
                            maxOutputTokens: nil, thinkingLevels: levels?.compactMap(WireThinkingLevel.init(rawValue:)),
                            inputs: nil, available: available)
    }

    private static func row(_ server: String, _ id: String, cwd: String) -> SessionsStore.Row {
        SessionsStore.Row(session: session(id, cwd: cwd), server: server, status: .idle, stale: false, unseen: false,
                          preview: nil, changed: nil)
    }

    private func form(_ server: FakeNewSessionServer, recent: [String] = ["/home/me/src/kit"]) -> NewSessionForm {
        NewSessionForm(places: [.init(server: "studio", recent: recent)], server: "studio") { _ in server }
    }

    @Test func eachServerOffersItsLatestDistinctFolders() {
        let rows = [Self.row("studio", "a", cwd: "/home/me/kit"), Self.row("vps", "b", cwd: "/srv/evals"),
                    Self.row("studio", "c", cwd: "/home/me/surf"), Self.row("studio", "d", cwd: "/home/me/kit")]
        #expect(NewSessionForm.places(servers: ["studio", "vps", "gpu"], rows: rows) == [
            .init(server: "studio", recent: ["/home/me/kit", "/home/me/surf"]),
            .init(server: "vps", recent: ["/srv/evals"]),
            .init(server: "gpu", recent: []),
        ])
    }

    @Test func theFormStartsOnTheLatestFolderAndTheServersDefaultModel() async {
        let server = FakeNewSessionServer {
            $0.models = [Self.model("a", ["low", "medium", "high"]), Self.model("b", nil)]
            $0.settings = ServerSettings(defaultModel: "a")
        }
        let form = form(server)
        await form.load()
        #expect(form.folder == NewSessionForm.Folder.recent("/home/me/src/kit"))
        #expect(form.display("/home/me/src/kit") == "~/src/kit")
        #expect(form.model == "a")
        #expect(form.thinking == "medium")
        #expect(form.canCreate)

        form.model = "b"
        #expect(form.levels.isEmpty)
        #expect(form.thinking == "")
    }

    @Test func aTypedFolderStartsFromTheServersHome() async {
        let server = FakeNewSessionServer { $0.models = [Self.model("a", nil)] }
        let form = form(server, recent: [])
        #expect(form.folder == nil)
        await form.load()
        #expect(!form.canCreate)
        form.folder = .typed("~/src/evals")
        #expect(form.path == "/home/me/src/evals")
        form.folder = .typed("/srv/app ")
        #expect(form.path == "/srv/app")
    }

    @Test func creatingSendsTheFormAndOpensTheSession() async throws {
        let server = FakeNewSessionServer { $0.models = [Self.model("a", ["low", "medium"])] }
        let form = form(server)
        await form.load()
        form.name = "  Eval runner  "
        let created = try #require(await form.create())
        #expect(created.server == "studio")
        let input = try #require(server.created.first)
        #expect(created.session == input.id)
        #expect(input.cwd == "/home/me/src/kit")
        #expect(input.name == "Eval runner")
        #expect(input.model == "a")
        #expect(input.thinkingLevel == "medium")
        #expect(input.temporary == false)
    }

    @Test func aRefusedFolderSaysWhereAndLetsYouChooseAnother() async {
        let server = FakeNewSessionServer {
            $0.models = [Self.model("a", nil)]
            $0.createError = ClientError.http(400)
        }
        let form = form(server)
        await form.load()
        #expect(await form.create() == nil)
        #expect(form.error == "studio couldn’t start a session in ~/src/kit. Check that the folder exists there.")
        #expect(!form.uncertain)
        #expect(form.canCreate)
    }

    @Test func aLostAnswerFindsTheSessionTheServerMade() async throws {
        let server = FakeNewSessionServer {
            $0.models = [Self.model("a", nil)]
            $0.createError = URLError(.networkConnectionLost)
        }
        let form = form(server)
        await form.load()
        let created = try #require(await form.create())
        #expect(created.session == server.created.first?.id)
        #expect(form.error == nil)
    }

    @Test func anUnconfirmedCreateHoldsTheFormSoItIsNotMadeTwice() async {
        let server = FakeNewSessionServer { $0.models = [Self.model("a", nil)] }
        let form = NewSessionForm(places: [.init(server: "studio", recent: ["/home/me/src/kit"])], server: "studio") { _ in
            UncertainServer(base: server)
        }
        await form.load()
        #expect(await form.create() == nil)
        #expect(form.uncertain)
        #expect(!form.canCreate)
        #expect(form.error?.hasPrefix("studio may have created the session.") == true)
    }

    @Test func switchingServersLoadsThatServersFoldersAndModels() async {
        let studio = FakeNewSessionServer { $0.models = [Self.model("a", nil)] }
        let vps = FakeNewSessionServer { $0.models = [Self.model("v", nil)]; $0.home = "/root" }
        let form = NewSessionForm(places: [.init(server: "studio", recent: ["/home/me/kit"]),
                                           .init(server: "vps", recent: ["/srv/evals"])], server: "vps") { name in
            name == "vps" ? vps : studio
        }
        #expect(form.server == "vps")
        await form.load()
        await form.select(server: "studio")
        #expect(form.folder == NewSessionForm.Folder.recent("/home/me/kit"))
        #expect(form.model == "a")
        #expect(form.home == "/home/me")
    }
}

/// A server whose creates fail without a trace, so the form can't recover the session.
private struct UncertainServer: NewSessionServer {
    let base: FakeNewSessionServer
    func models() async throws -> [WireModelCapability] { try await base.models() }
    func serverSettings() async throws -> ServerSettings { try await base.serverSettings() }
    func home() async throws -> String { try await base.home() }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt { throw ClientError.http(502) }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
}
