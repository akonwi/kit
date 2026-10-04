import SwiftUI

/// A compact, scrollable alternative to an unbounded native model menu.
struct ComposerModelPicker: View {
    @Environment(\.mica) private var theme
    let models: [WireModelCapability]
    let selectedID: String
    let select: (String) -> Void
    let reload: () -> Void
    let dismiss: () -> Void
    /// Names the configured target when it is not the attached session.
    var title: String? = nil
    @State private var selection = 0
    @State private var hoveredID: String?
    @FocusState private var focused: Bool

    static let width: CGFloat = 320
    static let rowHeight: CGFloat = 30
    static let maxHeight: CGFloat = 300
    static let titleHeight: CGFloat = 28

    static func listHeight(count: Int) -> CGFloat {
        min(maxHeight, CGFloat(max(count, 1)) * rowHeight + 8)
    }

    static func contextLabel(_ tokens: Int) -> String? {
        guard tokens > 0 else { return nil }
        if tokens >= 1_000_000 {
            return String(format: "%.1fM", locale: Locale(identifier: "en_US_POSIX"), Double(tokens) / 1_000_000)
        }
        if tokens >= 1_000 { return "\(tokens / 1_000)k" }
        return "\(tokens)"
    }

    static func rowDescription(_ model: WireModelCapability) -> String {
        let context = contextLabel(model.contextWindow).map { " · \($0) context" } ?? ""
        return "\(model.id)\(context)"
    }

    var body: some View {
        VStack(spacing: 0) {
            if let title {
                Text(title).font(.kit(size: 11, weight: .medium)).foregroundStyle(theme.muted)
                    .lineLimit(1).truncationMode(.middle)
                    .padding(.horizontal, 12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .frame(height: Self.titleHeight)
                    .accessibilityAddTraits(.isHeader)
                Rule()
            }
            list
        }
        .frame(width: Self.width)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .focusable().focused($focused)
        .onKeyPress(.downArrow) {
            guard !models.isEmpty else { return .ignored }
            hoveredID = nil
            selection = min(selection + 1, models.count - 1)
            return .handled
        }
        .onKeyPress(.upArrow) {
            guard !models.isEmpty else { return .ignored }
            hoveredID = nil
            selection = max(0, selection - 1)
            return .handled
        }
        .onKeyPress(.return) {
            if models.isEmpty { reload(); return .handled }
            guard models.indices.contains(selection) else { return .ignored }
            select(models[selection].id)
            return .handled
        }
        .onExitCommand(perform: dismiss)
    }

    private var list: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    if models.isEmpty {
                        Button("Reload models", action: reload)
                            .buttonStyle(.plain)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .frame(height: Self.rowHeight)
                            .padding(.horizontal, 8)
                    } else {
                        ForEach(Array(models.enumerated()), id: \.element.id) { index, model in
                            Button { selection = index; select(model.id) } label: {
                                HStack(spacing: 8) {
                                    Image(systemName: "checkmark")
                                        .opacity(selectedID == model.id ? 1 : 0)
                                        .frame(width: 14)
                                    Text(model.name).lineLimit(1).truncationMode(.middle)
                                        .layoutPriority(1)
                                    Spacer(minLength: 4)
                                    Text(model.provider).foregroundStyle(theme.muted)
                                        .lineLimit(1).truncationMode(.middle)
                                        .frame(maxWidth: 90, alignment: .trailing)
                                    Text(Self.contextLabel(model.contextWindow) ?? "")
                                        .foregroundStyle(theme.muted).monospacedDigit()
                                        .frame(width: 48, alignment: .trailing)
                                }
                                .font(.kit(size: 12))
                                .padding(.horizontal, 8)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .frame(height: Self.rowHeight)
                                .background(selection == index || hoveredID == model.id ? theme.raised : Color.clear)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .help(Self.rowDescription(model))
                            .accessibilityLabel("\(model.name) · \(Self.rowDescription(model))")
                            .onHover { hovering in
                                hoveredID = hovering ? model.id : nil
                                if hovering { selection = index }
                            }
                            .id(model.id)
                        }
                    }
                }.padding(4)
            }
            .frame(height: Self.listHeight(count: models.count))
            .onAppear {
                selection = models.firstIndex(where: { $0.id == selectedID }) ?? 0
                if models.indices.contains(selection) { proxy.scrollTo(models[selection].id, anchor: .center) }
                focused = true
            }
            .onChange(of: selection) {
                if hoveredID == nil, models.indices.contains(selection) {
                    proxy.scrollTo(models[selection].id, anchor: .center)
                }
            }
        }
    }
}
