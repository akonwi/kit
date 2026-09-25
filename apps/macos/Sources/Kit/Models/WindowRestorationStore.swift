import Foundation
import Observation

/// Intent only: server data, credentials, and drafts have separate owners.
@MainActor @Observable
final class WindowRestorationStore {
    struct Group: Codable, Equatable {
        var sessions: [SessionIdentity]
        var selected: SessionIdentity?
    }
    private struct Manifest: Codable {
        let version: Int
        var sessions: [SessionIdentity]
        var groups: [Group]?
    }
    private(set) var groups: [Group] = []
    private(set) var sessions: [SessionIdentity] = []
    private(set) var error: String?
    private let url: URL

    init(url: URL = URL.applicationSupportDirectory.appendingPathComponent("com.akonwi.kit/windows.json")) {
        self.url = url
        do {
            let data = try Data(contentsOf: url)
            guard data.count <= 1024 * 1024 else { throw ClientError.oversized }
            let manifest = try JSONDecoder().decode(Manifest.self, from: data)
            guard manifest.version == 1, manifest.sessions.count <= 100 else { throw ClientError.invalidPayload }
            var seen = Set<SessionIdentity>()
            sessions = manifest.sessions.filter { !$0.server.isEmpty && !$0.session.isEmpty && seen.insert($0).inserted }
            groups = normalized(manifest.groups ?? [])
        } catch let issue as CocoaError where issue.code == .fileReadNoSuchFile {
        } catch { self.error = "Could not restore windows: " + error.localizedDescription }
    }

    func remember(_ identity: SessionIdentity, replacing old: SessionIdentity? = nil) {
        guard !identity.server.isEmpty, !identity.session.isEmpty else { return }
        let previous = sessions
        if let old, old != identity {
            sessions.removeAll { $0 == old }
            groups = groups.map { group in
                Group(sessions: group.sessions.map { $0 == old ? identity : $0 },
                      selected: group.selected == old ? identity : group.selected)
            }
        }
        if !sessions.contains(identity) { sessions.append(identity) }
        if sessions != previous { save() }
    }

    func forget(_ identity: SessionIdentity) {
        let previous = sessions
        sessions.removeAll { $0 == identity }
        groups = normalized(groups)
        if sessions != previous { save() }
    }

    func group(containing identity: SessionIdentity) -> Group? {
        groups.first { $0.sessions.contains(identity) }
    }

    /// Update attached windows without discarding restoration intent for scenes
    /// whose connections/root views have not finished loading yet.
    func rememberGroups(_ visible: [Group]) {
        var result = visible
        let observed = Set(visible.flatMap(\.sessions))
        for old in groups {
            let missing = old.sessions.filter { !observed.contains($0) && sessions.contains($0) }
            guard !missing.isEmpty else { continue }
            if let index = result.firstIndex(where: { group in group.sessions.contains { old.sessions.contains($0) } }) {
                let members = Set(result[index].sessions + missing)
                result[index].sessions = old.sessions.filter { members.contains($0) }
                    + result[index].sessions.filter { !old.sessions.contains($0) }
                if let selected = old.selected, missing.contains(selected) { result[index].selected = selected }
            } else { result.append(Group(sessions: missing, selected: old.selected)) }
        }
        let next = normalized(result)
        guard next != groups else { return }
        groups = next
        save()
    }

    private func normalized(_ source: [Group]) -> [Group] {
        var seen = Set<SessionIdentity>()
        return source.compactMap { group in
            let members = group.sessions.filter { sessions.contains($0) && seen.insert($0).inserted }
            guard !members.isEmpty else { return nil }
            return Group(sessions: members, selected: group.selected.flatMap { members.contains($0) ? $0 : nil } ?? members.first)
        }
    }

    func save() {
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try JSONEncoder().encode(Manifest(version: 1, sessions: sessions, groups: groups)).write(to: url, options: .atomic)
            error = nil
        } catch { self.error = "Could not save open windows: " + error.localizedDescription }
    }
}
