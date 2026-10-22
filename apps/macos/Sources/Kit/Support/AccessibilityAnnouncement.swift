import AppKit

/// Speaks feedback that appears without taking focus, such as a toast.
enum AccessibilityAnnouncement {
    @MainActor static func post(_ text: String, urgent: Bool = false) {
        guard let app = NSApp, !text.isEmpty else { return }
        NSAccessibility.post(element: app.mainWindow ?? app, notification: .announcementRequested, userInfo: [
            .announcement: text,
            .priority: (urgent ? NSAccessibilityPriorityLevel.high : .medium).rawValue,
        ])
    }
}
