import SwiftUI

/// How a tile's tabs share its strip. Tabs keep their natural width while
/// they fit. Past that the widest shrink first, toward ``minimum``, while
/// the selected tab keeps at least ``selectedMinimum`` so it stays
/// readable. Tabs that still don't fit at those widths scroll.
struct TileTabWidths: Equatable {
    /// The narrowest a tab shrinks: its icon and a few characters.
    static let minimum: CGFloat = 88
    /// The narrowest the selected tab shrinks.
    static let selectedMinimum: CGFloat = 160

    let widths: [CGFloat]

    /// - Parameters:
    ///   - natural: each tab's width with its whole title, up to its cap.
    ///   - selected: the selected tab's index.
    ///   - available: the width the tabs may fill, or nil before the strip
    ///     is measured.
    init(natural: [CGFloat], selected: Int?, available: CGFloat?) {
        let floors = natural.indices.map { index in
            min(natural[index], index == selected ? Self.selectedMinimum : Self.minimum)
        }
        guard let available, natural.reduce(0, +) > available else { widths = natural; return }
        guard floors.reduce(0, +) < available else { widths = floors; return }
        // Every tab wider than the cap shrinks to it, so the tabs fill the
        // strip exactly. The total grows piecewise linearly with the cap;
        // find the piece where it reaches `available` and solve within it.
        func total(_ cap: CGFloat) -> CGFloat {
            natural.indices.reduce(0) { $0 + min(natural[$1], max(cap, floors[$1])) }
        }
        let points = Set(natural + floors).sorted()
        var cap = points.last ?? 0
        for (low, high) in zip(points, points.dropFirst()) where total(high) >= available {
            let growing = natural.indices.filter { floors[$0] <= low && natural[$0] >= high }.count
            cap = growing > 0 ? low + (available - total(low)) / CGFloat(growing) : high
            break
        }
        widths = natural.indices.map { min(natural[$0], max(cap, floors[$0])) }
    }
}

/// Lays a tile's tabs out in a row at ``TileTabWidths``. It sizes to the
/// tabs, so in a scroll view it is as wide as they are.
struct TileTabRow: Layout {
    /// The width the tabs may fill, or nil before the strip is measured.
    let available: CGFloat?
    let selected: Int?

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let height = proposal.height ?? subviews.map { $0.sizeThatFits(.unspecified).height }.max() ?? 0
        return CGSize(width: widths(subviews).reduce(0, +), height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        for (subview, width) in zip(subviews, widths(subviews)) {
            subview.place(at: CGPoint(x: x, y: bounds.minY), anchor: .topLeading,
                          proposal: ProposedViewSize(width: width, height: bounds.height))
            x += width
        }
    }

    private func widths(_ subviews: Subviews) -> [CGFloat] {
        TileTabWidths(natural: subviews.map { $0.sizeThatFits(.unspecified).width },
                      selected: selected, available: available).widths
    }
}

/// The widths of a tile strip's edge fades. Revealing a tab clears them.
enum TileTabFade {
    static let leading: CGFloat = 24
    static let trailing: CGFloat = 32
}
