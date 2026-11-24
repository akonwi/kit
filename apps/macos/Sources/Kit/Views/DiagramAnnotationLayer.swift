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
    /// The session's server can't take comments now: the marks stay, and
    /// annotate mode takes no clicks until it can.
    @Environment(\.sessionReadOnly) private var readOnly
    let source: String
    let drawing: KitMermaid.Canvas
    let metrics: MermaidScene.Metrics
    let size: CGSize
    @Bindable var annotator: DiagramAnnotator
    let draft: DiagramAnnotationDraft
    @State private var popover = AnchoredPopover()
    @State private var anchor = DiagramAnchor()

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
        .allowsHitTesting(annotator.annotating && !readOnly)
        .background(DiagramAnchorView(anchor: anchor))
        .onChange(of: annotator.selected) { presentComment() }
        .onChange(of: readOnly) { if readOnly { popover.dismiss() } }
        .onDisappear { popover.dismiss() }
        .accessibilityHidden(!annotator.annotating)
    }

    /// The comment box on the picked part, as the app's other popovers
    /// open: a flat card below the part, or above it near the screen's
    /// bottom. Closing it, by Esc or a click elsewhere, lets go of the part.
    private func presentComment() {
        guard !readOnly, let selected = annotator.selected else { popover.dismiss(); return }
        let bounds = MermaidHighlight(selected, in: drawing, metrics: metrics).bounds
        guard let frame = anchor.screenFrame(of: bounds), let window = anchor.view?.window else { return }
        let roomBelow = frame.minY - (window.screen?.visibleFrame.minY ?? 0)
        let annotator = annotator, draft = draft, source = source
        let content = DiagramCommentBox(annotator: annotator, summary: summary(selected),
                                        add: { annotator.commit(to: draft, source: source) })
            .popoverCard(theme)
        popover.show(anchor: frame, parent: window, edge: roomBelow < Self.roomForComment ? .above : .below,
                     content: content) {
            if annotator.selected == selected { annotator.deselect() }
        }
    }

    /// The space below a part that the comment box needs to open there.
    static let roomForComment: CGFloat = 180

    private func part(at point: CGPoint) -> Part? {
        MermaidHighlight.part(at: point, in: drawing, metrics: metrics)
    }

    private func summary(_ part: Part) -> String {
        (try? DiagramAnnotationText.summary(source, part)) ?? ""
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

/// Where a diagram's annotation layer is on screen, for the comment box to
/// open beside a part.
@MainActor final class DiagramAnchor {
    weak var view: NSView?

    /// rect, in the layer's points from its top left, on screen.
    func screenFrame(of rect: CGRect) -> NSRect? {
        guard let view, let window = view.window else { return nil }
        let local = NSRect(x: rect.minX, y: view.isFlipped ? rect.minY : view.bounds.height - rect.maxY,
                           width: rect.width, height: rect.height)
        return window.convertToScreen(view.convert(local, to: nil))
    }
}

private struct DiagramAnchorView: NSViewRepresentable {
    let anchor: DiagramAnchor

    func makeNSView(context: Context) -> NSView {
        let view = PassThroughView()
        anchor.view = view
        return view
    }

    func updateNSView(_ view: NSView, context: Context) { anchor.view = view }

    private final class PassThroughView: NSView {
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
    }
}

/// The comment box on a picked part: what it is, the comment, which can
/// be dictated, and Comment to add it to the message.
struct DiagramCommentBox: View {
    @Environment(\.mica) private var theme
    @Bindable var annotator: DiagramAnnotator
    let summary: String
    let add: () -> Void
    @State private var focused = false
    @State private var focusRequest = 0
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    static let width: CGFloat = 340

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(summary)
                .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .lineLimit(2).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
            DictatedCommentField(text: $annotator.comment, focused: $focused, focusRequest: focusRequest,
                                 prompt: "Comment on this part…",
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
        .onAppear { focusRequest += 1 }
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
    /// The session's server can't take comments now.
    @Environment(\.sessionReadOnly) private var readOnly
    let annotator: DiagramAnnotator
    var margin: CGFloat = 0

    var body: some View {
        Button(annotator.annotating ? "Done" : "Annotate", icon: .cursorClick, size: 10, margin: margin) {
            annotator.toggle()
        }
        .buttonStyle(HoverFillButtonStyle(margin: margin))
        .disabled(readOnly && !annotator.annotating)
        .font(.kit(size: 10))
        .foregroundStyle(annotator.annotating ? theme.accent : theme.muted)
        .help(annotator.annotating ? "Stop annotating" : "Annotate parts of the diagram")
        .accessibilityLabel(annotator.annotating ? "Stop annotating the diagram" : "Annotate the diagram")
    }
}
