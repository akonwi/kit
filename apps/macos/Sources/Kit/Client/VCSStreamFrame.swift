import Foundation

/// Strict nested wire validation for the volatile repository stream.
enum VCSStreamFrame {
    static func decode(_ data: Data, session: String) throws -> WireSessionVCSStatus {
        guard data.count < 64 * 1024, String(data: data, encoding: .utf8) != nil,
              uniqueMembers(data),
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              Set(object.keys).isSubset(of: ["sessionId", "cwd", "status"])
        else { throw ClientError.invalidPayload }
        if let status = object["status"] as? [String: Any] {
            guard Set(status.keys).isSubset(of: ["root", "head", "dirty", "pullRequest"]),
                  let head = status["head"] as? [String: Any],
                  Set(head.keys).isSubset(of: ["kind", "name", "oid"])
            else { throw ClientError.invalidPayload }
            if let pull = status["pullRequest"] as? [String: Any], Set(pull.keys) != ["number", "url"] {
                throw ClientError.invalidPayload
            }
        }
        let result = try JSONDecoder().decode(WireSessionVCSStatus.self, from: data)
        guard result.sessionId == session, safePath(result.cwd) else { throw ClientError.invalidPayload }
        if let status = result.status {
            guard safePath(status.root) else { throw ClientError.invalidPayload }
            switch status.head.kind {
            case .value0, .value2:
                guard let name = status.head.name, !name.isEmpty, PluginCommand.safeText(name, limit: 4096),
                      status.head.oid == nil || status.head.oid == "" else { throw ClientError.invalidPayload }
            case .value1:
                guard status.head.name == nil || status.head.name == "",
                      let oid = status.head.oid, [40, 64].contains(oid.utf8.count),
                      oid.allSatisfy({ $0.isASCII && $0.isHexDigit }) else { throw ClientError.invalidPayload }
            }
            if let pull = status.pullRequest {
                guard status.head.kind == .value0,
                      PullRequestLink.destination(number: pull.number, url: pull.url) != nil
                else { throw ClientError.invalidPayload }
            }
        }
        return result
    }

    private static func safePath(_ value: String) -> Bool {
        value.hasPrefix("/") && PluginCommand.safeText(value, limit: 4096)
    }

    // Foundation collapses duplicate keys; check each object's key set first.
    private static func uniqueMembers(_ data: Data) -> Bool {
        let bytes = Array(data)
        var index = 0, objects: [Set<String>] = []
        while index < bytes.count {
            if bytes[index] == 123 {
                guard objects.count < 8 else { return false }
                objects.append([]); index += 1; continue
            }
            if bytes[index] == 125 {
                guard !objects.isEmpty else { return false }
                objects.removeLast(); index += 1; continue
            }
            guard bytes[index] == 34 else { index += 1; continue }
            let start = index
            index += 1
            while index < bytes.count && bytes[index] != 34 { index += bytes[index] == 92 ? 2 : 1 }
            guard index < bytes.count else { return false }
            index += 1
            let end = index
            while index < bytes.count && [9, 10, 13, 32].contains(bytes[index]) { index += 1 }
            if index < bytes.count && bytes[index] == 58 {
                guard !objects.isEmpty,
                      let key = try? JSONDecoder().decode(String.self, from: Data(bytes[start..<end])),
                      objects[objects.count - 1].insert(key).inserted else { return false }
            }
        }
        return objects.isEmpty
    }
}
