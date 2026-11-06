import AppKit
import CoreText
import ImageIO
import UniformTypeIdentifiers

let root = URL(fileURLWithPath: CommandLine.arguments[1])
let fontURL = root.appendingPathComponent("fonts/JetBrainsMono-Bold.ttf")
CTFontManagerRegisterFontsForURL(fontURL as CFURL, .process, nil)
let font = CTFontCreateWithName("JetBrainsMono-Bold" as CFString, 1000, nil)
precondition(CTFontCopyPostScriptName(font) as String == "JetBrainsMono-Bold")
var characters: [UniChar] = [75, 95]
var glyphs = [CGGlyph](repeating: 0, count: 2)
CTFontGetGlyphsForCharacters(font, &characters, &glyphs, 2)
var advances = [CGSize](repeating: .zero, count: 2)
CTFontGetAdvancesForGlyphs(font, .horizontal, glyphs, &advances, 2)
var rawPaths = glyphs.map { CTFontCreatePathForGlyph(font, $0, nil)! }
// The logo uses a cursor resting on the K baseline, rather than the font's
// below-baseline punctuation placement. Preserve the actual glyph outlines.
var cursorAlignment = CGAffineTransform(translationX: 0,
    y: rawPaths[0].boundingBoxOfPath.minY - rawPaths[1].boundingBoxOfPath.minY)
rawPaths[1] = rawPaths[1].copy(using: &cursorAlignment)!
precondition(abs(rawPaths[0].boundingBoxOfPath.minY - rawPaths[1].boundingBoxOfPath.minY) < 0.001)
func color(_ hex: String) -> CGColor {
    let n = UInt32(hex, radix: 16)!
    return CGColor(colorSpace: CGColorSpace(name: CGColorSpace.sRGB)!,
                   components: [CGFloat((n >> 16) & 255) / 255, CGFloat((n >> 8) & 255) / 255,
                                CGFloat(n & 255) / 255, 1])!
}
func svgPath(_ path: CGPath) -> String {
    var result = ""
    func p(_ point: CGPoint) -> String { String(format: "%.3f %.3f", Double(point.x), Double(point.y)) }
    path.applyWithBlock { element in
        let e = element.pointee
        switch e.type {
        case .moveToPoint: result += "M" + p(e.points[0])
        case .addLineToPoint: result += "L" + p(e.points[0])
        case .addQuadCurveToPoint: result += "Q" + p(e.points[0]) + " " + p(e.points[1])
        case .addCurveToPoint: result += "C" + p(e.points[0]) + " " + p(e.points[1]) + " " + p(e.points[2])
        case .closeSubpath: result += "Z"
        @unknown default: break
        }
    }
    return result
}
// Kit Dev marks development builds with an amber ribbon across the top-right
// corner of the 1024-pt icon canvas, labelled DEV in the mark's typeface.
let amber = "FFB224", ink = "191A1D"
let ribbonPath: CGPath = {
    let path = CGMutablePath()
    path.addLines(between: [CGPoint(x: 540, y: 0), CGPoint(x: 720, y: 0),
                            CGPoint(x: 1024, y: 304), CGPoint(x: 1024, y: 484)])
    path.closeSubpath()
    return path
}()
let labelPath: CGPath = {
    let labelFont = CTFontCreateCopyWithAttributes(font, 92, nil, nil)
    var characters = Array("DEV".utf16)
    var glyphs = [CGGlyph](repeating: 0, count: characters.count)
    CTFontGetGlyphsForCharacters(labelFont, &characters, &glyphs, characters.count)
    var advances = [CGSize](repeating: .zero, count: glyphs.count)
    CTFontGetAdvancesForGlyphs(labelFont, .horizontal, glyphs, &advances, glyphs.count)
    let combined = CGMutablePath()
    var x: CGFloat = 0
    for (i, glyph) in glyphs.enumerated() {
        combined.addPath(CTFontCreatePathForGlyph(labelFont, glyph, nil)!, transform: CGAffineTransform(translationX: x, y: 0))
        x += advances[i].width + (i < glyphs.count - 1 ? 6 : 0)
    }
    // Centre the label on the band's midline and run it along the band.
    let bounds = combined.boundingBoxOfPath
    var transform = CGAffineTransform(translationX: 827, y: 197).rotated(by: .pi / 4)
        .scaledBy(x: 1, y: -1).translatedBy(x: -bounds.midX, y: -bounds.midY)
    return combined.copy(using: &transform)!
}()
func generate(name: String, foreground: String, border: Bool = false, tile: String? = nil, ribbon: Bool = false) throws {
    let width = 1024, height = tile == nil ? 768 : 1024
    let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                            space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.translateBy(x: 0, y: CGFloat(height)); context.scaleBy(x: 1, y: -1)
    var svg = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"\(width)\" height=\"\(height)\" viewBox=\"0 0 \(width) \(height)\">"
    let outline = CGPath(roundedRect: CGRect(x: 64, y: 64, width: 896, height: 896), cornerWidth: 196, cornerHeight: 196, transform: nil)
    if let tile {
        context.addPath(outline); context.setFillColor(color(tile)); context.fillPath()
        svg += "<rect x=\"64\" y=\"64\" width=\"896\" height=\"896\" rx=\"196\" fill=\"#\(tile)\"/>"
    }
    if border {
        let path = CGPath(roundedRect: CGRect(x: 48, y: 48, width: 928, height: 672), cornerWidth: 88, cornerHeight: 88, transform: nil)
        context.addPath(path); context.setStrokeColor(color(foreground)); context.setLineWidth(10); context.strokePath()
        svg += "<rect x=\"48\" y=\"48\" width=\"928\" height=\"672\" rx=\"88\" fill=\"none\" stroke=\"#\(foreground)\" stroke-width=\"10\"/>"
    }
    let scale: CGFloat = tile == nil ? 0.62 : 0.52
    let total = (advances[0].width + advances[1].width) * scale
    let x = (1024 - total) / 2
    // Center the aligned mark vertically.
    let bounds = rawPaths[0].boundingBoxOfPath.union(rawPaths[1].boundingBoxOfPath)
    let baseline = CGFloat(height) / 2 + (bounds.minY + bounds.maxY) * scale / 2
    for i in 0..<2 {
        var transform = CGAffineTransform(a: scale, b: 0, c: 0, d: -scale,
                                         tx: x + (i == 0 ? 0 : advances[0].width * scale), ty: baseline)
        let path = rawPaths[i].copy(using: &transform)!
        let fill = i == 0 ? foreground : "4F65FF"
        context.addPath(path); context.setFillColor(color(fill)); context.fillPath()
        svg += "<path fill=\"#\(fill)\" d=\"\(svgPath(path))\"/>"
    }
    if ribbon {
        // The tile is the icon canvas inset by 64 pt, so place the ribbon on it.
        precondition(tile != nil, "A ribbon needs a tile")
        context.saveGState(); context.addPath(outline); context.clip()
        svg += "<clipPath id=\"tile\"><rect x=\"64\" y=\"64\" width=\"896\" height=\"896\" rx=\"196\"/></clipPath><g clip-path=\"url(#tile)\">"
        for (shape, fill) in [(ribbonPath, amber), (labelPath, ink)] {
            var placement = CGAffineTransform(a: 0.875, b: 0, c: 0, d: 0.875, tx: 64, ty: 64)
            let path = shape.copy(using: &placement)!
            context.addPath(path); context.setFillColor(color(fill)); context.fillPath()
            svg += "<path fill=\"#\(fill)\" d=\"\(svgPath(path))\"/>"
        }
        context.restoreGState()
        svg += "</g>"
    }
    svg += "</svg>\n"
    try svg.write(to: root.appendingPathComponent(name + ".svg"), atomically: true, encoding: .utf8)
    let output = CGImageDestinationCreateWithURL(root.appendingPathComponent(name + ".png") as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(output, context.makeImage()!, nil)
    precondition(CGImageDestinationFinalize(output))
}
try generate(name: "logo-light", foreground: "191A1D")
try generate(name: "logo-dark", foreground: "F2EFE8")
try generate(name: "logo-light-bordered", foreground: "191A1D", border: true)
try generate(name: "logo-dark-bordered", foreground: "F2EFE8", border: true)
try generate(name: "app-icon", foreground: "F2EFE8", tile: "202124")
try generate(name: "app-icon-light", foreground: "191A1D", tile: "F2F2F4")
try generate(name: "app-icon-dev", foreground: "F2EFE8", tile: "202124", ribbon: true)

// KitDev.icon is Kit.icon with the ribbon and its label layered on top.
func layerSVG(_ path: CGPath) -> String {
    "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"1024\" height=\"1024\" viewBox=\"0 0 1024 1024\"><path fill=\"#FFFFFF\" d=\"\(svgPath(path))\"/></svg>\n"
}
func solid(_ hex: String) -> [String: Any] {
    let n = UInt32(hex, radix: 16)!
    let c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map { String(format: "%.5f", Double($0) / 255) }
    return ["solid": "srgb:\(c.joined(separator: ",")),1.00000"]
}
func layer(_ name: String, fill: String) -> [String: Any] {
    ["name": name, "image-name": "\(name).svg", "hidden": false, "glass-specializations": [["value": false]],
     "fill-specializations": [["value": solid(fill)], ["appearance": "dark", "value": solid(fill)]]]
}
let releaseIcon = root.appendingPathComponent("Kit.icon"), devIcon = root.appendingPathComponent("KitDev.icon")
try? FileManager.default.removeItem(at: devIcon)
try FileManager.default.createDirectory(at: devIcon.appendingPathComponent("Assets"), withIntermediateDirectories: true)
for asset in ["K.svg", "Cursor.svg"] {
    try FileManager.default.copyItem(at: releaseIcon.appendingPathComponent("Assets/\(asset)"),
                                     to: devIcon.appendingPathComponent("Assets/\(asset)"))
}
try layerSVG(ribbonPath).write(to: devIcon.appendingPathComponent("Assets/Ribbon.svg"), atomically: true, encoding: .utf8)
try layerSVG(labelPath).write(to: devIcon.appendingPathComponent("Assets/Label.svg"), atomically: true, encoding: .utf8)
var document = try JSONSerialization.jsonObject(with: Data(contentsOf: releaseIcon.appendingPathComponent("icon.json"))) as! [String: Any]
var groups = document["groups"] as! [[String: Any]]
groups[0]["layers"] = [layer("Label", fill: ink), layer("Ribbon", fill: amber)] + (groups[0]["layers"] as! [[String: Any]])
document["groups"] = groups
try JSONSerialization.data(withJSONObject: document, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
    .write(to: devIcon.appendingPathComponent("icon.json"))
print("Generated seven SVG and transparent PNG variants and KitDev.icon using JetBrains Mono Bold outlines.")
