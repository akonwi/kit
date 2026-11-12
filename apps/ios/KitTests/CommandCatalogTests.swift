import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct CommandCatalogTests {
    static func prompt(_ name: String, _ description: String, hint: String? = nil) -> PromptCommand {
        PromptCommand(name: name, description: description, source: "project", location: "/\(name).md", argumentHint: hint)
    }

    static let review = prompt("review", "Review the working tree")
    static let preview = PluginCommand(id: "netlify.preview", instance: "i1", description: "Deploy a preview", argName: nil)
    static let deploy = PluginCommand(id: "netlify.deploy", instance: "i1", description: "Deploy a branch", argName: "branch")

    static let state = CommandState(changes: "1 file +12 −3", model: "GPT-5.5", thinking: "High", hasThinking: true,
                                    contextPercent: 41)

    static func commands(_ state: CommandState = state) -> [SessionCommand] {
        CommandCatalog.commands(state, prompts: [review], plugins: [preview])
    }

    static func titles(_ sections: [CommandCatalog.Section]) -> [String] {
        sections.flatMap { section in (section.title.map { ["# " + $0] } ?? []) + section.rows.map(\.title) }
    }

    @Test func withNothingTypedRecentCommandsLeadThenTheRestByGroup() {
        let sections = CommandCatalog.sections(Self.commands(), query: "",
                                               recents: ["prompt:review", "plugin:gone.away", "builtin:model", "builtin:compact"])
        #expect(Self.titles(sections) == [
            "# Recent", "review", "Model", "Compact context",
            "# Session", "Changes", "Open File…", "Scratchpad", "Thinking", "Rename…", "Fork…", "Change folder…",
            "Reload context", "Details",
            "# Plugins", "preview"
        ])
    }

    @Test func onlyThreeRecentCommandsLead() {
        let sections = CommandCatalog.sections(Self.commands(), query: "",
                                               recents: ["builtin:fork", "builtin:rename", "builtin:details", "builtin:changes"])
        #expect(sections.first == .init(title: "Recent", rows: sections.first?.rows ?? []))
        #expect(sections.first?.rows.map(\.title) == ["Fork…", "Rename…", "Details"])
    }

    @Test func rowsShowTheSessionsValues() {
        let rows = Dictionary(uniqueKeysWithValues: Self.commands().map { ($0.id, $0.detail) })
        #expect(rows["builtin:changes"] == "1 file +12 −3")
        #expect(rows["builtin:model"] == "GPT-5.5")
        #expect(rows["builtin:thinking"] == "High")
        #expect(rows["builtin:compact"] == "41%")
        #expect(rows["prompt:review"] == "Review the working tree")
        #expect(rows["plugin:netlify.preview"] == "Deploy a preview")
    }

    @Test func typingRanksEveryKindTogether() {
        let sections = CommandCatalog.sections(Self.commands(), query: "re", recents: ["builtin:details"])
        #expect(sections.count == 1)
        #expect(sections[0].title == nil)
        // Equal matches keep the list's order, as in the TUI and Mac.
        #expect(Array(sections[0].rows.map(\.title).prefix(3)) == ["Rename…", "Reload context", "review"])
        #expect(CommandCatalog.sections(Self.commands(), query: "rev", recents: [])[0].rows.first?.title == "review")
    }

    @Test func theTUIsNamesFindTheirCommands() {
        func first(_ query: String) -> String? {
            CommandCatalog.sections(Self.commands(), query: query, recents: []).first?.rows.first?.title
        }
        #expect(first("diff") == "Changes")
        #expect(first("name") == "Rename…")
        #expect(first("cd") == "Change folder…")
        #expect(first("netlify") == "preview")
        #expect(first("zzz") == nil)
    }

    @Test func aCommandThatCanNotRunSaysWhyAndOptionalOnesLeave() {
        var state = Self.state
        state.modelLocked = "Changes when the turn ends"
        state.live = false
        state.hasThinking = false
        state.scratchpad = false
        state.pluginUnavailable = "A command is running"
        state.folderUnavailable = "Available when the turn ends"
        let rows = Self.commands(state)
        let reasons = Dictionary(uniqueKeysWithValues: rows.map { ($0.id, $0.unavailableReason) })
        #expect(reasons["builtin:model"] == "Changes when the turn ends")
        #expect(reasons["builtin:rename"] == "Reconnect to rename")
        #expect(reasons["plugin:netlify.preview"] == "A command is running")
        #expect(reasons["builtin:changeFolder"] == "Available when the turn ends")
        #expect(!rows.map(\.id).contains("builtin:thinking"))
        #expect(!rows.map(\.id).contains("builtin:scratchpad"))
    }

    @Test func promptsAndPluginsWithAnArgumentTakeText() {
        let rows = CommandCatalog.commands(Self.state, prompts: [Self.review], plugins: [Self.preview, Self.deploy])
        let takes = Dictionary(uniqueKeysWithValues: rows.map { ($0.id, $0.takesArgument) })
        #expect(takes["prompt:review"] == true)
        #expect(takes["plugin:netlify.deploy"] == true)
        #expect(takes["plugin:netlify.preview"] == false)
        #expect(takes["builtin:fork"] == false)
    }

    @Test func recentsKeepTheNewestFirstOnce() throws {
        let suite = "CommandCatalogTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let recents = CommandRecents(defaults: defaults)
        recents.record("builtin:model")
        recents.record("prompt:review")
        recents.record("builtin:model")
        #expect(recents.ids == ["builtin:model", "prompt:review"])
        for index in 0..<20 { recents.record("prompt:p\(index)") }
        #expect(recents.ids.count == CommandRecents.limit)
        #expect(recents.ids.first == "prompt:p19")
    }
}

@MainActor
struct PickedCommandTests {
    static func composer(_ client: FakeComposerClient = FakeComposerClient()) -> SessionComposer {
        SessionComposerTests.composer(client, snapshot: SessionComposerTests.snapshot(commands: [CommandCatalogTests.review]))
    }

    @Test func aPickedPromptRunsWithTheDraftAndLeavesOnceAccepted() async {
        let client = FakeComposerClient()
        let composer = Self.composer(client)
        composer.command = .prompt(CommandCatalogTests.review)
        #expect(composer.action == .send(enabled: true))
        composer.draft = "focus on SSE"
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.map(\.name) == ["review"])
        #expect(client.commands.first?.args == "focus on SSE")
        #expect(client.submitted.isEmpty)
        #expect(composer.command == nil)
        #expect(composer.draft == "")
    }

    @Test func aPromptThatLeftTheSessionIsNotSent() async {
        let client = FakeComposerClient()
        let composer = Self.composer(client)
        composer.command = .prompt(CommandCatalogTests.prompt("gone", "Gone"))
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.isEmpty)
        #expect(composer.notice == "Didn’t send. gone is no longer available. Choose it again.")
        #expect(composer.command == .prompt(CommandCatalogTests.prompt("gone", "Gone")))
    }

    @Test func anotherSourcesPromptOfTheSameNameIsNotRun() async {
        let client = FakeComposerClient()
        let composer = Self.composer(client)
        let other = PromptCommand(name: "review", description: "Review", source: "user", location: "/user/review.md")
        composer.command = .prompt(other)
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.isEmpty)
        #expect(composer.notice == "Didn’t send. review is no longer available. Choose it again.")
    }

    @Test func aCommentBeingWrittenHoldsACommand() async throws {
        let client = FakeComposerClient()
        let composer = Self.composer(client)
        let note = try SessionComposerTests.comment(1)
        composer.annotations.begin(anchor: note.anchor, editing: nil)
        composer.command = .prompt(CommandCatalogTests.review)
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.isEmpty)
        #expect(composer.notice == "Didn’t send. Finish the comment you’re writing before sending.")
    }

    @Test func aPickedPluginRunsWithTheDraftAndClearsAtOnce() {
        let composer = Self.composer()
        var runs: [(String, String)] = []
        composer.runPlugin = { runs.append(($0.id, $1)) }
        composer.command = .plugin(CommandCatalogTests.deploy)
        composer.draft = "main"
        composer.send()
        #expect(runs.map(\.0) == ["netlify.deploy"])
        #expect(runs.map(\.1) == ["main"])
        #expect(composer.command == nil)
        #expect(composer.draft == "")
    }

    @Test func aPluginThatCanNotRunKeepsTheChipAndSaysWhy() {
        let composer = Self.composer()
        var runs = 0
        composer.runPlugin = { _, _ in runs += 1 }
        composer.pluginUnavailable = { "A command is running" }
        composer.command = .plugin(CommandCatalogTests.deploy)
        composer.draft = "main"
        composer.send()
        #expect(runs == 0)
        #expect(composer.notice == "Didn’t send. A command is running")
        #expect(composer.command == .plugin(CommandCatalogTests.deploy))
        #expect(composer.draft == "main")
    }

    @Test func commandsTakeNoComments() async throws {
        let client = FakeComposerClient()
        var session = SessionComposerTests.commented([try SessionComposerTests.comment(1)])
        session.promptCommands = [CommandCatalogTests.review]
        let composer = SessionComposerTests.composer(client, snapshot: session)
        composer.command = .prompt(CommandCatalogTests.review)
        composer.send()
        await SessionComposerTests.settle()
        #expect(client.commands.isEmpty)
        #expect(composer.notice == "Didn’t send. Commands don’t take attachments or comments.")
    }
}

@MainActor
struct CommandActionsTests {
    static func actions(_ client: FakeActionsClient, plugins: [PluginCommand] = [CommandCatalogTests.preview]) async -> SessionActions {
        var snapshot = SessionActionsTests.snapshot()
        snapshot.pluginCommands = plugins
        return await SessionActionsTests.actions(client, snapshot: snapshot)
    }

    @Test func reloadingSaysItWorked() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client)
        actions.reloadContext()
        await SessionComposerTests.settle()
        #expect(client.reloads == ["s"])
        #expect(actions.notice == .init(text: "Context reloaded.", tone: .info))
    }

    @Test func aReloadWithIssuesNamesTheFirst() async {
        let client = FakeActionsClient()
        client.reloadResult = WireReloadSessionResult(sessionId: "s", eventStreamId: "e", sources: nil, diagnostics: nil,
                                                      warnings: ["AGENTS.md is too large", "skills/x has no name"])
        let actions = await Self.actions(client)
        actions.reloadContext()
        await SessionComposerTests.settle()
        #expect(actions.notice == .init(text: "Reloaded context with issues: AGENTS.md is too large (+1 more)", tone: .danger))
    }

    @Test func aFailedReloadIsNotRepeated() async {
        let client = FakeActionsClient()
        client.reloadError = ClientError.http(500)
        let actions = await Self.actions(client)
        actions.reloadContext()
        await SessionComposerTests.settle()
        #expect(client.reloads == ["s"])
        #expect(actions.notice?.tone == .danger)
        #expect(actions.notice?.text.hasPrefix("Couldn’t reload context.") == true)
    }

    @Test func aPluginCommandRunsOneAtATime() async {
        let client = FakeActionsClient()
        let (gate, release) = AsyncStream<Void>.makeStream()
        client.pluginGate = gate
        let actions = await Self.actions(client)
        actions.runPlugin(CommandCatalogTests.preview, args: "")
        #expect(actions.runningPlugin == "preview")
        #expect(actions.pluginUnavailableReason == "A command is running")
        actions.runPlugin(CommandCatalogTests.preview, args: "")
        await SessionComposerTests.settle()
        #expect(client.plugins.map(\.id) == ["netlify.preview"])
        release.yield()
        release.finish()
        await SessionComposerTests.settle()
        #expect(actions.runningPlugin == nil)
        #expect(actions.notice == nil)
    }

    @Test func aFailedPluginCommandSaysItWasNotRetried() async {
        let client = FakeActionsClient()
        client.pluginError = ClientError.http(500)
        let actions = await Self.actions(client)
        actions.runPlugin(CommandCatalogTests.preview, args: "")
        await SessionComposerTests.settle()
        #expect(client.plugins.count == 1)
        #expect(actions.notice?.tone == .danger)
        #expect(actions.notice?.text.hasPrefix("preview failed.") == true)
        #expect(actions.notice?.text.hasSuffix("It was not retried.") == true)
    }

    @Test func aPluginCommandThatLeftTheSessionIsNotRun() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client, plugins: [])
        actions.runPlugin(CommandCatalogTests.preview, args: "")
        await SessionComposerTests.settle()
        #expect(client.plugins.isEmpty)
        #expect(actions.notice == .init(text: "preview is no longer available. Choose it again.", tone: .danger))
    }

    // MARK: Change folder

    @Test func aFolderChangeSendsThePathAsTypedAndReadsTheSessionAgain() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client)
        var delivered = 0
        actions.deliver = { _ in delivered += 1 }
        #expect(await actions.changeFolder(to: "  ../web  "))
        #expect(client.folderChanges.map(\.path) == ["../web"])
        #expect(delivered == 1)
        #expect(actions.directory.request == nil)
    }

    @Test func theFolderChangesOnlyBetweenTurnsAndWhileConnected() async {
        var running = SessionActionsTests.snapshot(running: "t1")
        running.pluginCommands = []
        let client = FakeActionsClient()
        let busy = await SessionActionsTests.actions(client, snapshot: running)
        #expect(busy.folderUnavailableReason == "Available when the turn ends")
        #expect(await busy.changeFolder(to: "~/web") == false)
        let offline = await SessionActionsTests.actions(client, live: false)
        #expect(offline.folderUnavailableReason == "Reconnect to change")
        #expect(client.folderChanges.isEmpty)
    }

    @Test func aChangeThatMayHaveArrivedIsRetriedAsTheSameChange() async {
        let client = FakeActionsClient()
        client.folderErrors = [URLError(.networkConnectionLost)]
        let actions = await Self.actions(client)
        #expect(await actions.changeFolder(to: "~/web") == false)
        #expect(actions.directory.request?.path == "~/web")
        #expect(actions.directory.error != nil)
        // Another path waits until the first is settled.
        #expect(await actions.changeFolder(to: "~/api") == false)
        #expect(await actions.changeFolder(to: "~/web"))
        let ids = client.folderChanges.map(\.mutationId)
        #expect(ids.count == 2)
        #expect(Set(ids).count == 1)
    }

    @Test func aBusySessionSaysWhyTheFolderDidNotChange() async {
        let client = FakeActionsClient()
        client.folderErrors = [ClientError.http(409)]
        let actions = await Self.actions(client)
        #expect(await actions.changeFolder(to: "~/web") == false)
        #expect(actions.directory.error == "The session has active work. Wait for it to finish before changing directories.")
        // The server refused it, so another path can go.
        #expect(actions.directory.request == nil)
    }

    @Test func aRetryThatCouldNotBeSentKeepsTheEarlierChange() async {
        let client = FakeActionsClient()
        client.folderErrors = [URLError(.networkConnectionLost), MutationNotSent(reason: "Offline")]
        let actions = await Self.actions(client)
        #expect(await actions.changeFolder(to: "~/web") == false)
        // The retry never left the phone, but the first try may have arrived.
        #expect(await actions.changeFolder(to: "~/web") == false)
        #expect(actions.directory.request?.path == "~/web")
        #expect(await actions.changeFolder(to: "~/web"))
        #expect(Set(client.folderChanges.map(\.mutationId)).count == 1)
        #expect(client.folderChanges.count == 3)
    }

    @Test func aRunningPluginCommandHoldsTheFolder() async {
        let client = FakeActionsClient()
        let (gate, release) = AsyncStream<Void>.makeStream()
        client.pluginGate = gate
        let actions = await Self.actions(client)
        actions.runPlugin(CommandCatalogTests.preview, args: "")
        #expect(actions.folderUnavailableReason == "Wait for the command")
        release.finish()
        await SessionComposerTests.settle()
        #expect(actions.folderUnavailableReason == nil)
    }
}
