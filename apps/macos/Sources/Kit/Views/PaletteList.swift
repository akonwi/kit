import AppKit
import SwiftUI

/// Palette metrics from Paper's "Command palette" boards.
enum PaletteMetrics {
    static let width: CGFloat = 640
    static let height: CGFloat = 468
    static let searchHeight: CGFloat = 48
    static let footerHeight: CGFloat = 36
    static let rowHeight: CGFloat = 28
    static let listPadding: CGFloat = 6
}

/// Column widths shared by every row of one list, measured from the whole
/// catalog so columns hold still while the query filters it.
struct PaletteColumns: Equatable {
    var name: CGFloat = 104
    var hint: CGFloat = 0

    @MainActor init(_ rows: [PaletteCommand]) {
        let font = Typography.shared.font(size: 12, mono: true)
        func width(_ text: String) -> CGFloat {
            ceil((text as NSString).size(withAttributes: [.font: font]).width)
        }
        name = min(200, max(104, rows.map { width($0.name) }.max() ?? 0))
        let hints = rows.map(\.hint).filter { !$0.isEmpty }
        hint = hints.isEmpty ? 0 : min(140, max(72, hints.map(width).max() ?? 0))
    }
}

/// The search row, rows, and optional footer of one palette list. The parent
/// owns the query and the catalog; this view filters it and owns selection
/// and hover.
struct PaletteListScreen: View {
    @Environment(\.mica) private var theme
    @Binding var query: String
    let placeholder: String
    let catalog: [PaletteCommand]
    /// Lists that pick a value match the whole query; the command list
    /// matches only the command word, leaving the rest as arguments.
    var wholeQuery = false
    /// The centred message when nothing matches, given the match key.
    let emptyMessage: (String) -> String
    var footer: String? = nil
    /// The row selected when the list opens, such as the current model.
    var initialSelection: String? = nil
    /// Tab: the query to complete the selected row into, or nil to ignore.
    var complete: ((PaletteCommand) -> String?)? = nil
    let activate: (PaletteCommand) -> Void
    let dismiss: () -> Void
    @State private var selectedID: String?
    /// The match key the selection was last chosen for.
    @State private var selectionKey = ""
    @State private var hoveredID: String?
    @State private var scrollRequest = 0

    var body: some View {
        let rows = PaletteCommand.filter(catalog, query: query, wholeQuery: wholeQuery)
        let key = PaletteCommand.matchKey(query, wholeQuery: wholeQuery)
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                IconView(.magnifyingGlass, size: 16).foregroundStyle(theme.muted)
                PaletteSearchField(text: $query, placeholder: placeholder, move: move,
                                   complete: completeSelected, submit: activateSelected, cancel: dismiss)
            }
            .padding(.horizontal, 16).frame(height: PaletteMetrics.searchHeight)
            Rule()
            list(rows, key: key).frame(maxHeight: .infinity)
            if let footer {
                Rule()
                Text(footer).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                    .padding(.horizontal, 16)
                    .frame(maxWidth: .infinity, minHeight: PaletteMetrics.footerHeight, alignment: .leading)
            }
        }
        .frame(width: PaletteMetrics.width, height: PaletteMetrics.height)
        .foregroundStyle(theme.text).background(theme.surface)
        .onAppear {
            selectionKey = key
            selectedID = initialSelection.flatMap { id in rows.contains { $0.id == id } ? id : nil }
                ?? PaletteCommand.selection(in: rows, current: nil, key: key)
            scrollRequest += 1
        }
        .onChange(of: key) { reselect(for: key) }
        .onChange(of: catalog.map(\.id)) {
            // A row that left the catalog stays unselected until the pointer
            // or an arrow picks again, so Return never runs a different command.
            if let selectedID, !catalog.contains(where: { $0.id == selectedID }) { self.selectedID = nil }
        }
    }

    @ViewBuilder private func list(_ rows: [PaletteCommand], key: String) -> some View {
        if rows.isEmpty {
            Text(emptyMessage(key)).font(.kit(size: 13)).foregroundStyle(theme.text)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 24).padding(.bottom, 24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            let columns = PaletteColumns(catalog)
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(rows) { row in
                            PaletteRow(row: row, columns: columns, selected: selectedID == row.id,
                                       hovered: hoveredID == row.id)
                                .id(row.id)
                                .onHover { hoveredID = $0 ? row.id : (hoveredID == row.id ? nil : hoveredID) }
                                .onTapGesture { selectedID = row.id; activate(row) }
                                .accessibilityAction { selectedID = row.id; activate(row) }
                        }
                    }.padding(PaletteMetrics.listPadding)
                }
                .onChange(of: scrollRequest) {
                    if let selectedID { proxy.scrollTo(selectedID) }
                }
            }
        }
    }

    /// The rows for the query as it is now. The search field reports edits
    /// before SwiftUI renders them, so key commands read the live query.
    private var liveRows: [PaletteCommand] {
        PaletteCommand.filter(catalog, query: query, wholeQuery: wholeQuery)
    }

    private func reselect(for key: String) {
        guard key != selectionKey else { return }
        selectionKey = key
        selectedID = PaletteCommand.selection(in: liveRows, current: selectedID, key: key)
        scrollRequest += 1
    }

    private func move(_ delta: Int) {
        reselect(for: PaletteCommand.matchKey(query, wholeQuery: wholeQuery))
        let rows = liveRows
        guard !rows.isEmpty else { return }
        let index = selectedID.flatMap { id in rows.firstIndex { $0.id == id } }
        let next = index.map { min(max($0 + delta, 0), rows.count - 1) } ?? (delta > 0 ? 0 : rows.count - 1)
        selectedID = rows[next].id
        hoveredID = nil
        scrollRequest += 1
    }

    private var selectedRow: PaletteCommand? {
        reselect(for: PaletteCommand.matchKey(query, wholeQuery: wholeQuery))
        return selectedID.flatMap { id in liveRows.first { $0.id == id } }
    }

    private func completeSelected() -> String? {
        guard let complete, let row = selectedRow, let completed = complete(row) else { return nil }
        // Completing names the row, so it keeps the selection.
        selectionKey = PaletteCommand.matchKey(completed, wholeQuery: wholeQuery)
        return completed
    }

    private func activateSelected() {
        if let row = selectedRow { activate(row) }
    }
}

/// One 28 pt row: monospace name and argument hint, then the description,
/// or the reason it can't run, and right-aligned metadata.
struct PaletteRow: View {
    @Environment(\.mica) private var theme
    let row: PaletteCommand
    let columns: PaletteColumns
    let selected: Bool
    var hovered = false

    var body: some View {
        let dimmed = row.unavailableReason != nil
        HStack(spacing: 12) {
            Text(row.name).font(.kit(size: 12, design: .monospaced))
                .foregroundStyle(dimmed ? theme.strongBorder : theme.text)
                .lineLimit(1).truncationMode(.tail)
                .frame(width: columns.name, alignment: .leading)
            if columns.hint > 0 {
                Text(row.hint).font(.kit(size: 12, design: .monospaced)).foregroundStyle(theme.muted)
                    .lineLimit(1).truncationMode(.tail)
                    .frame(width: columns.hint, alignment: .leading)
            }
            if let reason = row.unavailableReason {
                HStack(spacing: 6) {
                    IconView(.prohibit, size: 12)
                    Text(row.description.isEmpty ? reason : reason + " · " + row.description)
                        .lineLimit(1).truncationMode(.tail)
                }
                .font(.kit(size: 13)).foregroundStyle(theme.strongBorder)
            } else {
                Text(row.description).font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .lineLimit(1).truncationMode(.tail)
            }
            Spacer(minLength: 0)
            if !row.meta.isEmpty {
                Text(row.meta).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                    .lineLimit(1).fixedSize()
            }
        }
        .padding(.leading, 14).padding(.trailing, 12)
        .frame(height: PaletteMetrics.rowHeight)
        .background(background, in: RoundedRectangle(cornerRadius: 3))
        .contentShape(Rectangle())
        .help(row.prompt.map { $0.sourceLabel + " · " + $0.location } ?? row.unavailableReason ?? row.description)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }

    private var background: Color {
        selected ? theme.accent.opacity(0.18) : hovered ? theme.hover : .clear
    }
}

/// The palette's search field. AppKit delivers arrows, Tab, Return, and Esc
/// as editing commands, so the field handles them before the key-view loop.
struct PaletteSearchField: NSViewRepresentable {
    @Environment(\.mica) private var theme
    @Binding var text: String
    let placeholder: String
    var fontSize: CGFloat = 14
    let move: (Int) -> Void
    let complete: () -> String?
    let submit: () -> Void
    let cancel: () -> Void

    func makeNSView(context: Context) -> NSTextField {
        let field = PaletteTextField()
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.usesSingleLineMode = true
        field.cell?.wraps = false
        field.cell?.isScrollable = true
        field.delegate = context.coordinator
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        context.coordinator.parent = self
        if field.stringValue != text { context.coordinator.replace(text, in: field) }
        field.font = Typography.shared.font(size: fontSize)
        field.textColor = NSColor(theme.text)
        field.placeholderAttributedString = NSAttributedString(string: placeholder, attributes: [
            .font: Typography.shared.font(size: fontSize), .foregroundColor: NSColor(theme.strongBorder)
        ])
        field.setAccessibilityLabel(placeholder)
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    @MainActor final class Coordinator: NSObject, NSTextFieldDelegate {
        var parent: PaletteSearchField
        init(_ parent: PaletteSearchField) { self.parent = parent }

        func controlTextDidChange(_ notification: Notification) {
            if let field = notification.object as? NSTextField { parent.text = field.stringValue }
        }

        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            switch selector {
            case #selector(NSResponder.moveUp(_:)): parent.move(-1)
            case #selector(NSResponder.moveDown(_:)): parent.move(1)
            case #selector(NSResponder.insertTab(_:)):
                if let completed = parent.complete(), let field = control as? NSTextField {
                    replace(completed, in: field)
                    parent.text = completed
                }
            case #selector(NSResponder.insertBacktab(_:)): break
            case #selector(NSResponder.insertNewline(_:)): parent.submit()
            case #selector(NSResponder.cancelOperation(_:)): parent.cancel()
            default: return false
            }
            return true
        }

        /// Replaces the text with the caret after it, so completed commands
        /// are ready for arguments.
        @MainActor func replace(_ text: String, in field: NSTextField) {
            field.stringValue = text
            field.currentEditor()?.selectedRange = NSRange(location: (text as NSString).length, length: 0)
        }
    }
}

/// Takes the keyboard as soon as it is in a window, without waiting for the
/// SwiftUI focus pass that the palette's hidden background would contend with.
final class PaletteTextField: NSTextField {
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard window != nil else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self, let window = self.window else { return }
            window.makeFirstResponder(self)
        }
    }
}

/// The frame shared by the palette's nested screens: a 48 pt title row, the
/// content, and an optional action row, at the palette's width.
struct PaletteSheet<Content: View, Actions: View>: View {
    @Environment(\.mica) private var theme
    @Environment(\.paletteDismiss) private var dismiss
    let title: String
    var detail: String? = nil
    var height: CGFloat? = nil
    @ViewBuilder let content: Content
    @ViewBuilder let actions: Actions

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Text(title).font(.kit(size: 14, weight: .medium)).lineLimit(1)
                Spacer(minLength: 12)
                if let detail {
                    Text(detail).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                        .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                }
            }
            .padding(.horizontal, 16).frame(height: PaletteMetrics.searchHeight)
            Rule()
            content.frame(maxWidth: .infinity, maxHeight: height == nil ? nil : .infinity, alignment: .topLeading)
            if Actions.self != EmptyView.self {
                Rule()
                HStack(spacing: 8) { actions }
                    .padding(.horizontal, 16).frame(height: 48)
            }
        }
        .frame(width: PaletteMetrics.width, height: height)
        .foregroundStyle(theme.text).background(theme.surface)
        .background {
            // Esc closes the palette wherever the keyboard is, including
            // screens without a text field.
            if let dismiss {
                Button("Close") { dismiss() }.keyboardShortcut(.cancelAction)
                    .opacity(0).allowsHitTesting(false).accessibilityHidden(true)
            }
        }
    }
}

/// Closes the palette; set by the palette for its nested screens.
struct PaletteDismiss {
    let action: () -> Void
    func callAsFunction() { action() }
}

extension EnvironmentValues {
    @Entry var paletteDismiss: PaletteDismiss? = nil
}

extension PaletteSheet where Actions == EmptyView {
    init(title: String, detail: String? = nil, height: CGFloat? = nil, @ViewBuilder content: () -> Content) {
        self.init(title: title, detail: detail, height: height, content: content, actions: { EmptyView() })
    }
}

/// A single-line text input inside a palette sheet: 3 pt corners and a
/// 1 pt border, like the composer's controls.
struct PaletteTextInput: View {
    @Environment(\.mica) private var theme
    let prompt: String
    @Binding var text: String
    var axis: Axis = .horizontal

    var body: some View {
        TextField(prompt, text: $text, axis: axis)
            .textFieldStyle(.plain).font(.kit(size: 13))
            .padding(.horizontal, 10).padding(.vertical, 7)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
            .accessibilityLabel(prompt)
    }
}
