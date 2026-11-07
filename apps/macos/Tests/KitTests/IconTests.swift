import AppKit
import Testing
@testable import Kit

@MainActor struct IconTests {
    @Test func everyIconLoadsItsBundledPhosphorVectorAsATemplate() {
        for icon in Icon.allCases {
            let image = icon.image
            #expect(!image.representations.isEmpty, "\(icon.rawValue).svg is missing from Resources/Icons")
            #expect(image.isTemplate)
            #expect(image.size == NSSize(width: 16, height: 16))
        }
    }

    @Test func aDestructiveIconIsDrawnInTheSystemRedForEachAppearance() throws {
        let image = Icon.trash.destructiveImage
        #expect(!image.isTemplate)
        #expect(image.size == NSSize(width: 16, height: 16))
        for name in [NSAppearance.Name.aqua, .darkAqua] {
            let appearance = try #require(NSAppearance(named: name))
            var expected = NSColor.clear
            appearance.performAsCurrentDrawingAppearance {
                expected = NSColor.systemRed.usingColorSpace(.sRGB) ?? .clear
            }
            let colors = try inkColors(of: image, appearance: appearance)
            #expect(!colors.isEmpty)
            for color in colors {
                #expect(abs(color.redComponent - expected.redComponent) < 0.02)
                #expect(abs(color.greenComponent - expected.greenComponent) < 0.02)
                #expect(abs(color.blueComponent - expected.blueComponent) < 0.02)
            }
        }
    }

    /// The colours of the image's fully opaque pixels, drawn at 2x.
    private func inkColors(of image: NSImage, appearance: NSAppearance) throws -> [NSColor] {
        let bitmap = try #require(NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: 32, pixelsHigh: 32, bitsPerSample: 8, samplesPerPixel: 4,
            hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        bitmap.size = image.size
        appearance.performAsCurrentDrawingAppearance {
            NSGraphicsContext.saveGraphicsState()
            NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
            image.draw(in: NSRect(origin: .zero, size: image.size))
            NSGraphicsContext.restoreGraphicsState()
        }
        var colors: [NSColor] = []
        for x in 0..<32 {
            for y in 0..<32 {
                guard let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.sRGB), color.alphaComponent == 1 else {
                    continue
                }
                colors.append(color)
            }
        }
        return colors
    }
}
