import KitClient
import KitTranscript
import SwiftUI

/// A transcript that opens at its end and follows new output while the
/// reader is there: Main's, or a subagent's. A reply too tall to see whole
/// shows from its top: one that arrives while it follows, as on the Mac, and
/// an idle transcript's last one when it opens.
struct TranscriptList<Header: View>: View {
    let messages: [TranscriptMessage]
    /// Whether a run is in progress, so its tool batches show as live.
    let active: Bool
    /// Bumped when the reader sends, which shows the end wherever they were.
    var sends = 0
    /// Rows shown above the messages, such as Show earlier messages.
    @ViewBuilder let header: () -> Header

    /// Whether the reader is at the end, so new output scrolls into view.
    @State private var following = true
    /// Whether the reader is scrolling the transcript.
    @State private var scrolling = false
    /// The reply just shown from its top, held there until the reader
    /// scrolls or sends.
    @State private var reading: String?
    /// Whether the end was in view at the last layout.
    @State private var atEnd = true
    /// Whether the transcript has been shown, so returning to it from
    /// another screen doesn't count as opening it.
    @State private var opened = false

    var body: some View {
        let live = ToolGroupActivity.liveGroups(in: messages, active: active)
        // A list, which sizes its own rows: under the composer's bar, a lazy
        // stack of rows this uneven can lay itself out without end as the
        // reader scrolls back.
        ScrollViewReader { proxy in
            List {
                header()
                ForEach(messages.filter(TranscriptRow.draws)) { message in
                    TranscriptRow(message: message, live: live.contains(message.id))
                        .transcriptRow()
                        // So a reply can be shown from its top.
                        .id(message.id)
                }
                Color.clear.frame(height: 0).transcriptRow(bottom: 0).id(Self.endRow)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            // A button in a row takes only its own taps.
            .buttonStyle(.borderless)
            .contentMargins(.top, 8, for: .scrollContent)
            // Open at the end, or at the top of the last reply when idle.
            .onAppear {
                if !opened, let reply = TranscriptTail.openingReply(TranscriptTail.of(messages, active: active)) {
                    following = false
                    reading = reply
                }
                opened = true
                if let reading { proxy.scrollTo(reading, anchor: .top) } else { scrollToEnd(proxy) }
            }
            // Only the reader's own scrolling stops or resumes following. The
            // screen's own scrolls aren't animated, so any scroll phase,
            // including a tap on the status bar, is the reader's.
            .onScrollPhaseChange { old, phase, context in
                scrolling = phase.isScrolling
                if old.isScrolling || phase.isScrolling {
                    reading = nil
                    following = Self.atEnd(context.geometry)
                }
            }
            .onScrollGeometryChange(for: EndGeometry.self) { geometry in
                EndGeometry(height: geometry.contentSize.height, inset: geometry.contentInsets.bottom,
                            atEnd: Self.atEnd(geometry))
            } action: { old, new in
                atEnd = new.atEnd
                if scrolling {
                    following = new.atEnd
                } else if let reading {
                    // Rows settling their heights, or a taller bar, move it;
                    // keep its top. One that fits stays at the end. While
                    // rows settle the end can seem in view, so this doesn't
                    // decide that it fits.
                    if new.height != old.height || new.inset != old.inset {
                        proxy.scrollTo(reading, anchor: .top)
                    }
                } else if following, !new.atEnd, new.height > old.height || new.inset > old.inset {
                    // A row that grows as it loads, such as an image, or a
                    // taller bar, such as the dock or a status line, hides
                    // the end without the reader scrolling; keep it in view.
                    scrollToEnd(proxy)
                }
            }
            .onChange(of: TranscriptTail.of(messages, active: active)) { old, new in
                if let held = reading, !messages.contains(where: { $0.id == held }) {
                    // The server's transcript no longer has it, as after a
                    // reconnect: follow the end again.
                    reading = nil
                    following = true
                } else if reading != nil, atEnd {
                    // The held reply fits, so what follows it shows as usual.
                    reading = nil
                    following = true
                }
                guard following else { return }
                if let reply = TranscriptTail.arrivedReply(after: old, in: new) {
                    // Shown from its top; what comes after it follows on
                    // only if it fits.
                    following = false
                    reading = reply
                    proxy.scrollTo(reply, anchor: .top)
                } else {
                    scrollToEnd(proxy)
                }
            }
            // Sending shows the message, wherever the reader was.
            .onChange(of: sends) {
                reading = nil
                following = true
                scrollToEnd(proxy)
            }
        }
    }

    /// The empty last row the transcript scrolls to for its end.
    private static var endRow: String { "transcript-end" }

    private func scrollToEnd(_ proxy: ScrollViewProxy) {
        proxy.scrollTo(Self.endRow, anchor: .bottom)
    }

    /// How tall the transcript is, how much the bar covers, and whether its
    /// end is in view.
    private struct EndGeometry: Equatable {
        let height: CGFloat
        let inset: CGFloat
        let atEnd: Bool
    }

    /// Whether the end is in view. The visible rect runs under the bar and
    /// the keyboard, so their inset counts as part of the content.
    private static func atEnd(_ geometry: ScrollGeometry) -> Bool {
        geometry.contentSize.height + geometry.contentInsets.bottom - geometry.visibleRect.maxY < 48
    }
}

extension View {
    /// A transcript row: no separator or background, inset from the screen's
    /// edges, 16 points from the next.
    func transcriptRow(bottom: CGFloat = 16) -> some View {
        listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: bottom, trailing: 20))
            .listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
    }
}

/// What changes when output arrives at the end of a transcript, and which of
/// it is a reply to show from its top.
struct TranscriptTail: Equatable {
    /// The last message.
    var last: String?
    /// The last reply with text: a reply is in the transcript without text
    /// until it's complete.
    var reply: String?
    /// Whether a run is in progress.
    var active = false
    var counts: [Int] = []

    static func of(_ messages: [TranscriptMessage], active: Bool) -> TranscriptTail {
        guard let last = messages.last else { return TranscriptTail(active: active) }
        return TranscriptTail(last: last.id, reply: messages.last(where: shows)?.id, active: active,
                              counts: [messages.count, last.text.count, last.tools.count, last.tools.filter(\.running).count])
    }

    /// The reply that a run just completed at the end of the transcript, if
    /// it wasn't shown before, as the Mac decides. Earlier pages leave the
    /// last reply as it was; loading a transcript, or one that changes
    /// without a run, such as after a reconnect, doesn't count.
    static func arrivedReply(after old: TranscriptTail, in new: TranscriptTail) -> String? {
        guard old.last != nil, old.active || new.active,
              let reply = new.reply, new.last == reply, reply != old.reply else { return nil }
        return reply
    }

    /// The reply an idle transcript opens at: its last message, when that's
    /// a reply.
    static func openingReply(_ tail: TranscriptTail) -> String? {
        guard !tail.active, let reply = tail.reply, tail.last == reply else { return nil }
        return reply
    }

    private static func shows(_ message: TranscriptMessage) -> Bool {
        message.role == "assistant" && !message.text.isEmpty
    }
}
