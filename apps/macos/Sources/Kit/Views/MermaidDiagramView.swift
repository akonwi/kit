import AppKit
import KitMermaid
import SwiftUI

/// A Mermaid diagram drawn natively from Kit's layout (ADR 0046). The layout
/// places everything on a grid of cells, as the terminal shows it; the scene
/// draws its figures as whole shapes (filled nodes and participants, notes,
/// and block frames), its lines as strokes through the cells' centers with
/// curved bends, and its text in the interface font centered where the
/// terminal centers it.
struct MermaidScene: Equatable {
    /// What an element is colored as, resolved against the theme when drawn.
    enum Ink: Hashable, CaseIterable {
        /// Flowchart edges and sequence messages.
        case line
        /// Sequence lifelines and block dividers.
        case lifeline
        case nodeFill, nodeStroke
        case noteFill, noteStroke
        case blockStroke
        /// A block keyword's tag.
        case tagFill
        case accent
        case text
        case secondary
        /// The card behind the diagram, to clear lines from behind text.
        case card
    }

    enum TextStyle: Equatable {
        /// A flowchart node's text.
        case node
        /// The text of a box standing in for what is drawn elsewhere.
        case elsewhere
        /// A participant's name.
        case name
        /// A flowchart edge's text, on a pill over its line.
        case edgeLabel
        /// A sequence message's text, above its line.
        case message
        case note
        case title
    }

    enum Element: Equatable {
        case stroke(Path, Ink, width: CGFloat, dash: [CGFloat], phase: CGFloat)
        case fill(Path, Ink)
        /// A figure's outline, filled if fill is set and lifted by a soft shadow.
        case shape(Path, fill: Ink?, stroke: Ink, width: CGFloat, dash: [CGFloat], lifted: Bool)
        /// Text centered on, or starting at, a point on its row's middle.
        case text(String, at: CGPoint, TextStyle, leading: Bool)
        /// A block's keyword as a tag, then its condition, starting at a
        /// point on the row of the line they sit on.
        case legend(keyword: String, condition: String, at: CGPoint)
    }

    let size: CGSize
    let elements: [Element]
    /// Horizontal slices of the drawing, each a whole number of cells tall,
    /// drawn as separate layers so a tall diagram stays within the largest
    /// texture the GPU can show.
    let bands: [Band]

    struct Band: Equatable {
        let top: CGFloat
        let height: CGFloat
        /// Indexes of the elements that reach into this band.
        let elements: [Int]
    }

    /// The tallest a band may be, in points.
    static let bandLimit: CGFloat = 1024

    /// Cell size and stroke scale for the font the diagram is laid out in.
    struct Metrics: Equatable {
        let cell: CGSize
        /// A light line's width; heavy lines are twice it.
        let line: CGFloat
        /// Device pixels per point, to keep lines on whole pixels.
        let scale: CGFloat
    }

    init(_ drawing: KitMermaid.Canvas, metrics: Metrics) {
        var builder = Builder(drawing: drawing, metrics: metrics)
        builder.build()
        size = CGSize(width: CGFloat(drawing.width) * metrics.cell.width,
                      height: CGFloat(drawing.height) * metrics.cell.height)
        elements = builder.elements
        bands = Self.bands(of: elements, rows: drawing.height, metrics: metrics)
    }

    private static func bands(of elements: [Element], rows: Int, metrics: Metrics) -> [Band] {
        let rowsPerBand = max(1, Int(bandLimit / metrics.cell.height))
        let count = max(1, (rows + rowsPerBand - 1) / rowsPerBand)
        let height = CGFloat(rowsPerBand) * metrics.cell.height
        var members = Array(repeating: [Int](), count: count)
        for (index, element) in elements.enumerated() {
            let reach: (CGFloat, CGFloat)
            switch element {
            case .stroke(let path, _, let width, _, _):
                let bounds = path.boundingRect
                reach = (bounds.minY - width, bounds.maxY + width)
            case .fill(let path, _):
                let bounds = path.boundingRect
                reach = (bounds.minY - 1, bounds.maxY + 1)
            case .shape(let path, _, _, let width, _, let lifted):
                let bounds = path.boundingRect
                let spread = width + (lifted ? 4 : 0)
                reach = (bounds.minY - spread, bounds.maxY + spread)
            case .text(_, let at, _, _), .legend(_, _, let at):
                reach = (at.y - metrics.cell.height, at.y + metrics.cell.height)
            }
            let first = max(0, Int((reach.0 / height).rounded(.down)))
            let last = min(count - 1, Int((reach.1 / height).rounded(.down)))
            if first <= last { for band in first...last { members[band].append(index) } }
        }
        let total = CGFloat(rows) * metrics.cell.height
        return members.enumerated().map { band, indexes in
            let top = CGFloat(band) * height
            return Band(top: top, height: max(0, min(height, total - top)), elements: indexes)
        }
    }

    private struct Builder {
        let drawing: KitMermaid.Canvas
        let metrics: Metrics
        var elements: [Element] = []

        /// For each cell, the figure that draws it, if any.
        private var owner: [Int] = []
        /// Columns that sequence lifelines run down.
        private var lifelines: Set<Int> = []
        /// Lines joined through the gaps the terminal leaves around edge text.
        private var bridges: [Int: Lines] = [:]
        /// Edge text beside its line rather than on it, by row and column.
        private var besideLine: Set<[Int]> = []
        /// The column of the line edge text sits on when it runs down
        /// through the text, by row and column.
        private var onColumn: [[Int]: Int] = [:]

        init(drawing: KitMermaid.Canvas, metrics: Metrics) {
            self.drawing = drawing
            self.metrics = metrics
        }

        /// Text gathered across a row's neighboring cells of one role.
        private struct Run {
            var text: String
            let x: Int
            var end: Int
            let y: Int
            let role: Role
            let owner: Int?
        }

        mutating func build() {
            claimCells()
            let runs = collectRuns()
            if drawing.kind == .flowchart { bridgeLabels(runs) }
            // Lifelines run unbroken behind what crosses them.
            for column in lifelines.sorted() {
                guard let head = drawing.figures.first(where: { lifeline(of: $0) == column }) else { continue }
                var path = Path()
                path.move(to: CGPoint(x: center(column, head.y1).x, y: outline(head).maxY))
                path.addLine(to: edge(column, drawing.height - 1, .down))
                elements.append(.stroke(path, .lifeline, width: metrics.line, dash: [], phase: 0))
            }
            // Blocks sit behind the events inside them.
            for figure in drawing.figures where figure.kind == .block {
                elements.append(.shape(roundedRect(outline(figure), radius: 6), fill: nil, stroke: .blockStroke,
                                       width: metrics.line, dash: [], lifted: false))
            }
            for y in 0..<drawing.height {
                for x in 0..<drawing.width {
                    let figure = owner(x, y)
                    switch drawing.glyph(x, y) {
                    case .lines(let lines):
                        if let figure {
                            strokeBorderLines(lines, x, y, drawing.figures[figure])
                        } else {
                            strokeLines(lines, x, y, drawing.role(x, y))
                        }
                    case .mark(let mark):
                        if figure == nil { drawMark(mark, x, y, ink(drawing.role(x, y))) }
                    case .blank, .text, .covered:
                        if let lines = bridges[y * drawing.width + x] { strokeLines(lines, x, y, .arrow) }
                    }
                }
            }
            for figure in drawing.figures where figure.kind != .block {
                drawFigure(figure)
            }
            placeText(runs)
        }

        /// The text on each row, gathered across neighboring cells of one
        /// role; a single space joins the text on either side of it.
        private func collectRuns() -> [Run] {
            var runs: [Run] = []
            for y in 0..<drawing.height {
                var run: Run?
                for x in 0..<drawing.width {
                    let figure = owner(x, y)
                    switch drawing.glyph(x, y) {
                    case .text(let text):
                        let role = drawing.role(x, y)
                        if var current = run, current.role == role, current.owner == figure {
                            current.text += (current.end + 1 < x ? " " : "") + text
                            current.end = x
                            run = current
                        } else {
                            if let current = run { runs.append(current) }
                            run = Run(text: text, x: x, end: x, y: y, role: role, owner: figure)
                        }
                        continue
                    case .covered:
                        run?.end = x
                        continue
                    case .blank:
                        if let current = run, current.end == x - 1 { continue }
                    case .lines, .mark:
                        break
                    }
                    if let current = run { runs.append(current) }
                    run = nil
                }
                if let current = run { runs.append(current) }
            }
            return runs
        }

        /// The terminal breaks an edge's line around its text; joins the
        /// line through the gap so the text's pill sits on an unbroken edge.
        private mutating func bridgeLabels(_ runs: [Run]) {
            for run in runs where run.owner == nil && run.role == .label {
                if let left = reach(from: run.x, run.y, toward: .left),
                   let right = reach(from: run.end, run.y, toward: .right) {
                    for x in (left + 1)..<right { bridges[run.y * drawing.width + x] = Lines(right: .light, left: .light) }
                    continue
                }
                var bridged = false
                for x in run.x...run.end {
                    if let top = reach(from: run.y, x, toward: .up), let bottom = reach(from: run.y, x, toward: .down) {
                        for y in (top + 1)..<bottom { bridges[y * drawing.width + x] = Lines(up: .light, down: .light) }
                        onColumn[[run.y, run.x]] = x
                        bridged = true
                        break
                    }
                }
                if !bridged { besideLine.insert([run.y, run.x]) }
            }
        }

        /// The cell past a label, across at most one blank, whose line or
        /// arrowhead continues toward it, by its column or row.
        private func reach(from start: Int, _ fixed: Int, toward side: Side) -> Int? {
            let step = offset(side)
            let along = step.dx != 0
            var position = start + (along ? step.dx : step.dy)
            for _ in 0..<2 {
                let x = along ? position : fixed, y = along ? fixed : position
                switch drawing.glyph(x, y) {
                case .lines(let lines):
                    return lines.weight(toward: side.opposite) == .none ? nil : position
                case .mark(let mark):
                    switch mark {
                    case .arrow(let toward), .openArrow(let toward):
                        return toward == side ? position : nil
                    case .cross, .circle: return position
                    default: return nil
                    }
                case .blank:
                    position += along ? step.dx : step.dy
                default:
                    return nil
                }
            }
            return nil
        }

        /// The column a participant's lifeline runs down.
        private func lifeline(of figure: Figure) -> Int? {
            guard case .participant = figure.kind else { return nil }
            return figure.x0 + (figure.x1 - figure.x0 + 1) / 2
        }

        // MARK: Figures

        /// Marks each figure's cells as drawn by it: a block's outline, or a
        /// solid figure's whole box. Later figures draw over earlier ones.
        private mutating func claimCells() {
            owner = Array(repeating: -1, count: drawing.width * drawing.height)
            for (index, figure) in drawing.figures.enumerated() {
                let solid = figure.kind != .block
                if let column = lifeline(of: figure) { lifelines.insert(column) }
                for y in max(figure.y0, 0)...min(figure.y1, drawing.height - 1) {
                    for x in max(figure.x0, 0)...min(figure.x1, drawing.width - 1) {
                        let border = x == figure.x0 || x == figure.x1 || y == figure.y0 || y == figure.y1
                        if solid || border { owner[y * drawing.width + x] = index }
                    }
                }
            }
        }

        private func owner(_ x: Int, _ y: Int) -> Int? {
            let index = owner[y * drawing.width + x]
            return index < 0 ? nil : index
        }

        /// The rectangle through the centers of a figure's outline cells.
        private func outline(_ figure: Figure) -> CGRect {
            let first = center(figure.x0, figure.y0), last = center(figure.x1, figure.y1)
            return CGRect(x: first.x, y: first.y, width: last.x - first.x, height: last.y - first.y)
        }

        private func roundedRect(_ rect: CGRect, radius: CGFloat) -> Path {
            Path(roundedRect: rect, cornerRadius: min(radius, rect.height / 2, rect.width / 2), style: .continuous)
        }

        private mutating func drawFigure(_ figure: Figure) {
            let rect = outline(figure)
            switch figure.kind {
            case .node(let shape):
                switch shape {
                case .box:
                    lift(roundedRect(rect, radius: 5))
                case .round:
                    lift(roundedRect(rect, radius: 10))
                case .stadium, .circle:
                    lift(Path(roundedRect: rect, cornerRadius: rect.height / 2))
                case .decision:
                    // Points out at both sides, as the terminal draws it.
                    let inset = min(rect.height / 2, metrics.cell.width * 1.5)
                    var path = Path()
                    path.move(to: CGPoint(x: rect.minX, y: rect.midY))
                    path.addLine(to: CGPoint(x: rect.minX + inset, y: rect.minY))
                    path.addLine(to: CGPoint(x: rect.maxX - inset, y: rect.minY))
                    path.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
                    path.addLine(to: CGPoint(x: rect.maxX - inset, y: rect.maxY))
                    path.addLine(to: CGPoint(x: rect.minX + inset, y: rect.maxY))
                    path.closeSubpath()
                    lift(path)
                case .database:
                    let rim = min(rect.height / 5, 5)
                    var body = Path()
                    body.move(to: CGPoint(x: rect.minX, y: rect.minY + rim))
                    body.addLine(to: CGPoint(x: rect.minX, y: rect.maxY - rim))
                    body.addCurve(to: CGPoint(x: rect.maxX, y: rect.maxY - rim),
                                  control1: CGPoint(x: rect.minX, y: rect.maxY + rim / 3),
                                  control2: CGPoint(x: rect.maxX, y: rect.maxY + rim / 3))
                    body.addLine(to: CGPoint(x: rect.maxX, y: rect.minY + rim))
                    body.addCurve(to: CGPoint(x: rect.minX, y: rect.minY + rim),
                                  control1: CGPoint(x: rect.maxX, y: rect.minY - rim / 3),
                                  control2: CGPoint(x: rect.minX, y: rect.minY - rim / 3))
                    body.closeSubpath()
                    lift(body)
                    // The near edge of its top.
                    var lip = Path()
                    lip.move(to: CGPoint(x: rect.minX, y: rect.minY + rim))
                    lip.addCurve(to: CGPoint(x: rect.maxX, y: rect.minY + rim),
                                 control1: CGPoint(x: rect.minX, y: rect.minY + rim * 7 / 3),
                                 control2: CGPoint(x: rect.maxX, y: rect.minY + rim * 7 / 3))
                    elements.append(.stroke(lip, .nodeStroke, width: metrics.line, dash: [], phase: 0))
                case .elsewhere:
                    let length = max(metrics.cell.width / 2, metrics.line * 2)
                    elements.append(.shape(roundedRect(rect, radius: 5), fill: nil, stroke: .nodeStroke,
                                           width: metrics.line, dash: [length, length], lifted: false))
                }
            case .participant(let actor):
                lift(actor ? Path(roundedRect: rect, cornerRadius: rect.height / 2) : roundedRect(rect, radius: 6))
            case .note:
                elements.append(.shape(roundedRect(rect, radius: 4), fill: .noteFill, stroke: .noteStroke,
                                       width: metrics.line, dash: [], lifted: false))
            case .block:
                break
            }
        }

        private mutating func lift(_ path: Path) {
            elements.append(.shape(path, fill: .nodeFill, stroke: .nodeStroke, width: metrics.line, dash: [],
                                   lifted: true))
        }

        /// Draws the lines that cross a figure's outline: lifelines through a
        /// block's frame, or edges meeting a node. Lines along the outline
        /// are the figure's own.
        private mutating func strokeBorderLines(_ lines: Lines, _ x: Int, _ y: Int, _ figure: Figure) {
            let across: [Side]
            let onSide = x == figure.x0 || x == figure.x1
            let onEnd = y == figure.y0 || y == figure.y1
            if onSide && onEnd { return }
            if onEnd { across = [.up, .down] } else if onSide { across = [.left, .right] } else { return }
            var middle = center(x, y)
            if case .node(let shape) = figure.kind, shape != .elsewhere {
                // Reach in under the node's fill to meet a slanted or curved side.
                let reach = onSide ? metrics.cell.width * 1.5 : metrics.cell.height / 2
                middle = middle + unit(x == figure.x0 ? .right : x == figure.x1 ? .left : y == figure.y0 ? .down : .up)
                    * reach
            }
            let divider = drawing.kind == .sequence && figure.kind == .block
            for side in across where lines.weight(toward: side) != .none && !onLifeline(x, side)
                && !(divider && (side == .left || side == .right)) {
                let ink = lineInk(x, side, drawing.role(x, y))
                line(middle, edge(x, y, side), ink, lines.weight(toward: side), dashed: lines.dashed, along: side)
            }
        }

        // MARK: Text

        private mutating func placeText(_ runs: [Run]) {
            var index = 0
            while index < runs.count {
                let run = runs[index]
                index += 1
                let middle = center(run.x, run.y).y
                if run.role == .keyword {
                    // A condition follows its keyword on the same line.
                    var condition = ""
                    if index < runs.count, runs[index].role == .condition, runs[index].y == run.y {
                        condition = runs[index].text
                        if condition.hasPrefix("[") && condition.hasSuffix("]") {
                            condition = String(condition.dropFirst().dropLast())
                        }
                        index += 1
                    }
                    // A divider's line runs the block's width beneath its legend.
                    if run.owner == nil, let block = drawing.figures.last(where: {
                        $0.kind == .block && $0.x0 < run.x && run.x <= $0.x1 && $0.y0 < run.y && run.y < $0.y1
                    }) {
                        let frame = outline(block)
                        var path = Path()
                        path.move(to: CGPoint(x: frame.minX, y: middle))
                        path.addLine(to: CGPoint(x: frame.maxX, y: middle))
                        let (pattern, phase) = dash(true, from: CGPoint(x: frame.minX, y: middle), along: .right)
                        elements.append(.stroke(path, .blockStroke, width: metrics.line, dash: pattern, phase: phase))
                    }
                    elements.append(.legend(keyword: run.text, condition: condition,
                                            at: CGPoint(x: origin(run.x, run.y).x, y: middle)))
                    continue
                }
                let style: TextStyle
                switch (run.role, run.owner.map { drawing.figures[$0].kind }) {
                case (.title, _): style = .title
                case (_, .node(.elsewhere)?): style = .elsewhere
                case (_, .node?): style = .node
                case (_, .participant?): style = .name
                case (_, .note?): style = .note
                case (.name, _): style = .name
                case (.note, _): style = .note
                default: style = drawing.kind == .sequence ? .message : .edgeLabel
                }
                if style == .title || (style == .edgeLabel && besideLine.contains([run.y, run.x])) {
                    elements.append(.text(run.text, at: CGPoint(x: origin(run.x, run.y).x, y: middle), style,
                                          leading: true))
                } else {
                    let left = origin(run.x, run.y).x, right = origin(run.end + 1, run.y).x
                    let x = onColumn[[run.y, run.x]].map { center($0, run.y).x } ?? (left + right) / 2
                    elements.append(.text(run.text, at: CGPoint(x: x, y: middle), style, leading: false))
                }
            }
        }

        // MARK: Geometry

        private func origin(_ x: Int, _ y: Int) -> CGPoint {
            CGPoint(x: CGFloat(x) * metrics.cell.width, y: CGFloat(y) * metrics.cell.height)
        }

        private func rect(_ x: Int, _ y: Int) -> CGRect {
            CGRect(origin: origin(x, y), size: metrics.cell)
        }

        private func snap(_ value: CGFloat) -> CGFloat {
            (value * metrics.scale).rounded() / metrics.scale
        }

        private func center(_ x: Int, _ y: Int) -> CGPoint {
            let cell = rect(x, y)
            return CGPoint(x: snap(cell.midX), y: snap(cell.midY))
        }

        /// The middle of the cell's edge on side.
        private func edge(_ x: Int, _ y: Int, _ side: Side) -> CGPoint {
            let cell = rect(x, y), middle = center(x, y)
            switch side {
            case .up: return CGPoint(x: middle.x, y: cell.minY)
            case .right: return CGPoint(x: cell.maxX, y: middle.y)
            case .down: return CGPoint(x: middle.x, y: cell.maxY)
            case .left: return CGPoint(x: cell.minX, y: middle.y)
            }
        }

        private func offset(_ side: Side) -> (dx: Int, dy: Int) {
            switch side {
            case .up: (0, -1)
            case .right: (1, 0)
            case .down: (0, 1)
            case .left: (-1, 0)
            }
        }

        private func unit(_ side: Side) -> CGVector {
            let step = offset(side)
            return CGVector(dx: CGFloat(step.dx), dy: CGFloat(step.dy))
        }

        private func ink(_ role: Role) -> Ink {
            role == .arrow ? .line : .lifeline
        }

        private func onLifeline(_ x: Int, _ side: Side) -> Bool {
            drawing.kind == .sequence && (side == .up || side == .down) && lifelines.contains(x)
        }

        /// A line's ink: in a sequence diagram, whatever runs down a
        /// lifeline's column is the lifeline, even where a message crosses.
        private func lineInk(_ x: Int, _ side: Side, _ role: Role) -> Ink {
            if drawing.kind == .sequence && (side == .up || side == .down) && lifelines.contains(x) {
                return .lifeline
            }
            return ink(role)
        }

        private func width(_ weight: Weight, _ ink: Ink) -> CGFloat {
            let light = ink == .line ? metrics.line * 1.25 : metrics.line
            return weight == .heavy ? light * 2 : light
        }

        /// Dashes a third of a cell long, phased from the drawing's origin so
        /// a dashed run reads as one line across its cells.
        private func dash(_ dashed: Bool, from start: CGPoint, along side: Side) -> ([CGFloat], CGFloat) {
            guard dashed else { return ([], 0) }
            let length = max(metrics.cell.width / 3, metrics.line * 2)
            let position = side == .left || side == .right ? start.x : start.y
            return ([length, length], position.truncatingRemainder(dividingBy: 2 * length))
        }

        private mutating func line(_ from: CGPoint, _ to: CGPoint, _ ink: Ink, _ weight: Weight, dashed: Bool,
                                   along side: Side) {
            // Draw from the lesser coordinate so dash phases line up.
            let (start, end) = from.x + from.y <= to.x + to.y ? (from, to) : (to, from)
            let (pattern, phase) = dash(dashed, from: start, along: side)
            if weight == .double {
                let gap = metrics.line * 1.5
                let normal = side == .up || side == .down ? CGVector(dx: gap, dy: 0) : CGVector(dx: 0, dy: gap)
                for sign in [-1.0, 1.0] {
                    var path = Path()
                    path.move(to: CGPoint(x: start.x + sign * normal.dx, y: start.y + sign * normal.dy))
                    path.addLine(to: CGPoint(x: end.x + sign * normal.dx, y: end.y + sign * normal.dy))
                    elements.append(.stroke(path, ink, width: metrics.line, dash: [], phase: 0))
                }
                return
            }
            var path = Path()
            path.move(to: start)
            path.addLine(to: end)
            elements.append(.stroke(path, ink, width: width(weight, ink), dash: pattern, phase: phase))
        }

        // MARK: Lines

        private mutating func strokeLines(_ given: Lines, _ x: Int, _ y: Int, _ role: Role) {
            var lines = given
            // A lifeline is drawn whole, and so is a block's divider.
            if onLifeline(x, .up) { lines.up = .none; lines.down = .none }
            if drawing.kind == .sequence && role == .frame { lines.left = .none; lines.right = .none }
            let arms = Side.allCases.filter { lines.weight(toward: $0) != .none }
            if arms.isEmpty { return }
            let middle = center(x, y)
            // Edges and messages bend in curves.
            let curved = lines.rounded || role == .arrow
            if curved, arms.count == 2, let first = arms.first, let second = arms.last, first.opposite != second,
               lines.weight(toward: first) == lines.weight(toward: second) {
                let radius = min(metrics.cell.width, metrics.cell.height) / 2
                let ink = lineInk(x, first, role)
                // Straight runs to the curve, where the cell is longer than it.
                let into = middle + unit(first) * radius, out = middle + unit(second) * radius
                var path = Path()
                path.move(to: edge(x, y, first))
                if edge(x, y, first) != into { path.addLine(to: into) }
                path.addQuadCurve(to: out, control: middle)
                if out != edge(x, y, second) { path.addLine(to: edge(x, y, second)) }
                let (pattern, phase) = dash(lines.dashed, from: edge(x, y, first), along: first)
                elements.append(.stroke(path, ink, width: width(lines.weight(toward: first), ink), dash: pattern,
                                        phase: phase))
                return
            }
            // A straight line runs edge to edge as one stroke.
            for (a, b) in [(Side.left, Side.right), (Side.up, Side.down)]
                where lines.weight(toward: a) != .none && lines.weight(toward: a) == lines.weight(toward: b) {
                line(edge(x, y, a), edge(x, y, b), lineInk(x, a, role), lines.weight(toward: a),
                     dashed: lines.dashed, along: a)
            }
            // Other arms reach back past the center so corners and junctions close.
            for side in arms {
                let weight = lines.weight(toward: side)
                let partner = side.opposite
                if lines.weight(toward: partner) == weight { continue }
                let overlap = arms.contains(partner) ? 0 : joinOverlap(lines, side, role)
                line(middle - unit(side) * overlap, edge(x, y, side), lineInk(x, side, role), weight,
                     dashed: lines.dashed || dashedBeyond(x, y, side), along: side)
            }
        }

        /// Whether the line a cell's arm joins runs on dashed. A junction's
        /// character can't show dashes, so its arm takes them from its line.
        private func dashedBeyond(_ x: Int, _ y: Int, _ side: Side) -> Bool {
            let step = offset(side)
            guard case .lines(let beyond) = drawing.glyph(x + step.dx, y + step.dy) else { return false }
            return beyond.dashed && beyond.weight(toward: side.opposite) != .none
        }

        /// How far an arm reaches past the center to meet the arms beside it.
        private func joinOverlap(_ lines: Lines, _ side: Side, _ role: Role) -> CGFloat {
            let across = side == .up || side == .down ? [Side.left, Side.right] : [Side.up, Side.down]
            let widest = across.map { lines.weight(toward: $0) }.map { weight -> CGFloat in
                switch weight {
                case .none: 0
                case .double: metrics.line * 2
                default: width(weight, ink(role)) / 2
                }
            }.max() ?? 0
            return widest
        }

        // MARK: Marks

        /// The lines that neighboring cells run into this one.
        private func incoming(_ x: Int, _ y: Int) -> [(side: Side, weight: Weight, dashed: Bool)] {
            Side.allCases.compactMap { side in
                let step = offset(side)
                let neighbor = (x + step.dx, y + step.dy)
                var found: Lines?
                if case .lines(let lines) = drawing.glyph(neighbor.0, neighbor.1) { found = lines }
                if neighbor.0 >= 0, neighbor.1 >= 0, neighbor.0 < drawing.width, neighbor.1 < drawing.height {
                    found = found ?? bridges[neighbor.1 * drawing.width + neighbor.0]
                }
                guard let lines = found else { return nil }
                let weight = lines.weight(toward: side.opposite)
                return weight == .none ? nil : (side, weight, lines.dashed)
            }
        }

        private mutating func drawMark(_ mark: Mark, _ x: Int, _ y: Int, _ ink: Ink) {
            let middle = center(x, y)
            let size = min(metrics.cell.width, metrics.cell.height)
            switch mark {
            case .arrow(let side), .openArrow(let side):
                // The head points to its far edge, reaching a line beyond it.
                let step = offset(side)
                var tip = edge(x, y, side)
                if case .lines(let beyond) = drawing.glyph(x + step.dx, y + step.dy),
                   beyond.weight(toward: side.opposite) == .none {
                    let reach = side == .left || side == .right ? metrics.cell.width : metrics.cell.height
                    tip = tip + unit(side) * (reach / 2 - width(.light, ink) / 2)
                }
                let length = size * 1.1
                let base = tip - unit(side) * length
                let spread = CGVector(dx: unit(side).dy, dy: unit(side).dx) * (length * 0.42)
                for arm in incoming(x, y) {
                    let end = arm.side == side.opposite ? base + unit(side) * (length * 0.2) : middle
                    line(edge(x, y, arm.side), end, ink, arm.weight, dashed: arm.dashed, along: arm.side)
                }
                // A head swept back at its base, as macOS draws arrows.
                var head = Path()
                head.move(to: tip)
                head.addLine(to: base + spread)
                head.addLine(to: base + unit(side) * (length * 0.25))
                head.addLine(to: base - spread)
                head.closeSubpath()
                if case .arrow = mark {
                    elements.append(.fill(head, ink))
                } else {
                    elements.append(.stroke(head, ink, width: metrics.line, dash: [], phase: 0))
                }
            case .cross, .circle:
                let radius = size * 0.4
                for arm in incoming(x, y) {
                    line(edge(x, y, arm.side), middle + unit(arm.side) * radius, ink, arm.weight, dashed: arm.dashed,
                         along: arm.side)
                }
                var shape = Path()
                if mark == .circle {
                    shape.addEllipse(in: CGRect(x: middle.x - radius, y: middle.y - radius, width: 2 * radius,
                                                height: 2 * radius))
                } else {
                    let arm = radius * 0.75
                    shape.move(to: CGPoint(x: middle.x - arm, y: middle.y - arm))
                    shape.addLine(to: CGPoint(x: middle.x + arm, y: middle.y + arm))
                    shape.move(to: CGPoint(x: middle.x + arm, y: middle.y - arm))
                    shape.addLine(to: CGPoint(x: middle.x - arm, y: middle.y + arm))
                }
                elements.append(.stroke(shape, ink, width: metrics.line * 1.25, dash: [], phase: 0))
            case .diamond:
                // Where a decision's branches fork: a dot the lines meet at.
                for arm in incoming(x, y) {
                    line(edge(x, y, arm.side), middle, ink, arm.weight, dashed: arm.dashed, along: arm.side)
                }
                let radius = size * 0.22
                elements.append(.fill(Path(ellipseIn: CGRect(x: middle.x - radius, y: middle.y - radius,
                                                             width: 2 * radius, height: 2 * radius)), ink))
            case .point, .bulge:
                // A node's sides, drawn by its figure.
                break
            }
        }
    }
}

private func + (point: CGPoint, vector: CGVector) -> CGPoint {
    CGPoint(x: point.x + vector.dx, y: point.y + vector.dy)
}

private func - (point: CGPoint, vector: CGVector) -> CGPoint {
    CGPoint(x: point.x - vector.dx, y: point.y - vector.dy)
}

private func * (vector: CGVector, scale: CGFloat) -> CGVector {
    CGVector(dx: vector.dx * scale, dy: vector.dy * scale)
}

/// Keeps scenes across view updates, since a large diagram takes a few
/// milliseconds to lay out as strokes.
@MainActor
final class MermaidSceneCache {
    static let shared = MermaidSceneCache()
    private final class Entry: NSObject {
        let scene: MermaidScene
        init(_ scene: MermaidScene) { self.scene = scene }
    }
    private let entries = NSCache<NSString, Entry>()

    private init() {
        entries.countLimit = 32
        // Counted in elements; a large diagram has tens of thousands.
        entries.totalCostLimit = 400_000
    }

    func scene(for diagram: MermaidDiagram, drawing: KitMermaid.Canvas, metrics: MermaidScene.Metrics) -> MermaidScene {
        let key = "\(metrics.cell.width) \(metrics.cell.height) \(metrics.line) \(metrics.scale)\n\(diagram.source)" as NSString
        if let entry = entries.object(forKey: key) { return entry.scene }
        let scene = MermaidScene(drawing, metrics: metrics)
        entries.setObject(Entry(scene), forKey: key, cost: scene.elements.count)
        return scene
    }
}

/// A drawn Mermaid diagram, in the transcript's monospaced font and the
/// theme's colors for each role.
struct MermaidPicture: View {
    @Environment(\.mica) private var theme
    @Environment(\.displayScale) private var displayScale
    let diagram: MermaidDiagram
    let drawing: KitMermaid.Canvas
    var fontSize: CGFloat = 12

    var body: some View {
        // The layout's cells are as wide as the transcript's code font, so
        // text in the interface font fits wherever the terminal's does.
        let metrics = Self.metrics(for: Typography.shared.font(size: fontSize, mono: true), scale: displayScale)
        let scene = MermaidSceneCache.shared.scene(for: diagram, drawing: drawing, metrics: metrics)
        let colors = Dictionary(uniqueKeysWithValues: MermaidScene.Ink.allCases.map { ($0, color($0)) })
        let fonts = Fonts(size: fontSize)
        VStack(spacing: 0) {
            ForEach(scene.bands.indices, id: \.self) { index in
                let band = scene.bands[index]
                SwiftUI.Canvas { context, _ in
                    context.translateBy(x: 0, y: -band.top)
                    for index in band.elements {
                        draw(scene.elements[index], in: &context, colors: colors, fonts: fonts)
                    }
                }
                .frame(width: scene.size.width, height: band.height)
            }
        }
        .frame(width: scene.size.width, height: scene.size.height)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Mermaid diagram")
        .accessibilityValue(diagram.source)
    }

    private struct Fonts {
        let node: Font, name: Font, small: Font, tag: Font, title: Font

        @MainActor init(size: CGFloat) {
            func font(_ size: CGFloat, _ weight: NSFont.Weight = .regular) -> Font {
                Font(Typography.shared.font(size: size, weight: weight))
            }
            node = font(size)
            name = font(size, .semibold)
            small = font(size - 1)
            tag = font(size - 3, .bold)
            title = font(size + 1, .semibold)
        }
    }

    private func draw(_ element: MermaidScene.Element, in context: inout GraphicsContext,
                      colors: [MermaidScene.Ink: Color], fonts: Fonts) {
        func ink(_ value: MermaidScene.Ink) -> Color { colors[value, default: theme.text] }
        switch element {
        case .stroke(let path, let value, let width, let dash, let phase):
            context.stroke(path, with: .color(ink(value)),
                           style: StrokeStyle(lineWidth: width, lineCap: .butt, lineJoin: .round, dash: dash,
                                              dashPhase: phase))
        case .fill(let path, let value):
            context.fill(path, with: .color(ink(value)))
        case .shape(let path, let fill, let stroke, let width, let dash, let lifted):
            if let fill {
                if lifted {
                    context.drawLayer { layer in
                        layer.addFilter(.shadow(color: .black.opacity(theme.dark ? 0.35 : 0.07), radius: 2, y: 1))
                        layer.fill(path, with: .color(ink(fill)))
                    }
                } else {
                    context.fill(path, with: .color(ink(fill)))
                }
            }
            context.stroke(path, with: .color(ink(stroke)),
                           style: StrokeStyle(lineWidth: width, lineJoin: .round, dash: dash))
        case .text(let text, let point, let style, let leading):
            let font: Font, color: Color
            switch style {
            case .node: (font, color) = (fonts.node, ink(.text))
            case .elsewhere: (font, color) = (fonts.node, ink(.secondary))
            case .name: (font, color) = (fonts.name, ink(.text))
            case .edgeLabel: (font, color) = (fonts.small, ink(.secondary))
            case .message: (font, color) = (fonts.node, ink(.text))
            case .note: (font, color) = (fonts.small, ink(.text))
            case .title: (font, color) = (fonts.title, ink(.text))
            }
            let resolved = context.resolve(Text(text).font(font).foregroundColor(color))
            if style == .message {
                // Clear the lifelines behind a message's text.
                let size = resolved.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
                context.fill(Path(CGRect(x: point.x - size.width / 2 - 3, y: point.y - size.height / 2,
                                         width: size.width + 6, height: size.height)), with: .color(ink(.card)))
            }
            if style == .edgeLabel {
                // Edge text sits on a pill over its line.
                let size = resolved.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
                let left = leading ? point.x - 6 : point.x - size.width / 2 - 6
                let pill = CGRect(x: left, y: point.y - size.height / 2 - 1.5,
                                  width: size.width + 12, height: size.height + 3)
                let shape = Path(roundedRect: pill, cornerRadius: pill.height / 2)
                context.fill(shape, with: .color(ink(.nodeFill)))
                context.stroke(shape, with: .color(ink(.blockStroke)), lineWidth: 1)
            }
            context.draw(resolved, at: point, anchor: leading ? .leading : .center)
        case .legend(let keyword, let condition, let point):
            // The keyword as a tag on the frame's line, then the condition,
            // both cleared from the line behind them.
            let tag = context.resolve(Text(keyword.uppercased()).font(fonts.tag).tracking(0.4)
                .foregroundColor(ink(.accent)))
            let tagSize = tag.measure(in: CGSize(width: CGFloat.infinity, height: .infinity))
            let pill = CGRect(x: point.x, y: point.y - tagSize.height / 2 - 2, width: tagSize.width + 10,
                              height: tagSize.height + 4)
            var end = pill.maxX
            var label: GraphicsContext.ResolvedText?
            if !condition.isEmpty {
                let text = context.resolve(Text(condition).font(fonts.small).foregroundColor(ink(.secondary)))
                end += 6 + text.measure(in: CGSize(width: CGFloat.infinity, height: .infinity)).width
                label = text
            }
            context.fill(Path(CGRect(x: pill.minX - 4, y: pill.minY, width: end - pill.minX + 8, height: pill.height)),
                         with: .color(ink(.card)))
            let tagShape = Path(roundedRect: pill, cornerRadius: 4, style: .continuous)
            context.fill(tagShape, with: .color(ink(.tagFill)))
            context.draw(tag, at: CGPoint(x: pill.midX, y: point.y), anchor: .center)
            if let label {
                context.draw(label, at: CGPoint(x: pill.maxX + 6, y: point.y), anchor: .leading)
            }
        }
    }

    /// The theme color of each ink. Lines are quieter than text, and fills
    /// stay close to the card so the diagram reads as part of it.
    private func color(_ ink: MermaidScene.Ink) -> Color {
        switch ink {
        case .line: theme.muted.mix(with: theme.border, by: 0.2)
        case .lifeline: theme.border.mix(with: theme.muted, by: 0.25)
        case .nodeFill: theme.dark ? theme.hover : theme.surface
        case .nodeStroke: theme.border.mix(with: theme.muted, by: 0.35)
        case .noteFill: theme.raised.mix(with: theme.accent, by: 0.08)
        case .noteStroke: theme.border.mix(with: theme.accent, by: 0.35)
        case .blockStroke: theme.border.mix(with: theme.muted, by: 0.15)
        case .tagFill: theme.raised.mix(with: theme.accent, by: 0.14)
        case .accent: theme.accent
        case .text: theme.text
        case .secondary: theme.muted
        case .card: theme.raised
        }
    }

    /// One cell is the font's advance wide and its line tall.
    static func metrics(for font: NSFont, scale: CGFloat) -> MermaidScene.Metrics {
        let advance = ("M" as NSString).size(withAttributes: [.font: font]).width
        let height = NSLayoutManager().defaultLineHeight(for: font)
        return MermaidScene.Metrics(cell: CGSize(width: advance, height: ceil(height)),
                                    line: max(1, (font.pointSize / 12).rounded()), scale: max(scale, 1))
    }
}

/// A fenced Mermaid diagram in Markdown: drawn when it can be, otherwise its
/// source with the reason it wasn't drawn.
struct MermaidBlock: View {
    @Environment(\.mica) private var theme
    @Environment(\.compactCodeBlocks) private var compact
    let diagram: MermaidDiagram

    var body: some View {
        switch diagram.drawing {
        case .success(let drawing):
            if compact {
                ScrollView(.horizontal) { MermaidPicture(diagram: diagram, drawing: drawing) }
                    .compactFence()
            } else {
                FenceCard(label: "MERMAID", copy: diagram.source) {
                    ScrollView(.horizontal) {
                        MermaidPicture(diagram: diagram, drawing: drawing).padding(14).frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        case .failure(let notDrawn):
            VStack(alignment: .leading, spacing: 6) {
                CodeBlock(language: "mermaid", source: diagram.source)
                Text("Diagram not drawn: \(notDrawn.reason)")
                    .font(.kit(size: 12)).italic().foregroundStyle(theme.muted)
            }
        }
    }
}
