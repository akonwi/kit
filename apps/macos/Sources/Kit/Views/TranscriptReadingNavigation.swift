import SwiftUI

struct TranscriptReadingSection: Equatable, Identifiable {
    let id: Int
    let title: String
    let offset: CGFloat
}

@MainActor @Observable final class TranscriptReadingState {
    struct Location: Equatable {
        var message: String
        var sections: [TranscriptReadingSection]
        var selected: Int
    }
    var location: Location?
    var navigate: ((Int) -> Void)?
}

struct TranscriptReadingNavigation: View {
    let state: TranscriptReadingState
    @Environment(\.mica) private var theme

    var body: some View {
        HStack(spacing: 12) {
            Spacer()
            if let location = state.location, !location.sections.isEmpty {
                Menu {
                    ForEach(location.sections) { section in
                        Button(section.title) { state.navigate?(section.id) }
                    }
                } label: {
                    Text(location.sections[location.selected].title).lineLimit(1)
                }
                .menuStyle(.borderlessButton).fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: 260, alignment: .trailing)
                .accessibilityLabel("Jump to response section")
                Text("\(location.selected + 1) / \(location.sections.count)")
                    .monospacedDigit().foregroundStyle(theme.muted)
                Button { state.navigate?(location.sections[location.selected - 1].id) } label: {
                    Image(systemName: "arrow.up")
                }.disabled(location.selected == 0).accessibilityLabel("Previous section")
                Button { state.navigate?(location.sections[location.selected + 1].id) } label: {
                    Image(systemName: "arrow.down")
                }.disabled(location.selected == location.sections.count - 1).accessibilityLabel("Next section")
            }
        }
        .buttonStyle(.plain).font(.kit(size: 12)).foregroundStyle(theme.text)
        .frame(maxWidth: 780)
        .frame(height: 32).padding(.horizontal, 32)
        .frame(maxWidth: .infinity, alignment: .center)
        .background(theme.surface)
    }
}

struct MarkdownSectionFrames: PreferenceKey {
    static var defaultValue: [Int: CGFloat] { [:] }
    static func reduce(value: inout [Int: CGFloat], nextValue: () -> [Int: CGFloat]) {
        value.merge(nextValue(), uniquingKeysWith: { _, new in new })
    }
}
