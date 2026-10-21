import AppKit
import SwiftUI
import Testing
@testable import Kit

/// Option rows in the interaction dock: each label whole on its own lines,
/// its detail below in up to three lines, at wide and tile widths. Set
/// KIT_SNAPSHOT_DIR to write the renders there for review.
@MainActor struct InteractionDockPresentationTests {
    private static let labels = [
        "a": "Move urls to a new operation (recommended)",
        "b": "Keep the field and accept the break",
        "c": "Keep the field and make Go clients tolerant",
    ]
    private static let details = [
        "a": "Add GET /v1/pairing/urls (additive). Old clients never call it. The app calls it and, on older servers, falls back to the existing field when the operation is missing.",
        "b": "Note it in the release. It only affects an old CLI talking to a new daemon on the same machine.",
        "c": "Fixes future clients only. v0.48 still breaks.",
    ]

    private func flow(kind: InteractionFlow.Kind = .select, options: [String] = ["a", "b", "c"],
                      labels: [String: String] = labels, details: [String: String] = details) -> InteractionFlow {
        InteractionFlow(title: "Phase C review: two decisions", guided: false, questions: [
            .init(prompt: "How should the pairing urls field change?", kind: kind, options: options,
                  optionLabels: labels, optionDetails: details),
        ])
    }

    /// The height SwiftUI gives `text` in `font` at `width`.
    private func textHeight(_ text: String, _ font: Font, width: CGFloat, lineLimit: Int? = nil) -> CGFloat {
        NSHostingView(rootView: Text(text).font(font).lineLimit(lineLimit)
            .fixedSize(horizontal: false, vertical: true).frame(width: width, alignment: .leading)).fittingSize.height
    }
    private func labelHeight(_ text: String, width: CGFloat) -> CGFloat {
        textHeight(text, .kit(size: 13, weight: .medium), width: width)
    }
    private func detailHeight(_ text: String, width: CGFloat) -> CGFloat {
        textHeight(text, .kit(size: 12), width: width, lineLimit: DockOptionList.detailLines)
    }
    /// A row is its label and detail stacked inside its padding.
    private func expectedRowHeight(label: String, detail: String, column: CGFloat) -> CGFloat {
        let padding = DockOptionRow.padding.top + DockOptionRow.padding.bottom
        let text = labelHeight(label, width: column) + (detail.isEmpty ? 0 : DockOptionRow.detailSpacing + detailHeight(detail, width: column))
        return max(DockOptionList.minimumRowHeight, padding + text)
    }
    /// The text column beside the letter, and the checkbox in a multiple choice.
    private func column(rowWidth: CGFloat, multiple: Bool = false) -> CGFloat {
        rowWidth - DockOptionRow.padding.leading - DockOptionRow.padding.trailing
            - DockOptionRow.markerWidth - DockOptionRow.spacing - (multiple ? DockCheckbox.size + DockOptionRow.spacing : 0)
    }

    private func host<Content: View>(_ content: Content, width: CGFloat, snapshot: String? = nil) async throws -> NSHostingView<some View> {
        _ = NSApplication.shared
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: content.frame(width: width).environment(\.mica, theme)
            .environment(\.colorScheme, .dark).background(theme.surface))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: width, height: 800), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        host.frame = NSRect(origin: .zero, size: host.fittingSize)
        host.layoutSubtreeIfNeeded()
        if let snapshot, let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] {
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            try #require(bitmap.representation(using: .png, properties: [:]))
                .write(to: URL(fileURLWithPath: directory).appendingPathComponent("dock-options-\(snapshot).png"))
        }
        return host
    }

    @Test(arguments: [240.0, 720.0]) func rowsShowWholeLabelsAboveUpToThreeLinesOfDetail(width: CGFloat) async throws {
        let flow = flow()
        let column = column(rowWidth: width)
        for option in flow.question.options {
            let row = try await host(DockOptionRow(flow: flow, index: 0, option: option) { _ in }, width: width)
            let label = Self.labels[option]!, detail = Self.details[option]!
            #expect(row.fittingSize.height == expectedRowHeight(label: label, detail: detail, column: column))
        }
        let oneLabelLine = labelHeight("A", width: column)
        let recommended = labelHeight(Self.labels["a"]!, width: column)
        let recommendedDetail = detailHeight(Self.details["a"]!, width: column)
        if width == 240 {
            // In a narrow tile the recommended label wraps onto a second line,
            // and its long detail stops at three lines.
            #expect(recommended == labelHeight("A\nB", width: column))
            #expect(recommendedDetail == textHeight("1\n2\n3", .kit(size: 12), width: column))
        } else {
            #expect(recommended == oneLabelLine)
            #expect(recommendedDetail == textHeight("1\n2", .kit(size: 12), width: column))
        }
    }

    @Test func multipleChoiceRowsKeepTheLabelBesideTheCheckbox() async throws {
        let flow = flow(kind: .multiselect)
        _ = flow.choose(1)
        let width: CGFloat = 320
        let row = try await host(DockOptionRow(flow: flow, index: 1, option: "b") { _ in }, width: width, snapshot: "multiple")
        #expect(row.fittingSize.height == expectedRowHeight(label: Self.labels["b"]!, detail: Self.details["b"]!,
                                                           column: column(rowWidth: width, multiple: true)))
    }

    @Test func movingTheHighlightKeepsEveryRowInPlace() async throws {
        let flow = flow()
        let width: CGFloat = 240, column = column(rowWidth: width)
        for (index, option) in flow.question.options.enumerated() {
            let expected = expectedRowHeight(label: Self.labels[option]!, detail: Self.details[option]!, column: column)
            for cursor in flow.question.options.indices {
                flow.cursor = cursor
                let row = try await host(DockOptionRow(flow: flow, index: index, option: option) { _ in }, width: width,
                                         snapshot: cursor == index ? "row-\(option)-highlighted" : nil)
                #expect(row.fittingSize.height == expected)
            }
        }
    }

    @Test func shortListsStackEveryOptionWithoutOverflow() async throws {
        let flow = flow()
        // The list bleeds 6 points past each side so row fills align with the dock text.
        let width: CGFloat = 228, rowWidth = width + 12
        let rows = flow.question.options.map {
            expectedRowHeight(label: Self.labels[$0]!, detail: Self.details[$0]!, column: column(rowWidth: rowWidth))
        }
        let list = try await host(DockOptionList(flow: flow) { _ in }, width: width, snapshot: "list-narrow")
        #expect(list.fittingSize.height == rows.reduce(0, +) + DockOptionList.spacing * CGFloat(rows.count - 1))
    }

    private func longFlow(_ count: Int) -> InteractionFlow {
        let options = (0..<count).map { "option\($0)" }
        return flow(options: options,
                    labels: Dictionary(uniqueKeysWithValues: options.map { ($0, "Choice \($0)") }),
                    details: Dictionary(uniqueKeysWithValues: options.map { ($0, "Explains \($0) in a sentence.") }))
    }

    @Test func longListsScrollWithinTheirLimitAndCountWhatIsHidden() async throws {
        let flow = longFlow(10)
        let width: CGFloat = 400
        let row = expectedRowHeight(label: "Choice option0", detail: "Explains option0 in a sentence.", column: column(rowWidth: width + 12))
        let frames = (0..<10).map { index in
            let top = CGFloat(index) * (row + DockOptionList.spacing)
            return top...(top + row)
        }
        // Opened at the top, six options show and four are counted below.
        #expect(DockOptionList.hidden(frames: frames, offset: 0, viewport: DockOptionList.maxScrollHeight) == (0, 4))
        let list = try await host(DockOptionList(flow: flow) { _ in }, width: width, snapshot: "list-long")
        let overflow = 22 + DockOptionList.spacing
        #expect(list.fittingSize.height == DockOptionList.maxScrollHeight + overflow)
    }

    @Test func aListOpensOnItsEarlierAnswer() async throws {
        let flow = longFlow(10)
        flow.cursor = 9
        let width: CGFloat = 400
        let list = try await host(DockOptionList(flow: flow) { _ in }, width: width, snapshot: "list-long-answered")
        // Scrolled to the last option, the count moves above: still one overflow row.
        #expect(list.fittingSize.height == DockOptionList.maxScrollHeight + 22 + DockOptionList.spacing)
        // The highlight's fill is at the bottom of the scroll view, on the
        // last option, and not on the first option shown under the count.
        let highlighted = try await host(DockOptionRow(flow: flow, index: 9, option: "option9") { _ in }, width: width + 12)
        let fill = try pixel(highlighted, x: width - 14, fromBottom: 10)
        let bottom = try pixel(list, x: width - 20, fromBottom: 10)
        let firstShown = try pixel(list, x: width - 20, fromBottom: list.bounds.height - 22 - DockOptionList.spacing - 10)
        #expect(zip(bottom, fill).allSatisfy { abs($0 - $1) <= 12 })
        #expect(zip(firstShown, fill).contains { abs($0 - $1) > 24 })
    }

    /// A rendered point's colour, as 8-bit device RGB.
    private func pixel(_ view: NSView, x: CGFloat, fromBottom: CGFloat) throws -> [Int] {
        let bitmap = try #require(view.bitmapImageRepForCachingDisplay(in: view.bounds))
        view.cacheDisplay(in: view.bounds, to: bitmap)
        let scale = CGFloat(bitmap.pixelsWide) / view.bounds.width
        let color = try #require(bitmap.colorAt(x: Int(x * scale), y: Int((view.bounds.height - fromBottom) * scale))?
            .usingColorSpace(.deviceRGB))
        return [color.redComponent, color.greenComponent, color.blueComponent].map { Int(($0 * 255).rounded()) }
    }

    @Test func listsThatFitUnderTheLimitShowEveryOption() async throws {
        let options = (0..<7).map { "option\($0)" }
        let flow = flow(options: options, labels: Dictionary(uniqueKeysWithValues: options.map { ($0, "Choice \($0)") }),
                        details: [:])
        let list = try await host(DockOptionList(flow: flow) { _ in }, width: 400)
        // Seven one-line options fit under the limit, with nothing hidden.
        #expect(list.fittingSize.height == 7 * DockOptionList.minimumRowHeight + 6 * DockOptionList.spacing)
    }
}
