import KitDictation
import KitMermaid
import KitMermaidScene
import SwiftUI
import UIKit

// Comments on parts of a transcript's Mermaid diagrams, as on the Mac: a
// part is picked in the diagram's own screen, its comment waits above the
// composer as a chip, and on send it becomes a block of text after the
// message (ADR 0046's annotations).

/// Annotate mode's outlines and marks over a diagram's image, in the
/// image's points, so they ride the zoom. The part a pointer is over is
/// outlined; the picked part is outlined more strongly over a wash; a part
/// with a waiting comment carries an accent dot at its top right.
final class MermaidAnnotationOverlay: UIView {
    let hoveredLines = CAShapeLayer()
    let hoveredBoxes = CAShapeLayer()
    let pickedWash = CAShapeLayer()
    let pickedLines = CAShapeLayer()
    let pickedBoxes = CAShapeLayer()
    let marks = CAShapeLayer()
    private var shown: MermaidMarks?
    private var shownImage: MermaidImage?

    /// A mark's size and where it sits past its part's top right corner.
    static let markSize: CGFloat = 9

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        for line in [hoveredLines, pickedLines] {
            line.fillColor = nil
            line.lineCap = .round
            line.lineJoin = .round
        }
        hoveredLines.lineWidth = 4
        pickedLines.lineWidth = 5
        for box in [hoveredBoxes, pickedBoxes] { box.fillColor = nil }
        hoveredBoxes.lineWidth = 1
        pickedBoxes.lineWidth = 1.5
        pickedWash.strokeColor = nil
        marks.lineWidth = 1.5
        for layer in [hoveredLines, pickedLines, pickedWash, hoveredBoxes, pickedBoxes, marks] {
            self.layer.addSublayer(layer)
        }
        recolor()
        registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (self: Self, _) in self.recolor() }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func show(_ state: MermaidMarks, in image: MermaidImage) {
        guard state != shown || image != shownImage else { return }
        shown = state
        shownImage = image
        let offset = CGAffineTransform(translationX: MermaidImage.margin, y: MermaidImage.margin)
        let picked = state.annotating ? state.picked.map(image.highlight) : nil
        let hovered = state.annotating && state.hovered != state.picked ? state.hovered.map(image.highlight) : nil
        func shape(_ path: Path?) -> CGPath? {
            guard let path, !path.isEmpty else { return nil }
            return path.applying(offset).cgPath
        }
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        hoveredLines.path = shape(hovered?.linePath)
        hoveredBoxes.path = shape(hovered?.boxPath)
        pickedLines.path = shape(picked?.linePath)
        pickedBoxes.path = shape(picked?.boxPath)
        pickedWash.path = pickedBoxes.path
        let dots = CGMutablePath()
        for part in state.marked {
            dots.addEllipse(in: Self.markRect(image.highlight(part)).applying(offset))
        }
        marks.path = state.marked.isEmpty ? nil : dots
        CATransaction.commit()
    }

    /// Where a part's mark is drawn, in the scene's points.
    static func markRect(_ highlight: MermaidHighlight) -> CGRect {
        CGRect(x: highlight.bounds.maxX - 5, y: highlight.bounds.minY - 4, width: markSize, height: markSize)
    }

    private func recolor() {
        func color(_ color: Color, _ opacity: CGFloat = 1) -> CGColor {
            UIColor(color).resolvedColor(with: traitCollection).withAlphaComponent(opacity).cgColor
        }
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        hoveredLines.strokeColor = color(Theme.accent, 0.35)
        hoveredBoxes.strokeColor = color(Theme.accent)
        pickedLines.strokeColor = color(Theme.accent, 0.55)
        pickedBoxes.strokeColor = color(Theme.accent)
        pickedWash.fillColor = color(Theme.accent, 0.1)
        marks.fillColor = color(Theme.accent)
        marks.strokeColor = color(Theme.raised)
        CATransaction.commit()
    }
}

/// The box under the diagram where a picked part's comment is written:
/// what the part is, the comment, which can be dictated, then Cancel and
/// Comment.
struct DiagramCommentEditor: View {
    @Bindable var annotator: DiagramAnnotator
    let summary: String
    let error: String?
    let commit: () -> Void
    @FocusState private var focused: Bool
    @State private var dictation = Dictation(engine: DictationSupport.engine)

    /// Comment waits for dictated words to arrive.
    private var canSave: Bool { annotator.canCommit && !dictation.isActive }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(summary)
                .font(.system(size: 13, design: .monospaced))
                .foregroundStyle(Theme.muted)
                .lineLimit(2)
                .truncationMode(.tail)
                .accessibilityIdentifier("diagram-comment-target")
            FieldDictation(dictation: dictation, enabled: true, fill: fill) {
                TextField("Add a comment", text: $annotator.comment, axis: .vertical)
                    .font(.system(size: 16))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1...6)
                    .focused($focused)
                    .padding(.vertical, 10)
                    .accessibilityIdentifier("diagram-comment-field")
            }
            .padding(.leading, 12)
            .padding(.trailing, 4)
            .frame(minHeight: 42)
            .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
            .overlay {
                RoundedRectangle(cornerRadius: Theme.cardRadius)
                    .strokeBorder(focused || dictation.isActive ? Theme.accent : Theme.border, lineWidth: 1)
            }
            if let problem = dictation.problem {
                DictationProblemLine(problem: problem, dismiss: dictation.dismissProblem)
            }
            if let error {
                Text(error)
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel") { annotator.deselect() }
                    .buttonStyle(CommentButtonStyle(prominent: false))
                    .accessibilityIdentifier("diagram-comment-cancel")
                Button("Comment", action: commit)
                    .buttonStyle(CommentButtonStyle(prominent: true))
                    .disabled(!canSave)
                    .accessibilityIdentifier("diagram-comment-save")
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface)
        .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 0.5) }
        .onAppear { focused = true }
        .dictationLifecycle(dictation)
        .onChange(of: dictation.isActive) { _, active in
            if active { focused = false }
        }
        .onChange(of: annotator.comment) { dictation.dismissProblem() }
    }

    /// Puts dictated words in the comment, after what's typed.
    private func fill(_ text: String) {
        let current = annotator.comment
        annotator.comment = DictationText.join(
            current.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : current, text)
    }
}

/// The session's diagram comments above the composer, a chip each, to go
/// with the next message. A tap shows the comment and what the model gets
/// about its part; × deletes it.
struct DiagramChips: View {
    let draft: DiagramAnnotationDraft
    let enabled: Bool

    var body: some View {
        if !draft.isEmpty {
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(draft.items) { item in
                        DiagramChip(item: item, enabled: enabled) { draft.remove(item.id) }
                    }
                }
                .padding(.horizontal, 4)
                .padding(.vertical, 4)
            }
            .scrollIndicators(.hidden)
            .accessibilityIdentifier("diagram-chips")
        }
    }
}

private struct DiagramChip: View {
    let item: DiagramAnnotationDraft.Item
    let enabled: Bool
    let remove: () -> Void
    @State private var inspecting = false

    var body: some View {
        HStack(spacing: 0) {
            Button { inspecting = true } label: {
                HStack(spacing: 6) {
                    Image(systemName: "point.3.connected.trianglepath.dotted")
                        .font(.system(size: 13))
                        .foregroundStyle(Theme.accent)
                    Text(item.summary)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                        .frame(maxWidth: 220, alignment: .leading)
                        .fixedSize(horizontal: true, vertical: false)
                }
                .padding(.leading, 12)
                .padding(.trailing, 4)
                .frame(minHeight: 36)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Comment on \(item.summary), \(item.annotation.comment)")
            .accessibilityHint("Shows the comment")
            .accessibilityIdentifier("diagram-chip")
            .popover(isPresented: $inspecting) {
                DiagramChipDetail(item: item)
                    .presentationCompactAdaptation(.popover)
            }
            Button(action: remove) {
                Image(systemName: "xmark")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 32, height: 36)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!enabled)
            .accessibilityLabel("Delete comment on \(item.summary)")
            .accessibilityIdentifier("diagram-chip-delete")
        }
        .padding(.trailing, 4)
        .background(Theme.surface, in: Capsule())
        .overlay { Capsule().strokeBorder(Theme.border, lineWidth: 0.5) }
    }
}

/// A waiting diagram comment in full: the comment, then what the model
/// gets about its part.
private struct DiagramChipDetail: View {
    let item: DiagramAnnotationDraft.Item

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Text(item.annotation.comment)
                    .font(.system(size: 16))
                    .foregroundStyle(Theme.text)
                    .fixedSize(horizontal: false, vertical: true)
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(Array(item.facts.enumerated()), id: \.offset) { _, line in
                        Text(line)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(Theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            .padding(14)
            .frame(width: 320, alignment: .leading)
        }
        .frame(maxHeight: 360)
    }
}

/// The diagram comments a sent message carried, a card each under its
/// text: the part it's on and the comment.
struct SentDiagramComments: View {
    let annotations: [DiagramAnnotationText.Sent]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(annotations) { annotation in
                VStack(alignment: .leading, spacing: 6) {
                    Label {
                        Text(annotation.target)
                            .font(.system(size: 13, design: .monospaced))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(2)
                            .truncationMode(.tail)
                    } icon: {
                        Image(systemName: "point.3.connected.trianglepath.dotted")
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.accent)
                    }
                    .labelStyle(.titleAndIcon)
                    Text(annotation.comment)
                        .font(.system(size: 16))
                        .foregroundStyle(Theme.text)
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cardRadius - 2))
                .accessibilityElement(children: .combine)
            }
        }
    }
}
