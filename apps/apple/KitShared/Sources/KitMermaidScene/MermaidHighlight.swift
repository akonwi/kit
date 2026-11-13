import Foundation
import KitMermaid
import SwiftUI

/// Where a part of a drawn diagram sits in a scene, for pointing at it and
/// showing it picked. Both apps draw it over the scene with their own
/// colors (ADR 0046's annotations).
public struct MermaidHighlight: Equatable, Sendable {
    /// The part's boxes: a node, participant, note, message, legend, or an
    /// edge's text, each grown a little past its cells.
    public let boxes: [CGRect]
    /// The part's lines, each from one cell's center to another's.
    public let lines: [Segment]
    /// Every box and line together, for anchoring a comment to the part.
    public let bounds: CGRect

    public struct Segment: Equatable, Sendable {
        public let from: CGPoint
        public let to: CGPoint
    }

    /// How far a box's highlight reaches past its cells, in points.
    public static let outset: CGFloat = 3

    public init(_ part: Part, in drawing: KitMermaid.Canvas, metrics: MermaidScene.Metrics) {
        let cell = metrics.cell
        var boxes: [CGRect] = []
        var lines: [Segment] = []
        var bounds = CGRect.null
        for region in drawing.regions(of: part) {
            if region.line {
                let segment = Segment(
                    from: CGPoint(x: (CGFloat(region.x0) + 0.5) * cell.width, y: (CGFloat(region.y0) + 0.5) * cell.height),
                    to: CGPoint(x: (CGFloat(region.x1) + 0.5) * cell.width, y: (CGFloat(region.y1) + 0.5) * cell.height))
                if !lines.contains(segment) { lines.append(segment) }
                bounds = bounds.union(CGRect(x: CGFloat(region.x0) * cell.width, y: CGFloat(region.y0) * cell.height,
                                             width: CGFloat(region.x1 - region.x0 + 1) * cell.width,
                                             height: CGFloat(region.y1 - region.y0 + 1) * cell.height))
            } else {
                let rect = Self.outline(region, cell: cell).insetBy(dx: -Self.outset, dy: -Self.outset)
                if !boxes.contains(rect) { boxes.append(rect) }
                bounds = bounds.union(rect)
            }
        }
        self.boxes = boxes
        self.lines = lines
        self.bounds = bounds.isNull ? .zero : bounds
    }

    /// The cells a region covers, in points.
    public static func rect(_ region: Region, cell: CGSize) -> CGRect {
        CGRect(x: CGFloat(region.x0) * cell.width, y: CGFloat(region.y0) * cell.height,
               width: CGFloat(region.x1 - region.x0 + 1) * cell.width,
               height: CGFloat(region.y1 - region.y0 + 1) * cell.height)
    }

    /// Where a box region is drawn: an outline at least three cells each
    /// way, such as a node's, runs through its cells' centers, as the scene
    /// draws it; text and a message fill their cells.
    static func outline(_ region: Region, cell: CGSize) -> CGRect {
        guard region.x1 - region.x0 >= 2, region.y1 - region.y0 >= 2 else { return rect(region, cell: cell) }
        return CGRect(x: (CGFloat(region.x0) + 0.5) * cell.width, y: (CGFloat(region.y0) + 0.5) * cell.height,
                      width: CGFloat(region.x1 - region.x0) * cell.width,
                      height: CGFloat(region.y1 - region.y0) * cell.height)
    }

    /// The part drawn under a point in the scene, if any.
    public static func part(at point: CGPoint, in drawing: KitMermaid.Canvas, metrics: MermaidScene.Metrics) -> Part? {
        guard point.x >= 0, point.y >= 0, metrics.cell.width > 0, metrics.cell.height > 0 else { return nil }
        let x = Int(point.x / metrics.cell.width), y = Int(point.y / metrics.cell.height)
        guard x < drawing.width, y < drawing.height else { return nil }
        return drawing.hit(x, y).map { Part($0.target, $0.index) }
    }

    /// The part's boxes as rounded rectangles.
    public var boxPath: Path {
        var path = Path()
        for box in boxes { path.addRoundedRect(in: box, cornerSize: CGSize(width: 4, height: 4)) }
        return path
    }

    /// The part's lines.
    public var linePath: Path {
        var path = Path()
        for line in lines {
            path.move(to: line.from)
            path.addLine(to: line.to)
        }
        return path
    }
}
