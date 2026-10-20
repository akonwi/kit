import Foundation

// Routing and drawing for the layered layout: ports, tracks, bands, and the
// drawing itself, ported from `route` and its helpers in
// `apps/cli/tui/mermaid/layers.ard`.

extension Layers {
    /// What `draw` hands to `route`.
    struct Route {
        let graph: Graph
        let title: String
        let layer: [Int]
        let boxes: [Int]
        let widths: [Int]
        let spots: [Spot]
        let segments: [Segment]
        let rows: [[Int]]
        let real: Int
        let virtualStart: Int
        /// Each loop's column from layout, or -1 for an edge that isn't a loop.
        let outer: [Int]
        let thick: [Int]
        let flow: Flow
        let ports: Ports
    }

    /// Edges that share a track: those from one source, those into one
    /// target, or a single edge.
    struct Star {
        var key: String
        var left: Int
        var right: Int
        /// Whether the star is one edge running straight down, needing no track.
        var straight: Bool
        var track: Int
        /// The columns of its stems and drops, and its set of joined lines.
        var stems: [Int]
        var drops: [Int]
        var group: Int
    }

    /// Ports, tracks, rows, and the drawing.
    static func route(_ input: Route, centers: [Int]) throws -> Canvas {
        let graph = input.graph, layer = input.layer, boxes = input.boxes, widths = input.widths
        let spots = input.spots, segments = input.segments, rows = input.rows, real = input.real
        let virtualStart = input.virtualStart, thick = input.thick, flow = input.flow, ports = input.ports
        let count = layer.count
        let depth = rows.count
        var center = centers
        // A stem leaving one vertex must not share a column with a drop into
        // an unrelated vertex below, or the two would read as one edge. Drops
        // shift within their box, stems within theirs; placeholders move aside.
        var stemShift = fill(count, 0)
        var dropShift = fill(count, 0)
        // Ports sit two cells apart across a vertex, in the order of where
        // their other ends lie.
        let stemOffset = portOffsets(segments, ports.stem, center, stems: true)
        let dropOffset = portOffsets(segments, ports.drop, center, stems: false)
        // The cells each vertex's ports reach past their middle one.
        let stemCounts = portCounts(ports.stem, segments.map(\.from), count)
        let dropCounts = portCounts(ports.drop, segments.map(\.to), count)
        let stemReach = (0..<count).map { get(stemCounts, $0) - 1 }
        let dropReach = (0..<count).map { get(dropCounts, $0) - 1 }
        // A vertex's drops shift to line one up with its predecessor's stem
        // when they all stay inside its box, so that edge runs straight; the
        // smallest shift wins.
        for vertex in 0..<real {
            let box = get(boxes, vertex)
            let x = get(center, vertex)
            let reach = get(dropReach, vertex)
            let low = x - box / 2 + 1 + reach
            let high = x - box / 2 + box - 2 - reach
            var best = 0
            var found = false
            for (index, segment) in segments.enumerated() {
                let stem = get(center, segment.from) + get(stemOffset, index)
                let shift = stem - x - get(dropOffset, index)
                if segment.to == vertex && x + shift >= low && x + shift <= high {
                    if !found || distance(shift, 0) < distance(best, 0) {
                        best = shift
                        found = true
                    }
                }
            }
            if found && !hasDropLabel(graph, spots, vertex) { dropShift.put(vertex, best) }
        }
        func stemAt(_ index: Int) -> Int {
            let from = segments[index].from
            return get(center, from) + get(stemShift, from) + get(stemOffset, index)
        }
        func dropAt(_ index: Int) -> Int {
            let to = segments[index].to
            return get(center, to) + get(dropShift, to) + get(dropOffset, index)
        }
        // Each channel's segments, since only those can meet.
        var channels = Array(repeating: [Int](), count: rows.count)
        for (numbered, segment) in segments.enumerated() {
            let k = get(layer, segment.from)
            if k >= 0 && k < channels.count { channels[k].append(numbered) }
        }
        var changed = true
        var rounds = 0
        while changed && rounds < 64 {
            changed = false
            // Cheap tests first: only a stem and drop that meet need the
            // search for a path between them.
            for (ai, a) in segments.enumerated() {
                for bi in channels.at(get(layer, a.from)) ?? [] {
                    let b = segments[bi]
                    // A loop's entry has no stem and its exit no drop.
                    let drawn = a.from < virtualStart && b.to < virtualStart
                    let meet = drawn && stemAt(ai) == dropAt(bi)
                    // Lines in one set are joined anyway.
                    let related = get(ports.group, ai) == get(ports.group, bi)
                    if meet && !related {
                        let target = get(boxes, b.to)
                        let source = get(boxes, a.from)
                        let dropRoom = target - target / 2 - 2 - get(dropReach, b.to)
                        let stemRoom = 1 - source / 2 + get(stemReach, a.from)
                        if b.to < real && get(dropShift, b.to) < dropRoom {
                            dropShift.put(b.to, get(dropShift, b.to) + 1)
                        } else if a.from < real && get(stemShift, a.from) > stemRoom {
                            stemShift.put(a.from, get(stemShift, a.from) - 1)
                        } else {
                            // Move the placeholder and everything after it in its layer.
                            var moving = false
                            for member in rows.at(get(layer, b.to)) ?? [] {
                                if member == b.to { moving = true }
                                if moving { center.put(member, get(center, member) + 1) }
                            }
                        }
                        changed = true
                    }
                }
            }
            rounds += 1
        }
        // Moving vertices aside may leave room before the first; close it,
        // with the loops' columns.
        var least = -1
        for vertex in 0..<virtualStart {
            let edgeLeft = get(center, vertex) - get(widths, vertex) / 2
            if least < 0 || edgeLeft < least { least = edgeLeft }
        }
        let gap = max(least, 0)
        for vertex in 0..<count { center[vertex] -= gap }
        // Loops take their columns past the right edge only now that nothing
        // moves: in the order chosen for them, shorter loops inside.
        let across = flow == .right || flow == .left
        var right = 0
        for vertex in 0..<virtualStart {
            let room = get(widths, vertex)
            right = max(right, get(center, vertex) - room / 2 + room)
        }
        var looped: [Int] = []
        var numbers: [Int] = []
        for (edgeIndex, x) in input.outer.enumerated() {
            if x >= 0 {
                numbers.append(looped.count)
                looped.append(edgeIndex)
            } else {
                numbers.append(-1)
            }
        }
        var columns = fill(input.outer.count, -1)
        var column = right + 1
        for edgeNumber in sortBy(looped, input.outer) {
            let edge = graph.edges[edgeNumber]
            columns.put(edgeNumber, column)
            // The loop's exit and entry stand in its column.
            let exit = virtualStart + 2 * get(numbers, edgeNumber)
            center.put(exit, column)
            center.put(exit + 1, column)
            column += 2
            if edge.label != "" && !across { column += TextWidth.of(edge.label) + 2 }
        }
        if changed { throw NotDrawn("the flowchart couldn't be laid out") }

        // Group each channel's segments into stars and give them tracks.
        let stemUses = uses(ports.stem)
        let dropUses = uses(ports.drop)
        var starOf = fill(segments.count, 0)
        var stars: [Star] = []
        var tracks = fill(depth, 0)
        var k = 0
        while k + 1 < depth {
            var found: [Int] = []
            let first = stars.count
            for (index, segment) in segments.enumerated() where get(layer, segment.from) == k {
                let stemPort = get(ports.stem, index)
                let dropPort = get(ports.drop, index)
                let key = get(stemUses, stemPort) > 1 ? "from:\(stemPort)"
                    : get(dropUses, dropPort) > 1 ? "into:\(dropPort)" : "edge:\(index)"
                let low = min(stemAt(index), dropAt(index))
                let high = max(stemAt(index), dropAt(index))
                var star = -1
                for search in first..<stars.count where stars[search].key == key { star = search }
                if star < 0 {
                    starOf.put(index, stars.count)
                    found.append(stars.count)
                    stars.append(Star(key: key, left: low, right: high, straight: low == high && key == "edge:\(index)",
                                      track: -1, stems: [stemAt(index)], drops: [dropAt(index)],
                                      group: get(ports.group, index)))
                } else {
                    starOf.put(index, star)
                    stars[star].left = min(stars[star].left, low)
                    stars[star].right = max(stars[star].right, high)
                    stars[star].stems.append(stemAt(index))
                    stars[star].drops.append(dropAt(index))
                }
            }
            // Tracks: taking stars from the left, each joins the track, or
            // opens a new one at the place among them, where it crosses the
            // fewest lines, preferring to join and then the topmost. Stars
            // share a track only when they stay clear of each other.
            var lefts = fill(stars.count, 0)
            for star in found { lefts.put(star, stars[star].left) }
            var level = fill(stars.count, -1)
            var placed: [Int] = []
            var used = 0
            for star in sortBy(found, lefts) {
                let current = stars[star]
                if current.straight { continue }
                // Candidates: joining track t is 2t + 1; opening a track
                // before track t is 2t.
                var best = 2 * used
                var fewest = -1
                var candidate = 0
                while candidate <= 2 * used {
                    let joining = candidate % 2 == 1
                    let at = candidate / 2
                    var cost = 0
                    for other in placed {
                        let them = stars[other]
                        let track = get(level, other)
                        let clear = them.right + 1 < current.left || current.right + 1 < them.left
                        if cost < 0 {
                            continue
                        } else if joining && track == at && !clear {
                            cost = -1
                        } else if joining && track == at {
                            continue
                        } else if track < at {
                            cost += crossingsAbove(them, current)
                        } else {
                            cost += crossingsAbove(current, them)
                        }
                    }
                    let better = fewest < 0 || cost < fewest || cost == fewest && joining && best % 2 == 0
                    if cost >= 0 && better {
                        fewest = cost
                        best = candidate
                    }
                    candidate += 1
                }
                let at = best / 2
                if best % 2 == 0 {
                    // Open a track: those from here on move down one.
                    for other in placed where get(level, other) >= at { level.put(other, get(level, other) + 1) }
                    used += 1
                }
                level.put(star, at)
                placed.append(star)
            }
            for star in placed { stars[star].track = get(level, star) }
            tracks.put(k, used)
            k += 1
        }

        // Bands along the flow: each layer as thick as its thickest member,
        // then its channel: stem labels, tracks, drop labels, and arrows.
        // Flowing across, labels lie along their lines, so their bands are as
        // long as the labels, and lines get a cell of lead at each end.
        let lead = across ? 1 : 0
        var stemRows = fill(depth, lead)
        var dropRows = fill(depth, lead)
        for segment in segments {
            let edge = graph.edges[segment.edge]
            let spot = spots.at(segment.edge) ?? .none
            let k = get(layer, segment.from)
            let size = across ? TextWidth.of(edge.label) + 2 + 2 * lead : 1
            if spot == .stem && segment.from == edge.from { stemRows.put(k, max(get(stemRows, k), size)) }
            if spot == .drop && segment.to == edge.to { dropRows.put(k, max(get(dropRows, k), size)) }
        }
        // A layer added only to carry loops is empty and takes no room.
        let heights = rows.map { row in row.reduce(0) { max($0, get(thick, $1)) } }
        var tops: [Int] = []
        var y = 0
        for k in 0..<depth {
            tops.append(y)
            y += get(heights, k) + get(stemRows, k) + max(get(tracks, k), 1) + get(dropRows, k) + 1
        }
        // A channel added after the last layer ends with its tracks.
        let last = depth - 1
        let span = get(heights, last) == 0
            ? get(tops, last - 1) + get(heights, last - 1) + get(stemRows, last - 1) + max(get(tracks, last - 1), 1)
            : get(tops, last) + get(heights, last)
        var cross = 0
        for (edgeIndex, edge) in graph.edges.enumerated() {
            let x = get(columns, edgeIndex)
            if x >= 0 {
                cross = max(cross, x + 1)
                if edge.label != "" && !across { cross = max(cross, x + TextWidth.of(edge.label) + 3) }
            }
        }
        for vertex in 0..<count {
            let room = get(widths, vertex)
            cross = max(cross, get(center, vertex) - room / 2 + room)
        }
        let offset = input.title == "" ? 0 : 2
        let pen = Pen(flow: flow, span: span, offset: offset)
        let width = max(TextWidth.of(input.title), across ? span : cross)
        let height = offset + (across ? cross : span)
        if width > maxWidth { throw NotDrawn("the flowchart is wider than \(maxWidth) columns") }
        if height > maxHeight { throw NotDrawn("the flowchart is taller than \(maxHeight) rows") }

        var drawing = Canvas(width: width, height: height)
        if input.title != "" { drawing.write(0, 0, input.title, .title, bold: true) }
        for vertex in 0..<count {
            let k = get(layer, vertex)
            let top = get(tops, k)
            let c = get(center, vertex)
            if vertex < real {
                let node = graph.nodes[vertex]
                let box = get(boxes, vertex)
                if across {
                    let x0 = min(pen.x(c, top), pen.x(c, top + get(thick, vertex) - 1))
                    drawNode(&drawing, node, x0: x0, top: pen.y(c, top) - box / 2, box: get(thick, vertex), height: box)
                } else {
                    let y0 = min(pen.y(c, top), pen.y(c, top + 2))
                    drawNode(&drawing, node, x0: c - box / 2, top: y0, box: box, height: 3)
                }
            } else if vertex < virtualStart {
                // A placeholder carries its edge's line through the layer.
                let stroke = strokeInto(graph, segments, vertex)
                pen.along(&drawing, c, top, top + get(heights, k) - 1, dashed: stroke == .dotted, heavy: stroke == .thick)
            }
        }
        var heads: [(x: Int, y: Int, glyph: String)] = []
        // Where decisions fork: column and row on the drawing.
        var forks: [(x: Int, y: Int)] = []
        // Labels: edge, first column, row, and the column of the label's own
        // line beside it, or -1 when it lies along its line.
        var labels: [(edge: Int, x: Int, y: Int, line: Int)] = []
        var exitRows = fill(graph.edges.count, 0)
        var entryRows = fill(graph.edges.count, 0)
        // Loops' runs beneath a diagram flowing sideways, and climbs beside
        // one flowing down or up, for their labels.
        var runs: [(edge: Int, x0: Int, x1: Int, y: Int)] = []
        var climbs: [(edge: Int, x: Int, y0: Int, y1: Int, left: Int)] = []
        // Places a label across the band from a0 to a1 on the line at cross;
        // flowing down or up, on the band's middle row.
        func place(_ edge: Int, _ c: Int, _ a0: Int, _ a1: Int) {
            let size = TextWidth.of(" \(graph.edges[edge].label) ")
            if across {
                let x0 = min(pen.x(c, a0), pen.x(c, a1))
                let x1 = max(pen.x(c, a0), pen.x(c, a1))
                labels.append((edge, (x0 + x1 + 1 - size) / 2, pen.y(c, a0), -1))
            } else {
                labels.append((edge, c - size / 2, pen.y(c, (a0 + a1) / 2), c))
            }
        }
        for (index, segment) in segments.enumerated() {
            let edge = graph.edges[segment.edge]
            let spot = spots.at(segment.edge) ?? .none
            let k = get(layer, segment.from)
            let bandEnd = get(tops, k) + get(heights, k) - 1
            // A line leaves its node's far border; a placeholder's band.
            let start = segment.from < real ? get(tops, k) + get(thick, segment.from) - 1 : bandEnd
            let below = get(tops, k + 1)
            let target = segment.to == edge.to
            let head = target && edge.head != .none
            let end = head ? below - 1 : below
            let dashed = edge.stroke == .dotted
            let heavy = edge.stroke == .thick
            let star = stars.at(get(starOf, index)) ?? Star(key: "", left: 0, right: 0, straight: false, track: 0,
                                                            stems: [], drops: [], group: 0)
            let from = stemAt(index)
            let to = dropAt(index)
            let row = bandEnd + 1 + get(stemRows, k) + star.track
            if segment.to >= virtualStart {
                // A loop leaves its source for its outer column, and enters
                // its target from it.
                pen.along(&drawing, from, start, row, dashed: dashed, heavy: heavy)
                pen.side(&drawing, row, from, to, dashed: dashed, heavy: heavy)
                exitRows.put(segment.edge, row)
            } else if segment.from >= virtualStart {
                pen.side(&drawing, row, to, from, dashed: dashed, heavy: heavy)
                pen.along(&drawing, to, row, end, dashed: dashed, heavy: heavy)
                entryRows.put(segment.edge, row)
            } else if star.straight {
                pen.along(&drawing, from, start, end, dashed: dashed, heavy: heavy)
            } else {
                pen.along(&drawing, from, start, row, dashed: dashed, heavy: heavy)
                if from != to { pen.side(&drawing, row, from, to, dashed: dashed, heavy: heavy) }
                pen.along(&drawing, to, row, end, dashed: dashed, heavy: heavy)
            }
            if head { heads.append((pen.x(to, below - 1), pen.y(to, below - 1), pen.head(edge.head))) }
            let first = segment.from == edge.from
            if spot == .drop && target { place(segment.edge, to, below - 1 - get(dropRows, k), below - 2) }
            if spot == .stem && first { place(segment.edge, from, bandEnd + 1, bandEnd + get(stemRows, k)) }
            if spot == .middle && first { place(segment.edge, to, below, below + get(heights, k + 1) - 1) }
            // A decision's branches fork at a diamond.
            let decision = segment.from < real && graph.nodes[segment.from].shape == .decision
            if decision && get(stemUses, get(ports.stem, index)) > 1 && !star.straight {
                forks.append((pen.x(from, row), pen.y(from, row)))
            }
        }
        for fork in forks { drawing.write(fork.x, fork.y, "◆", .arrow, bold: false) }
        for head in heads { drawing.write(head.x, head.y, head.glyph, .arrow, bold: false) }
        // Each loop climbs its outer column from its exit to its entry. Its
        // label sits beside the climb, or along it when flowing across.
        for (edgeIndex, edge) in graph.edges.enumerated() {
            let x = get(columns, edgeIndex)
            guard x >= 0 else { continue }
            let top = get(entryRows, edgeIndex)
            let bottom = get(exitRows, edgeIndex)
            pen.along(&drawing, x, top, bottom, dashed: edge.stroke == .dotted, heavy: edge.stroke == .thick)
            let x0 = min(pen.x(x, top), pen.x(x, bottom))
            let x1 = max(pen.x(x, top), pen.x(x, bottom))
            let y0 = min(pen.y(x, top), pen.y(x, bottom))
            let y1 = max(pen.y(x, top), pen.y(x, bottom))
            if edge.label != "" {
                if across {
                    runs.append((edgeIndex, x0, x1, y0))
                } else {
                    climbs.append((edgeIndex, x0, y0, y1, x0 + 1))
                }
            }
        }
        // A loop's label takes the clearest stretch of its run, or the
        // clearest row beside its climb, once every line is drawn.
        for run in runs {
            let size = TextWidth.of(" \(graph.edges[run.edge].label) ")
            let x = clearRun(drawing, y: run.y, x0: run.x0, x1: run.x1, width: size)
            if x >= 0 { labels.append((run.edge, x - size / 2, run.y, -1)) }
        }
        for climb in climbs {
            let size = TextWidth.of(" \(graph.edges[climb.edge].label) ")
            let y = clearRows(drawing, line: climb.x, y0: climb.y0, y1: climb.y1, left: climb.left, width: size)
            if y >= 0 { labels.append((climb.edge, climb.left, y, climb.x)) }
        }
        // Labels go last, each over its own line and nothing else.
        var fits = true
        var placed = fill(graph.edges.count, 0)
        for label in labels {
            let text = " \(graph.edges[label.edge].label) "
            let x1 = label.x + TextWidth.of(text) - 1
            var column = label.x
            while column <= x1 {
                let shown = column >= 0 && column < width ? drawing.shown(column, label.y) : ""
                let clear = label.line < 0
                    ? shown == "─" || shown == "┈" || shown == "━"
                    : shown == " " || column == label.line
                if !clear { fits = false }
                column += 1
            }
            if fits {
                drawing.write(label.x, label.y, text, .label, bold: false)
                placed.put(label.edge, 1)
            }
        }
        for (edgeIndex, edge) in graph.edges.enumerated() where edge.label != "" && get(placed, edgeIndex) == 0 {
            fits = false
        }
        guard fits else { throw NotDrawn("the edge labels don't fit") }
        return drawing
    }

    /// Maps layout coordinates onto the drawing. Layout stacks layers down
    /// the `along` axis and places each layer's members along the `cross`
    /// axis; the drawing flows down, up, right, or left. `span` is the
    /// layout's extent along the flow, and `offset` the rows above it for a title.
    struct Pen {
        let flow: Flow
        let span: Int
        let offset: Int

        func x(_ cross: Int, _ along: Int) -> Int {
            switch flow {
            case .down, .up: return cross
            case .right: return along
            case .left: return span - 1 - along
            }
        }

        func y(_ cross: Int, _ along: Int) -> Int {
            switch flow {
            case .down: return offset + along
            case .up: return offset + span - 1 - along
            case .right, .left: return offset + cross
            }
        }

        /// Draws a line along the flow, at cross from a0 to a1.
        func along(_ drawing: inout Canvas, _ c: Int, _ a0: Int, _ a1: Int, dashed: Bool, heavy: Bool) {
            let x0 = x(c, a0), x1 = x(c, a1), y0 = y(c, a0), y1 = y(c, a1)
            if flow == .down || flow == .up {
                drawing.vertical(x0, min(y0, y1), max(y0, y1), .arrow, dashed: dashed, heavy: heavy)
            } else {
                drawing.horizontal(y0, min(x0, x1), max(x0, x1), .arrow, dashed: dashed, heavy: heavy)
            }
        }

        /// Draws a line across the flow, at along from c0 to c1.
        func side(_ drawing: inout Canvas, _ a: Int, _ c0: Int, _ c1: Int, dashed: Bool, heavy: Bool) {
            let x0 = x(c0, a), x1 = x(c1, a), y0 = y(c0, a), y1 = y(c1, a)
            if flow == .down || flow == .up {
                drawing.horizontal(y0, min(x0, x1), max(x0, x1), .arrow, dashed: dashed, heavy: heavy)
            } else {
                drawing.vertical(x0, min(y0, y1), max(y0, y1), .arrow, dashed: dashed, heavy: heavy)
            }
        }

        /// The arrowhead pointing along the flow.
        func head(_ head: Head) -> String {
            switch head {
            case .arrow:
                switch flow {
                case .down: return "▼"
                case .up: return "▲"
                case .right: return "►"
                case .left: return "◄"
                }
            case .cross: return "×"
            case .circle: return "○"
            case .none: return ""
            }
        }
    }

    /// The center of the window of width cells on row y, strictly between
    /// x0 and x1, that covers only a plain sideways line, nearest the
    /// middle; or -1.
    static func clearRun(_ drawing: Canvas, y: Int, x0: Int, x1: Int, width: Int) -> Int {
        let middle = (x0 + x1) / 2
        var best = -1
        var start = x0 + 1
        while start + width - 1 <= x1 - 1 {
            var clear = true
            for column in start..<(start + width) {
                let shown = drawing.shown(column, y)
                if shown != "─" && shown != "┈" && shown != "━" { clear = false }
            }
            let x = start + width / 2
            if clear && (best < 0 || distance(x, middle) < distance(best, middle)) { best = x }
            start += 1
        }
        return best
    }

    /// The row strictly between y0 and y1, nearest the middle, where the
    /// upright line at column line runs plainly and the width cells from
    /// left are blank but for that line; or -1.
    static func clearRows(_ drawing: Canvas, line: Int, y0: Int, y1: Int, left: Int, width: Int) -> Int {
        let middle = (y0 + y1) / 2
        var best = -1
        var y = y0 + 1
        while y < y1 {
            let upright = drawing.shown(line, y)
            var clear = upright == "│" || upright == "┊" || upright == "┃"
            for column in left..<(left + width) {
                let outside = column < 0 || column >= drawing.width
                if outside || column != line && drawing.shown(column, y) != " " { clear = false }
            }
            if clear && (best < 0 || distance(y, middle) < distance(best, middle)) { best = y }
            y += 1
        }
        return best
    }

    /// Whether an edge into node carries a label on its drop, so the drop
    /// stays centered under the label.
    static func hasDropLabel(_ graph: Graph, _ spots: [Spot], _ node: Int) -> Bool {
        graph.edges.enumerated().contains { index, edge in edge.to == node && spots.at(index) == .drop }
    }

    /// Draws a node's outline in its shape, with its text inside.
    static func drawNode(_ drawing: inout Canvas, _ node: Node, x0: Int, top: Int, box: Int, height: Int) {
        let x1 = x0 + box - 1
        let bottom = top + height - 1
        let middle = top + height / 2
        let rounded = node.shape == .round || node.shape == .stadium || node.shape == .circle
        let shape: Figure.NodeShape
        switch node.shape {
        case .box: shape = .box
        case .round: shape = .round
        case .stadium: shape = .stadium
        case .circle: shape = .circle
        case .database: shape = .database
        case .decision: shape = .decision
        case .elsewhere: shape = .elsewhere
        }
        drawing.figures.append(Figure(kind: .node(shape), x0: x0, y0: top, x1: x1, y1: bottom))
        if node.shape == .elsewhere {
            drawing.horizontal(top, x0, x1, .frame, dashed: true)
            drawing.horizontal(bottom, x0, x1, .frame, dashed: true)
            drawing.vertical(x0, top, bottom, .frame, dashed: true)
            drawing.vertical(x1, top, bottom, .frame, dashed: true)
        } else {
            drawing.outline(x0, top, x1, bottom, .frame, rounded: rounded)
        }
        switch node.shape {
        case .stadium:
            drawing.write(x0, middle, "(", .frame, bold: false)
            drawing.write(x1, middle, ")", .frame, bold: false)
        case .database:
            drawing.write(x0, top, "╓", .frame, bold: false)
            drawing.write(x1, top, "╖", .frame, bold: false)
            var y = top + 1
            while y < bottom {
                drawing.write(x0, y, "║", .frame, bold: false)
                drawing.write(x1, y, "║", .frame, bold: false)
                y += 1
            }
            drawing.write(x0, bottom, "╙", .frame, bold: false)
            drawing.write(x1, bottom, "╜", .frame, bold: false)
        case .decision:
            // A decision points out at both sides; its branches fork at a diamond.
            drawing.write(x0, middle, "<", .frame, bold: false)
            drawing.write(x1, middle, ">", .frame, bold: false)
        default: break
        }
        drawing.write(x0 + (box - TextWidth.of(node.text)) / 2, middle, node.text, .label, bold: false)
    }

    static func distance(_ a: Int, _ b: Int) -> Int { a > b ? a - b : b - a }

    /// The lines that cross a run when star a runs on a track above star b:
    /// b's stems pass down through a's run, and a's drops through b's, except
    /// where they are the run's own stem or drop.
    static func crossingsAbove(_ a: Star, _ b: Star) -> Int {
        var total = 0
        for x in b.stems where x >= a.left && x <= a.right && !a.stems.contains(x) { total += 1 }
        for x in a.drops where x >= b.left && x <= b.right && !b.drops.contains(x) { total += 1 }
        return total
    }

    /// The stroke of the edge whose segment enters vertex.
    static func strokeInto(_ graph: Graph, _ segments: [Segment], _ vertex: Int) -> Stroke {
        var stroke = Stroke.solid
        for segment in segments where segment.to == vertex { stroke = graph.edges[segment.edge].stroke }
        return stroke
    }

    /// The number of segments using each port.
    static func uses(_ ports: [Int]) -> [Int] {
        var top = 0
        for port in ports { top = max(top, port + 1) }
        var counts = fill(top, 0)
        for port in ports { counts.put(port, get(counts, port) + 1) }
        return counts
    }

    /// Each segment's port offset from its vertex's middle: a vertex's ports
    /// sit two cells apart, ordered by the mean position of the vertices at
    /// their lines' other ends, then by port.
    static func portOffsets(_ segments: [Segment], _ ports: [Int], _ center: [Int], stems: Bool) -> [Int] {
        var top = 0
        for port in ports { top = max(top, port + 1) }
        var owner = fill(top, -1)
        var sums = fill(top, 0)
        var counts = fill(top, 0)
        var vertices = 0
        for (index, segment) in segments.enumerated() {
            let port = get(ports, index)
            let vertex = stems ? segment.from : segment.to
            let far = stems ? get(center, segment.to) : get(center, segment.from)
            owner.put(port, vertex)
            sums.put(port, get(sums, port) + far)
            counts.put(port, get(counts, port) + 1)
            vertices = max(vertices, vertex + 1)
        }
        var lists = Array(repeating: [Int](), count: vertices)
        for port in 0..<top {
            let vertex = get(owner, port)
            if vertex >= 0 && vertex < vertices { lists[vertex].append(port) }
        }
        var offsetOf = fill(top, 0)
        for list in lists {
            for mine in list {
                var rank = 0
                for other in list {
                    let before = get(sums, other) * get(counts, mine) < get(sums, mine) * get(counts, other)
                    let tied = get(sums, other) * get(counts, mine) == get(sums, mine) * get(counts, other)
                    if before || tied && other < mine { rank += 1 }
                }
                offsetOf.put(mine, 2 * rank - (list.count - 1))
            }
        }
        return ports.map { get(offsetOf, $0) }
    }
}
