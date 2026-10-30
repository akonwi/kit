import Foundation

// A character grid that Mermaid layouts draw on. Line cells record the
// directions they connect, so crossings and junctions resolve to the matching
// box-drawing character no matter the drawing order; text replaces lines.

/// What a cell depicts, which a client maps to theme colors.
public enum Role: Int, Sendable, CaseIterable {
    /// Participant boxes, lifelines, block frames, and node outlines.
    case frame
    /// Message and edge lines and their arrowheads.
    case arrow
    /// Participant names.
    case name
    /// Message, node, and edge text.
    case label
    /// Note boxes and their text.
    case note
    /// Block keywords such as `loop` and `alt`.
    case keyword
    /// Block conditions.
    case condition
    case title
}

/// Directions a line cell connects, as bits.
public enum Direction {
    public static let up = 1
    public static let right = 2
    public static let down = 4
    public static let left = 8
}

// A cell's lines, styles, and role are packed into one number: the low four
// bits are the directions its line connects, then a bit for each style, then
// its role.
private let maskBits = 16
private let dashedBit = 16
private let heavyBit = 32
private let roundedBit = 64
private let coveredBit = 128
private let boldBit = 256
private let roleUnit = 512

private func flagged(_ cell: Int, _ flag: Int) -> Bool { cell / flag % 2 == 1 }

private func flag(_ cell: Int, _ flag: Int, _ on: Bool) -> Int {
    let cleared = flagged(cell, flag) ? cell - flag : cell
    return on ? cleared + flag : cleared
}

/// A shape a layout drew, in the cells its outline runs through, so a client
/// that isn't bound to a grid can draw it whole. The terminal draws only the
/// cells; figures don't change them.
public struct Figure: Equatable, Sendable {
    public enum Kind: Equatable, Sendable {
        /// A flowchart node, drawn opaque over the lines that meet it.
        case node(NodeShape)
        /// A sequence participant's box at the head of its lifeline.
        case participant(actor: Bool)
        /// A sequence note, covering the lifelines behind it.
        case note
        /// A sequence block such as `loop` or `alt`, around the events inside.
        case block
    }

    public enum NodeShape: Equatable, Sendable {
        case box, round, stadium, circle, database, decision
        /// A box standing in for what is drawn elsewhere, such as a subgraph.
        case elsewhere
    }

    public let kind: Kind
    /// The outline's first and last columns and rows, inclusive.
    public let x0: Int, y0: Int, x1: Int, y1: Int
}

/// The kind of diagram a canvas shows.
public enum DiagramKind: Equatable, Sendable {
    case flowchart, sequence
}

/// A drawn diagram: a grid of cells, each showing a character in a role.
public struct Canvas: Equatable, Sendable {
    public let width: Int
    public let height: Int
    public internal(set) var kind: DiagramKind = .flowchart
    /// The shapes drawn, in drawing order.
    public internal(set) var figures: [Figure] = []
    /// The text a cell shows, where empty is blank. Line cells show their
    /// mask's glyph, and a wide character's trailing cells are skipped.
    var texts: [String]
    /// Each cell's line directions, styles, and role, packed.
    var cells: [Int]

    /// A blank canvas.
    init(width: Int, height: Int) {
        self.width = width
        self.height = height
        let size = max(width * height, 0)
        texts = Array(repeating: "", count: size)
        cells = Array(repeating: 0, count: size)
    }

    /// parts drawn one under another, a blank row apart.
    static func stack(_ parts: [Canvas]) -> Canvas {
        var width = 0
        var height = 0
        for part in parts {
            width = max(width, part.width)
            if height > 0 { height += 1 }
            height += part.height
        }
        var drawing = Canvas(width: width, height: height)
        var top = 0
        for part in parts {
            for y in 0..<max(part.height, 0) {
                for x in 0..<max(part.width, 0) {
                    let from = y * part.width + x
                    let to = (top + y) * width + x
                    drawing.texts[to] = part.texts[from]
                    drawing.cells[to] = part.cells[from]
                }
            }
            for figure in part.figures {
                drawing.figures.append(Figure(kind: figure.kind, x0: figure.x0, y0: figure.y0 + top,
                                              x1: figure.x1, y1: figure.y1 + top))
            }
            top += part.height + 1
        }
        return drawing
    }

    private func inside(_ x: Int, _ y: Int) -> Bool {
        x >= 0 && y >= 0 && x < width && y < height
    }

    private func cell(_ index: Int) -> Int {
        index >= 0 && index < cells.count ? cells[index] : 0
    }

    private func text(_ index: Int) -> String {
        index >= 0 && index < texts.count ? texts[index] : ""
    }

    private mutating func setCell(_ index: Int, _ value: Int) {
        if index >= 0 && index < cells.count { cells[index] = value }
    }

    private mutating func setText(_ index: Int, _ value: String) {
        if index >= 0 && index < texts.count { texts[index] = value }
    }

    /// Adds the bits of mask to the line at x, y. A text cell is left alone.
    mutating func connect(_ x: Int, _ y: Int, _ mask: Int, _ role: Role, dashed: Bool, rounded: Bool, heavy: Bool = false) {
        guard inside(x, y) else { return }
        let index = y * width + x
        let current = cell(index)
        let lines = current % maskBits
        let fresh = lines == 0
        let shown = text(index)
        guard !fresh || shown == "" || shown == " " else { return }
        var next = lines
        for bit in [Direction.up, Direction.right, Direction.down, Direction.left] where mask / bit % 2 == 1 {
            if next / bit % 2 == 0 { next += bit }
        }
        // Keep whether the cell is covered or bold; replace the rest.
        var packed = next + role.rawValue * roleUnit
        packed = flag(packed, coveredBit, flagged(current, coveredBit))
        packed = flag(packed, boldBit, flagged(current, boldBit))
        packed = flag(packed, dashedBit, dashed && (fresh || flagged(current, dashedBit)))
        packed = flag(packed, heavyBit, heavy && (fresh || flagged(current, heavyBit)))
        packed = flag(packed, roundedBit, rounded && (fresh || flagged(current, roundedBit)))
        cells[index] = packed
    }

    /// Draws a horizontal line from x0 to x1 on row y.
    mutating func horizontal(_ y: Int, _ x0: Int, _ x1: Int, _ role: Role, dashed: Bool, heavy: Bool = false) {
        var x = x0
        while x <= x1 {
            var mask = 0
            if x > x0 { mask += Direction.left }
            if x < x1 { mask += Direction.right }
            if mask == 0 { mask = Direction.left + Direction.right }
            connect(x, y, mask, role, dashed: dashed, rounded: false, heavy: heavy)
            x += 1
        }
    }

    /// Draws a vertical line from y0 to y1 in column x.
    mutating func vertical(_ x: Int, _ y0: Int, _ y1: Int, _ role: Role, dashed: Bool, heavy: Bool = false) {
        var y = y0
        while y <= y1 {
            var mask = 0
            if y > y0 { mask += Direction.up }
            if y < y1 { mask += Direction.down }
            if mask == 0 { mask = Direction.up + Direction.down }
            connect(x, y, mask, role, dashed: dashed, rounded: false, heavy: heavy)
            y += 1
        }
    }

    /// Draws a box's outline with the given corners.
    mutating func outline(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int, _ role: Role, rounded: Bool) {
        horizontal(y0, x0, x1, role, dashed: false)
        horizontal(y1, x0, x1, role, dashed: false)
        vertical(x0, y0, y1, role, dashed: false)
        vertical(x1, y0, y1, role, dashed: false)
        for (x, y) in [(x0, y0), (x1, y0), (x0, y1), (x1, y1)] where inside(x, y) {
            let index = y * width + x
            cells[index] = flag(cells[index], roundedBit, rounded)
        }
    }

    /// Writes value from x on row y, replacing what was drawn there. A wide
    /// character covers the cells after it; text past the right edge is cut.
    mutating func write(_ x: Int, _ y: Int, _ value: String, _ role: Role, bold: Bool) {
        var column = x
        for part in TextWidth.graphemes(value) {
            if part.width > 0 && column + part.width <= width {
                put(column, y, part.text, role, bold: bold)
                var covered = 1
                while covered < part.width {
                    put(column + covered, y, "", role, bold: bold)
                    let index = y * width + column + covered
                    setCell(index, flag(cell(index), coveredBit, true))
                    covered += 1
                }
            }
            column += part.width
        }
    }

    private mutating func put(_ x: Int, _ y: Int, _ value: String, _ role: Role, bold: Bool) {
        guard inside(x, y) else { return }
        let index = y * width + x
        // Replacing half of a wide character blanks its other half.
        if flagged(cell(index), coveredBit) && x > 0 {
            setText(index - 1, " ")
        }
        if x + 1 < width && flagged(cell(index + 1), coveredBit) {
            setText(index + 1, " ")
            setCell(index + 1, flag(cell(index + 1), coveredBit, false))
        }
        texts[index] = value
        cells[index] = flag(role.rawValue * roleUnit, boldBit, bold)
    }

    /// The character shown at a cell; empty for a wide character's trailing half.
    public func shown(_ x: Int, _ y: Int) -> String {
        let index = y * width + x
        let current = cell(index)
        let mask = current % maskBits
        if flagged(current, coveredBit) { return "" }
        if mask > 0 {
            return Self.glyph(mask, dashed: flagged(current, dashedBit), rounded: flagged(current, roundedBit),
                              heavy: flagged(current, heavyBit))
        }
        let value = text(index)
        return value == "" ? " " : value
    }

    public func role(_ x: Int, _ y: Int) -> Role {
        Role(rawValue: cell(y * width + x) / roleUnit) ?? .frame
    }

    public func isBold(_ x: Int, _ y: Int) -> Bool {
        flagged(cell(y * width + x), boldBit)
    }

    /// The drawing as plain rows, with trailing spaces removed.
    public func lines() -> [String] {
        (0..<max(height, 0)).map { y in
            var row = ""
            for x in 0..<max(width, 0) { row += shown(x, y) }
            return Go.trimRight(row, " ")
        }
    }

    /// The box-drawing character for a set of connected directions.
    static func glyph(_ mask: Int, dashed: Bool, rounded: Bool, heavy: Bool) -> String {
        let up = Direction.up, right = Direction.right, down = Direction.down, left = Direction.left
        let vertical = mask == up || mask == down || mask == up + down
        let horizontal = mask == left || mask == right || mask == left + right
        switch true {
        case heavy && vertical: return "┃"
        case heavy && horizontal: return "━"
        case dashed && vertical: return "┊"
        case dashed && horizontal: return "┈"
        case vertical: return "│"
        case horizontal: return "─"
        case heavy && mask == right + down: return "┏"
        case heavy && mask == left + down: return "┓"
        case heavy && mask == up + right: return "┗"
        case heavy && mask == up + left: return "┛"
        case heavy && mask == up + right + down: return "┣"
        case heavy && mask == up + left + down: return "┫"
        case heavy && mask == left + right + down: return "┳"
        case heavy && mask == up + left + right: return "┻"
        case heavy && mask == up + right + down + left: return "╋"
        case mask == right + down: return rounded ? "╭" : "┌"
        case mask == left + down: return rounded ? "╮" : "┐"
        case mask == up + right: return rounded ? "╰" : "└"
        case mask == up + left: return rounded ? "╯" : "┘"
        case mask == up + right + down: return "├"
        case mask == up + left + down: return "┤"
        case mask == left + right + down: return "┬"
        case mask == up + left + right: return "┴"
        case mask == up + right + down + left: return "┼"
        default: return " "
        }
    }
}
