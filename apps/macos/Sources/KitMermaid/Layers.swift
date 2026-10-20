import Foundation

// Layered drawings of directed graphs, ported from the reference in
// `apps/cli/tui/mermaid/layers.ard`.
//
// Edges that close a loop are set aside first. Each later climbs its own
// column past the right edge, leaving its source and entering its target
// through the channels below and above them. Nodes are assigned layers by
// their longest path from a source, and an edge that spans several layers
// passes through a placeholder in each layer between. Each layer is
// reordered by its neighbors' positions to reduce crossings, and nodes slide
// toward their neighbors without overlapping. Between two layers runs a
// channel of tracks; routing and drawing are in `LayersRoute.swift`.

enum Layers {
    static let maxVertices = 400
    static let maxWidth = 480
    static let maxHeight = 800
    /// Cells kept clear between neighboring boxes, and beside a passing line.
    static let boxGap = 3
    static let lineGap = 2
    /// Ordering and placement passes.
    static let sweeps = 4
    static let placementPasses = 8
    /// Cells a label leaves around itself on a node's border.
    static let labelRoom = 4

    /// The direction a graph flows: layers stack down, up, right, or left.
    enum Flow { case down, up, right, left }

    enum Shape {
        case box, round, stadium, circle, database, decision
        /// A subgraph drawn on its own, or a node outside the drawing: a
        /// dashed box standing in for what is drawn elsewhere.
        case elsewhere
    }

    enum Stroke: Int { case solid = 0, dotted = 1, thick = 2 }

    enum Head: Int { case arrow = 0, cross = 1, circle = 2, none = 3 }

    struct Node {
        var id: String
        var text: String
        var shape: Shape
    }

    struct Edge {
        var from: Int
        var to: Int
        var stroke: Stroke
        var head: Head
        /// Text drawn on the edge, or empty.
        var label: String
    }

    /// Where an edge's label is drawn: on its drop into a target no other
    /// edge enters, on its stem from a source no other edge leaves, or else
    /// on the first placeholder of an edge that skips layers. A loop's label
    /// goes `back`, beside its outer column.
    enum Spot { case none, drop, stem, middle, back }

    struct Graph {
        var nodes: [Node]
        var edges: [Edge]
    }

    /// One hop of an edge between adjacent layers.
    struct Segment {
        var from: Int
        var to: Int
        var edge: Int
    }

    /// Where segments leave and enter their vertices: each segment's stem
    /// port and drop port, and its set of joined lines.
    struct Ports {
        var stem: [Int]
        var drop: [Int]
        var group: [Int]
    }

    // MARK: - Ard list semantics

    /// values[index], or 0 out of range.
    static func get(_ values: [Int], _ index: Int) -> Int {
        index >= 0 && index < values.count ? values[index] : 0
    }

    static func fill(_ size: Int, _ value: Int) -> [Int] {
        Array(repeating: value, count: max(size, 0))
    }

    // MARK: - Layering

    /// Marks with 1 each edge that closes a loop: a depth-first walk from
    /// each node in declaration order finds the edges that return to a node
    /// still being walked, including an edge from a node to itself.
    static func backEdges(_ graph: Graph) -> [Int] {
        var state = fill(graph.nodes.count, 0)
        var back = fill(graph.edges.count, 0)
        func visit(_ node: Int) {
            state.put(node, 1)
            for (index, edge) in graph.edges.enumerated() where edge.from == node {
                switch get(state, edge.to) {
                case 0: visit(edge.to)
                case 1: back.put(index, 1)
                default: break
                }
            }
            state.put(node, 2)
        }
        for node in graph.nodes.indices where get(state, node) == 0 {
            visit(node)
        }
        return back
    }

    /// The nodes in an order where every edge points forward, preferring
    /// earlier declarations.
    static func topological(_ graph: Graph) throws -> [Int] {
        let count = graph.nodes.count
        var incoming = fill(count, 0)
        for edge in graph.edges { incoming.put(edge.to, get(incoming, edge.to) + 1) }
        var done = fill(count, 0)
        var order: [Int] = []
        var progress = true
        while progress {
            progress = false
            var picked = -1
            for node in 0..<count where picked < 0 && get(done, node) == 0 && get(incoming, node) == 0 {
                picked = node
            }
            if picked >= 0 {
                done.put(picked, 1)
                order.append(picked)
                progress = true
                for edge in graph.edges where edge.from == picked {
                    incoming.put(edge.to, get(incoming, edge.to) - 1)
                }
            }
        }
        guard order.count == count else { throw NotDrawn("the flowchart couldn't be laid out") }
        return order
    }

    /// Each node's layer: one past its deepest predecessor. A source moves
    /// down to sit just above its nearest successor.
    static func layers(_ graph: Graph, _ order: [Int], _ lengths: [Int]) -> [Int] {
        var layer = fill(graph.nodes.count, 0)
        for node in order {
            for (index, edge) in graph.edges.enumerated() where edge.from == node {
                layer.put(edge.to, max(get(layer, edge.to), get(layer, node) + get(lengths, index)))
            }
        }
        var index = order.count - 1
        while index >= 0 {
            let node = get(order, index)
            var hasIncoming = false
            var nearest = -1
            for (edgeIndex, edge) in graph.edges.enumerated() {
                if edge.to == node { hasIncoming = true }
                let length = get(lengths, edgeIndex)
                if edge.from == node {
                    let below = get(layer, edge.to) - length
                    nearest = nearest < 0 ? below : min(nearest, below)
                }
            }
            if !hasIncoming && nearest > get(layer, node) { layer.put(node, nearest) }
            index -= 1
        }
        return layer
    }

    /// The vertices of layer k, ordered by position.
    static func members(_ layer: [Int], _ pos: [Int], _ k: Int) -> [Int] {
        sortBy(layer.indices.filter { layer[$0] == k }, pos)
    }

    /// values ordered by their keys, keeping the given order among equal
    /// keys: the reference's bottom-up merge sort.
    static func sortBy(_ values: [Int], _ keys: [Int]) -> [Int] {
        var sorted = values
        var width = 1
        while width < sorted.count {
            var merged: [Int] = []
            merged.reserveCapacity(sorted.count)
            var start = 0
            while start < sorted.count {
                let middle = min(start + width, sorted.count)
                let end = min(start + 2 * width, sorted.count)
                var left = start
                var right = middle
                while left < middle || right < end {
                    let takeLeft = right >= end
                        || left < middle && get(keys, get(sorted, left)) <= get(keys, get(sorted, right))
                    if takeLeft {
                        merged.append(get(sorted, left))
                        left += 1
                    } else {
                        merged.append(get(sorted, right))
                        right += 1
                    }
                }
                start = end
            }
            sorted = merged
            width *= 2
        }
        return sorted
    }

    /// Reorders layer k by the mean position of each vertex's neighbors.
    static func reorder(_ layer: [Int], _ pos: inout [Int], _ neighbors: [[Int]], _ k: Int) {
        let current = members(layer, pos, k)
        var keys = fill(layer.count, 0)
        for vertex in current {
            let near = neighbors.at(vertex) ?? []
            var total = 0
            for other in near { total += get(pos, other) }
            keys.put(vertex, near.isEmpty ? get(pos, vertex) * 1000 : total * 1000 / near.count)
        }
        for (index, vertex) in sortBy(current, keys).enumerated() {
            pos.put(vertex, index)
        }
    }

    static func crossings(_ layer: [Int], _ pos: [Int], _ segments: [Segment]) -> Int {
        // Only segments leaving the same layer can cross.
        var depth = 0
        for k in layer { depth = max(depth, k + 1) }
        var tops = Array(repeating: [Int](), count: depth)
        var bottoms = Array(repeating: [Int](), count: depth)
        for segment in segments {
            let k = get(layer, segment.from)
            guard k >= 0 && k < depth else { continue }
            tops[k].append(get(pos, segment.from))
            bottoms[k].append(get(pos, segment.to))
        }
        var total = 0
        for k in 0..<depth {
            let top = tops[k], bottom = bottoms[k]
            for first in top.indices {
                var second = first + 1
                while second < top.count {
                    let above = top[first] - top[second]
                    let below = bottom[first] - bottom[second]
                    if above < 0 && below > 0 || above > 0 && below < 0 { total += 1 }
                    second += 1
                }
            }
        }
        return total
    }

    /// The least distance between the centers of neighboring vertices a and
    /// b. Across the flow, neighbors are rows apart rather than columns, so a
    /// single blank row keeps them apart.
    static func separation(_ across: Bool, _ widths: [Int], _ a: Int, _ b: Int) -> Int {
        let left = get(widths, a)
        let right = get(widths, b)
        let gap = across ? 1 : left == 1 || right == 1 ? lineGap : boxGap
        return left - left / 2 + gap + right / 2
    }

    /// Moves the vertices of a layer toward desired centers while keeping
    /// them apart, balancing pushes from the left and the right.
    static func settle(_ across: Bool, _ row: [Int], _ widths: [Int], _ desired: [Int], _ center: inout [Int]) {
        let count = row.count
        var fromLeft = fill(count, 0)
        var fromRight = fill(count, 0)
        for index in 0..<count {
            let vertex = row[index]
            let want = get(desired, vertex)
            fromLeft[index] = index == 0
                ? want : max(want, fromLeft[index - 1] + separation(across, widths, row[index - 1], vertex))
        }
        var index = count - 1
        while index >= 0 {
            let vertex = row[index]
            let want = get(desired, vertex)
            fromRight[index] = index == count - 1
                ? want : min(want, fromRight[index + 1] - separation(across, widths, vertex, row[index + 1]))
            index -= 1
        }
        var previous = 0
        for index in 0..<count {
            let vertex = row[index]
            var x = (fromLeft[index] + fromRight[index]) / 2
            if index > 0 { x = max(x, previous + separation(across, widths, row[index - 1], vertex)) }
            center.put(vertex, x)
            previous = x
        }
    }

    // MARK: - Ports

    private static func root(_ parent: [Int], _ port: Int) -> Int {
        var at = port
        while get(parent, at) != at { at = get(parent, at) }
        return at
    }

    /// The set of joined lines each segment belongs to: segments sharing a
    /// stem or drop port are joined.
    static func join(_ stem: [Int], _ drop: [Int], _ ports: Int) -> [Int] {
        var parent = Array(0..<max(ports, 0))
        for index in stem.indices {
            let a = root(parent, stem[index])
            let b = root(parent, get(drop, index))
            if a != b { parent.put(a, b) }
        }
        return stem.map { root(parent, $0) }
    }

    /// Gives each segment its ports. Edges share a stem when they leave one
    /// vertex with the same stroke, and a drop when they enter one vertex
    /// with the same stroke and head, so every shared line is drawn as all
    /// its edges are. A set of joined lines missing one of its stem-to-drop
    /// edges gives each of its segments its own drop, or its own stem when
    /// that takes fewer ports.
    static func assignPorts(_ graph: Graph, _ segments: [Segment]) -> Ports {
        var ids: [String: Int] = [:]
        var total = 0
        var stem: [Int] = []
        var drop: [Int] = []
        for segment in segments {
            let edge = graph.edges[segment.edge]
            let stemKey = "s\(segment.from):\(edge.stroke.rawValue)"
            let dropKey = segment.to == edge.to
                ? "d\(segment.to):\(edge.stroke.rawValue):\(edge.head.rawValue)" : "d\(segment.to)"
            for key in [stemKey, dropKey] where ids[key] == nil {
                ids[key] = total
                total += 1
            }
            stem.append(ids[stemKey] ?? 0)
            drop.append(ids[dropKey] ?? 0)
        }
        let groups = join(stem, drop, total)
        // Count each set's segments, stems, and drops.
        var lines = fill(total, 0)
        var stems = fill(total, 0)
        var drops = fill(total, 0)
        var seen = fill(total, 0)
        for (index, group) in groups.enumerated() {
            lines.put(group, get(lines, group) + 1)
            let a = stem[index], b = drop[index]
            if get(seen, a) == 0 {
                seen.put(a, 1)
                stems.put(group, get(stems, group) + 1)
            }
            if get(seen, b) == 0 {
                seen.put(b, 1)
                drops.put(group, get(drops, group) + 1)
            }
        }
        for (index, group) in groups.enumerated() {
            let n = get(lines, group), s = get(stems, group), d = get(drops, group)
            if n != s * d {
                if d >= s { drop[index] = total } else { stem[index] = total }
                total += 1
            }
        }
        // A chain of segments, an edge skipping layers or a loop, joins the
        // lines at its two ends across channels, so it shares a port at one
        // end only: when both are shared, its last drop gets its own.
        var stemUses = fill(total, 0)
        var dropUses = fill(total, 0)
        for port in stem { stemUses.put(port, get(stemUses, port) + 1) }
        for port in drop { dropUses.put(port, get(dropUses, port) + 1) }
        var firsts = fill(graph.edges.count, -1)
        var lasts = fill(graph.edges.count, -1)
        for (index, segment) in segments.enumerated() {
            if get(firsts, segment.edge) < 0 { firsts.put(segment.edge, index) }
            lasts.put(segment.edge, index)
        }
        for edgeNumber in graph.edges.indices {
            let first = get(firsts, edgeNumber)
            let last = get(lasts, edgeNumber)
            let chained = first >= 0 && last != first
            if chained && get(stemUses, get(stem, first)) > 1 && get(dropUses, get(drop, last)) > 1 {
                let old = get(drop, last)
                dropUses.put(old, get(dropUses, old) - 1)
                drop.put(last, total)
                dropUses.append(1)
                stemUses.append(0)
                total += 1
            }
        }
        return Ports(stem: stem, drop: drop, group: join(stem, drop, total))
    }

    /// Each vertex's number of ports, where vertices holds the vertex each
    /// segment's port belongs to.
    static func portCounts(_ ports: [Int], _ vertices: [Int], _ count: Int) -> [Int] {
        var top = 0
        for port in ports { top = max(top, port + 1) }
        var seen = fill(top, 0)
        var counts = fill(count, 0)
        for (index, port) in ports.enumerated() where get(seen, port) == 0 {
            seen.put(port, 1)
            let vertex = get(vertices, index)
            counts.put(vertex, get(counts, vertex) + 1)
        }
        return counts
    }

    static func edgesInto(_ graph: Graph, _ node: Int) -> Int {
        graph.edges.reduce(0) { $0 + ($1.to == node ? 1 : 0) }
    }

    static func edgesFrom(_ graph: Graph, _ node: Int) -> Int {
        graph.edges.reduce(0) { $0 + ($1.from == node ? 1 : 0) }
    }

    // MARK: - Layout

    /// Draws graph, with title above it when given.
    static func draw(_ graph: Graph, title: String, flow: Flow) throws -> Canvas {
        // Flowing across, nodes are three rows tall side by side, and as
        // thick as their text along the flow.
        let across = flow == .right || flow == .left
        // Edges that close a loop are set aside; the rest flow forward.
        let back = backEdges(graph)
        var forwardEdges: [Edge] = []
        // A labeled edge whose ends are both shared has no line of its own
        // between adjacent layers, so it spans two, with a placeholder for
        // its label.
        var lengths: [Int] = []
        for (edgeNumber, edge) in graph.edges.enumerated() where get(back, edgeNumber) == 0 {
            forwardEdges.append(edge)
            let shared = edgesInto(graph, edge.to) > 1 && edgesFrom(graph, edge.from) > 1
            lengths.append(edge.label != "" && shared ? 2 : 1)
        }
        let forward = Graph(nodes: graph.nodes, edges: forwardEdges)
        let order = try topological(forward)
        let nodeLayers = layers(forward, order, lengths)
        let real = graph.nodes.count

        // Vertices: the nodes, then a placeholder for each layer an edge
        // skips. Box widths, and the widths vertices are spaced by: a node
        // with a label on its border makes room for it. Each vertex's extent
        // along the flow.
        var layer: [Int] = []
        var boxes: [Int] = []
        var widths: [Int] = []
        var thick: [Int] = []
        for (index, node) in graph.nodes.enumerated() {
            let size = across ? 3 : TextWidth.of(node.text) + 4
            layer.append(get(nodeLayers, index))
            boxes.append(size)
            widths.append(size)
            thick.append(across ? TextWidth.of(node.text) + 4 : 3)
        }
        var spots: [Spot] = []
        for edge in graph.edges {
            let label = TextWidth.of(edge.label)
            let looped = get(back, spots.count) == 1
            let spot: Spot
            if edge.label == "" {
                spot = .none
            } else if looped {
                spot = .back
            } else if edgesInto(graph, edge.to) == 1 {
                spot = .drop
            } else if edgesFrom(graph, edge.from) == 1 {
                spot = .stem
            } else {
                spot = .middle
            }
            // Labels lie along their lines when flowing across, needing no
            // room beside them.
            if spot == .drop && !across { widths.put(edge.to, max(get(widths, edge.to), label + labelRoom)) }
            if spot == .stem && !across { widths.put(edge.from, max(get(widths, edge.from), label + labelRoom)) }
            spots.append(spot)
        }
        var segments: [Segment] = []
        for (edgeIndex, edge) in graph.edges.enumerated() {
            let looped = get(back, edgeIndex) == 1
            var from = edge.from
            var k = looped ? get(layer, edge.to) : get(layer, edge.from) + 1
            while k < get(layer, edge.to) {
                let placeholder = layer.count
                layer.append(k)
                boxes.append(1)
                // The first placeholder of an edge labeled there makes room for it.
                let middle = spots.at(edgeIndex) == .middle && from == edge.from
                let room = middle ? TextWidth.of(edge.label) + labelRoom : 1
                widths.append(across ? 1 : room)
                thick.append(across ? room : 3)
                segments.append(Segment(from: from, to: placeholder, edge: edgeIndex))
                from = placeholder
                k += 1
            }
            if !looped { segments.append(Segment(from: from, to: edge.to, edge: edgeIndex)) }
        }
        let count = layer.count
        var depth = 0
        for k in layer { depth = max(depth, k + 1) }
        // Each loop leaves through an exit and enters through an entry, two
        // vertices added after the rest once loops have their columns.
        var loops: [Int] = []
        var allSegments = segments
        for (edgeIndex, edge) in graph.edges.enumerated() where get(back, edgeIndex) == 1 {
            let exit = count + 2 * loops.count
            allSegments.append(Segment(from: edge.from, to: exit, edge: edgeIndex))
            allSegments.append(Segment(from: exit + 1, to: edge.to, edge: edgeIndex))
            loops.append(edgeIndex)
        }
        let ports = assignPorts(graph, allSegments)
        // A node is long enough across the flow for its ports, two cells apart.
        let vertexCount = count + 2 * loops.count
        let stemCounts = portCounts(ports.stem, allSegments.map(\.from), vertexCount)
        let dropCounts = portCounts(ports.drop, allSegments.map(\.to), vertexCount)
        for node in 0..<real {
            let needed = 2 * max(get(stemCounts, node), get(dropCounts, node)) + 1
            if needed > get(boxes, node) {
                boxes.put(node, needed)
                widths.put(node, max(get(widths, node), needed))
            }
        }
        if count > maxVertices { throw NotDrawn("the flowchart is too large") }
        var preds: [[Int]] = []
        var succs: [[Int]] = []
        for vertex in 0..<count {
            preds.append(segments.filter { $0.to == vertex }.map(\.from))
            succs.append(segments.filter { $0.from == vertex }.map(\.to))
        }

        // Order each layer, keeping the order with the fewest crossings.
        var pos = fill(count, 0)
        var filled = fill(depth, 0)
        for vertex in 0..<count {
            let k = get(layer, vertex)
            pos.put(vertex, get(filled, k))
            filled.put(k, get(filled, k) + 1)
        }
        var best = pos
        var fewest = crossings(layer, pos, segments)
        var sweep = 0
        while sweep < sweeps && fewest > 0 {
            var k = 1
            while k < depth {
                reorder(layer, &pos, preds, k)
                k += 1
            }
            k = depth - 2
            while k >= 0 {
                reorder(layer, &pos, succs, k)
                k -= 1
            }
            let found = crossings(layer, pos, segments)
            if found < fewest {
                fewest = found
                best = pos
            }
            sweep += 1
        }

        // Place each layer, then pull vertices toward their neighbors.
        var center = fill(count, 0)
        var rows: [[Int]] = []
        for k in 0..<depth {
            let row = members(layer, best, k)
            rows.append(row)
            var x = 0
            for (index, member) in row.enumerated() {
                x = index == 0 ? get(widths, member) / 2 : x + separation(across, widths, row[index - 1], member)
                center.put(member, x)
            }
        }
        for pass in 0..<placementPasses {
            let downward = pass % 2 == 0
            for step in 0..<depth {
                let current = downward ? step : depth - 1 - step
                let row = rows.at(current) ?? []
                var desired = center
                for member in row {
                    let near = (downward ? preds.at(member) : succs.at(member)) ?? []
                    if !near.isEmpty {
                        var total = 0
                        for other in near { total += get(center, other) }
                        desired.put(member, (2 * total + near.count) / (2 * near.count))
                    }
                }
                settle(across, row, widths, desired, &center)
            }
        }
        // Straighten long edges: line each edge's placeholders up under its
        // source, or over its target, where their neighbors leave room.
        let chainPos = best
        for (edgeIndex, edge) in graph.edges.enumerated() {
            let chain = segments.filter { $0.edge == edgeIndex && $0.to >= real }.map(\.to)
            guard !chain.isEmpty else { continue }
            var placed = false
            // Failing both, the rightmost column the chain already reaches
            // always has room on the left.
            var rightmost = 0
            for member in chain { rightmost = max(rightmost, get(center, member)) }
            for x in [get(center, edge.from), get(center, edge.to), rightmost] where !placed {
                // The left side must already have room; the right side is
                // pushed over to make it.
                var fits = true
                for member in chain {
                    let row = rows.at(get(layer, member)) ?? []
                    if let before = row.at(get(chainPos, member) - 1),
                       x < get(center, before) + separation(across, widths, before, member) {
                        fits = false
                    }
                }
                if fits {
                    for member in chain {
                        center.put(member, x)
                        let row = rows.at(get(layer, member)) ?? []
                        var index = get(chainPos, member) + 1
                        while index < row.count {
                            let previous = row[index - 1], next = row[index]
                            let least = get(center, previous) + separation(across, widths, previous, next)
                            if get(center, next) < least { center.put(next, least) }
                            index += 1
                        }
                    }
                    placed = true
                }
            }
        }
        var left = 0
        for vertex in 0..<count {
            let edgeLeft = get(center, vertex) - get(widths, vertex) / 2
            if vertex == 0 || edgeLeft < left { left = edgeLeft }
        }
        for vertex in 0..<count { center[vertex] -= left }
        // Loops: each edge that loops back climbs its own column past the
        // right edge, shorter loops inside longer ones. It leaves its source
        // through an exit in the channel below and enters its target through
        // an entry in the channel above, adding a channel at either end when
        // needed.
        var right = 0
        for vertex in 0..<count {
            let room = get(widths, vertex)
            right = max(right, get(center, vertex) - room / 2 + room)
        }
        var spans = fill(graph.edges.count, 0)
        for looped in loops {
            let edge = graph.edges[looped]
            spans.put(looped, get(layer, edge.from) - get(layer, edge.to))
        }
        var columns = fill(graph.edges.count, -1)
        var column = right + 1
        for looped in sortBy(loops, spans) {
            let edge = graph.edges[looped]
            columns.put(looped, column)
            column += 2
            if edge.label != "" && !across { column += TextWidth.of(edge.label) + 2 }
        }
        var above = false
        var below = false
        for looped in loops {
            let edge = graph.edges[looped]
            if get(layer, edge.to) == 0 { above = true }
            if get(layer, edge.from) == depth - 1 { below = true }
        }
        if above {
            for vertex in 0..<count { layer[vertex] += 1 }
        }
        var allRows: [[Int]] = above ? [[]] : []
        allRows.append(contentsOf: rows)
        if below { allRows.append([]) }
        let virtualStart = layer.count
        for looped in loops {
            let edge = graph.edges[looped]
            let x = get(columns, looped)
            let exit = layer.count
            layer.append(get(layer, edge.from) + 1)
            boxes.append(1)
            widths.append(1)
            thick.append(0)
            center.append(x)
            segments.append(Segment(from: edge.from, to: exit, edge: looped))
            let entry = layer.count
            layer.append(get(layer, edge.to) - 1)
            boxes.append(1)
            widths.append(1)
            thick.append(0)
            center.append(x)
            segments.append(Segment(from: entry, to: edge.to, edge: looped))
        }
        return try route(
            Route(graph: graph, title: title, layer: layer, boxes: boxes, widths: widths, spots: spots,
                  segments: segments, rows: allRows, real: real, virtualStart: virtualStart, outer: columns,
                  thick: thick, flow: flow, ports: ports),
            centers: center)
    }
}
