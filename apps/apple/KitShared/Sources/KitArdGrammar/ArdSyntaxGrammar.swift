import Foundation
import KitHighlighting
import SwiftTreeSitter
import TreeSitterArd

/// Ard's parser and highlight query from tree-sitter-ard, which both apps
/// bundle: the Mac beside CodeEditLanguages, which has no Ard, and iOS beside
/// its other upstream grammars.
public enum ArdSyntaxGrammar {
    /// The Ard grammar, with the highlight query from tree-sitter-ard's
    /// resource bundle.
    public static func grammar() throws -> SyntaxGrammar {
        let language = Language(tree_sitter_ard())
        guard let url = queryURL("highlights") else { throw CocoaError(.fileReadNoSuchFile) }
        let source = try String(contentsOf: url, encoding: .utf8)
        return SyntaxGrammar(language: language, query: try Query(language: language, data: Data(source.utf8)))
    }

    /// A query file in the resource bundle built for TreeSitterArd. Xcode
    /// names the bundle after the package's name and SwiftPM after its
    /// identity, so the bundle is found by its target suffix. A bundle keeps
    /// its files at its root on iOS and under Contents/Resources on macOS.
    static func queryURL(_ file: String) -> URL? {
        let manager = FileManager.default
        for directory in directories {
            guard let names = try? manager.contentsOfDirectory(atPath: directory.path) else { continue }
            for name in names where name.hasSuffix("_TreeSitterArd.bundle") {
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
