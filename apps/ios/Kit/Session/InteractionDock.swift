import KitClient
import SwiftUI

/// A request the session is waiting on, in the composer's place: a
/// confirmation, a typed answer, a choice, guided questions, or an
/// authorization. The transcript stays readable above it.
struct InteractionDock: View {
    @Bindable var flow: InteractionFlow
    let interactions: SessionInteractions
    @Environment(\.openURL) private var openURL
    @FocusState private var typing: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
                // A swipe down on the header folds it away to read the
                // transcript; the content below may scroll.
                .contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 16).onEnded { drag in
                    if drag.translation.height > 40, abs(drag.translation.width) < drag.translation.height {
                        interactions.collapsed = true
                    }
                })
            content
            if let error = flow.error {
                Text(error)
                    .font(.system(size: 14))
                    .foregroundStyle(Theme.danger)
                    .lineLimit(3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            actions
            if flow.guided {
                Button("Skip questions", role: .cancel) { interactions.cancel(flow) }
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity)
                    .disabled(!enabled)
            }
        }
        .padding(16)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 28, style: .continuous).strokeBorder(Theme.border, lineWidth: 0.5))
        .shadow(color: .black.opacity(0.12), radius: 20, y: -4)
        .animation(.snappy(duration: 0.2), value: flow.step)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("interaction-dock")
    }

    private var enabled: Bool { interactions.live && !flow.submitting }

    // MARK: Header

    @ViewBuilder
    private var header: some View {
        if flow.guided {
            HStack(spacing: 10) {
                Text("QUESTION \(flow.step + 1) OF \(flow.questions.count)")
                    .font(.system(size: 12, weight: .medium))
                    .tracking(0.6)
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Spacer(minLength: 8)
                StepTrail(flow: flow)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Question \(flow.step + 1) of \(flow.questions.count)")
                CollapseButton { interactions.collapsed = true }
            }
            Text(flow.question.prompt)
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
                .fixedSize(horizontal: false, vertical: true)
        } else {
            HStack(spacing: 10) {
                Text("?")
                    .font(.system(size: 12, weight: .bold))
                    .foregroundStyle(Theme.surface)
                    .frame(width: 20, height: 20)
                    .background(Theme.warning, in: Circle())
                    .accessibilityHidden(true)
                Text(flow.title)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Theme.text)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if let plugin = flow.plugin {
                    Text(plugin)
                        .font(.system(size: 12, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                }
                if interactions.flows.count > 1 {
                    Text("1 of \(interactions.flows.count)")
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.muted)
                }
                CollapseButton { interactions.collapsed = true }
                if flow.mode != .unsupported {
                    Button { interactions.cancel(flow) } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(Theme.muted)
                            .frame(width: 28, height: 28)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(!enabled)
                    .accessibilityLabel("Cancel request")
                }
            }
        }
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if let unsupported = flow.unsupported {
            Label(unsupported, systemImage: "exclamationmark.circle")
                .font(.system(size: 14))
                .foregroundStyle(Theme.danger)
        } else {
            let detail = flow.guided ? flow.question.detail : flow.detail
            if !detail.isEmpty {
                ScrollView {
                    MarkdownBlocks(source: detail)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(maxHeight: 220)
                .fixedSize(horizontal: false, vertical: true)
            }
            if let url = flow.authorizationURL {
                Link(url.absoluteString, destination: url)
                    .font(.system(size: 13))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text("Approve access in your browser, then paste the address it ends up at.")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
            }
            answer
        }
    }

    @ViewBuilder
    private var answer: some View {
        switch flow.mode {
        case .text:
            TextField(placeholder, text: $flow.answers[flow.step].text, axis: .vertical)
                .font(.system(size: 17))
                .lineLimit(1...5)
                .focused($typing)
                .textInputAutocapitalization(flow.authorizationURL == nil ? .sentences : .never)
                .autocorrectionDisabled(flow.authorizationURL != nil)
                .padding(.horizontal, 14)
                .padding(.vertical, 11)
                .background(Theme.raised, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .disabled(!enabled)
                .accessibilityIdentifier("interaction-text")
        case .choice, .multiple:
            OptionList(flow: flow, enabled: enabled) { index in
                if flow.mode == .multiple {
                    interactions.forward(flow) { $0.choose(index) }
                } else {
                    flow.cursor = index
                }
            }
        case .confirm, .unsupported:
            EmptyView()
        }
    }

    private var placeholder: String {
        if flow.guided { return "Type an answer" }
        if flow.authorizationURL != nil { return "Paste the address" }
        return flow.request?.placeholder.flatMap { $0.isEmpty ? nil : $0 } ?? "Type a response"
    }

    // MARK: Actions

    @ViewBuilder
    private var actions: some View {
        HStack(spacing: 10) {
            switch flow.mode {
            case .unsupported:
                DockButton(title: "Cancel request", primary: true, busy: flow.submitting) { interactions.cancel(flow) }
                    .disabled(!enabled)
            case .confirm:
                // Without a default choice the affirmative answer leads.
                let affirmative = !flow.offersChoice || flow.confirming
                confirmButton(false, primary: !affirmative)
                confirmButton(true, primary: affirmative)
            case .text, .choice, .multiple:
                if flow.guided && flow.step > 0 {
                    DockButton(title: "Back", primary: false) { flow.back() }
                        .frame(maxWidth: 110)
                        .disabled(flow.submitting)
                }
                if flow.guided && !flow.question.required {
                    DockButton(title: "Skip", primary: false) { interactions.forward(flow) { $0.skip() } }
                        .frame(maxWidth: 110)
                        .disabled(!enabled)
                }
                if let url = flow.authorizationURL {
                    DockButton(title: "Open", primary: false) { openURL(url) }
                        .disabled(flow.submitting)
                }
                DockButton(title: flow.guided && !flow.isLastStep ? "Next" : "Submit", primary: true, busy: flow.submitting) {
                    interactions.forward(flow) { $0.next() }
                }
                .disabled(!enabled)
            }
        }
    }

    private func confirmButton(_ value: Bool, primary: Bool) -> some View {
        let label = flow.question.optionLabels[value ? "true" : "false"] ?? (value ? "Yes" : "No")
        return DockButton(title: label, primary: primary, busy: primary && flow.submitting) {
            interactions.forward(flow) { $0.confirm(value) }
        }
        .disabled(!enabled)
    }
}

/// Folds the dock into its pill.
private struct CollapseButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: "chevron.down")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Theme.muted)
                .frame(width: 28, height: 28)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Show the transcript")
        .accessibilityIdentifier("collapse-dock")
    }
}

/// A folded request in the composer's place: what it waits on, and Answer to
/// open it again.
struct CollapsedDock: View {
    let flow: InteractionFlow
    let expand: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Text("?")
                .font(.system(size: 12, weight: .bold))
                .foregroundStyle(Theme.surface)
                .frame(width: 20, height: 20)
                .background(Theme.warning, in: Circle())
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text("Waiting for you")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.text)
                Text(flow.title)
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: expand) {
                Text("Answer")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.surface)
                    .padding(.horizontal, 16)
                    .frame(height: 36)
                    .background(Theme.text, in: Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("expand-dock")
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .frame(minHeight: 56)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 28, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 28, style: .continuous))
        .onTapGesture(perform: expand)
        .gesture(DragGesture(minimumDistance: 24).onEnded { drag in
            if drag.translation.height < -40 { expand() }
        })
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("interaction-dock-collapsed")
    }
}

/// A full-width capsule button: black for the answer that leads, grey for the other.
private struct DockButton: View {
    let title: String
    let primary: Bool
    var busy = false
    let action: () -> Void
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        Button(action: action) {
            ZStack {
                Text(title).opacity(busy ? 0 : 1)
                if busy { ProgressView().controlSize(.small).tint(primary ? Theme.surface : Theme.text) }
            }
            .font(.system(size: 17, weight: .semibold))
            .foregroundStyle(primary ? Theme.surface : Theme.text)
            .frame(maxWidth: .infinity, minHeight: 48)
            .background(primary ? Theme.text : Theme.raised, in: Capsule())
            .overlay(Capsule().strokeBorder(primary ? .clear : Theme.border, lineWidth: 0.5))
            .opacity(isEnabled || busy ? 1 : 0.4)
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
    }
}

/// The options of a choice: radio buttons for one, checkboxes for several.
private struct OptionList: View {
    let flow: InteractionFlow
    let enabled: Bool
    let tap: (Int) -> Void

    var body: some View {
        let question = flow.question
        let rows = VStack(spacing: 0) {
            ForEach(Array(question.options.enumerated()), id: \.element) { index, option in
                if index > 0 { Divider().padding(.leading, 48) }
                Button { tap(index) } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 12) {
                        mark(selected(index, option))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(question.optionLabels[option] ?? option)
                                .font(.system(size: 17))
                                .foregroundStyle(Theme.text)
                            if let detail = question.optionDetails[option], !detail.isEmpty {
                                Text(detail)
                                    .font(.system(size: 13))
                                    .foregroundStyle(Theme.muted)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 11)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(!enabled)
                .accessibilityAddTraits(selected(index, option) ? .isSelected : [])
            }
        }
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        // Long lists scroll inside the dock.
        if question.options.count > 5 {
            ScrollView { rows }.frame(maxHeight: 300)
        } else {
            rows
        }
    }

    private func selected(_ index: Int, _ option: String) -> Bool {
        flow.mode == .multiple ? flow.answers[flow.step].choices.contains(option) : flow.cursor == index
    }

    @ViewBuilder
    private func mark(_ on: Bool) -> some View {
        if flow.mode == .multiple {
            Image(systemName: on ? "checkmark.square.fill" : "square")
                .font(.system(size: 20))
                .foregroundStyle(on ? Theme.text : Theme.strongBorder)
                .frame(width: 22)
        } else {
            ZStack {
                Circle().strokeBorder(on ? Theme.text : Theme.strongBorder, lineWidth: on ? 6 : 1.5)
            }
            .frame(width: 22, height: 22)
            .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 6 }
        }
    }
}

/// One dash per guided question: done and current ones dark, the rest light.
private struct StepTrail: View {
    let flow: InteractionFlow

    var body: some View {
        HStack(spacing: 4) {
            ForEach(flow.questions.indices, id: \.self) { index in
                Capsule()
                    .fill(index <= flow.step ? Theme.text : Theme.border)
                    .frame(width: 18, height: 4)
            }
        }
    }
}
