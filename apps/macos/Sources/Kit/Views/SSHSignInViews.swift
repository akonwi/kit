import SwiftUI

/// Asks the user ssh's questions for one window (S9). A view that signs in
/// to an SSH server holds one and shows its question as a sheet, so the
/// question appears in the window the user is using.
@MainActor @Observable
final class SSHSignIn {
    /// The question on screen, with the name of the server that asks it.
    private(set) var question: Question?
    /// A sign-in is underway.
    private(set) var signingIn = false
    @ObservationIgnored private var pending: CheckedContinuation<SSHAnswer?, Never>?

    struct Question: Identifiable, Equatable {
        let id = UUID()
        let server: String
        let destination: String
        let request: SSHPromptRequest
    }

    /// Signs in to `connection`, asking this window's questions, and starts
    /// its Kit server first when `start` is set.
    func signIn(_ connection: ServerConnection, start: Bool = false) async {
        guard !signingIn else { return }
        signingIn = true
        defer { signingIn = false }
        guard case .ssh(let destination) = connection.connector else { return connection.checkNow() }
        let name = connection.name
        await connection.signIn(start: start) { [weak self] request in
            await self?.ask(Question(server: name, destination: destination, request: request))
        }
    }

    private func ask(_ question: Question) async -> SSHAnswer? {
        // One question at a time: a new one cancels any left open.
        pending?.resume(returning: nil)
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                pending = continuation
                self.question = question
            }
        } onCancel: {
            Task { @MainActor [weak self] in self?.answer(nil) }
        }
    }

    /// Answers the question on screen; nil cancels connecting.
    func answer(_ answer: SSHAnswer?) {
        question = nil
        pending?.resume(returning: answer)
        pending = nil
    }
}

/// How the S9 sheet presents one of ssh's questions.
struct SSHPromptPresentation: Equatable {
    enum Field: Equatable {
        /// No field: the question is answered by the button.
        case none
        case secure
        case plain
    }

    let title: String
    let message: String
    /// The key to check, for a new host: its description and fingerprint.
    let key: (label: String, fingerprint: String)?
    /// The label above the field, as the host wrote it.
    let label: String?
    let field: Field
    /// Whether the answer may be saved, and whether saving starts on.
    let save: Bool?
    let action: String
    /// Why the question is asked again.
    let error: String?
    /// The text sent when the question is answered by its button.
    let fixedAnswer: String?

    static func == (a: Self, b: Self) -> Bool {
        a.title == b.title && a.message == b.message && a.key?.label == b.key?.label && a.key?.fingerprint == b.key?.fingerprint
            && a.label == b.label && a.field == b.field && a.save == b.save && a.action == b.action && a.error == b.error
            && a.fixedAnswer == b.fixedAnswer
    }

    init(server: String, destination: String, request: SSHPromptRequest) {
        switch request.prompt {
        case .hostKey(let key):
            title = "Connect to \(server)?"
            message = "This Mac hasn’t connected to \(server) before. Check that its key matches before you connect."
            self.key = ("\(key.type) key · \(key.host)", key.fingerprint)
            (label, field, save, action, fixedAnswer) = (nil, .none, nil, "Connect", "yes")
            error = nil
        case .password(let account):
            title = "Password for \(account ?? destination)"
            message = "\(server) asked for a password. Kit sends it to ssh and doesn’t keep it unless you choose to."
            (key, label, field, save, action, fixedAnswer) = (nil, nil, .secure, false, "Connect", nil)
            error = request.retry ? "That password didn’t work. Try again." : nil
        case .passphrase(let path):
            title = "Unlock your SSH key"
            message = "\(server) needs \(ServerStatePresentation.abbreviated(path)), which is locked with a passphrase."
            (key, label, field, save, action, fixedAnswer) = (nil, nil, .secure, true, "Unlock", nil)
            error = request.retry ? "That passphrase didn’t unlock the key. Try again." : nil
        case .confirm:
            title = "\(server) is asking"
            message = request.prompt.display
            (key, label, field, save, action, fixedAnswer) = (nil, nil, .none, nil, "Continue", "yes")
            error = nil
        case .other:
            title = "\(server) is asking"
            message = "Answer the host’s question to keep connecting."
            let text = request.prompt.display
            label = text
            // A PIN or password is never shown as it's typed.
            field = text.range(of: #"(?i)\b(pin|password|passphrase|secret)\b"#, options: .regularExpression) != nil ? .secure : .plain
            (key, save, action, fixedAnswer) = (nil, nil, "Continue", nil)
            error = request.retry ? "That answer didn’t work. Try again." : nil
        }
    }
}

/// One of ssh's questions as a sheet (S9).
struct SSHPromptSheet: View {
    @Environment(\.mica) private var theme
    let question: SSHSignIn.Question
    let answer: (SSHAnswer?) -> Void
    @State private var text = ""
    @State private var save: Bool
    @FocusState private var focused: Bool

    init(question: SSHSignIn.Question, answer: @escaping (SSHAnswer?) -> Void) {
        self.question = question
        self.answer = answer
        let presentation = SSHPromptPresentation(server: question.server, destination: question.destination, request: question.request)
        _save = State(initialValue: presentation.save ?? false)
    }

    var body: some View {
        let presentation = SSHPromptPresentation(server: question.server, destination: question.destination, request: question.request)
        VStack(alignment: .leading, spacing: 18) {
            DialogHeading(title: presentation.title, message: presentation.message)
            if let key = presentation.key {
                VStack(alignment: .leading, spacing: 4) {
                    Text(key.label).font(.kit(size: 11)).foregroundStyle(theme.muted)
                    Text(key.fingerprint).font(.kit(size: 11, design: .monospaced)).textSelection(.enabled)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 10).padding(.horizontal, 12)
                .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
                .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border) }
            }
            if presentation.field != .none {
                VStack(alignment: .leading, spacing: 8) {
                    if let label = presentation.label {
                        Text(label).font(.kit(size: 12, design: .monospaced)).fixedSize(horizontal: false, vertical: true)
                    }
                    Group {
                        if presentation.field == .secure {
                            SecureField("", text: $text)
                        } else {
                            TextField("", text: $text)
                        }
                    }
                    .textFieldStyle(.plain).font(.kit(size: 13, design: .monospaced))
                    .focused($focused).onSubmit { submit(presentation) }
                    .padding(.horizontal, 10).frame(height: 30)
                    .fieldBox(theme, focused: focused)
                    .accessibilityLabel(presentation.label ?? presentation.title)
                    if presentation.save != nil {
                        Toggle("Save in Keychain", isOn: $save).toggleStyle(.checkbox).font(.kit(size: 12))
                    }
                    if let error = presentation.error { ProviderErrorLine(message: error) }
                }
            }
            Rule()
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel") { answer(nil) }.buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction)
                Button(presentation.action) { submit(presentation) }
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .keyboardShortcut(.defaultAction)
                    .disabled(presentation.field != .none && text.isEmpty)
            }
        }
        .font(.kit(size: 13)).foregroundStyle(theme.text)
        .padding(24).frame(width: 460)
        .background(theme.surface)
        .interactiveDismissDisabled()
        .task { focused = true }
    }

    private func submit(_ presentation: SSHPromptPresentation) {
        if let fixed = presentation.fixedAnswer { return answer(SSHAnswer(text: fixed)) }
        guard !text.isEmpty else { return }
        answer(SSHAnswer(text: text, save: presentation.save != nil && save))
    }
}

extension View {
    /// Shows `signIn`'s questions as sheets on this view's window.
    func sshSignInSheet(_ signIn: SSHSignIn) -> some View {
        modifier(SSHSignInSheet(signIn: signIn))
    }
}

private struct SSHSignInSheet: ViewModifier {
    @Environment(\.mica) private var theme
    let signIn: SSHSignIn

    func body(content: Content) -> some View {
        content.sheet(item: Binding(get: { signIn.question }, set: { if $0 == nil { signIn.answer(nil) } })) { question in
            SSHPromptSheet(question: question) { signIn.answer($0) }
                .environment(\.mica, theme)
        }
    }
}
