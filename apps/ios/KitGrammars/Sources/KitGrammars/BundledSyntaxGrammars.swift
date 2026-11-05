import Foundation
import KitHighlighting
import SwiftTreeSitter
import TreeSitterArd
import TreeSitterBash
import TreeSitterCSS
import TreeSitterGo
import TreeSitterJSON
import TreeSitterJavaScript
import TreeSitterPython
import TreeSitterRuby
import TreeSitterSwift
import TreeSitterTSX
import TreeSitterTypeScript

/// The upstream tree-sitter grammars and their highlight queries, layered the
/// way the macOS app layers CodeEditLanguages' so both apps color code alike.
public struct BundledSyntaxGrammars: SyntaxGrammars {
    public init() {}

    public func grammar(named name: String) throws -> SyntaxGrammar {
        let javascript = Queries(target: "TreeSitterJavaScript")
        let typescript = Queries(target: "TreeSitterTypeScript")
        let (pointer, files): (OpaquePointer, [(Queries, String)]) = switch name {
        case "bash": (tree_sitter_bash(), [(Queries(target: "TreeSitterBash"), "highlights")])
        case "go": (tree_sitter_go(), [(Queries(target: "TreeSitterGo"), "highlights")])
        case "json": (tree_sitter_json(), [(Queries(target: "TreeSitterJSON"), "highlights")])
        case "swift": (tree_sitter_swift(), [(Queries(target: "TreeSitterSwift"), "highlights")])
        case "python": (tree_sitter_python(), [(Queries(target: "TreeSitterPython"), "highlights")])
        case "ruby": (tree_sitter_ruby(), [(Queries(target: "TreeSitterRuby"), "highlights")])
        case "css": (tree_sitter_css(), [(Queries(target: "TreeSitterCSS"), "highlights")])
        case "ard": (tree_sitter_ard(), [(Queries(target: "TreeSitterArd"), "highlights")])
        case "javascript": (tree_sitter_javascript(),
                            [(javascript, "highlights"), (javascript, "highlights-jsx")])
        case "typescript": (tree_sitter_typescript(),
                            [(javascript, "highlights"), (typescript, "highlights")])
        case "tsx": (tree_sitter_tsx(),
                     [(javascript, "highlights"), (javascript, "highlights-jsx"), (typescript, "highlights")])
        default: throw CocoaError(.fileReadNoSuchFile)
        }
        let language = Language(pointer)
        let source = try files.map { queries, file in
            guard let url = queries.url(file) else { throw CocoaError(.fileReadNoSuchFile) }
            return try String(contentsOf: url, encoding: .utf8)
        }.joined(separator: "\n")
        return SyntaxGrammar(language: language, query: try Query(language: language, data: Data(source.utf8)))
    }
}

/// A grammar target's query files, in the resource bundle SwiftPM builds for
/// it. Xcode names the bundle after the package's name and SwiftPM after its
/// identity, so the bundle is found by its target suffix.
private struct Queries {
    let target: String

    func url(_ file: String) -> URL? {
        let manager = FileManager.default
        for directory in Self.directories {
            guard let names = try? manager.contentsOfDirectory(atPath: directory.path) else { continue }
            for name in names where name.hasSuffix("_\(target).bundle") {
                let bundle = directory.appendingPathComponent(name, isDirectory: true)
                for queries in ["queries", "Contents/Resources/queries"] {
                    let url = bundle.appendingPathComponent(queries).appendingPathComponent(file + ".scm")
                    if manager.fileExists(atPath: url.path) { return url }
                }
            }
        }
        return nil
    }

    /// The app's resources, then the folders around the bundle holding this
    /// code and other loaded bundles, where a test runner finds package resources.
    private static let directories: [URL] = {
        var urls = [Bundle.main.resourceURL, Bundle.main.bundleURL].compactMap { $0 }
        for bundle in [Bundle(for: BundleToken.self)] + Bundle.allBundles {
            urls.append(bundle.bundleURL.deletingLastPathComponent())
        }
        var seen = Set<String>()
        return urls.filter { seen.insert($0.standardizedFileURL.path).inserted }
    }()
}

private final class BundleToken {}
