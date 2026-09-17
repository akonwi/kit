import SwiftUI

struct InteractionCard: View {
    @Environment(\.mica) private var theme
    @Bindable var flow: InteractionFlow
    let pendingCount: Int
    let finish: (Bool) -> Void
    private enum Field: Hashable { case answer, option(String), back, skip, submit, cancel }
    @FocusState private var focused: Field?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: flow.confirmation ? "checkmark.shield" : "text.bubble")
                    .foregroundStyle(theme.accent)
                Text(flow.title).font(.kit(size: 13, weight: .semibold))
                Spacer()
                if pendingCount > 1 { Text("1 of \(pendingCount) requests").font(.kit(size: 11)).foregroundStyle(theme.muted) }
                Button { finish(true) } label: { Image(systemName: "xmark").frame(width: 24, height: 24) }
                    .buttonStyle(.plain).focusable().focused($focused, equals: .cancel).accessibilityLabel("Cancel request")
            }.padding(.horizontal, 18).padding(.top, 14)
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if flow.request?.kind.rawValue == "guided", let detail = flow.request?.detail, !detail.isEmpty {
                        Text(detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    }
                    Text(flow.question.prompt).font(.kit(size: 16, weight: .medium))
                    if !flow.question.detail.isEmpty {
                        if flow.confirmation { HighlightedCodeText(language: "bash", source: flow.question.detail) }
                        else { Text(flow.question.detail).font(.kit(size: 12)).foregroundStyle(theme.muted) }
                    }
                    if flow.question.kind == .text {
                        InteractionTextInput(text: $flow.answers[flow.step].text, prompt: flow.question.prompt,
                            submit: { submitStep() }, previous: { flow.step = max(0, flow.step - 1) }, cancel: { finish(true) })
                            .frame(height: 64).padding(12)
                            .background(theme.surface, in: RoundedRectangle(cornerRadius: 10))
                            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.focusBorder))
                    } else {
                        VStack(spacing: 6) {
                            ForEach(flow.question.options, id: \.self) { option in
                                let selected = flow.answers[flow.step].choices.contains(option)
                                Button { focused = .option(option); flow.choose(option) } label: {
                                    HStack(spacing: 10) {
                                        Image(systemName: flow.question.kind == .multiselect
                                              ? (selected ? "checkmark.square.fill" : "square")
                                              : (selected ? "largecircle.fill.circle" : "circle"))
                                            .foregroundStyle(selected ? theme.accent : theme.muted)
                                        VStack(alignment: .leading, spacing: 3) {
                                            Text(flow.question.optionLabels[option] ?? option)
                                            if let detail = flow.optionDetail(option), !detail.isEmpty {
                                                Text(detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
                                            }
                                        }.multilineTextAlignment(.leading)
                                        Spacer(minLength: 0)
                                    }.padding(.horizontal, 12).padding(.vertical, 9)
                                        .background(selected ? theme.accent.opacity(0.12) : theme.surface, in: RoundedRectangle(cornerRadius: 8))
                                        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(focused == .option(option) ? theme.focusBorder : selected ? theme.accent : theme.border, lineWidth: focused == .option(option) ? 2 : 1))
                                        .contentShape(Rectangle())
                                }.buttonStyle(.plain).focusable().focused($focused, equals: .option(option)).accessibilityAddTraits(selected ? .isSelected : [])
                            }
                        }.font(.kit(size: 13))
                    }
                }.frame(maxWidth: .infinity, alignment: .leading).padding(18)
            }.frame(maxHeight: 250)
            if let error = flow.error { Text(error).font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12) }
            Rule()
            HStack(spacing: 10) {
                if flow.questions.count > 1 {
                    Button { flow.step -= 1 } label: { Image(systemName: "chevron.left") }
                        .buttonStyle(MicaButtonStyle(compact: true)).disabled(flow.step == 0).focusable().focused($focused, equals: .back).accessibilityLabel("Previous question")
                    Text("\(flow.step + 1) / \(flow.questions.count)").foregroundStyle(theme.muted)
                }
                Spacer()
                if !flow.question.required {
                    Button("Skip") { advance(skip: true) }.buttonStyle(MicaButtonStyle(compact: true))
                        .focusable().focused($focused, equals: .skip)
                }
                Button(flow.step == flow.questions.count - 1 ? "Submit" : "Continue") { advance() }
                    .buttonStyle(MicaButtonStyle(primary: true, compact: true)).disabled(!flow.canContinue)
                    .focusable().focused($focused, equals: .submit)
                    .keyboardShortcut(.return, modifiers: .command)
                    .help("Continue or submit (⌘Return)")
            }.font(.kit(size: 12)).padding(12)
        }
        .disabled(flow.submitting)
        .foregroundStyle(theme.text).background(theme.raised)
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.border))
        .padding(.horizontal, 28).padding(.bottom, 12)
        .frame(maxWidth: 844)
        .task(id: flow.step) {
            await Task.yield()
            focused = flow.question.kind == .text ? .answer : (flow.question.options.first { flow.answers[flow.step].choices.contains($0) } ?? flow.question.options.first).map(Field.option)
        }
        .onKeyPress(.tab, phases: .down) { press in
            if press.modifiers.contains(.shift) { flow.step = max(0, flow.step - 1) }
            else { submitStep(selectBoolean: false) }
            return .handled
        }
        .onKeyPress(keys: [.upArrow, .downArrow, "j", "k"]) { press in
            guard case let .option(option) = focused,
                  let index = flow.question.options.firstIndex(of: option) else { return .ignored }
            let options = flow.question.options
            let delta = (press.key == .upArrow || press.key == "k") ? -1 : 1
            focused = .option(options[min(max(0, index + delta), options.count - 1)])
            return .handled
        }
        .onKeyPress(.return) { submitStep(); return .handled }
        .onKeyPress(.space) {
            guard case let .option(option) = focused else { return .ignored }
            flow.choose(option)
            if flow.question.kind != .multiselect { advance() }
            return .handled
        }
        .onExitCommand { finish(true) }
    }
    private func submitStep(selectBoolean: Bool = true) {
        let option: String?
        if case let .option(value) = focused { option = value }
        else { option = flow.question.options.first { flow.answers[flow.step].choices.contains($0) } ?? flow.question.options.first }
        if flow.submitStep(option: option, selectBoolean: selectBoolean) { finish(false) }
    }

    private func advance(skip: Bool = false) {
        if flow.advance(skip: skip) { finish(false) }
    }
}
