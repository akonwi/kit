import SwiftUI

/// A destructive menu item's label, in red. macOS menus ignore a button's
/// destructive role, so the title and icon carry the colour themselves.
struct DestructiveMenuLabel: View {
    let title: String
    let icon: Icon

    init(_ title: String, icon: Icon) {
        self.title = title
        self.icon = icon
    }

    var body: some View {
        Label { Text(title).foregroundStyle(.red) } icon: { Image(nsImage: icon.destructiveImage) }
    }
}
