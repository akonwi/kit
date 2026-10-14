import SwiftUI

/// The target picker under the Diff pane's target button: the server's
/// catalog (the working tree, this branch against each base, and recent
/// commits) with a search field. It lists targets afresh each time it opens,
/// showing the last list meanwhile. A pick switches the pane and closes.
struct DiffTargetPicker: View {
    let model: DiffState
    let theme: MicaTheme
    let choose: (WireDiffTargetEntry) -> Void
    let dismiss: () -> Void
    @State private var query = ""
    @State private var selection = 0

    static let width: CGFloat = 320
    static let rowHeight: CGFloat = 28
    static let maxListHeight: CGFloat = 280

    private var targets: [WireDiffTargetEntry] { Self.filter(model.catalog?.targets ?? [], query: query) }

    var body: some View {
        let targets = targets
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").font(.system(size: 11)).foregroundStyle(theme.muted)
                PaletteSearchField(text: $query, placeholder: "Search targets…", fontSize: 12,
                    move: { step in
                        guard !targets.isEmpty else { return }
                        selection = min(max(selection + step, 0), targets.count - 1)
                    },
                    complete: { nil },
                    submit: { if targets.indices.contains(selection) { choose(targets[selection]) } },
                    cancel: dismiss)
            }
            .padding(.horizontal, 10).frame(height: 32)
            Rule()
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if targets.isEmpty {
                            Text(model.catalog == nil ? "Loading diff targets…" : "No matching targets")
                                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, 8).frame(height: Self.rowHeight)
                        }
                        ForEach(Array(targets.enumerated()), id: \.element.targetId) { index, target in
                            row(target, selected: index == selection)
                                .onHover { if $0 { selection = index } }
                                .id(target.targetId)
                        }
                    }.padding(4)
                }
                .frame(height: min(Self.maxListHeight, CGFloat(max(targets.count, 1)) * Self.rowHeight + 8))
                .onChange(of: selection) {
                    if targets.indices.contains(selection) { proxy.scrollTo(targets[selection].targetId) }
                }
            }
        }
        .frame(width: Self.width)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .onAppear { selection = targets.firstIndex { $0.targetId == model.target?.targetId } ?? 0 }
        .onChange(of: query) { selection = 0 }
        .popoverCard(theme)
        .accessibilityLabel("Compare")
    }

    private func row(_ target: WireDiffTargetEntry, selected: Bool) -> some View {
        Button { choose(target) } label: {
            HStack(spacing: 8) {
                Image(systemName: "checkmark").font(.kit(size: 10, weight: .semibold))
                    .opacity(target.targetId == model.target?.targetId ? 1 : 0).frame(width: 12)
                DiffTargetName(target: target)
                Spacer(minLength: 8)
                Text(Self.meta(target)).font(.kit(size: 11)).foregroundStyle(theme.muted).fixedSize()
            }
            .padding(.horizontal, 8)
            .frame(height: Self.rowHeight)
            .background(selected ? theme.accent.opacity(0.18) : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .environment(\.mica, theme)
        .accessibilityAddTraits(target.targetId == model.target?.targetId ? .isSelected : [])
    }

    /// The row's right column: its saved notes, or what kind of target it is.
    static func meta(_ target: WireDiffTargetEntry) -> String {
        if let count = target.annotationCount, count > 0 { return count == 1 ? "1 note" : "\(count) notes" }
        return switch target.kind {
        case "working_tree": "uncommitted"
        case "branch": "branch"
        case "commit": "commit"
        default: ""
        }
    }

    /// Targets whose label, subject, refs, or short hash contain every word of `query`.
    static func filter(_ targets: [WireDiffTargetEntry], query: String) -> [WireDiffTargetEntry] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return targets }
        return targets.filter { target in
            let metadata = target.metadata
            let haystack = [metadata.label, metadata.subject, metadata.refName, metadata.baseRefName, metadata.abbreviatedOid]
                .compactMap { $0 }.joined(separator: " ").lowercased()
            return words.allSatisfy { haystack.contains($0) }
        }
    }
}

/// The ⋯ menu at the end of the Diff pane's header: wrapping, then the
/// layout. Side by side is dimmed with the reason when the tile is too
/// narrow. A pick applies and closes.
struct DiffViewMenu: View {
    let model: DiffState
    let theme: MicaTheme
    let splitFits: Bool
    let dismiss: () -> Void
    @State private var hovered: Int?

    static let width: CGFloat = 188
    static let rowHeight: CGFloat = 26
    static let narrowNote = "Tile is too narrow"

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            item(0, "Wrap lines", checked: model.wrapLines) { model.wrapLines.toggle() }
            Rule().padding(.vertical, 4)
            item(1, DiffLayout.unified.rawValue, checked: model.layout == .unified) { model.layout = .unified }
            item(2, DiffLayout.split.rawValue, checked: model.layout == .split,
                 note: splitFits ? nil : Self.narrowNote) { model.layout = .split }
        }
        .padding(4)
        .frame(width: Self.width)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .popoverCard(theme)
        .accessibilityLabel("View options")
    }

    private func item(_ index: Int, _ title: String, checked: Bool, note: String? = nil,
                      action: @escaping () -> Void) -> some View {
        Button {
            dismiss()
            action()
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: "checkmark").font(.kit(size: 10, weight: .semibold))
                    .opacity(checked && note == nil ? 1 : 0).frame(width: 12)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.kit(size: 12))
                        .foregroundStyle(note == nil ? theme.text : theme.muted.opacity(0.7))
                    if let note { Text(note).font(.kit(size: 11)).foregroundStyle(theme.muted) }
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 8)
            .frame(height: note == nil ? Self.rowHeight : 40)
            .background(note == nil && hovered == index ? theme.accent.opacity(0.18) : .clear,
                        in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(note != nil)
        .onHover { hovered = $0 ? index : (hovered == index ? nil : hovered) }
        .accessibilityAddTraits(checked ? .isSelected : [])
    }
}
