import Foundation

extension WireSessionFileIndex {
    // The truncated flag was added within protocol version 31. Earlier daemons
    // omit it; preserve their complete-index interpretation during an update.
    private enum IndexKeys: String, CodingKey { case sessionId, cwd, entries, truncated }
    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: IndexKeys.self)
        sessionId = try values.decode(String.self, forKey: .sessionId)
        cwd = try values.decode(String.self, forKey: .cwd)
        entries = try values.decodeIfPresent([WireFileIndexEntry].self, forKey: .entries)
        truncated = try values.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
    }
}
