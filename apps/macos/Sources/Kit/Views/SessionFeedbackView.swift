import SwiftUI

struct SessionFeedbackView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var feedback: SessionFeedback
    var body: some View {
        VStack(spacing: 0) {
            if let item = feedback.visible {
                SessionFeedbackCard(item: item, close: { feedback.dismiss(item.id) }, expire: { feedback.expire(item.id) })
                    .id(item.id)
                    .padding(.horizontal, 28).padding(.bottom, 12)
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .move(edge: .bottom)))
            }
        }
        .frame(maxWidth: 844)
        .frame(maxWidth: .infinity)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: feedback.visible?.id)
    }
}

struct SessionFeedbackCard: View {
    @Environment(\.mica) private var theme
    let item: SessionFeedback.Item
    let close: () -> Void
    let expire: () -> Void
    @State private var hovered = false
    @State private var expanded = false
    @FocusState private var focused: Bool
    private var color: Color {
        switch item.tone { case .info: theme.accent; case .warning: theme.warning; case .error: theme.danger }
    }
    private var symbol: String {
        switch item.tone { case .info: "checkmark.circle"; case .warning: "exclamationmark.triangle"; case .error: "exclamationmark.circle" }
    }
    var body: some View {
        HStack(alignment: .top, spacing: 11) {
            Image(systemName: symbol).foregroundStyle(color).padding(.top, 2).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 5) {
                Text(item.title).font(.kit(size: 13, weight: .medium))
                if !item.detail.isEmpty {
                    Text(item.detail).font(.kit(size: 12)).foregroundStyle(theme.muted)
                        .lineLimit(expanded ? nil : 2).textSelection(.enabled)
                }
                HStack(spacing: 14) {
                    if let title = item.actionTitle, let action = item.action {
                        Button(title, action: action).focused($focused)
                    }
                    if item.persistent && !item.detail.isEmpty {
                        Button(expanded ? "Less detail" : "View details") { expanded.toggle() }.focused($focused)
                    }
                }.font(.kit(size: 12)).foregroundStyle(theme.accent).buttonStyle(.plain)
            }.frame(maxWidth: .infinity, alignment: .leading)
            Button(action: close) { Image(systemName: "xmark").font(.kit(size: 11)).foregroundStyle(theme.muted) }
                .buttonStyle(.plain).padding(3).focused($focused)
                .accessibilityLabel(item.persistent ? "Dismiss notice" : "Dismiss feedback")
        }
        .padding(14).background(theme.raised, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).stroke(theme.border, lineWidth: 1))
        .onHover { hovered = $0 }
        .task(id: hovered || focused) {
            guard !item.persistent, !hovered, !focused else { return }
            do { try await Task.sleep(for: .seconds(5)) } catch { return }
            expire()
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session feedback")
    }
}

struct SessionNoticeButton: View {
    @Environment(\.mica) private var theme
    @Bindable var feedback: SessionFeedback
    @State private var presented = false
    var body: some View {
        if !feedback.notices.isEmpty {
            Button { presented.toggle() } label: {
                Label("\(feedback.notices.count) \(feedback.notices.count == 1 ? "notice" : "notices")", systemImage: "exclamationmark.triangle")
            }.buttonStyle(.plain).foregroundStyle(theme.warning)
            .popover(isPresented: $presented, arrowEdge: .top) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        Text("Session notices").font(.kit(size: 13, weight: .medium))
                        ForEach(feedback.notices) { item in
                            VStack(alignment: .leading, spacing: 6) {
                                Text(item.title).font(.kit(size: 13, weight: .medium))
                                Text(item.detail).font(.kit(size: 12)).foregroundStyle(theme.muted).textSelection(.enabled)
                                HStack(spacing: 16) {
                                    Button("Show") { feedback.reveal(item.id); presented = false }
                                    Button("Dismiss") { feedback.dismiss(item.id); if feedback.notices.isEmpty { presented = false } }
                                }.font(.kit(size: 12)).buttonStyle(.plain).foregroundStyle(theme.accent)
                            }
                        }
                    }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
                }.frame(width: 380, height: min(360, CGFloat(feedback.notices.count) * 152 + 54))
                    .background(theme.raised).foregroundStyle(theme.text)
            }
        }
    }
}
