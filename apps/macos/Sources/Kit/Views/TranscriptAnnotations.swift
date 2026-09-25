import SwiftUI

/// Accepted evidence is rendered from the message snapshot, never from the current file.
struct TranscriptAnnotations: View {
    @Environment(\.mica) private var theme
    let annotations: [FileAnnotation]
    @State private var expanded: Bool
    init(annotations: [FileAnnotation], expanded: Bool = false) {
        self.annotations = annotations
        _expanded = State(initialValue: expanded)
    }
    var body: some View {
        if !annotations.isEmpty {
            DisclosureGroup("\(annotations.count) \(annotations.count == 1 ? "annotation" : "annotations")", isExpanded: $expanded) {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(annotations) { annotation in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(annotation.path + ":" + annotation.rangeLabel)
                                .font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                            if let context = annotation.diffContext {
                                Text(context).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted).textSelection(.enabled)
                            }
                            Text(annotation.body).font(.kit(size: 14)).textSelection(.enabled)
                            ScrollView(.vertical) {
                                Text(annotation.source).font(.kit(size: 12, design: .monospaced))
                                    .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                            }.frame(maxHeight: 180)
                            if annotation.truncated { Text("Captured source truncated").foregroundStyle(theme.muted) }
                        }
                    }
                }.padding(.top, 8)
            }.font(.kit(size: 12)).foregroundStyle(theme.text)
        }
    }
}
