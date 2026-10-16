import Foundation
import Observation

/// Restoration intent only: which windows were open, their session tabs in
/// order, each window's selected session, each open session's tile layout,
/// and the sidebar's collapsed families. Server data, credentials, and drafts have separate owners.
@MainActor @Observable
final class WindowRestorationStore {
    struct Window: Codable, Equatable, Identifiable {
        var id: UUID
        var sessions: [SessionIdentity]
        var selected: SessionIdentity?
        /// Tile layouts of open session tabs. Tabs with the default layout
        /// have none.
        var layouts: [SessionIdentity: TileArrangement] = [:]
        /// Sessions whose children the sidebar hides, which may include a
        /// closed parent of an open tab.
        var collapsed: Set<String> = []

        init(id: UUID, sessions: [SessionIdentity], selected: SessionIdentity?,
             layouts: [SessionIdentity: TileArrangement] = [:], collapsed: Set<String> = []) {
            self.id = id
            self.sessions = sessions
            self.selected = selected
            self.layouts = layouts
            self.collapsed = collapsed
        }

        private struct Layout: Codable {
            var session: SessionIdentity
            var arrangement: TileArrangement
        }
        private enum CodingKeys: String, CodingKey { case id, sessions, selected, layouts, collapsed }

        init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            id = try container.decode(UUID.self, forKey: .id)
            sessions = try container.decode([SessionIdentity].self, forKey: .sessions)
            selected = try container.decodeIfPresent(SessionIdentity.self, forKey: .selected)
            // Windows saved before layouts existed have none.
            let saved = try container.decodeIfPresent([Layout].self, forKey: .layouts) ?? []
            layouts = Dictionary(saved.map { ($0.session, $0.arrangement) }) { first, _ in first }
            collapsed = Set(try container.decodeIfPresent([String].self, forKey: .collapsed) ?? [])
        }

        func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(id, forKey: .id)
            try container.encode(sessions, forKey: .sessions)
            try container.encodeIfPresent(selected, forKey: .selected)
            // Tab order keeps the file stable between saves.
            let saved = sessions.compactMap { session in layouts[session].map { Layout(session: session, arrangement: $0) } }
            if !saved.isEmpty { try container.encode(saved, forKey: .layouts) }
            if !collapsed.isEmpty { try container.encode(collapsed.sorted(), forKey: .collapsed) }
        }
    }
    /// Version 1 saved one session per window, grouped by native window tabs.
    private struct LegacyGroup: Codable {
        var sessions: [SessionIdentity]
        var selected: SessionIdentity?
    }
    private struct Manifest: Codable {
        let version: Int
        var windows: [Window]?
        var sessions: [SessionIdentity]?
        var groups: [LegacyGroup]?
    }
    static let maximumSessions = 100

    private(set) var windows: [Window] = []
    private(set) var error: String?
    private let url: URL

    init(url: URL = URL.applicationSupportDirectory.appendingPathComponent(AppIdentity.bundleIdentifier + "/windows.json")) {
        self.url = url
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= 1024 * 1024 else { throw ClientError.oversized }
            let manifest = try JSONDecoder().decode(Manifest.self, from: data)
            switch manifest.version {
            case 1: windows = Self.migrate(sessions: manifest.sessions ?? [], groups: manifest.groups ?? [])
            case 2: windows = Self.normalized(manifest.windows ?? [])
            default: throw ClientError.invalidPayload
            }
        } catch let issue as CocoaError where issue.code == .fileReadNoSuchFile {
        } catch { self.error = "Could not restore windows: " + error.localizedDescription }
    }

    /// Every restorable session, in window order.
    var sessions: [SessionIdentity] { windows.flatMap(\.sessions) }

    func window(_ id: UUID) -> Window? { windows.first { $0.id == id } }

    /// Records a window's session tabs. A window without sessions is not
    /// restoration intent and is forgotten. A session belongs to one window.
    func update(_ window: Window) {
        var next = windows
        let sessions = Set(window.sessions)
        next = next.map { saved in
            guard saved.id != window.id else { return saved }
            var trimmed = saved
            trimmed.sessions.removeAll { sessions.contains($0) }
            return trimmed
        }
        if let index = next.firstIndex(where: { $0.id == window.id }) {
            next[index] = window
        } else {
            next.append(window)
        }
        commit(Self.normalized(next))
    }

    func remove(_ id: UUID) {
        commit(windows.filter { $0.id != id })
    }

    private func commit(_ next: [Window]) {
        guard next != windows else { return }
        windows = next
        save()
    }

    private static func normalized(_ source: [Window]) -> [Window] {
        var seen = Set<SessionIdentity>()
        var windowIDs = Set<UUID>()
        var count = 0
        return source.compactMap { window in
            guard windowIDs.insert(window.id).inserted else { return nil }
            let members = window.sessions.filter { identity in
                guard !identity.server.isEmpty, !identity.session.isEmpty,
                      count < maximumSessions, seen.insert(identity).inserted else { return false }
                count += 1
                return true
            }
            guard !members.isEmpty else { return nil }
            let selected = window.selected.flatMap { members.contains($0) ? $0 : nil } ?? members.first
            let layouts = window.layouts.filter { members.contains($0.key) && !$0.value.isDefault }
            let collapsed = Set(window.collapsed.filter { !$0.isEmpty }.sorted().prefix(maximumSessions))
            return Window(id: window.id, sessions: members, selected: selected, layouts: layouts, collapsed: collapsed)
        }
    }

    /// Native tab groups become windows; sessions outside any group open in a
    /// window of their own.
    private static func migrate(sessions: [SessionIdentity], groups: [LegacyGroup]) -> [Window] {
        let known = Set(sessions)
        var windows = groups.map { group in
            Window(id: UUID(), sessions: group.sessions.filter { known.contains($0) }, selected: group.selected)
        }
        let grouped = Set(windows.flatMap(\.sessions))
        windows += sessions.filter { !grouped.contains($0) }.map { Window(id: UUID(), sessions: [$0], selected: $0) }
        return normalized(windows)
    }

    func save() {
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            // Layouts and collapsed families are optional fields of version
            // 2, which earlier builds ignore, so downgrading keeps windows
            // and tabs.
            try JSONEncoder().encode(Manifest(version: 2, windows: windows)).write(to: url, options: .atomic)
            error = nil
        } catch { self.error = "Could not save open windows: " + error.localizedDescription }
    }
}
