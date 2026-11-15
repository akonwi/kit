// The launch-time floor: an empty SwiftUI app that reports the same
// milestones as Kit (Sources/Kit/Support/LaunchMetrics.swift), so
// `measure_launch.sh --floor` can show how much of a launch is the system's.
import AppKit
import Darwin
import SwiftUI

@MainActor private var reached: [String: Double] = [:]

private func now() -> Double {
    var time = timeval()
    gettimeofday(&time, nil)
    return Double(time.tv_sec) + Double(time.tv_usec) / 1_000_000
}

private func processStart() -> Double {
    var info = kinfo_proc()
    var size = MemoryLayout<kinfo_proc>.stride
    var name: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
    guard sysctl(&name, 4, &info, &size, nil, 0) == 0 else { return now() }
    let start = info.kp_proc.p_un.__p_starttime
    return Double(start.tv_sec) + Double(start.tv_usec) / 1_000_000
}

@MainActor private func mark(_ milestone: String) {
    if reached[milestone] == nil { reached[milestone] = now() }
}

@MainActor private func report() {
    guard let path = ProcessInfo.processInfo.environment["KIT_LAUNCH_REPORT"] else { return }
    let start = processStart()
    let milestones = reached.mapValues { (($0 - start) * 1_000_000).rounded() / 1_000 }
    let report: [String: Any] = ["pid": Int(getpid()), "processStart": start, "milestonesMs": milestones]
    if let data = try? JSONSerialization.data(withJSONObject: report) { try? data.write(to: URL(fileURLWithPath: path)) }
}

private final class Delegate: NSObject, NSApplicationDelegate {
    func applicationWillFinishLaunching(_ notification: Notification) { MainActor.assumeIsolated { mark("willFinishLaunching") } }
    func applicationDidFinishLaunching(_ notification: Notification) { MainActor.assumeIsolated { mark("didFinishLaunching") } }
}

private struct Content: View {
    var body: some View {
        Text("Hello").frame(width: 600, height: 400).onAppear {
            mark("contentReady")
            let observer = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, false, 2_000_001) { observer, _ in
                CFRunLoopObserverInvalidate(observer)
                MainActor.assumeIsolated {
                    mark("firstContentFrame")
                    report()
                }
            }
            CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
        }
    }
}

private struct FloorApp: App {
    @NSApplicationDelegateAdaptor(Delegate.self) private var delegate
    var body: some Scene { WindowGroup { Content() } }
}

@main enum Floor {
    static func main() {
        MainActor.assumeIsolated { mark("main") }
        FloorApp.main()
    }
}
