import AppKit
import KitDictation
import KitMermaid
import KitMermaidScene
import SwiftUI

/// What a diagram's annotations need from where it's shown: the session's
/// waiting annotations, to mark their parts and add to them.
struct DiagramAnnotationContext {
    let draft: DiagramAnnotationDraft
}

extension EnvironmentValues {
    /// The session's diagram annotations, where diagrams can be annotated.
    @Entry var diagramAnnotations: DiagramAnnotationContext? = nil
}

/// Annotate mode over a drawn diagram, in the diagram's own points: the
/// pointer outlines the part a click would pick, a click picks it and opens
/// a comment box beside it, and parts with a waiting annotation carry a
/// mark. Outside the mode only the marks show, and the layer takes no
/// clicks.
struct DiagramAnnotationLayer: View {
    @Environment(\.mica) private var theme
    let source: String
    let drawing: KitMermaid.Canvas
    let metrics: MermaidScene.Metrics
    let size: CGSize
    @Bindable var annotator: DiagramAnnotator
    let draft: DiagramAnnotationDraft

    var body: some View {
        ZStack(alignment: .topLeading) {
            ForEach(draft.parts(of: source), id: \.self) { part in
                mark(MermaidHighlight(part, in: drawing, metrics: metrics))
            }
            if annotator.annotating {
                if let hovered = annotator.hovered, hovered != annotator.selected {
                    highlight(MermaidHighlight(hovered, in: drawing, metrics: metrics), picked: false)
                }
                if let selected = annotator.selected {
                    highlight(MermaidHighlight(selected, in: drawing, metrics: metrics), picked: true)
                }
                Color.clear
                    .contentShape(Rectangle())
                    .onContinuousHover(coordinateSpace: .local) { phase in
                        switch phase {
                        case .active(let point): annotator.hover(part(at: point))
                        case .ended: annotator.hover(nil)
                        }
                    }
                    .onTapGesture(coordinateSpace: .local) { point in annotator.click(part(at: point)) }
            }
        }
        .frame(width: size.width, height: size.height, alignment: .topLeading)
        .allowsHitTesting(annotator.annotating)
        .popover(isPresented: commentShown, attachmentAnchor: .rect(.rect(anchor)), arrowEdge: .bottom) {
            if let selected = annotator.selected {
                DiagramCommentBox(annotator: annotator, summary: summary(selected), add: add)
            }
        }
        .accessibilityHidden(!annotator.annotating)
    }

    private var commentShown: Binding<Bool> {
        Binding(get: { annotator.selected != nil }, set: { shown in if !shown { annotator.deselect() } })
    }

    private var anchor: CGRect {
        guard let selected = annotator.selected else { return .zero }
        return MermaidHighlight(selected, in: drawing, metrics: metrics).bounds
    }

    private func part(at point: CGPoint) -> Part? {
        MermaidHighlight.part(at: point, in: drawing, metrics: metrics)
    }

    private func summary(_ part: Part) -> String {
        (try? DiagramAnnotationText.summary(source, part)) ?? ""
    }

    private func add() {
        annotator.commit(to: draft, source: source)
    }

    /// The part the pointer is over, outlined; the picked part, outlined
    /// more strongly over a wash.
    private func highlight(_ highlight: MermaidHighlight, picked: Bool) -> some View {
        ZStack(alignment: .topLeading) {
            highlight.linePath.stroke(theme.accent.opacity(picked ? 0.55 : 0.35),
                                      style: StrokeStyle(lineWidth: picked ? 5 : 4, lineCap: .round, lineJoin: .round))
            if picked { highlight.boxPath.fill(theme.accent.opacity(0.1)) }
            highlight.boxPath.stroke(theme.accent, lineWidth: picked ? 1.5 : 1)
        }
        .frame(width: size.width, height: size.height, alignment: .topLeading)
        .allowsHitTesting(false)
    }

    /// A part with a waiting annotation: a small accent dot at its top
    /// right.
    private func mark(_ highlight: MermaidHighlight) -> some View {
        Circle().fill(theme.accent)
            .overlay(Circle().strokeBorder(theme.raised, lineWidth: 1.5))
            .frame(width: 9, height: 9)
            .offset(x: highlight.bounds.maxX - 5, y: highlight.bounds.minY - 4)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// The comment box on a picked part: what it is, the comment, which can
/// be dictated, and Comment to add it to the message.
struct DiagramCommentBox: View {
    @Environment(\.mica) private var theme
    @Bindable var annotator: DiagramAnnotator
    let summary: String
    let add: () -> Void
    @FocusState private var focused: Bool
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    static let width: CGFloat = 340

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(summary)
                .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .lineLimit(2).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            DictatedCommentField(text: $annotator.comment, focused: $focused, prompt: "Comment on this part…",
                                 accessibilityLabel: "Comment on the selected part", submit: submit,
                                 cancel: { annotator.deselect() })
                .environment(\.dictationSlot, dictationSlot)
            if #available(macOS 26, *), dictation.hasNotice { DictationNotice(dictation: dictation) }
            HStack(spacing: 8) {
                Spacer(minLength: 4)
                Button("Cancel") { annotator.deselect() }.buttonStyle(SmallButtonStyle(bordered: true))
                Button("Comment", action: submit)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(empty || dictating)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .frame(width: Self.width)
        .background(theme.surface)
        .onAppear { focused = true }
        .onDisappear { dictationSlot.reset() }
    }

    private var empty: Bool { annotator.comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private func submit() {
        guard !empty, !dictating else { return }
        add()
    }

    private var dictationSlot: DictationSlot { givenDictation ?? ownDictation }

    @available(macOS 26, *)
    private var dictation: Dictation { dictationSlot.dictation }

    private var dictating: Bool {
        if #available(macOS 26, *) { return dictation.isActive }
        return false
    }
}

/// The button that turns a diagram's Annotate mode on and off, as a card's
/// or pane's header shows it.
struct DiagramAnnotateButton: View {
    @Environment(\.mica) private var theme
    let annotator: DiagramAnnotator
    var margin: CGFloat = 0

    var body: some View {
        Button(annotator.annotating ? "Done" : "Annotate", icon: .cursorClick, size: 10, margin: margin) {
            annotator.toggle()
        }
        .buttonStyle(HoverFillButtonStyle(margin: margin))
        .font(.kit(size: 10))
        .foregroundStyle(annotator.annotating ? theme.accent : theme.muted)
        .help(annotator.annotating ? "Stop annotating" : "Annotate parts of the diagram")
        .accessibilityLabel(annotator.annotating ? "Stop annotating the diagram" : "Annotate the diagram")
    }
}
