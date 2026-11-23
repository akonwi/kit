import Foundation

/// A workspace file's text as a client downloaded it (ADR 0074), which
/// comments anchor to by its workspace, path, and revision.
public struct WorkspaceFile: Equatable, Sendable {
    /// The workspace the file was downloaded from.
    public let workspace: WireWorkspaceRef
    /// The file's canonical workspace path.
    public let path: String
    public let revision: String
    public let content: String
    /// The file's size, which is larger than the content when it was cut.
    public let size: Int64
    public let modifiedAt: String?
    /// The content stops before the file's end.
    public let truncated: Bool

    public init(workspace: WireWorkspaceRef, path: String, revision: String, content: String, size: Int64,
                modifiedAt: String? = nil, truncated: Bool = false) {
        self.workspace = workspace; self.path = path; self.revision = revision; self.content = content
        self.size = size; self.modifiedAt = modifiedAt; self.truncated = truncated
    }
}
