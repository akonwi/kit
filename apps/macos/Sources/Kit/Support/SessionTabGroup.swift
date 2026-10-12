import Foundation

/// Session tabs grouped by project directory, in order of each project's
/// first tab. The display order also defines ⌘1–⌘9.
struct SessionTabGroup<Tab>: Identifiable {
    let directory: String
    var tabs: [Tab]
    var id: String { directory }

    /// The project's folder name, or nil for tabs without a directory.
    var name: String? {
        directory.isEmpty ? nil : URL(fileURLWithPath: directory).lastPathComponent
    }

    static func grouped(_ tabs: [Tab], directory: (Tab) -> String) -> [SessionTabGroup] {
        var groups: [SessionTabGroup] = []
        for tab in tabs {
            let key = directory(tab)
            if let index = groups.firstIndex(where: { $0.directory == key }) {
                groups[index].tabs.append(tab)
            } else {
                groups.append(SessionTabGroup(directory: key, tabs: [tab]))
            }
        }
        // Tabs without a project, such as the launcher, follow every project.
        if let index = groups.firstIndex(where: { $0.directory.isEmpty }) {
            groups.append(groups.remove(at: index))
        }
        return groups
    }
}
