import Foundation

// How a client draws each cell of a canvas: as lines from the cell's center,
// as a mark such as an arrowhead, or as text. Lines read the cell's packed
// directions and styles, resolved as the box-drawing character the terminal
// shows would draw them, so a stroked drawing matches the terminal's.

/// The weight of a cell's line toward one side.
public enum Weight: Equatable, Sendable {
    case none, light, heavy, double
}

/// A cell's line toward each side, and its style.
public struct Lines: Equatable, Sendable {
    public var up: Weight = .none
    public var right: Weight = .none
    public var down: Weight = .none
    public var left: Weight = .none
    public var dashed = false
    /// Whether a corner turns in a curve.
    public var rounded = false

    public init(up: Weight = .none, right: Weight = .none, down: Weight = .none, left: Weight = .none,
                dashed: Bool = false, rounded: Bool = false) {
        self.up = up
        self.right = right
        self.down = down
        self.left = left
        self.dashed = dashed
        self.rounded = rounded
    }

    public func weight(toward side: Side) -> Weight {
        switch side {
        case .up: up
        case .right: right
        case .down: down
        case .left: left
        }
    }
}

public enum Side: CaseIterable, Sendable {
    case up, right, down, left

    public var opposite: Side {
        switch self {
        case .up: .down
        case .right: .left
        case .down: .up
        case .left: .right
        }
    }
}

/// A drawn mark that isn't a line or text.
public enum Mark: Equatable, Sendable {
    /// A filled arrowhead pointing toward a side.
    case arrow(Side)
    /// An open arrowhead pointing toward a side.
    case openArrow(Side)
    case cross
    case circle
    /// Where a decision's branches fork.
    case diamond
    /// A decision's side, pointing out toward a side.
    case point(Side)
    /// A stadium's side, bulging out toward a side.
    case bulge(Side)
}

/// How a cell is drawn.
public enum Glyph: Equatable, Sendable {
    case blank
    /// The trailing cells of a wide character, drawn by the cell before.
    case covered
    case lines(Lines)
    case mark(Mark)
    case text(String)
}

extension Canvas {
    /// How the cell at x, y is drawn.
    public func glyph(_ x: Int, _ y: Int) -> Glyph {
        guard x >= 0, y >= 0, x < width, y < height else { return .blank }
        let shown = shown(x, y)
        if shown == "" { return .covered }
        if shown == " " { return .blank }
        let packed = cells[y * width + x]
        if packed % 16 > 0 { return .lines(Self.lines(packed)) }
        let role = role(x, y)
        if role == .arrow, let mark = Self.arrowMarks[shown] { return .mark(mark) }
        if role == .frame {
            if let mark = Self.frameMarks[shown] { return .mark(mark) }
            if let lines = Self.frameLines[shown] { return .lines(lines) }
        }
        return .text(shown)
    }

    /// A line cell's directions and styles, as its box-drawing character
    /// shows them: a line toward one side runs straight through, dashes
    /// show only on straight lines, and only light corners round.
    private static func lines(_ packed: Int) -> Lines {
        var mask = packed % 16
        let up = Direction.up, right = Direction.right, down = Direction.down, left = Direction.left
        if mask == up || mask == down { mask = up + down }
        if mask == left || mask == right { mask = left + right }
        let heavy = packed / 32 % 2 == 1
        let straight = mask == up + down || mask == left + right
        let corner = [right + down, left + down, up + right, up + left].contains(mask)
        let weight: Weight = heavy ? .heavy : .light
        return Lines(up: mask & up != 0 ? weight : .none, right: mask & right != 0 ? weight : .none,
                     down: mask & down != 0 ? weight : .none, left: mask & left != 0 ? weight : .none,
                     dashed: straight && !heavy && packed / 16 % 2 == 1,
                     rounded: corner && !heavy && packed / 64 % 2 == 1)
    }

    private static let arrowMarks: [String: Mark] = [
        "►": .arrow(.right), "◄": .arrow(.left), "▲": .arrow(.up), "▼": .arrow(.down),
        "▷": .openArrow(.right), "◁": .openArrow(.left), "×": .cross, "○": .circle, "◆": .diamond,
    ]

    private static let frameMarks: [String: Mark] = [
        "<": .point(.left), ">": .point(.right), "(": .bulge(.left), ")": .bulge(.right),
    ]

    /// A database's sides, written as double lines.
    private static let frameLines: [String: Lines] = [
        "║": Lines(up: .double, down: .double),
        "╓": Lines(right: .light, down: .double),
        "╖": Lines(down: .double, left: .light),
        "╙": Lines(up: .double, right: .light),
        "╜": Lines(up: .double, left: .light),
    ]
}
