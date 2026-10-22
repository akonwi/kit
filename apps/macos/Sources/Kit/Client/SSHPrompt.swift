import Foundation

/// A question ssh asks through the app's askpass helper (S9).
enum SSHPrompt: Equatable, Sendable {
    /// The first connection to a host: its key, to confirm before ssh adds it
    /// to known_hosts.
    case hostKey(SSHHostKey)
    /// A password for `account`, such as `me@devbox`, or for the host when
    /// ssh doesn't say whose.
    case password(account: String?)
    /// The passphrase of the private key at `key`.
    case passphrase(key: String)
    /// A yes-or-no question, such as confirming a key's use.
    case confirm(String)
    /// Any other question, such as a 2FA code, shown as the host sent it.
    case other(String)

    /// Classifies the text ssh passes its askpass helper. `kind` is the
    /// helper's `SSH_ASKPASS_PROMPT`: `confirm` for a yes-or-no question.
    init(_ text: String, kind: String? = nil) {
        let text = Self.cleaned(text)
        if kind == "confirm" {
            self = .confirm(text)
        } else if let key = SSHHostKey(prompt: text) {
            self = .hostKey(key)
        } else if let key = Self.match(text, #"^Enter passphrase for key '(.+)':\s*$"#) {
            self = .passphrase(key: key)
        } else if let account = Self.match(text, #"^([^\s'()]+@[^\s'()]+)'s password:\s*$"#) {
            self = .password(account: account)
        } else if let account = Self.match(text, #"^\(([^\s()]+@[^\s()]+)\) Password:\s*$"#) {
            // Keyboard-interactive password prompts name the account in parentheses.
            self = .password(account: account)
        } else if text.range(of: #"^Password:\s*$"#, options: .regularExpression) != nil {
            self = .password(account: nil)
        } else {
            self = .other(text)
        }
    }

    /// The prompt without control characters, trailing space, or the
    /// account prefix ssh adds to keyboard-interactive questions.
    var display: String {
        switch self {
        case .other(let text), .confirm(let text):
            text.replacingOccurrences(of: #"^\([^\s()]+@[^\s()]+\) "#, with: "", options: .regularExpression)
        case .hostKey, .password, .passphrase: ""
        }
    }

    /// Whether the answer may be saved in the Keychain.
    var saveable: Bool {
        switch self {
        case .password, .passphrase: true
        case .hostKey, .confirm, .other: false
        }
    }

    private static func match(_ text: String, _ pattern: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let result = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(result.range(at: 1), in: text) else { return nil }
        return String(text[range])
    }

    /// Text from a host is shown, so control and format characters, which
    /// could move the cursor, reorder text, or hide it, are dropped. Lines
    /// are kept.
    static func cleaned(_ text: String) -> String {
        let scalars = text.unicodeScalars.filter {
            $0 == "\n" || ($0.properties.generalCategory != .control && $0.properties.generalCategory != .format)
        }
        return String(String.UnicodeScalarView(scalars)).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// A host key ssh hasn't seen, from its first-connection question.
struct SSHHostKey: Equatable, Sendable {
    /// The host as ssh names it, such as `devbox (10.0.4.12)`.
    let host: String
    /// The key type, such as `ED25519`.
    let type: String
    /// The key's fingerprint, such as `SHA256:…`.
    let fingerprint: String

    init(host: String, type: String, fingerprint: String) {
        (self.host, self.type, self.fingerprint) = (host, type, fingerprint)
    }

    /// Reads ssh's question: "The authenticity of host 'devbox (10.0.4.12)'
    /// can't be established. ED25519 key fingerprint is SHA256:…. … Are you
    /// sure you want to continue connecting (yes/no/[fingerprint])?" OpenSSH
    /// 10 writes "fingerprint is: SHA256:…", without the period.
    init?(prompt: String) {
        // ssh's own question starts so; a host's keyboard-interactive
        // question starts with "(user@host) ", so a host can't fake it.
        guard prompt.hasPrefix("The authenticity of host '"), prompt.contains("continue connecting"),
              let host = Self.capture(prompt, #"The authenticity of host '([^']+)' can't be established"#),
              let type = Self.capture(prompt, #"(?m)^(\S+) key fingerprint is:? (\S+?)\.?$"#),
              let fingerprint = Self.capture(prompt, #"(?m)^\S+ key fingerprint is:? (\S+?)\.?$"#) else { return nil }
        (self.host, self.type, self.fingerprint) = (host, type, fingerprint)
    }

    private static func capture(_ text: String, _ pattern: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let result = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(result.range(at: 1), in: text) else { return nil }
        return String(text[range])
    }
}

/// A host whose key no longer matches the one this Mac saved (S10). ssh
/// refuses it, and so does Kit: the user removes the old key in Terminal.
struct SSHHostKeyChange: Equatable, Sendable {
    /// The name the key is saved under, which `ssh-keygen -R` removes.
    let host: String
    /// The saved key's fingerprint, when it could be read.
    var saved: String?
    /// The fingerprint of the key the host sent.
    let now: String
    /// The known_hosts file holding the saved key, when ssh named one other
    /// than the default.
    var file: String?

    /// The command that removes the saved key.
    var removeCommand: String {
        var command = "ssh-keygen"
        if let file { command += " -f " + Self.quoted(file) }
        return command + " -R " + Self.quoted(host)
    }

    /// Reads ssh's "REMOTE HOST IDENTIFICATION HAS CHANGED" warning.
    init?(diagnostics: String) {
        guard diagnostics.contains("REMOTE HOST IDENTIFICATION HAS CHANGED"),
              let now = Self.capture(diagnostics, #"sent by the remote host is\s*\n\s*(\S+)\.?"#) else { return nil }
        self.now = now.hasSuffix(".") ? String(now.dropLast()) : now
        // ssh doesn't always print how to remove the key, but always names
        // the file that holds it.
        let removal = Self.capture(diagnostics, #"ssh-keygen -f '([^']+)' -R '([^']+)'"#)
            ?? Self.capture(diagnostics, #"(?m)^Offending \S+ key in (.+):\d+$"#)
        let name = Self.capture(diagnostics, #"ssh-keygen -f '[^']+' -R '([^']+)'"#)
            ?? Self.capture(diagnostics, #"Host key for (\S+) has changed"#)
        guard let name else { return nil }
        host = name
        let defaultFile = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".ssh/known_hosts").path
        file = removal.flatMap { $0 == defaultFile ? nil : $0 }
    }

    init(host: String, saved: String?, now: String, file: String? = nil) {
        (self.host, self.saved, self.now, self.file) = (host, saved, now, file)
    }

    /// The fingerprint `ssh-keygen -l -F` prints for the saved key.
    static func savedFingerprint(fromLookup output: String) -> String? {
        for line in output.split(separator: "\n") where !line.hasPrefix("#") {
            if let fingerprint = line.split(separator: " ").first(where: { $0.hasPrefix("SHA256:") || $0.hasPrefix("MD5:") }) {
                return String(fingerprint)
            }
        }
        return nil
    }

    private static func capture(_ text: String, _ pattern: String) -> String? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let result = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(result.range(at: 1), in: text) else { return nil }
        return String(text[range])
    }

    private static func quoted(_ value: String) -> String {
        // Brackets, as in `[host]:2222`, are a pattern to the shell, so they're quoted.
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-+:@%"))
        guard value.unicodeScalars.contains(where: { !safe.contains($0) }) else { return value }
        return "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}
