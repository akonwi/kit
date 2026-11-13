import KitMermaid
import SwiftUI

/// A diagram annotation waiting in the composer: the part it's on and the
/// comment. Clicking it shows what the model gets about the part; its ×
/// removes it.
struct DiagramAnnotationChip: View {
    @Environment(\.mica) private var theme
    let item: DiagramAnnotationDraft.Item
    let draft: DiagramAnnotationDraft
    @State private var inspecting = false
    @State private var hovering = false

    var body: some View {
        HStack(spacing: 6) {
            Button { inspecting = true } label: {
                HStack(spacing: 6) {
                    IconView(.treeStructure, size: 12).foregroundStyle(theme.muted)
                    Text(item.summary)
                        .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                        .lineLimit(1).truncationMode(.tail).frame(maxWidth: 160, alignment: .leading).fixedSize()
                    Text(item.annotation.comment).lineLimit(1).truncationMode(.tail)
                        .frame(maxWidth: 180, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .popover(isPresented: $inspecting, arrowEdge: .top) {
                DiagramAnnotationDetail(comment: item.annotation.comment, facts: item.facts)
            }
            Button { draft.remove(item.id) } label: {
                IconView(.x, size: 10).frame(width: 18, height: 18).contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(theme.muted)
            .accessibilityLabel("Remove annotation on \(item.summary)")
        }
        .font(.kit(size: 12))
        .padding(.leading, 6).padding(.trailing, 4).padding(.vertical, 4)
        .background(hovering ? theme.hover : theme.surface, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.border, lineWidth: 1))
        .onHover { hovering = $0 }
        .help(item.annotation.comment)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Diagram annotation on \(item.summary): \(item.annotation.comment)")
    }
}

/// The whole of a waiting diagram annotation, read-only: the comment, then
/// what the model gets about the part.
private struct DiagramAnnotationDetail: View {
    @Environment(\.mica) private var theme
    let comment: String
    let facts: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(comment).font(.kit(size: 13)).foregroundStyle(theme.text)
                .fixedSize(horizontal: false, vertical: true)
            BrowserAnnotationFacts(lines: facts)
        }
        .padding(14)
        .frame(width: 400, alignment: .leading)
        .background(theme.surface)
    }
}

/// The diagram annotations a sent message carried, collapsed under a count.
/// Each shows the part it's on and its comment. Whether it's open lives
/// with the transcript, so it survives the row being rebuilt, and opening
/// it measures the row again.
struct TranscriptDiagramAnnotations: View {
    @Environment(\.mica) private var theme
    let annotations: [DiagramAnnotationText.Sent]
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}

    var body: some View {
        if !annotations.isEmpty {
            let open = state.expanded ?? false
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    IconView(open ? .caretDown : .caretRight, size: 10).frame(width: 10)
                    IconView(.treeStructure, size: 12)
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
                                Text(annotation.target)
                                    .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                                    .lineLimit(2).textSelection(.enabled)
                                Text(annotation.comment).font(.kit(size: 14)).foregroundStyle(theme.text)
                                    .textSelection(.enabled)
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

    static func title(_ annotations: [DiagramAnnotationText.Sent]) -> String {
        "\(annotations.count) diagram \(annotations.count == 1 ? "annotation" : "annotations")"
    }
}
