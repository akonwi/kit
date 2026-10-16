import AppKit
import SwiftUI

/// Esc cancels a drag. The mouse is held during a drag, so the key goes to
/// the window rather than any SwiftUI focus; a local monitor catches it only
/// while a drag is in progress.
struct DragCancellation: View {
    let active: Bool
    /// Cancels the drag, returning whether one was in progress.
    let cancel: () -> Bool
    @State private var monitor: Any?

    var body: some View {
        Color.clear
            .onChange(of: active, initial: true) {
                if active, monitor == nil {
                    monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [cancel] event in
                        guard event.keyCode == 53, cancel() else { return event }
                        return nil
                    }
                } else if !active, let monitor {
                    NSEvent.removeMonitor(monitor)
                    self.monitor = nil
                }
            }
            .onDisappear {
                if let monitor { NSEvent.removeMonitor(monitor) }
                monitor = nil
            }
    }
}
