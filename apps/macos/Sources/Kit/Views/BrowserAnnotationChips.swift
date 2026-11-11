import KitClient
import SwiftUI

/// A browser annotation waiting in the composer: the element's screenshot,
/// its name, and the comment. Clicking it shows the whole annotation; its
/// × removes it.
struct BrowserAnnotationChip: View {
    @Environment(\.mica) private var theme
    let item: BrowserAnnotationDraft.Item
    let draft: BrowserAnnotationDraft
    @State private var inspecting = false
    @State private var hovering = false

    private var upload: ComposerAttachments.Item? { item.upload.flatMap(draft.screenshot) }

    var body: some View {
        HStack(spacing: 6) {
            Button { inspecting = true } label: {
                HStack(spacing: 6) {
                    if let upload, upload.busy {
                        KitSpinner()
                    } else if let preview = upload?.preview {
                        Image(decorative: preview, scale: 1).resizable().scaledToFill()
                            .frame(width: 28, height: 20)
                            .clipShape(RoundedRectangle(cornerRadius: 3))
                            .overlay(RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border))
                    } else {
                        IconView(upload?.error != nil ? .warning : .cursorClick, size: 12)
                            .foregroundStyle(upload?.error != nil ? theme.warning : theme.muted)
                    }
                    Text(item.capture.element.label)
                        .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                    Text(item.comment).lineLimit(1).truncationMode(.tail).frame(maxWidth: 180, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .popover(isPresented: $inspecting, arrowEdge: .top) {
                BrowserAnnotationDetail(item: item, upload: upload) { draft.dropScreenshot(item.id) }
            }
            Button { draft.remove(item.id) } label: {
                IconView(.x, size: 10).frame(width: 18, height: 18).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .accessibilityLabel("Remove annotation on \(item.capture.element.label)")
        }
        .font(.kit(size: 12))
        .padding(.leading, 6).padding(.trailing, 4).padding(.vertical, 4)
        .background(hovering ? theme.hover : theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.border, lineWidth: 1))
        .onHover { hovering = $0 }
        .help(item.comment)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Browser annotation on \(item.capture.element.label): \(item.comment)")
    }
}

/// The whole of a waiting browser annotation, read-only.
private struct BrowserAnnotationDetail: View {
    @Environment(\.mica) private var theme
    let item: BrowserAnnotationDraft.Item
    let upload: ComposerAttachments.Item?
    let dropScreenshot: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(item.comment).font(.kit(size: 13)).foregroundStyle(theme.text)
                .fixedSize(horizontal: false, vertical: true)
            if let preview = upload?.preview {
                Image(decorative: preview, scale: 1).resizable().scaledToFit()
                    .frame(maxWidth: 360, maxHeight: 160, alignment: .leading)
                    .clipShape(RoundedRectangle(cornerRadius: 4))
            }
            if let error = upload?.error {
                HStack(spacing: 8) {
                    Text("Screenshot: " + error).foregroundStyle(theme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                    Button("Send Without It", action: dropScreenshot).buttonStyle(SmallButtonStyle(bordered: true))
                }.font(.kit(size: 12))
            }
            BrowserAnnotationFacts(lines: BrowserAnnotationText.pageLines(item.capture))
        }
        .padding(14)
        .frame(width: 400, alignment: .leading)
        .background(theme.surface)
    }
}

/// A capture's page lines in a bounded, selectable, monospaced well.
struct BrowserAnnotationFacts: View {
    @Environment(\.mica) private var theme
    let lines: [String]
    /// The lines' own height; a scroll view has none to offer its row.
    @State private var contentHeight: CGFloat = 1

    static let maximumHeight: CGFloat = 180

    var body: some View {
        ScrollView(.vertical) {
            Text(lines.joined(separator: "\n"))
                .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
        }
        .frame(height: min(max(1, contentHeight), Self.maximumHeight))
    }
}

/// The browser annotations a sent message carried, collapsed under a
/// count. Each shows its comment and element, and the page's facts the
/// model got. Whether it's open lives with the transcript, so it survives
/// the row being rebuilt, and opening it measures the row again.
struct TranscriptBrowserAnnotations: View {
    @Environment(\.mica) private var theme
    let annotations: [BrowserAnnotationText.Sent]
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}

    var body: some View {
        if !annotations.isEmpty {
            let open = state.expanded ?? false
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    IconView(open ? .caretDown : .caretRight, size: 10).frame(width: 10)
                    IconView(.cursorClick, size: 12)
                    Text(Self.title(annotations))
                }
                .font(.kit(size: 12)).foregroundStyle(theme.text)
                .frame(height: 20)
                .contentShape(Rectangle())
                .onTapGesture { toggle(open) }
                .accessibilityElement(children: .combine)
                .accessibilityLabel(Self.title(annotations))
                .accessibilityAddTraits(.isButton)
                .accessibilityValue(open ? "Expanded" : "Collapsed")
                .accessibilityAction { toggle(open) }
                if open {
                    VStack(alignment: .leading, spacing: 16) {
                        ForEach(annotations) { annotation in
                            VStack(alignment: .leading, spacing: 8) {
                                Text(heading(annotation))
                                    .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                                    .lineLimit(2).textSelection(.enabled)
                                Text(annotation.comment).font(.kit(size: 14)).foregroundStyle(theme.text)
                                    .textSelection(.enabled)
                                BrowserAnnotationFacts(lines: annotation.page)
                            }
                        }
                    }
                    .padding(.top, 10).padding(.leading, 16)
                }
            }
        }
    }

    private func toggle(_ open: Bool) {
        state.expanded = !open
        if !open { onExpand() }
    }

    static func title(_ annotations: [BrowserAnnotationText.Sent]) -> String {
        "\(annotations.count) browser \(annotations.count == 1 ? "annotation" : "annotations")"
    }

    /// The element's selector on its page's address.
    private func heading(_ annotation: BrowserAnnotationText.Sent) -> String {
        [annotation.value("Selector"), annotation.value("URL")].compactMap { $0 }.joined(separator: " · ")
    }
}
