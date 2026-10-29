import SwiftUI

/// A Markdown diff fence (```diff or ```patch) in the fence card, drawn with
/// the rows tool output gives a unified diff. Long lines scroll sideways with
/// the body, and the fills span its scrolled width.
struct DiffFence: View {
    let language: String
    let source: String
    @State private var viewport: CGFloat = 0

    var body: some View {
        let diff = UnifiedDiff(fence: source)
        FenceCard(label: language.uppercased(), copy: source) {
            ScrollView(.horizontal) {
                UnifiedDiffView(diff: diff, fontSize: 12)
                    // A leading file name brings its own 6pt above it.
                    .padding(.top, diff.lines.first.map { if case .header = $0 { 8 } else { 14 } } ?? 14)
                    .padding(.bottom, 14)
                    .frame(minWidth: viewport, alignment: .leading)
            }
            .scrollIndicators(.automatic)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
        }
    }
}
