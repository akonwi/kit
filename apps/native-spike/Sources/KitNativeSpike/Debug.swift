import Foundation

/// Appends a line to /tmp/nativespike/events.log so interactive tests can be
/// checked afterwards.
func spikeLog(_ message: @autoclosure () -> String) {
    let line = "\(Date().formatted(.iso8601.time(includingFractionalSeconds: true))) \(message())\n"
    let url = URL(fileURLWithPath: "/tmp/nativespike/events.log")
    if let handle = try? FileHandle(forWritingTo: url) {
        handle.seekToEndOfFile()
        handle.write(Data(line.utf8))
        try? handle.close()
    } else {
        try? line.write(to: url, atomically: false, encoding: .utf8)
    }
}
