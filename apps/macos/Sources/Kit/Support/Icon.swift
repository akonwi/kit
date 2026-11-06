import AppKit
import SwiftUI

/// A Phosphor Regular icon bundled under `Resources/Icons`. Raw values are
/// the Phosphor file names, so the set stays traceable to the upstream
/// release recorded in `Resources/Phosphor-LICENSE.txt`.
enum Icon: String, CaseIterable, Sendable {
    case arrowClockwise = "arrow-clockwise"
    case arrowDown = "arrow-down"
    case arrowRight = "arrow-right"
    case arrowUp = "arrow-up"
    case arrowUpRight = "arrow-up-right"
    case arrowsInLineVertical = "arrows-in-line-vertical"
    case arrowsLeftRight = "arrows-left-right"
    case arrowsOutSimple = "arrows-out-simple"
    case arrowSquareOut = "arrow-square-out"
    case browser
    case caretDown = "caret-down"
    case caretLeft = "caret-left"
    case caretRight = "caret-right"
    case caretUp = "caret-up"
    case caretUpDown = "caret-up-down"
    case chatCenteredText = "chat-centered-text"
    case chatCircle = "chat-circle"
    case chatCircleDots = "chat-circle-dots"
    case check
    case checkCircle = "check-circle"
    case circleHalf = "circle-half"
    case copy
    case cornersIn = "corners-in"
    case cornersOut = "corners-out"
    case dotsThree = "dots-three"
    case eye
    case eyeSlash = "eye-slash"
    case file
    case fileMagnifyingGlass = "file-magnifying-glass"
    case filePlus = "file-plus"
    case files
    case folder
    case folderSimple = "folder-simple"
    case gitBranch = "git-branch"
    case gitFork = "git-fork"
    case gitDiff = "git-diff"
    case gitPullRequest = "git-pull-request"
    case hardDrives = "hard-drives"
    case hash
    case image
    case info
    case key
    case lightbulb
    case magnifyingGlass = "magnifying-glass"
    case moon
    case note
    case notePencil = "note-pencil"
    case paperclip
    case pencilSimple = "pencil-simple"
    case plug
    case plugsConnected = "plugs-connected"
    case plus
    case plusSquare = "plus-square"
    case prohibit
    case puzzlePiece = "puzzle-piece"
    case question
    case questionMark = "question-mark"
    case quotes
    case robot
    case sidebarSimple = "sidebar-simple"
    case sparkle
    case squareSplitHorizontal = "square-split-horizontal"
    case squareSplitVertical = "square-split-vertical"
    case stack
    case stop
    case sun
    case terminalWindow = "terminal-window"
    case trash
    case treeStructure = "tree-structure"
    case warning
    case warningCircle = "warning-circle"
    case warningOctagon = "warning-octagon"
    case wrench
    case x
    case xCircle = "x-circle"

    /// The template image, sized for menus and labels. Views that need a
    /// specific box size use ``IconView``.
    @MainActor var image: NSImage { IconCache.shared.image(self) }
}

@MainActor
private final class IconCache {
    static let shared = IconCache()
    private var images: [Icon: NSImage] = [:]

    func image(_ icon: Icon) -> NSImage {
        if let image = images[icon] { return image }
        let image = Bundle.module.url(forResource: icon.rawValue, withExtension: "svg")
            .flatMap(NSImage.init(contentsOf:)) ?? NSImage()
        image.size = NSSize(width: 16, height: 16)
        image.isTemplate = true
        images[icon] = image
        return image
    }
}

extension Image {
    /// A Phosphor icon as a template image, for menu items and labels.
    @MainActor init(_ icon: Icon) {
        self.init(nsImage: icon.image)
    }
}

/// A Phosphor icon drawn in a square box of `size` points, coloured by the
/// foreground style. Sizes follow the Paper design's icon boxes. Without a
/// label the icon is decorative and hidden from accessibility; controls
/// that show only an icon label themselves.
struct IconView: View {
    let icon: Icon
    var size: CGFloat = 14
    var label: String? = nil

    init(_ icon: Icon, size: CGFloat = 14, label: String? = nil) {
        self.icon = icon
        self.size = size
        self.label = label
    }

    var body: some View {
        let image = Image(icon).renderingMode(.template).resizable().interpolation(.high)
            .frame(width: size, height: size)
        if let label {
            image.accessibilityLabel(label)
        } else {
            image.accessibilityHidden(true)
        }
    }
}

/// Names the icon type where SwiftUI's generic parameters shadow it.
typealias KitIcon = Icon

extension Label where Title == Text, Icon == IconView {
    /// A label with a Phosphor icon in a `size`-point box. Menu items need an
    /// `Image` icon instead: `Label { Text(title) } icon: { Image(icon) }`.
    init(_ title: String, icon: KitIcon, size: CGFloat = 12) {
        self.init { Text(title) } icon: { IconView(icon, size: size) }
    }
}

/// The label of ``Button/init(_:icon:size:margin:role:action:)``: a title
/// and a Phosphor icon that take the pointer across their whole frame.
///
/// A button styled `.plain` takes clicks only where its label draws, the
/// icon's box and the title's, so the gap between them and the edges around
/// them miss. This label's frame is its hit shape instead, widened by
/// `margin` on every side. The margin is layout space, as SwiftUI hit-tests
/// no further than a view's frame, so a caller that keeps its layout takes
/// the same amount out of its own padding.
struct IconButtonLabel: View {
    let title: String
    let icon: KitIcon
    var size: CGFloat = 12
    var margin: CGFloat = 0

    var body: some View {
        Label(title, icon: icon, size: size).padding(margin).contentShape(Rectangle())
    }
}

extension Button where Label == IconButtonLabel {
    /// A button labelled with a title and a Phosphor icon. The whole label,
    /// widened by `margin` on every side, takes clicks; see ``IconButtonLabel``.
    init(_ title: String, icon: KitIcon, size: CGFloat = 12, margin: CGFloat = 0, role: ButtonRole? = nil,
         action: @escaping () -> Void) {
        self.init(role: role, action: action) { IconButtonLabel(title: title, icon: icon, size: size, margin: margin) }
    }
}
