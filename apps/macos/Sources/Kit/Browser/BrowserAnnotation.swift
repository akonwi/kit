import Foundation

/// An element the annotation script outlines: its short name, a source
/// hint when the page gives one, and its box in the page's viewport, in
/// CSS pixels.
struct BrowserAnnotationTarget: Decodable, Equatable {
    struct Rect: Decodable, Equatable {
        var x: Double, y: Double, width: Double, height: Double
        var cgRect: CGRect { CGRect(x: x, y: y, width: width, height: height) }
    }
    let label: String
    var hint: String?
    let rect: Rect
}

/// What the annotation script reports after each operation: the element
/// under the pointer, the selection, the group holding it, and the
/// selection's ancestors from outermost to itself.
struct BrowserAnnotationPageState: Decodable, Equatable {
    struct Viewport: Decodable, Equatable { var width: Double, height: Double }
    var hovered: BrowserAnnotationTarget?
    var selected: BrowserAnnotationTarget?
    var scope: BrowserAnnotationTarget?
    var crumbs: [String] = []
    var canParent = false
    var canChild = false
    var viewport: Viewport?

    static let empty = BrowserAnnotationPageState()

    init(hovered: BrowserAnnotationTarget? = nil, selected: BrowserAnnotationTarget? = nil, scope: BrowserAnnotationTarget? = nil,
         crumbs: [String] = [], canParent: Bool = false, canChild: Bool = false, viewport: Viewport? = nil) {
        self.hovered = hovered; self.selected = selected; self.scope = scope
        self.crumbs = crumbs; self.canParent = canParent; self.canChild = canChild; self.viewport = viewport
    }

    private enum CodingKeys: String, CodingKey { case hovered, selected, scope, crumbs, canParent, canChild, viewport }

    init(from decoder: any Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        self.init(hovered: try values.decodeIfPresent(BrowserAnnotationTarget.self, forKey: .hovered),
                  selected: try values.decodeIfPresent(BrowserAnnotationTarget.self, forKey: .selected),
                  scope: try values.decodeIfPresent(BrowserAnnotationTarget.self, forKey: .scope),
                  crumbs: try values.decodeIfPresent([String].self, forKey: .crumbs) ?? [],
                  canParent: try values.decodeIfPresent(Bool.self, forKey: .canParent) ?? false,
                  canChild: try values.decodeIfPresent(Bool.self, forKey: .canChild) ?? false,
                  viewport: try values.decodeIfPresent(Viewport.self, forKey: .viewport))
    }
}

/// What Kit captures about an annotated element. It is bounded and holds
/// no field values, cookies, or storage. Its shape is what a
/// `browser_element` annotation anchor would carry.
struct BrowserElementCapture: Codable, Equatable {
    struct Size: Codable, Equatable { var width: Double, height: Double }
    struct Point: Codable, Equatable { var x: Double, y: Double }
    struct Page: Codable, Equatable {
        var url: String
        var title: String
        var viewport: Size
        var scroll: Point
    }
    /// The frame the element is in: `top`, `same-origin` (with the frame
    /// element's selector), or `cross-origin` when the element is a frame
    /// whose contents can't be read.
    struct Frame: Codable, Equatable {
        var kind: String
        var selector: String?
        var src: String?
    }
    struct Rect: Codable, Equatable { var x: Double, y: Double, width: Double, height: Double }
    struct Element: Codable, Equatable {
        var tag: String
        var id: String?
        var classes: [String]
        var role: String?
        var label: String
        var text: String
        var selector: String
        var path: String
        var rect: Rect
        /// `fixed` or `sticky` when the element doesn't scroll with the page.
        var position: String?
        var attributes: [[String]]
        var styles: [[String]]
        var html: String
        var htmlTruncated: Bool
    }
    /// Dev tooling's source-location attributes, on the element or the
    /// nearest ancestor `distance` levels up, named `on`.
    struct Source: Codable, Equatable {
        var attributes: [[String]]
        var on: String?
        var distance: Int
    }
    var page: Page
    var frame: Frame
    var shadowHosts: [String]
    var element: Element
    var source: Source?
}

/// The text block the model gets for a browser element annotation, and
/// the parts of a sent message that hold them.
enum BrowserAnnotationText {
    static let open = "<browser-annotation n=\""
    static let close = "</browser-annotation>"

    /// One annotation's block. The person's comment and the screenshot's
    /// name are Kit's; everything from the page is fenced as untrusted.
    static func block(number: Int, comment: String, screenshot: String?, capture: BrowserElementCapture,
                      fence: UntrustedPageContent = UntrustedPageContent()) -> String {
        // A comment's later lines are indented, so none can pass for a
        // line of the block.
        let comment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
            .split(separator: "\n", omittingEmptySubsequences: false).joined(separator: "\n  ")
        var head = ["\(open)\(number)\">", "Comment: " + comment]
        if let screenshot { head.append("Screenshot: \(screenshot) (attached image)") }
        let page = pageLines(capture).map { $0.replacingOccurrences(of: "\n", with: " ") }
        return (head + [fence.wrap(page.joined(separator: "\n")), close]).joined(separator: "\n")
    }

    /// The page's part of a block, a line per fact that has a value.
    static func pageLines(_ capture: BrowserElementCapture) -> [String] {
        let element = capture.element, page = capture.page
        var lines: [String] = []
        if !page.title.isEmpty { lines.append("Page: " + page.title) }
        lines.append("URL: " + page.url)
        lines.append("Viewport: \(number(page.viewport.width))×\(number(page.viewport.height)) CSS px, scrolled to "
            + "\(number(page.scroll.x)),\(number(page.scroll.y))")
        var name = "Element: <\(element.tag)>"
        if let id = element.id { name += " #" + id }
        if !element.classes.isEmpty { name += " " + element.classes.map { "." + $0 }.joined(separator: " ") }
        if let role = element.role { name += ", role " + role }
        lines.append(name)
        if !element.text.isEmpty { lines.append("Text: " + quoted(element.text)) }
        lines.append("Selector: " + element.selector)
        lines.append("Path: " + element.path)
        let rect = element.rect
        var box = "Box: x \(number(rect.x)), y \(number(rect.y)), \(number(rect.width))×\(number(rect.height)) (viewport CSS px"
        if let position = element.position { box += ", position " + position }
        lines.append(box + ")")
        if !element.styles.isEmpty {
            lines.append("Styles: " + element.styles.compactMap { pair in
                pair.count == 2 ? pair[0] + " " + pair[1] : nil
            }.joined(separator: "; "))
        }
        if !element.attributes.isEmpty { lines.append("Attributes: " + attributes(element.attributes)) }
        if let source = capture.source, !source.attributes.isEmpty {
            var line = "Source: " + attributes(source.attributes)
            if let on = source.on {
                line += " (on \(on), \(source.distance) \(source.distance == 1 ? "level" : "levels") up)"
            }
            lines.append(line)
        }
        lines.append("Frame: " + frame(capture))
        lines.append(element.htmlTruncated ? "HTML (truncated):" : "HTML:")
        lines.append(element.html)
        return lines
    }

    private static func frame(_ capture: BrowserElementCapture) -> String {
        var text: String
        switch capture.frame.kind {
        case "same-origin":
            text = "same-origin iframe " + (capture.frame.selector ?? "")
            if let src = capture.frame.src, !src.isEmpty { text += " (src \(src))" }
        case "cross-origin":
            text = "cross-origin iframe; contents not inspectable"
            if let src = capture.frame.src, !src.isEmpty { text += " (src \(src))" }
        default:
            text = "top document"
        }
        if !capture.shadowHosts.isEmpty { text += "; inside shadow DOM of " + capture.shadowHosts.joined(separator: " >> ") }
        return text
    }

    private static func attributes(_ pairs: [[String]]) -> String {
        pairs.compactMap { pair in pair.count == 2 ? pair[0] + "=" + quoted(pair[1]) : nil }.joined(separator: " ")
    }

    private static func quoted(_ text: String) -> String {
        "\"" + text.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
    }

    private static func number(_ value: Double) -> String {
        value.rounded() == value ? String(Int(value)) : String(format: "%.1f", value)
    }

    /// A sent annotation, as the transcript shows it.
    struct Sent: Equatable, Identifiable {
        var id: Int { number }
        let number: Int
        let comment: String
        let screenshot: String?
        /// The fenced page lines, without the fence.
        let page: [String]

        /// The value of the page line starting with `key`, such as `URL`.
        func value(_ key: String) -> String? {
            page.first { $0.hasPrefix(key + ": ") }.map { String($0.dropFirst(key.count + 2)) }
        }
    }

    /// Splits a message into its own text and the annotation blocks Kit
    /// appended to it. Text without blocks comes back unchanged.
    static func split(_ message: String) -> (text: String, annotations: [Sent]) {
        guard message.contains(open) else { return (message, []) }
        var text = "", annotations: [Sent] = []
        var rest = Substring(message)
        while let start = rest.range(of: open) {
            guard let end = rest.range(of: "\n" + close, range: start.upperBound..<rest.endIndex),
                  let sent = parse(rest[start.lowerBound..<end.upperBound]) else { break }
            text += rest[..<start.lowerBound]
            annotations.append(sent)
            rest = rest[end.upperBound...]
        }
        text += rest
        return (text.trimmingCharacters(in: .whitespacesAndNewlines), annotations)
    }

    private static func parse(_ block: Substring) -> Sent? {
        let lines = block.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        guard let first = lines.first, first.hasPrefix(open), first.hasSuffix("\">"),
              let number = Int(first.dropFirst(open.count).dropLast(2)),
              lines.count > 1, lines[1].hasPrefix("Comment: ") else { return nil }
        var comment = String(lines[1].dropFirst("Comment: ".count))
        var index = 2
        while index < lines.count, lines[index].hasPrefix("  ") {
            comment += "\n" + lines[index].dropFirst(2)
            index += 1
        }
        var screenshot: String?
        if index < lines.count, lines[index].hasPrefix("Screenshot: ") {
            screenshot = lines[index].dropFirst("Screenshot: ".count).components(separatedBy: " (attached").first
            index += 1
        }
        guard index < lines.count, lines[index].hasPrefix("[UNTRUSTED_PAGE_CONTENT nonce="),
              let fenceEnd = lines.lastIndex(where: { $0.hasPrefix("[/UNTRUSTED_PAGE_CONTENT nonce=") }),
              fenceEnd > index else { return nil }
        return Sent(number: number, comment: comment, screenshot: screenshot, page: Array(lines[(index + 1)..<fenceEnd]))
    }
}
