import SwiftUI

struct WorkspaceLocation: View {
    let session: SessionExcerpt?

    static func homeRelative(_ path: String, home: String = FileManager.default.homeDirectoryForCurrentUser.path) -> String {
        if path == home { return "~" }
        guard home != "/", path.hasPrefix(home + "/") else { return path }
        return "~" + path.dropFirst(home.count)
    }

    static func shortened(_ path: String) -> String {
        let parts = path.split(separator: "/")
        guard parts.count > 2 else { return path }
        return "…/" + parts.suffix(2).joined(separator: "/")
    }

    static func repositoryLabel(_ session: SessionExcerpt?) -> String? {
        guard let head = session?.gitHead, !head.isEmpty else { return nil }
        let label = session?.gitHeadKind == "detached" ? "detached \(head)" : head
        return label + (session?.gitDirty == true ? "*" : "")
    }

    var body: some View {
        let path = session?.cwd ?? session?.workspace ?? ""
        let displayPath = Self.homeRelative(path)
        let head = Self.repositoryLabel(session)
        HStack(spacing: 4) {
            ViewThatFits(in: .horizontal) {
                Text(displayPath).fixedSize()
                Text(Self.shortened(displayPath)).lineLimit(1).truncationMode(.head)
            }
            if let head, !head.isEmpty {
                Text("(\(head))")
                    .lineLimit(1).truncationMode(.middle).layoutPriority(1)
            }
        }
        .font(.kit(size: 12, design: .monospaced))
        .frame(maxWidth: 340, alignment: .trailing)
        .help(path + (head.map { "\nGit snapshot: \($0)\(session?.gitDirty == true ? " · uncommitted changes" : " · clean")" } ?? ""))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(path + (head.map { ", \($0), \(session?.gitDirty == true ? "uncommitted changes" : "clean")" } ?? ""))
    }
}
