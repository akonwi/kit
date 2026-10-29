import KitFigures
import SwiftUI

/// A fenced figure (ADR 0054): a fence card titled by the figure's title,
/// or its kind, holding the figure drawn natively. A figure that isn't
/// drawn shows its source with the reason, as a Mermaid diagram does.
struct FigureBlockView: View {
    @Environment(\.mica) private var theme
    @Environment(\.compactCodeBlocks) private var compact
    let block: FigureBlock

    var body: some View {
        switch block.figure {
        case .success(let figure):
            if compact {
                FigureView(figure: figure).compactFence()
            } else {
                FenceCard(label: (figure.title.isEmpty ? figure.kind.rawValue : figure.title).uppercased(), copy: block.fenced) {
                    FigureView(figure: figure).padding(.horizontal, 20).padding(.top, 18).padding(.bottom, 20)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        case .failure(let notDrawn):
            VStack(alignment: .leading, spacing: 6) {
                CodeBlock(language: block.name, source: block.source)
                Text("Figure not drawn: \(notDrawn.reason)")
                    .font(.kit(size: 12)).italic().foregroundStyle(theme.muted)
            }
        }
    }
}

/// A figure's body, drawn for its kind.
struct FigureView: View {
    let figure: Figure

    var body: some View {
        switch figure.kind {
        case .stat: StatFigure(items: figure.items)
        case .bars: BarsFigure(items: figure.items)
        case .meter: MeterFigure(items: figure.items)
        case .timeline: ListingFigure(items: figure.items, marker: TimelineMarker.init, lane: true)
        case .steps: ListingFigure(items: figure.items, marker: StepMarker.init, lane: false)
        case .decision: DecisionFigure(figure: figure)
        case .delta: DeltaFigure(figure: figure)
        case .spec: SpecFigure(items: figure.items)
        case .tree: TreeFigure(figure: figure)
        case .compare: CompareFigure(figure: figure)
        case .score: ScoreFigure(items: figure.items)
        case .stack: StackFigure(figure: figure)
        }
    }
}

/// Large numbers side by side, each over its label, falling to two columns
/// and then one when the card is narrow. A value never shrinks or truncates.
private struct StatFigure: View {
    let items: [Figure.Item]

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .top, spacing: 44) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in StatColumn(item: item) }
            }
            Grid(alignment: .topLeading, horizontalSpacing: 24, verticalSpacing: 14) {
                ForEach(Array(stride(from: 0, to: items.count, by: 2)), id: \.self) { start in
                    GridRow {
                        ForEach(start..<min(start + 2, items.count), id: \.self) { StatColumn(item: items[$0]) }
                    }
                }
            }
            VStack(alignment: .leading, spacing: 14) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in StatColumn(item: item) }
            }
        }
    }
}

private struct StatColumn: View {
    @Environment(\.mica) private var theme
    let item: Figure.Item

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(item.value)
                .font(.kit(size: 26, weight: .semibold)).monospacedDigit().tracking(-0.5)
                .foregroundStyle(item.accent ? theme.accent : item.muted ? theme.muted : theme.text)
                .fixedSize()
            Text(item.label).font(.kit(size: 12)).foregroundStyle(theme.muted)
            if !item.spark.isEmpty {
                Sparkline(points: item.spark).padding(.top, 8)
            }
            if !item.note.isEmpty {
                Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).padding(.top, item.spark.isEmpty ? 2 : 4)
            }
        }
    }
}

/// A stat's sparkline: a muted line over a hover-tone fill, its last point
/// in the accent.
private struct Sparkline: View {
    @Environment(\.mica) private var theme
    let points: [Int]
    static let height: CGFloat = 28

    private func location(_ index: Int, in size: CGSize) -> CGPoint {
        let step = points.count > 1 ? (size.width - 4) / CGFloat(points.count - 1) : 0
        let y = size.height - 2 - CGFloat(points[index]) / 1000 * (size.height - 4)
        return CGPoint(x: 2 + step * CGFloat(index), y: y)
    }

    var body: some View {
        Canvas { context, size in
            guard points.count > 1 else { return }
            var line = Path()
            line.move(to: location(0, in: size))
            for index in points.indices.dropFirst() { line.addLine(to: location(index, in: size)) }
            var area = line
            area.addLine(to: CGPoint(x: location(points.count - 1, in: size).x, y: size.height))
            area.addLine(to: CGPoint(x: location(0, in: size).x, y: size.height))
            area.closeSubpath()
            context.fill(area, with: .color(theme.hover))
            context.stroke(line, with: .color(theme.muted), style: StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
            let last = location(points.count - 1, in: size)
            context.fill(Path(ellipseIn: CGRect(x: last.x - 2.5, y: last.y - 2.5, width: 5, height: 5)), with: .color(theme.accent))
        }
        .frame(width: min(120, max(60, CGFloat(points.count) * 8)), height: Self.height)
        .accessibilityHidden(true)
    }
}

/// One capsule bar per row on a track, with the label and value in lanes
/// of their own. The bold row takes the accent; the others recede.
private struct BarsFigure: View {
    @Environment(\.mica) private var theme
    let items: [Figure.Item]

    private var emphasized: Bool { items.contains(where: \.accent) }
    private var shares: Bool { items.contains { !$0.share.isEmpty } }
    private var notes: Bool { items.contains { !$0.note.isEmpty } }

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 10) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                let quiet = item.muted || (emphasized && !item.accent)
                GridRow {
                    Text(item.label)
                        .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                        .foregroundStyle(item.muted ? theme.muted : theme.text)
                        .lineLimit(1).truncationMode(.middle)
                        .help(item.label)
                    // The track takes what the lanes leave, down to its
                    // least; a long label truncates first.
                    Track(scale: item.scale, fill: item.accent ? theme.accent : quiet ? theme.strongBorder : theme.muted)
                    Text(item.value)
                        .font(.kit(size: 13, weight: item.accent ? .semibold : .regular)).monospacedDigit()
                        .foregroundStyle(quiet ? theme.muted : theme.text)
                        .gridColumnAlignment(.trailing).fixedSize()
                    if shares {
                        Text(item.share).font(.kit(size: 13)).monospacedDigit().foregroundStyle(theme.muted)
                            .gridColumnAlignment(.trailing).fixedSize()
                    }
                    if notes {
                        Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }
                }
            }
        }
    }
}

/// A 6 pt capsule track filled scale thousandths of its width, with an
/// optional target tick a share of the way along. A value above zero
/// always shows.
private struct Track: View {
    @Environment(\.mica) private var theme
    let scale: Int
    let fill: Color
    var tick: CGFloat?

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.dark ? theme.hover : theme.border)
                if scale > 0 {
                    Capsule().fill(fill).frame(width: max(6, geometry.size.width * CGFloat(scale) / 1000))
                }
                if let tick {
                    Rectangle().fill(theme.text).frame(width: 1.5, height: 12)
                        .offset(x: geometry.size.width * tick - 0.75)
                }
            }
            .frame(height: geometry.size.height)
        }
        .frame(minWidth: 64, maxWidth: .infinity).frame(height: 6)
        .accessibilityHidden(true)
    }
}

/// A single meter's share as one track with its value, and the note under
/// it; or rows like bars, each against its own target, the targets' ticks
/// lined up four fifths along. A meter past its target turns warning.
private struct MeterFigure: View {
    @Environment(\.mica) private var theme
    let items: [Figure.Item]

    private var emphasized: Bool { items.contains(where: \.accent) }
    private var notes: Bool { items.contains { !$0.note.isEmpty } }

    var body: some View {
        if let item = items.first, items.count == 1, item.target.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 14) {
                    Track(scale: item.scale, fill: item.over ? theme.warning : theme.accent)
                    Text(item.value)
                        .font(.kit(size: 15, weight: .semibold)).monospacedDigit()
                        .foregroundStyle(item.over ? theme.warning : theme.text).fixedSize()
                }
                if !item.note.isEmpty {
                    Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
            }
        } else {
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 12) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    let quiet = item.muted || (emphasized && !item.accent)
                    GridRow {
                        Text(item.label)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.muted ? theme.muted : theme.text)
                            .lineLimit(1).truncationMode(.middle)
                            .help(item.label)
                        Track(
                            scale: item.scale,
                            fill: item.over ? theme.warning : item.accent ? theme.accent : quiet ? theme.strongBorder : theme.muted,
                            tick: 0.8)
                        Text("\(item.value) / \(item.target)")
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular)).monospacedDigit()
                            .foregroundStyle(item.over ? theme.warning : quiet ? theme.muted : theme.text)
                            .gridColumnAlignment(.trailing).fixedSize()
                        if notes {
                            Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                        }
                    }
                }
            }
        }
    }
}

/// A timeline item's marker: filled when done or when nothing is bold, the
/// accent with a wash ring when now, hollow when still to come.
private struct TimelineMarker: View {
    @Environment(\.mica) private var theme
    let item: Figure.Item

    var body: some View {
        switch item.state {
        case .now:
            Circle().fill(theme.accent).frame(width: 9, height: 9)
                .background(Circle().fill(theme.accent.opacity(0.25)).frame(width: 15, height: 15))
        case .next:
            Circle().strokeBorder(theme.muted, lineWidth: 1.25).frame(width: 8, height: 8)
        case .done, .none:
            Circle().fill(theme.muted).frame(width: 7, height: 7)
        }
    }
}

/// A step's marker: a check when done, and otherwise its number, in the
/// accent when it is the current step.
private struct StepMarker: View {
    @Environment(\.mica) private var theme
    let item: Figure.Item

    var body: some View {
        Group {
            if item.state == .done {
                IconView(.check, size: 11).foregroundStyle(theme.muted)
            } else {
                Text(item.label)
                    .font(.kit(size: 11, weight: item.state == .now ? .semibold : .regular)).monospacedDigit()
                    .foregroundStyle(item.state == .now ? theme.accent : theme.muted)
            }
        }
        .frame(width: 20, height: 20)
        .background(Circle().fill(item.state == .now ? theme.accent.opacity(0.15) : .clear))
        .overlay(Circle().strokeBorder(item.state == .now ? theme.accent.opacity(0.5) : theme.border, lineWidth: 1))
    }
}

/// Timeline items or steps: a marker on a rail that joins it to the next,
/// an optional lane such as a date, then the heading with its note and
/// body, which wrap.
private struct ListingFigure<Marker: View>: View {
    @Environment(\.mica) private var theme
    let items: [Figure.Item]
    let marker: (Figure.Item) -> Marker
    /// Whether each item's label is shown in a lane before its heading.
    let lane: Bool
    /// The widest a lane is drawn, in points.
    private static var maxLane: CGFloat { 128 }

    var body: some View {
        Grid(alignment: .topLeading, horizontalSpacing: 12, verticalSpacing: 0) {
            ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                let last = index == items.count - 1
                GridRow {
                    VStack(spacing: 4) {
                        marker(item).frame(height: 18)
                        if !last {
                            Rectangle().fill(theme.border).frame(width: 1).frame(maxHeight: .infinity)
                        }
                    }
                    .frame(width: 20)
                    if lane {
                        // A long date truncates rather than pushing the
                        // event out of the card; the lane is as wide as
                        // the widest date, up to the cap, and no wider.
                        Text(item.label)
                            .font(.kit(size: 13, weight: item.state == .now ? .semibold : .regular)).monospacedDigit()
                            .foregroundStyle(item.state == .now ? theme.accent : theme.muted)
                            .lineLimit(1).truncationMode(.tail).help(item.label)
                            .frame(maxWidth: Self.maxLane, minHeight: 18, maxHeight: 18, alignment: .leading)
                            .fixedSize()
                    }
                    VStack(alignment: .leading, spacing: 3) {
                        Text(item.value)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.muted || item.state == .done && !lane ? theme.muted : theme.text)
                            .frame(minHeight: 18)
                        if !item.note.isEmpty {
                            Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                        if !item.body.isEmpty {
                            Text(item.body).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                    }
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.bottom, last ? 0 : 14)
                }
            }
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// A decision: its status as a chip washed in the status's color and its
/// date, then each option with its marker and reason, then the closing note.
private struct DecisionFigure: View {
    @Environment(\.mica) private var theme
    let figure: Figure

    private var statusColor: Color {
        switch figure.status {
        case "accepted": theme.success
        case "rejected": theme.danger
        case "superseded": theme.muted
        default: theme.text
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if !figure.status.isEmpty || !figure.date.isEmpty {
                HStack(spacing: 8) {
                    if !figure.status.isEmpty {
                        Text(figure.status.prefix(1).uppercased() + figure.status.dropFirst())
                            .font(.kit(size: 11, weight: .semibold)).foregroundStyle(statusColor)
                            .padding(.horizontal, 6).padding(.vertical, 2)
                            .background(RoundedRectangle(cornerRadius: 4).fill(statusColor.opacity(0.12)))
                    }
                    if !figure.date.isEmpty {
                        Text(Figure.cut(figure.date, 20)).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }
                }
            }
            Grid(alignment: .topLeading, horizontalSpacing: 12, verticalSpacing: 8) {
                ForEach(Array(figure.items.enumerated()), id: \.offset) { _, item in
                    GridRow {
                        DecisionMarker(item: item).frame(width: 12, height: 18)
                        Text(item.label)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.muted ? theme.muted : theme.text)
                            .lineLimit(1).truncationMode(.tail).help(item.label)
                            .frame(minHeight: 18)
                        Text(item.note).font(.kit(size: 13)).foregroundStyle(theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                            .frame(maxWidth: .infinity, minHeight: 18, alignment: .leading)
                    }
                }
            }
            if !figure.note.isEmpty {
                Text(figure.note).font(.kit(size: 13)).foregroundStyle(theme.text)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

/// An option's marker: the choice an accent dot, an option ruled out a
/// muted ×, and one considered a hollow ring.
private struct DecisionMarker: View {
    @Environment(\.mica) private var theme
    let item: Figure.Item

    var body: some View {
        if item.accent {
            Circle().fill(theme.accent).frame(width: 8, height: 8)
        } else if item.muted {
            IconView(.x, size: 10).foregroundStyle(theme.muted)
        } else {
            Circle().strokeBorder(theme.muted, lineWidth: 1.25).frame(width: 8, height: 8)
        }
    }
}

/// Labelled values: the labels in one muted column as wide as the widest,
/// the values wrapping beside them with notes muted under them. The bold
/// row's label is semibold and its value takes the accent.
private struct SpecFigure: View {
    @Environment(\.mica) private var theme
    let items: [Figure.Item]
    /// The widest a label is drawn, in points.
    private static var maxLabel: CGFloat { 180 }

    var body: some View {
        Grid(alignment: .topLeading, horizontalSpacing: 20, verticalSpacing: 8) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                GridRow {
                    Text(item.label)
                        .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                        .foregroundStyle(item.accent ? theme.text : theme.muted)
                        .lineLimit(1).truncationMode(.tail).help(item.label)
                        // As wide as the label, up to the cap, so the
                        // column fits the widest label and no wider.
                        .frame(maxWidth: Self.maxLabel, alignment: .leading)
                        .fixedSize()
                    VStack(alignment: .leading, spacing: 3) {
                        Text(item.value)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.accent ? theme.accent : item.muted ? theme.muted : theme.text)
                        if !item.note.isEmpty {
                            Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                        if !item.body.isEmpty {
                            Text(item.body).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        }
                    }
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// A tree as outline views draw one: a 1 pt guide in the border color for
/// each level above a node, 16 pt apart, and notes in one muted column
/// after the widest name. Bold nodes take the accent.
private struct TreeFigure: View {
    @Environment(\.mica) private var theme
    let figure: Figure
    private var items: [Figure.Item] { figure.items }
    private var notes: Bool { items.contains { !$0.note.isEmpty } }

    /// One guide column: a line through the row, down to its middle where
    /// the last child ends it, or none.
    private func guide(_ kind: Figure.Guide) -> some View {
        VStack(spacing: 0) {
            Rectangle().fill(kind == .none ? .clear : theme.border).frame(width: 1, height: 11)
            Rectangle().fill(kind == .through || kind == .branch ? theme.border : .clear).frame(width: 1, height: 11)
        }
        .padding(.leading, 4).frame(width: 16, alignment: .leading)
    }

    var body: some View {
        let guides = figure.guides
        Grid(alignment: .leading, horizontalSpacing: 32, verticalSpacing: 0) {
            ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                GridRow {
                    HStack(spacing: 0) {
                        ForEach(Array(guides[index].enumerated()), id: \.offset) { _, kind in guide(kind) }
                        Text(Figure.cut(item.label))
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.accent ? theme.accent : item.muted ? theme.muted : theme.text)
                            .lineLimit(1).help(item.label)
                            .padding(.vertical, 2)
                    }
                    .frame(height: 22)
                    if notes {
                        Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }
                }
            }
        }
    }
}

/// Options side by side: centred columns under muted headings and a rule,
/// `yes` as a check and `no` as a muted dash. The pick's column sits on an
/// accent wash and takes the accent.
private struct CompareFigure: View {
    @Environment(\.mica) private var theme
    let figure: Figure

    private func wash(_ column: Int, top: Bool, bottom: Bool) -> some View {
        UnevenRoundedRectangle(topLeadingRadius: top ? 6 : 0, bottomLeadingRadius: bottom ? 6 : 0,
                               bottomTrailingRadius: bottom ? 6 : 0, topTrailingRadius: top ? 6 : 0)
            .fill(column == figure.pick ? theme.accent.opacity(0.10) : .clear)
    }

    @ViewBuilder private func cell(_ text: String, column: Int) -> some View {
        let picked = column == figure.pick
        switch text {
        case "yes": IconView(.check, size: 12).foregroundStyle(picked ? theme.accent : theme.text)
        case "no": Text("–").font(.kit(size: 13)).foregroundStyle(theme.muted)
        default:
            Text(Figure.cut(text)).font(.kit(size: 13, weight: picked ? .semibold : .regular)).monospacedDigit()
                .foregroundStyle(picked ? theme.accent : theme.text).lineLimit(1)
        }
    }

    var body: some View {
        // Cells never wrap: a table wider than the card scrolls sideways,
        // as a Markdown table does.
        ScrollView(.horizontal) { table.fixedSize() }
    }

    private var table: some View {
        Grid(alignment: .center, horizontalSpacing: 0, verticalSpacing: 0) {
            GridRow {
                Color.clear.frame(width: 0, height: 0).gridColumnAlignment(.leading)
                ForEach(Array(figure.headings.enumerated()), id: \.offset) { column, heading in
                    Text(Figure.cut(heading))
                        .font(.kit(size: 12, weight: .semibold))
                        .foregroundStyle(column == figure.pick ? theme.accent : theme.muted)
                        .lineLimit(1)
                        .padding(.horizontal, 14).padding(.top, 6).padding(.bottom, 8)
                        .background(wash(column, top: true, bottom: false))
                }
            }
            Rectangle().fill(theme.border).frame(height: 1).gridCellUnsizedAxes(.horizontal)
            ForEach(Array(figure.items.enumerated()), id: \.offset) { row, item in
                let last = row == figure.items.count - 1
                GridRow {
                    Text(Figure.cut(item.label)).font(.kit(size: 13)).foregroundStyle(theme.text)
                        .lineLimit(1).help(item.label)
                        .padding(.trailing, 20).padding(.vertical, 6)
                        .gridColumnAlignment(.leading)
                    ForEach(Array(item.cells.enumerated()), id: \.offset) { column, text in
                        cell(text, column: column)
                            .frame(maxWidth: .infinity, minHeight: 18)
                            .padding(.horizontal, 14).padding(.vertical, 6)
                            .background(wash(column, top: false, bottom: last))
                    }
                }
            }
        }
    }
}

/// Changes: a label, a sign lane, and the value right-aligned, with the
/// total under a rule; or values before (muted) and after, under muted
/// headings, with the change semibold in success or danger when the figure
/// says which way is better.
private struct DeltaFigure: View {
    @Environment(\.mica) private var theme
    let figure: Figure

    private var paired: Bool { figure.items.contains { !$0.from.isEmpty } }
    private var signs: Bool { figure.items.contains { !$0.sign.isEmpty } }
    private var changes: Bool { figure.items.contains { !$0.change.isEmpty } }
    private var notes: Bool { figure.items.contains { !$0.note.isEmpty } }

    private func trendColor(_ item: Figure.Item, otherwise: Color) -> Color {
        switch item.trend {
        case .better: theme.success
        case .worse: theme.danger
        case .none: otherwise
        }
    }

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 8) {
            if figure.headings.count == 2 {
                GridRow {
                    Color.clear.frame(width: 0, height: 0)
                    Text(figure.headings[0]).font(.kit(size: 12)).foregroundStyle(theme.muted).gridColumnAlignment(.trailing)
                    Color.clear.frame(width: 0, height: 0)
                    Text(figure.headings[1]).font(.kit(size: 12)).foregroundStyle(theme.muted).gridColumnAlignment(.trailing)
                }
            }
            ForEach(Array(figure.items.enumerated()), id: \.offset) { _, item in
                let base = item.muted ? theme.muted : theme.text
                let weight: Font.Weight = item.accent ? .semibold : .regular
                if item.accent && figure.items.count > 1 {
                    Rectangle().fill(theme.border).frame(height: 1).gridCellUnsizedAxes(.horizontal)
                }
                GridRow {
                    Text(item.label).font(.kit(size: 13, weight: weight)).foregroundStyle(base)
                        .lineLimit(1).truncationMode(.middle).help(item.label)
                    if paired {
                        Text(item.from).font(.kit(size: 13)).monospacedDigit().foregroundStyle(theme.muted)
                            .gridColumnAlignment(.trailing).fixedSize()
                        IconView(.arrowRight, size: 11).foregroundStyle(theme.muted)
                    } else if signs {
                        Text(item.sign).font(.kit(size: 13, weight: weight)).monospacedDigit()
                            .foregroundStyle(trendColor(item, otherwise: base))
                            .gridColumnAlignment(.center).fixedSize()
                    }
                    Text(item.value).font(.kit(size: 13, weight: weight)).monospacedDigit()
                        .foregroundStyle(item.accent ? theme.accent : paired ? base : trendColor(item, otherwise: base))
                        .gridColumnAlignment(.trailing).fixedSize()
                    if changes {
                        Text(item.change).font(.kit(size: 13, weight: .semibold)).monospacedDigit()
                            .foregroundStyle(trendColor(item, otherwise: base))
                            .gridColumnAlignment(.trailing).fixedSize()
                    }
                    if notes {
                        Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }
                }
            }
        }
    }
}

/// Ratings as 9 pt dots 5 pt apart, with the value as written. Filled dots
/// take the row's color: the accent for the bold row, the strong border for
/// the others, and muted when nothing is bold. Empty dots are the border.
private struct ScoreFigure: View {
    @Environment(\.mica) private var theme
    let items: [Figure.Item]

    private var emphasized: Bool { items.contains(where: \.accent) }
    private var notes: Bool { items.contains { !$0.note.isEmpty } }

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 10) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                let quiet = item.muted || (emphasized && !item.accent)
                let fill = item.accent ? theme.accent : quiet ? theme.strongBorder : theme.muted
                GridRow {
                    Text(item.label)
                        .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                        .foregroundStyle(item.muted ? theme.muted : theme.text)
                        .lineLimit(1).truncationMode(.middle).help(item.label)
                    HStack(spacing: 5) {
                        ForEach(0..<item.slots, id: \.self) { slot in
                            ScoreDot(halves: min(max(item.filled - slot * 2, 0), 2), fill: fill)
                        }
                    }
                    .accessibilityHidden(true)
                    Text(item.value)
                        .font(.kit(size: 13, weight: item.accent ? .semibold : .regular)).monospacedDigit()
                        .foregroundStyle(quiet ? theme.muted : theme.text).fixedSize()
                    if notes {
                        Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }
                }
            }
        }
    }
}

/// One 9 pt score dot: empty in the border color, or filled by halves.
private struct ScoreDot: View {
    @Environment(\.mica) private var theme
    let halves: Int
    let fill: Color

    var body: some View {
        ZStack(alignment: .leading) {
            Circle().fill(theme.border)
            if halves > 0 {
                Circle().fill(fill)
                    .mask(alignment: .leading) { Rectangle().frame(width: halves == 2 ? 9 : 4.5) }
            }
        }
        .frame(width: 9, height: 9)
    }
}

/// Rows split into shaded segments 2 pt apart on an 8 pt track, each
/// segment 100%, 65%, 40%, then 25% of the row's color, with each row's
/// total after it and a legend of muted swatches under them. With a shared
/// scale a shorter row leaves the rest of its track on the hover tone.
private struct StackFigure: View {
    @Environment(\.mica) private var theme
    let figure: Figure
    static let shades: [Double] = [1, 0.65, 0.40, 0.25]

    private var emphasized: Bool { figure.items.contains(where: \.accent) }
    private var notes: Bool { figure.items.contains { !$0.note.isEmpty } }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 10) {
                ForEach(Array(figure.items.enumerated()), id: \.offset) { _, item in
                    let quiet = item.muted || (emphasized && !item.accent)
                    GridRow {
                        Text(item.label)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular))
                            .foregroundStyle(item.muted ? theme.muted : theme.text)
                            .lineLimit(1).truncationMode(.middle).help(item.label)
                        StackTrack(item: item, fill: item.accent ? theme.accent : quiet ? theme.strongBorder : theme.muted)
                        Text(item.value)
                            .font(.kit(size: 13, weight: item.accent ? .semibold : .regular)).monospacedDigit()
                            .foregroundStyle(quiet ? theme.muted : theme.text)
                            .gridColumnAlignment(.trailing).fixedSize()
                        if notes {
                            Text(item.note).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                        }
                    }
                }
            }
            HStack(spacing: 14) {
                ForEach(Array(figure.headings.enumerated()), id: \.offset) { index, name in
                    HStack(spacing: 5) {
                        RoundedRectangle(cornerRadius: 2).fill(theme.muted.opacity(Self.shades[min(index, 3)]))
                            .frame(width: 8, height: 8)
                        Text(name).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    }
                }
            }
        }
    }
}

/// A stack row's track: its segments over the scale it spans, the rest on
/// the hover tone.
private struct StackTrack: View {
    @Environment(\.mica) private var theme
    let item: Figure.Item
    let fill: Color

    var body: some View {
        Canvas { context, size in
            let gap: CGFloat = 2
            let span = size.width * CGFloat(item.scale) / 1000
            let shown = item.parts.filter { $0 > 0 }.count
            let room = max(0, span - gap * CGFloat(max(shown - 1, 0)))
            var x: CGFloat = 0
            for (index, part) in item.parts.enumerated() where part > 0 {
                let width = room * CGFloat(part) / 1000
                let rect = CGRect(x: x, y: 0, width: max(width, 1), height: size.height)
                context.fill(Path(roundedRect: rect, cornerRadius: 2), with: .color(fill.opacity(StackFigure.shades[min(index, 3)])))
                x += width + gap
            }
            if span < size.width - 1 {
                let rest = CGRect(x: min(x, span) + (shown > 0 ? gap : 0), y: 0, width: size.width - span - (shown > 0 ? gap : 0), height: size.height)
                if rest.width > 0 { context.fill(Path(roundedRect: rest, cornerRadius: 2), with: .color(theme.hover)) }
            }
        }
        .frame(minWidth: 96, maxWidth: .infinity).frame(height: 8)
        .accessibilityHidden(true)
    }
}
